package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type offlineAsset struct {
	open    func() (io.ReadCloser, error)
	mapOpen func(string) (io.ReadCloser, string, error)
}

type harContent struct {
	MimeType string  `json:"mimeType"`
	Text     *string `json:"text"`
	Encoding string  `json:"encoding"`
}

func (c harContent) open() (io.ReadCloser, error) {
	if c.Text == nil {
		return nil, fmt.Errorf("response body is absent from HAR")
	}
	switch c.Encoding {
	case "":
		return io.NopCloser(strings.NewReader(*c.Text)), nil
	case "base64":
		return io.NopCloser(base64.NewDecoder(base64.StdEncoding, strings.NewReader(*c.Text))), nil
	default:
		return nil, fmt.Errorf("unsupported HAR encoding %q", c.Encoding)
	}
}

func offlineAssets(c config) (map[string]offlineAsset, error) {
	if c.HAR != "" {
		return readHAR(c.HAR)
	}
	directory, err := filepath.Abs(c.Directory)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("-d must be a directory")
	}
	assets := map[string]offlineAsset{}
	err = filepath.WalkDir(directory, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(name), ".js") {
			return nil
		}
		u := (&url.URL{Scheme: "file", Path: filepath.ToSlash(name)}).String()
		assets[u] = offlineAsset{
			open: func() (io.ReadCloser, error) { return os.Open(name) },
			mapOpen: func(ref string) (io.ReadCloser, string, error) {
				base, _ := url.Parse(u)
				resolved, err := url.Parse(ref)
				if err != nil {
					return nil, ref, err
				}
				resolved = base.ResolveReference(resolved)
				if resolved.Scheme != "file" || resolved.Host != "" || resolved.RawQuery != "" {
					return nil, ref, fmt.Errorf("offline map must reference a local file")
				}
				relative, err := filepath.Rel(directory, filepath.FromSlash(resolved.Path))
				if err != nil || !filepath.IsLocal(relative) {
					return nil, ref, fmt.Errorf("source map is outside input directory")
				}
				root, err := os.OpenRoot(directory)
				if err != nil {
					return nil, ref, err
				}
				defer root.Close()
				f, err := root.Open(relative)
				return f, resolved.String(), err
			},
		}
		return nil
	})
	return assets, err
}

func readHAR(name string) (map[string]offlineAsset, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var archive struct {
		Log struct {
			Entries []struct {
				Request struct {
					URL string `json:"url"`
				} `json:"request"`
				Response struct {
					Content harContent `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	lr := &io.LimitedReader{R: f, N: maxJS + 1}
	dec := json.NewDecoder(lr)
	if err := dec.Decode(&archive); err != nil {
		return nil, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing HAR data")
	}
	if lr.N == 0 {
		return nil, fmt.Errorf("HAR exceeds 512 MiB")
	}
	bodies := map[string]harContent{}
	for _, entry := range archive.Log.Entries {
		u, err := httpURL(entry.Request.URL, nil)
		if err != nil {
			continue
		}
		// Prefer a captured body over a duplicate entry whose body was omitted.
		if old, ok := bodies[u.String()]; !ok || old.Text == nil {
			bodies[u.String()] = entry.Response.Content
		}
	}
	assets := map[string]offlineAsset{}
	for raw, content := range bodies {
		u, _ := url.Parse(raw)
		if !strings.Contains(strings.ToLower(content.MimeType), "javascript") && !strings.EqualFold(filepath.Ext(u.Path), ".js") {
			continue
		}
		assets[raw] = offlineAsset{open: content.open, mapOpen: func(ref string) (io.ReadCloser, string, error) {
			resolved, err := httpURL(ref, u)
			if err != nil {
				return nil, ref, err
			}
			body, ok := bodies[resolved.String()]
			if !ok {
				return nil, resolved.String(), fmt.Errorf("source map response is absent from HAR")
			}
			reader, err := body.open()
			return reader, resolved.String(), err
		}}
	}
	return assets, nil
}

func processOffline(ctx context.Context, root *os.Root, raw string, a offlineAsset, emit func(Finding)) FileResult {
	if err := ctx.Err(); err != nil {
		return FileResult{URL: raw, Error: err.Error()}
	}
	reader, err := a.open()
	if err != nil {
		return FileResult{URL: raw, Error: err.Error()}
	}
	defer reader.Close()
	r, ref := processStream(root, raw, reader, emit)
	if r.Error != "" || ref == "" {
		return r
	}
	mapReader, mapURL, err := a.mapOpen(ref)
	if err != nil {
		r.SourceMap = &MapResult{URL: mapURL, Status: "SourceMap unavailable offline", Errors: []string{err.Error()}}
		return r
	}
	defer mapReader.Close()
	r.SourceMap = extractMap(ctx, root, mapReader, mapURL, assetName(raw))
	return r
}

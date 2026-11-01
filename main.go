package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const (
	scanChunk          = 64 << 10
	maxHTML            = 16 << 20
	maxJS              = 512 << 20
	maxMap             = 64 << 20
	fullyReconstructed = "Fully Reconstructed via SourceMap"
)

type config struct {
	URL, List, Output string
	HAR, Directory    string
	Concurrency       int
	Timeout           time.Duration
}

type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Category string `json:"category"`
	Snippet  string `json:"snippet"`
}

type MapResult struct {
	URL       string   `json:"url"`
	File      string   `json:"file,omitempty"`
	Directory string   `json:"directory,omitempty"`
	Status    string   `json:"status"`
	Sources   int      `json:"sources"`
	Extracted int      `json:"extracted"`
	Missing   int      `json:"missing"`
	Errors    []string `json:"errors,omitempty"`
}

type FileResult struct {
	URL          string     `json:"url"`
	File         string     `json:"file"`
	Findings     []Finding  `json:"findings"`
	SourceMap    *MapResult `json:"source_map,omitempty"`
	Error        string     `json:"error,omitempty"`
	AnalysisNote string     `json:"analysis_note,omitempty"`
}

type Report struct {
	StartedAt time.Time    `json:"started_at"`
	Input     string       `json:"input"`
	Files     []FileResult `json:"files"`
}

var patterns = []struct {
	category string
	re       *regexp.Regexp
}{
	{"WebCrypto", regexp.MustCompile(`crypto\.subtle\.(encrypt|decrypt|sign|verify|importKey)`)},
	{"Third-party Crypto", regexp.MustCompile(`(CryptoJS|forge\.cipher|JSEncrypt|U2FsdGVkX1)`)},
	{"Custom Header / Signature", regexp.MustCompile(`(?i)["'](X-Signature|X-Sign|X-Timestamp|X-Request-Id)["']`)},
	{"Base64 Helper", regexp.MustCompile(`\b(atob|btoa)\(`)},
}

var (
	jsReference  = regexp.MustCompile("(?i)[\"'`]((?:https?://|//)?[a-z0-9_./%~:@+\\-]+\\.js(?:[?#][^\"'`\\s]*)?)[\"'`]")
	mapReference = regexp.MustCompile(`(?s)(?://[#@]|/\*[#@])\s*sourceMappingURL=([^\s*]+)\s*(?:\*/)?\s*$`)
	safeName     = regexp.MustCompile(`[^a-zA-Z0-9._-]`)
)

func main() {
	c, err := parseCLI(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, c, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, c config, output io.Writer) error {
	inputs := 0
	for _, input := range []string{c.URL, c.List, c.HAR, c.Directory} {
		if input != "" {
			inputs++
		}
	}
	if inputs != 1 {
		return errors.New("provide exactly one of -u, -l, -har or -d")
	}
	if c.Concurrency < 1 || c.Timeout <= 0 || c.Output == "" {
		return errors.New("-c, -timeout and -o must be positive/nonempty")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = c.Concurrency
	transport.MaxIdleConnsPerHost = c.Concurrency
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: c.Timeout}
	var logMu sync.Mutex
	log := func(color, format string, args ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		fmt.Fprintf(output, "\x1b[%sm%s\x1b[0m\n", color, fmt.Sprintf(format, args...))
	}
	report := Report{StartedAt: time.Now().UTC(), Input: c.URL, Files: []FileResult{}}
	var urls []string
	var offline map[string]offlineAsset
	var err error
	if c.HAR != "" || c.Directory != "" {
		report.Input = c.HAR
		if c.Directory != "" {
			report.Input = c.Directory
		}
		offline, err = offlineAssets(c)
		for u := range offline {
			urls = append(urls, u)
		}
		sort.Strings(urls)
	} else if c.URL != "" {
		log("36", "[crawl] %q", c.URL)
		urls, err = discover(ctx, client, c.URL)
	} else {
		report.Input = c.List
		urls, err = readList(c.List)
	}
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return errors.New("no JavaScript URLs found")
	}
	root, err := prepareOutput(c.Output)
	if err != nil {
		return err
	}
	defer root.Close()
	log("36", "[queue] %d assets, %d workers", len(urls), min(c.Concurrency, len(urls)))
	jobs := make(chan string)
	results := make(chan FileResult)
	var workers sync.WaitGroup
	for i := 0; i < min(c.Concurrency, len(urls)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for u := range jobs {
				log("36", "[asset] %q", u)
				emit := func(f Finding) {
					log("33", "[finding] %s:%d [%s] %q", f.File, f.Line, f.Category, f.Snippet)
				}
				var r FileResult
				if a, ok := offline[u]; ok {
					r = processOffline(ctx, root, u, a, emit)
				} else {
					r = processJS(ctx, client, root, u, emit)
				}
				if r.AnalysisNote != "" {
					log("36", "[analysis] %q: %q", u, r.AnalysisNote)
				}
				if r.Error != "" {
					log("31", "[error] %q: %q", u, r.Error)
				}
				if r.SourceMap != nil {
					log("35", "[map] %q: %s (%d/%d sources)", u, r.SourceMap.Status, r.SourceMap.Extracted, r.SourceMap.Sources)
					for _, e := range r.SourceMap.Errors {
						log("31", "[map error] %q: %q", u, e)
					}
				}
				results <- r
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, u := range urls {
			jobs <- u
		}
	}()
	go func() { workers.Wait(); close(results) }()
	failures, findings, completed := 0, 0, 0
	for r := range results {
		report.Files = append(report.Files, r)
		findings += len(r.Findings)
		completed++
		log("36", "[progress] %d/%d assets complete", completed, len(urls))
		if r.Error != "" || (r.SourceMap != nil && r.SourceMap.Status != fullyReconstructed) {
			failures++
		}
	}
	sort.Slice(report.Files, func(i, j int) bool { return report.Files[i].URL < report.Files[j].URL })
	if err := writeReports(root, report); err != nil {
		return err
	}
	log("32", "[done] %d assets, %d findings, %d failed/partial; reports: %q", len(report.Files), findings, failures, c.Output)
	if failures > 0 {
		return fmt.Errorf("%d asset(s) failed or have incomplete source maps; see summary.json", failures)
	}
	return ctx.Err()
}

func httpURL(raw string, base *url.URL) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("expected an HTTP(S) URL without credentials: %q", raw)
	}
	u.Fragment, u.RawFragment = "", ""
	return u, nil
}

func request(ctx context.Context, client *http.Client, method, raw string) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, err
	}
	time.Sleep(time.Duration(150+rand.IntN(151)) * time.Millisecond)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Sec-Fetch-Dest", "script")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s %s: %s", method, raw, resp.Status)
	}
	return resp, nil
}

func discover(ctx context.Context, client *http.Client, raw string) ([]string, error) {
	u, err := httpURL(raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := request(ctx, client, http.MethodGet, u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	lr := &io.LimitedReader{R: resp.Body, N: maxHTML + 1}
	doc, err := html.Parse(lr)
	if err != nil {
		return nil, err
	}
	if lr.N == 0 {
		return nil, fmt.Errorf("HTML exceeds %d MiB", maxHTML>>20)
	}
	base := resp.Request.URL
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "base" {
			continue
		}
		found := false
		for _, a := range n.Attr {
			if a.Key == "href" {
				if b, e := httpURL(a.Val, base); e == nil {
					base = b
				}
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	seen := make(map[string]bool)
	add := func(ref string) {
		if u, e := httpURL(ref, base); e == nil {
			seen[u.String()] = true
		}
	}
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "script" {
			continue
		}
		hasSrc := false
		for _, a := range n.Attr {
			if a.Key == "src" {
				hasSrc = true
				if strings.TrimSpace(a.Val) != "" {
					add(a.Val)
				}
			}
		}
		if hasSrc {
			continue
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				for _, ref := range jsReference.FindAllStringSubmatch(child.Data, -1) {
					add(ref[1])
				}
			}
		}
	}
	return sortedURLs(seen), nil
}

func sortedURLs(seen map[string]bool) []string {
	urls := make([]string, 0, len(seen))
	for u := range seen {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	return urls
}

func readList(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	seen := make(map[string]bool)
	line := 0
	for scanner.Scan() {
		line++
		s := strings.TrimSpace(scanner.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		u, err := httpURL(s, nil)
		if err != nil {
			return nil, fmt.Errorf("list line %d: %w", line, err)
		}
		seen[u.String()] = true
	}
	return sortedURLs(seen), scanner.Err()
}

func prepareOutput(name string) (*os.Root, error) {
	if err := os.MkdirAll(name, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	dir, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	entries, readErr := dir.ReadDir(1)
	dir.Close()
	if len(entries) > 0 || (readErr != nil && readErr != io.EOF) {
		root.Close()
		return nil, errors.New("output directory must be empty; choose another -o to preserve existing files")
	}
	for _, sub := range []string{"raw", "unpacked"} {
		if err := root.Mkdir(sub, 0700); err != nil {
			root.Close()
			return nil, err
		}
	}
	return root, nil
}

func assetName(raw string) string {
	u, _ := url.Parse(raw)
	base := safeName.ReplaceAllString(path.Base(u.Path), "_")
	if len(base) > 80 {
		base = base[:80]
	}
	if !strings.HasSuffix(strings.ToLower(base), ".js") {
		base += ".js"
	}
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x-%s", sum[:8], base)
}

func processJS(ctx context.Context, client *http.Client, root *os.Root, raw string, emit func(Finding)) FileResult {
	r := FileResult{URL: raw, File: "raw/" + assetName(raw), Findings: []Finding{}}
	resp, err := request(ctx, client, http.MethodGet, raw)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer resp.Body.Close()
	r, ref := processStream(root, raw, resp.Body, emit)
	if r.Error == "" && ref != "" {
		r.SourceMap = unpackMap(ctx, client, root, resp.Request.URL, ref, assetName(raw))
	}
	return r
}

func processStream(root *os.Root, raw string, reader io.Reader, emit func(Finding)) (FileResult, string) {
	r := FileResult{URL: raw, File: "raw/" + assetName(raw), Findings: []Finding{}}
	f, err := root.OpenFile(r.File, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		r.Error = err.Error()
		return r, ""
	}
	lr := &io.LimitedReader{R: reader, N: maxJS + 1}
	ref, scanErr := scanJS(io.TeeReader(lr, f), func(hit Finding) { hit.File = r.File; r.Findings = append(r.Findings, hit); emit(hit) })
	closeErr := f.Close()
	if lr.N == 0 {
		scanErr = fmt.Errorf("JavaScript exceeds %d MiB", maxJS>>20)
	}
	if err = errors.Join(scanErr, closeErr); err != nil {
		r.Error = err.Error()
		if e := root.Remove(r.File); e != nil {
			r.Error += "; remove incomplete file: " + e.Error()
		}
		return r, ""
	}
	analyzeAST(root, &r, emit)
	return r, ref
}

// Split long minified lines without ever buffering an entire bundle in Scanner.
func scanParts(data []byte, atEOF bool) (int, []byte, error) {
	end := min(len(data), scanChunk)
	if i := bytes.IndexByte(data[:end], '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if len(data) >= scanChunk || (atEOF && len(data) > 0) {
		return end, data[:end], nil
	}
	return 0, nil, nil
}

func scanJS(reader io.Reader, emit func(Finding)) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 25*1024*1024)
	scanner.Split(scanParts)
	line := 1
	seen := make([]bool, len(patterns))
	carry, tail, lastLine := "", "", ""
	for scanner.Scan() {
		part := scanner.Text()
		endLine := strings.HasSuffix(part, "\n")
		text := carry + strings.TrimSuffix(part, "\n")
		for i, p := range patterns {
			if seen[i] {
				continue
			}
			for offset := 0; offset < len(text); {
				loc := p.re.FindStringIndex(text[offset:])
				if loc == nil {
					break
				}
				loc[0], loc[1] = loc[0]+offset, loc[1]+offset
				offset = loc[1]
				// Only new bytes may complete a match; the overlap has already been examined.
				if loc[1] <= len(carry) {
					continue
				}
				start, end := max(0, loc[0]-60), min(len(text), loc[1]+100)
				emit(Finding{Line: line, Category: p.category, Snippet: strings.ToValidUTF8(text[start:end], "�")})
				seen[i] = true
				break
			}
		}
		carry = text[max(0, len(text)-256):]
		tail += part
		// shortcut: trailing sourceMappingURL annotations are limited to 8 KiB; add a data-URI decoder for inline maps.
		if len(tail) > 8192 {
			tail = tail[len(tail)-8192:]
		}
		if endLine {
			if strings.TrimSpace(tail) != "" {
				lastLine += tail
				lastLine = lastLine[max(0, len(lastLine)-8192):]
			}
			line++
			clear(seen)
			carry, tail = "", ""
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(tail) != "" {
		lastLine += tail
		lastLine = lastLine[max(0, len(lastLine)-8192):]
	}
	matches := mapReference.FindAllStringSubmatch(lastLine, -1)
	if len(matches) > 0 {
		return matches[len(matches)-1][1], nil
	}
	return "", nil
}

type sourceMap struct {
	Version        int             `json:"version"`
	SourceRoot     string          `json:"sourceRoot"`
	Sources        []string        `json:"sources"`
	SourcesContent []*string       `json:"sourcesContent"`
	Sections       json.RawMessage `json:"sections"`
}

func unpackMap(ctx context.Context, client *http.Client, root *os.Root, base *url.URL, ref, name string) *MapResult {
	m := &MapResult{URL: ref, Status: "SourceMap unavailable"}
	fail := func(err error) *MapResult { m.Errors = append(m.Errors, err.Error()); return m }
	u, err := httpURL(ref, base)
	if err != nil {
		return fail(err)
	}
	m.URL = u.String()
	resp, err := request(ctx, client, http.MethodHead, m.URL)
	if err != nil {
		return fail(err)
	}
	resp.Body.Close()
	resp, err = request(ctx, client, http.MethodGet, m.URL)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	return extractMap(ctx, root, resp.Body, m.URL, name)
}

func extractMap(ctx context.Context, root *os.Root, reader io.Reader, mapURL, name string) *MapResult {
	m := &MapResult{URL: mapURL, Status: "SourceMap unavailable"}
	fail := func(err error) *MapResult { m.Errors = append(m.Errors, err.Error()); return m }
	mapFile := "raw/" + name + ".map"
	f, err := root.OpenFile(mapFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail(err)
	}
	n, copyErr := io.Copy(f, io.LimitReader(reader, maxMap+1))
	closeErr := f.Close()
	if n > maxMap {
		copyErr = fmt.Errorf("source map exceeds %d MiB", maxMap>>20)
	}
	if err = errors.Join(copyErr, closeErr); err != nil {
		removeErr := root.Remove(mapFile)
		return fail(errors.Join(err, removeErr))
	}
	m.File = mapFile
	m.Status = "SourceMap invalid"
	f, err = root.Open(mapFile)
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	var sm sourceMap
	decoder := json.NewDecoder(f)
	if err = decoder.Decode(&sm); err != nil {
		return fail(err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return fail(errors.New("source map contains trailing JSON data"))
	}
	if sm.Version != 3 {
		return fail(fmt.Errorf("unsupported source map version %d", sm.Version))
	}
	if sm.Sections != nil {
		return fail(errors.New("indexed source maps with sections are not supported"))
	}
	if len(sm.Sources) == 0 {
		return fail(errors.New("source map contains no sources"))
	}
	if len(sm.SourcesContent) > len(sm.Sources) {
		return fail(errors.New("sourcesContent is longer than sources"))
	}
	m.Sources = len(sm.Sources)
	m.Status = "Partially Reconstructed via SourceMap"
	m.Directory = "unpacked/" + strings.TrimSuffix(name, ".js")
	if err = root.Mkdir(m.Directory, 0700); err != nil {
		return fail(err)
	}
	dest, err := root.OpenRoot(m.Directory)
	if err != nil {
		return fail(err)
	}
	defer dest.Close()
	for i, source := range sm.Sources {
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
		if i >= len(sm.SourcesContent) || sm.SourcesContent[i] == nil {
			m.Missing++
			continue
		}
		p, e := sourcePath(sm.SourceRoot, source)
		if e == nil {
			e = mkdirAllRoot(dest, filepath.Dir(p), 0700)
		}
		if e == nil {
			e = writeFile(dest, p, strings.NewReader(*sm.SourcesContent[i]))
		}
		if e != nil {
			m.Errors = append(m.Errors, fmt.Sprintf("source %q: %s", source, e))
			continue
		}
		m.Extracted++
	}
	if m.Extracted == m.Sources && len(m.Errors) == 0 {
		m.Status = fullyReconstructed
	}
	return m
}

func sourcePath(sourceRoot, source string) (string, error) {
	if source == "" {
		return "", errors.New("empty source path")
	}
	source = strings.ReplaceAll(source, `\`, "/")
	if sourceRoot != "" && !strings.Contains(source, ":") && !strings.HasPrefix(source, "/") {
		source = strings.TrimRight(sourceRoot, "/") + "/" + source
	}
	u, err := url.Parse(source)
	if err != nil {
		return "", err
	}
	if len(u.Scheme) == 1 {
		return "", errors.New("drive paths are not allowed")
	}
	p := u.Path
	if u.Host != "" {
		p = u.Host + "/" + strings.TrimLeft(p, "/")
	}
	p = strings.ReplaceAll(p, `\`, "/")
	p = path.Clean(strings.TrimLeft(p, "/"))
	if p == "." || !filepath.IsLocal(filepath.FromSlash(p)) || strings.ContainsAny(p, "\x00:") {
		return "", fmt.Errorf("unsafe source path %q", source)
	}
	return filepath.FromSlash(p), nil
}

func mkdirAllRoot(root *os.Root, name string, mode os.FileMode) error {
	current := ""
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		if err := root.Mkdir(current, mode); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		f, err := root.Open(current)
		if err != nil {
			return err
		}
		info, statErr := f.Stat()
		f.Close()
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() {
			return fmt.Errorf("%q is not a directory", current)
		}
	}
	return nil
}

func writeFile(root *os.Root, name string, data io.Reader) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, data)
	closeErr := f.Close()
	if err = errors.Join(copyErr, closeErr); err != nil {
		return errors.Join(err, root.Remove(name))
	}
	return nil
}

func writeReports(root *os.Root, report Report) error {
	f, err := root.OpenFile("summary.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(report)
	if err = errors.Join(encodeErr, f.Close()); err != nil {
		return err
	}
	var text strings.Builder
	fmt.Fprintf(&text, "jsmap\nStarted: %s\nInput: %q\nAssets: %d\n", report.StartedAt.Format(time.RFC3339), report.Input, len(report.Files))
	for _, r := range report.Files {
		fmt.Fprintf(&text, "\nURL: %q\nFile: %s\n", r.URL, r.File)
		if r.AnalysisNote != "" {
			fmt.Fprintf(&text, "Analysis: %q\n", r.AnalysisNote)
		}
		if r.Error != "" {
			fmt.Fprintf(&text, "ERROR: %q\n", r.Error)
		}
		for _, hit := range r.Findings {
			fmt.Fprintf(&text, "  %s:%d [%s] %q\n", hit.File, hit.Line, hit.Category, hit.Snippet)
		}
		if m := r.SourceMap; m != nil {
			fmt.Fprintf(&text, "SourceMap: %q\nStatus: %s\nExtracted: %d/%d; missing content: %d\nDirectory: %s\n", m.URL, m.Status, m.Extracted, m.Sources, m.Missing, m.Directory)
			for _, e := range m.Errors {
				fmt.Fprintf(&text, "  ERROR: %q\n", e)
			}
		} else {
			fmt.Fprintln(&text, "SourceMap: not detected")
		}
	}
	return writeFile(root, "summary.txt", strings.NewReader(text.String()))
}

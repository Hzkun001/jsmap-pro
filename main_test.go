package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEndToEnd(t *testing.T) {
	var heads atomic.Int32
	js := "crypto.subtle.encrypt(x);\nCryptoJS.AES(); 'x-signature'; btoa(x);\n//# sourceMappingURL=app.js.map\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/site/index.html", http.StatusFound)
		case "/site/index.html":
			fmt.Fprint(w, `<html><head><base href="/assets/"></head><body><script src="app.js?v=1&amp;x=2"></script><script src="app.js?v=1&amp;x=2#duplicate"></script><script>load("lazy.chunk.js");</script></body></html>`)
		case "/assets/app.js":
			fmt.Fprint(w, js)
		case "/assets/lazy.chunk.js":
			fmt.Fprint(w, "console.log('lazy');")
		case "/assets/app.js.map":
			if r.Method == http.MethodHead {
				heads.Add(1)
				return
			}
			fmt.Fprint(w, `{"version":3,"sourceRoot":"webpack:///","sources":["./src/a.js","./src/empty.js"],"sourcesContent":["export const a = 1;", ""],"mappings":""}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "out")
	var logs bytes.Buffer
	if err := run(context.Background(), config{URL: srv.URL, Output: out, Concurrency: 2, Timeout: time.Second * 5}, &logs); err != nil {
		t.Fatal(err)
	}
	var report Report
	b, err := os.ReadFile(filepath.Join(out, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 2 {
		t.Fatalf("want two deduplicated assets, got %+v", report)
	}
	var app FileResult
	for _, f := range report.Files {
		if strings.Contains(f.URL, "app.js") {
			app = f
		}
	}
	if len(app.Findings) != 4 {
		t.Fatalf("want all four categories, got %+v", app.Findings)
	}
	if app.Findings[0].Line != 1 || app.Findings[1].Line != 2 {
		t.Fatalf("wrong line numbers: %+v", app.Findings)
	}
	if app.SourceMap == nil || app.SourceMap.Status != fullyReconstructed || heads.Load() != 1 {
		t.Fatalf("map not reconstructed: %+v, HEADs=%d", app.SourceMap, heads.Load())
	}
	if app.SourceMap.Extracted != 2 {
		t.Fatalf("empty source lost: %+v", app.SourceMap)
	}
	raw, err := os.ReadFile(filepath.Join(out, app.File))
	if err != nil || string(raw) != js {
		t.Fatalf("raw changed: %q, %v", raw, err)
	}
	source, err := os.ReadFile(filepath.Join(out, app.SourceMap.Directory, "src/a.js"))
	if err != nil || string(source) != "export const a = 1;" {
		t.Fatalf("bad source: %q %v", source, err)
	}
	txt, err := os.ReadFile(filepath.Join(out, "report.txt"))
	if err != nil || !bytes.Contains(txt, []byte(fullyReconstructed)) {
		t.Fatalf("text report missing: %s %v", txt, err)
	}
	if !strings.Contains(logs.String(), "WebCrypto") {
		t.Fatalf("live findings missing: %s", logs.String())
	}
}

func TestStreamingLongLinesAndBoundaries(t *testing.T) {
	// The crypto call crosses a chunk boundary; a later category is over 10 MiB into the same line.
	js := strings.Repeat(" ", scanChunk-7) + "crypto.subtle.decrypt(x);" + strings.Repeat("x", 10<<20) + "; btoa(x);\r\nJSEncrypt();\n//# sourceMappingURL=last.js.map\n"
	var raw bytes.Buffer
	var findings []Finding
	ref, err := scanJS(io.TeeReader(strings.NewReader(js), &raw), func(f Finding) { findings = append(findings, f) })
	if err != nil {
		t.Fatal(err)
	}
	if raw.String() != js {
		t.Fatal("streamed copy is not byte-for-byte identical")
	}
	if ref != "last.js.map" {
		t.Fatalf("wrong map: %q", ref)
	}
	if len(findings) != 3 || findings[0].Line != 1 || findings[1].Line != 1 || findings[2].Line != 2 {
		t.Fatalf("lost matches or wrong lines: %+v", findings)
	}
}

func TestPartialMapAndTraversal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".map") {
			fmt.Fprint(w, `{"version":3,"sources":["src/ok.js","../../escape.js","src/missing.js"],"sourcesContent":["ok","escape",null]}`)
			return
		}
		fmt.Fprint(w, "//# sourceMappingURL=partial.map")
	}))
	defer srv.Close()
	tmp := t.TempDir()
	list := filepath.Join(tmp, "urls.txt")
	if err := os.WriteFile(list, []byte(srv.URL+"/app.js\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "out")
	if err := run(context.Background(), config{List: list, Output: out, Concurrency: 1, Timeout: time.Second}, io.Discard); err == nil {
		t.Fatal("expected a reported partial failure")
	}
	data, err := os.ReadFile(filepath.Join(out, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	m := report.Files[0].SourceMap
	if m == nil || m.Status == fullyReconstructed || m.Extracted != 1 || m.Missing != 1 || len(m.Errors) != 1 {
		t.Fatalf("misleading partial report: %+v", m)
	}
	if _, err := os.Stat(filepath.Join(out, "escape.js")); !os.IsNotExist(err) {
		t.Fatal("path traversal escaped extraction directory")
	}
}

func TestListFailuresAndSameBasename(t *testing.T) {
	var active, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(15 * time.Millisecond)
		if r.URL.Path == "/missing.js" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "atob('a');")
	}))
	defer srv.Close()
	tmp := t.TempDir()
	list := filepath.Join(tmp, "urls.txt")
	if err := os.WriteFile(list, []byte(fmt.Sprintf("# assets\n%s/a/app.js\n%s/b/app.js\n%s/a/app.js#x\n%s/missing.js\n", srv.URL, srv.URL, srv.URL, srv.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "out")
	if err := run(context.Background(), config{List: list, Output: out, Concurrency: 2, Timeout: time.Second}, io.Discard); err == nil {
		t.Fatal("HTTP failure should return a nonzero result")
	}
	b, err := os.ReadFile(filepath.Join(out, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 3 || report.Files[0].File == report.Files[1].File || report.Files[2].Error == "" {
		t.Fatalf("bad results: %+v", report)
	}
	if peak.Load() > 2 || peak.Load() < 2 {
		t.Fatalf("worker pool did not respect concurrency: peak=%d", peak.Load())
	}
	if err := run(context.Background(), config{List: list, Output: out, Concurrency: 1, Timeout: time.Second}, io.Discard); err == nil {
		t.Fatal("existing output should be preserved")
	}
}

func TestSourcePaths(t *testing.T) {
	for _, tc := range []struct{ root, source, want string }{
		{"webpack:///", "./src/a.js", "src/a.js"},
		{"", "webpack://app/./src/a.js", "app/src/a.js"},
		{"src", "lib/a.js", "src/lib/a.js"},
		{"", "/app/src/a.js", "app/src/a.js"},
	} {
		got, err := sourcePath(tc.root, tc.source)
		if err != nil || got != tc.want {
			t.Errorf("%q + %q: %q %v", tc.root, tc.source, got, err)
		}
	}
	for _, name := range []string{"../../escape", "%2e%2e/%2e%2e/escape", `..\..\escape`, "", "a\x00.js", "C:/outside.js"} {
		if p, err := sourcePath("", name); err == nil {
			t.Errorf("accepted unsafe %q as %q", name, p)
		}
	}
}

func TestScannerDoesNotInventWordBoundaryInOverlap(t *testing.T) {
	js := strings.Repeat("x", scanChunk-256) + "atob(" + strings.Repeat("x", 251) + "; btoa(x);"
	var hits []Finding
	_, err := scanJS(strings.NewReader(js), func(f Finding) { hits = append(hits, f) })
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Snippet, "btoa(") {
		t.Fatalf("invented a word boundary at overlap start: %+v", hits)
	}
}

func TestHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body      string
		head            int
		truncated, slow bool
	}{
		{name: "HEAD denied", head: http.StatusForbidden, body: `{"version":3}`},
		{name: "bad JSON", head: http.StatusOK, body: `not JSON`},
		{name: "indexed map", head: http.StatusOK, body: `{"version":3,"sections":[]}`},
		{name: "truncated download", truncated: true},
		{name: "timeout", slow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mapGets atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.slow {
					<-r.Context().Done()
					return
				}
				if r.URL.Path == "/app.js" {
					if tc.truncated {
						w.Header().Set("Content-Length", "1000")
						fmt.Fprint(w, "CryptoJS")
						return
					}
					fmt.Fprint(w, "//# sourceMappingURL=app.js.map")
					return
				}
				if r.Method == http.MethodHead {
					w.WriteHeader(tc.head)
					return
				}
				mapGets.Add(1)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			root, err := prepareOutput(filepath.Join(t.TempDir(), "out"))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			timeout := time.Second
			if tc.slow {
				timeout = 20 * time.Millisecond
			}
			result := processJS(context.Background(), &http.Client{Timeout: timeout}, root, srv.URL+"/app.js", func(Finding) {})
			if tc.truncated || tc.slow {
				if result.Error == "" {
					t.Fatal("download failure was swallowed")
				}
				if _, err := root.Stat(result.File); !os.IsNotExist(err) {
					t.Fatalf("incomplete raw file retained: %v", err)
				}
			} else {
				if result.SourceMap == nil || len(result.SourceMap.Errors) == 0 || result.SourceMap.Status == fullyReconstructed {
					t.Fatalf("invalid map reported as successful: %+v", result)
				}
				if tc.head != http.StatusOK && mapGets.Load() != 0 {
					t.Fatal("map fetched despite rejected HEAD")
				}
			}
		})
	}
}

func TestExtractionRejectsSymlinkEscape(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := writeFile(root, "link/escape.js", strings.NewReader("escape")); err == nil {
		t.Fatal("symlink escaped extraction root")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.js")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists: %v", err)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, c := range []config{
		{Output: "unused", Concurrency: 1, Timeout: time.Second},
		{URL: "https://example.com", List: "list.txt", Output: "unused", Concurrency: 1, Timeout: time.Second},
		{URL: "https://example.com", Output: "unused", Concurrency: 0, Timeout: time.Second},
		{URL: "https://example.com", Output: "unused", Concurrency: 1},
		{URL: "file:///etc/passwd", Output: "unused", Concurrency: 1, Timeout: time.Second},
	} {
		if err := run(context.Background(), c, io.Discard); err == nil {
			t.Errorf("accepted invalid configuration: %+v", c)
		}
	}
}

func TestDiscoveryOnlyIncludesCompleteJSReferences(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<script>fetch(\"config.json\"); load('chunk.js'); load(`template.js?v=1`); x=\"app.js.map\"; user.json;</script>")
	}))
	defer srv.Close()
	got, err := discover(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{srv.URL + "/chunk.js", srv.URL + "/template.js?v=1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestMultilineSourceMapAnnotation(t *testing.T) {
	ref, err := scanJS(strings.NewReader("console.log(1);\n/*# sourceMappingURL=app.js.map\n*/\n"), func(Finding) {})
	if err != nil || ref != "app.js.map" {
		t.Fatalf("lost trailing block annotation: %q %v", ref, err)
	}
}

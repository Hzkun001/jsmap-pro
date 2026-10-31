package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInliner(t *testing.T) {
	for _, tc := range []struct {
		source string
		count  int
	}{
		{`const a=["X-Signature"];function get(i){return a[i]}send(get(0));`, 1},
		{`var a=['CryptoJS'];function get(i){return a[i]}send(get(0x0));`, 1},
		{`const a=['CryptoJS'];function get(i){return a[i]}a[0]='safe';send(get(0));`, 0},
		{`const a=['CryptoJS'];function get(i){return a[i]}get=other;send(get(0));`, 0},
		{`const a=['CryptoJS'];function get(i){return a[i]}send(get(4),get(x));`, 0},
		{`const a=['CryptoJS'];function get(i){return a[i]}function f(get){return get(0)}send(get(0));`, 1},
		{`const a=['CryptoJS'];function get(i){return a[i]}eval('a[0]="safe"');send(get(0));`, 0},
	} {
		out, count, err := inlineStrings(strings.NewReader(tc.source))
		if err != nil || count != tc.count {
			t.Fatalf("%s: got %d %q %v", tc.source, count, out, err)
		}
		if count > 0 && (!strings.Contains(out, `send("X-Signature")`) && !strings.Contains(out, `send('CryptoJS')`)) {
			t.Fatalf("call was not replaced: %s", out)
		}
	}
}

func TestOfflineInputs(t *testing.T) {
	for _, mode := range []string{"HAR", "directory"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "out")
			script := `const a=["X-Signature"];function get(i){return a[i]}send(get(0));` + "\n//# sourceMappingURL=app.js.map\n"
			sourceMap := `{"version":3,"sources":["src/app.js"],"sourcesContent":["original"]}`
			c := config{Output: out, Concurrency: 2, Timeout: time.Second}
			if mode == "HAR" {
				entries := []any{}
				for _, item := range []struct{ url, mime, body string }{{"https://offline.invalid/app.js?v=1", "application/javascript", script}, {"https://offline.invalid/app.js.map", "application/json", sourceMap}, {"https://offline.invalid/config.json", "application/json", "{}"}} {
					entries = append(entries, map[string]any{"request": map[string]any{"url": item.url}, "response": map[string]any{"content": map[string]any{"mimeType": item.mime, "encoding": "base64", "text": base64.StdEncoding.EncodeToString([]byte(item.body))}}})
				}
				body, _ := json.Marshal(map[string]any{"log": map[string]any{"entries": entries}})
				c.HAR = filepath.Join(dir, "input.har")
				if err := os.WriteFile(c.HAR, body, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				c.Directory = filepath.Join(dir, "input")
				os.Mkdir(c.Directory, 0700)
				os.WriteFile(filepath.Join(c.Directory, "app.js"), []byte(script), 0600)
				os.WriteFile(filepath.Join(c.Directory, "app.js.map"), []byte(sourceMap), 0600)
			}
			if err := run(context.Background(), c, io.Discard); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(out, "summary.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report Report
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if len(report.Files) != 1 {
				t.Fatalf("wrong inputs: %+v", report)
			}
			file := report.Files[0]
			if file.SourceMap == nil || file.SourceMap.Status != fullyReconstructed {
				t.Fatalf("map unavailable: %+v", file)
			}
			if !strings.Contains(file.AnalysisNote, "Inlined 1") {
				t.Fatalf("AST not applied: %+v", file)
			}
			transformed := false
			for _, f := range file.Findings {
				if strings.HasPrefix(f.File, "analyzed/") {
					transformed = true
				}
			}
			if !transformed {
				t.Fatal("transformed output was not scanned")
			}
			raw, err := os.ReadFile(filepath.Join(out, file.File))
			if err != nil || string(raw) != script {
				t.Fatal("raw content changed", err)
			}
		})
	}
}

func TestInlinerRejectsGlobalMutation(t *testing.T) {
	for _, mutation := range []string{`globalThis.a[0]='safe';`, `window.get=other;`, `this.a[0]='safe';`} {
		source := `var a=['CryptoJS'];function get(i){return a[i]}` + mutation + `send(get(0));`
		_, count, err := inlineStrings(strings.NewReader(source))
		if err != nil || count != 0 {
			t.Fatalf("unsafe rewrite %q: %d %v", source, count, err)
		}
	}
}

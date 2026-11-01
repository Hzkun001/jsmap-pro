package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstaller(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix installer")
	}
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"source", "bundle", "missing", "outside-path"} {
		t.Run(mode, func(t *testing.T) {
			prefix := t.TempDir()
			bin := filepath.Join(prefix, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			installer := filepath.Join(repo, "install.sh")
			if mode != "source" {
				bundle := t.TempDir()
				installer = filepath.Join(bundle, "install.sh")
				if err := os.WriteFile(installer, script, 0700); err != nil {
					t.Fatal(err)
				}
				if mode == "bundle" {
					if err := os.WriteFile(filepath.Join(bundle, "jsmap"), []byte("#!/bin/sh\necho bundled-jsmap\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			installed := filepath.Join(bin, "jsmap")
			if err := os.WriteFile(installed, []byte("previous binary"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", installer)
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "PREFIX="+prefix, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			if mode == "outside-path" {
				cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))
			}
			output, err := cmd.CombinedOutput()
			if mode == "missing" || mode == "outside-path" {
				if err == nil {
					t.Fatal("invalid installation accepted")
				}
				data, e := os.ReadFile(installed)
				if e != nil || string(data) != "previous binary" {
					t.Fatalf("existing installation modified: %s %v", data, e)
				}
				return
			}
			if err != nil {
				t.Fatalf("installer failed: %v\n%s", err, output)
			}
			output, err = exec.Command(installed, "-h").CombinedOutput()
			if err != nil || !strings.Contains(string(output), "jsmap") {
				t.Fatalf("installed executable cannot run: %s %v", output, err)
			}
		})
	}
}

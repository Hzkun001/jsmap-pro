package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

func TestCLIArguments(t *testing.T) {
	for _, args := range [][]string{
		{"example.com", "-o", "results", "-c", "2"},
		{"-o", "results", "example.com", "-c=2"},
		{"-u", "example.com", "-o=results", "-c", "2"},
		{"-c", "2", "-o", "results", "--", "example.com"},
	} {
		c, err := parseCLI(args, io.Discard)
		if err != nil || c.URL != "https://example.com" || c.Output != "results" || c.Concurrency != 2 {
			t.Fatalf("%v: %+v %v", args, c, err)
		}
	}
	c, err := parseCLI([]string{"http://localhost:8080"}, io.Discard)
	if err != nil || c.URL != "http://localhost:8080" {
		t.Fatalf("explicit scheme changed: %+v %v", c, err)
	}
	c, err = parseCLI([]string{"-l", "urls.txt"}, io.Discard)
	if err != nil || c.List != "urls.txt" || c.URL != "" {
		t.Fatalf("URL list became a target: %+v %v", c, err)
	}
	for _, args := range [][]string{
		{"one.example", "two.example"}, {"-u", "one.example", "two.example"},
		{"-l", "urls.txt", "example.com"}, {"-o"}, {"-unknown"},
	} {
		if _, err := parseCLI(args, io.Discard); err == nil {
			t.Fatalf("accepted invalid arguments: %v", args)
		}
	}
}

func TestCLIHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}} {
		var output bytes.Buffer
		_, err := parseCLI(args, &output)
		if !errors.Is(err, flag.ErrHelp) || !strings.Contains(output.String(), "jsmap-pro example.com") {
			t.Fatalf("missing useful help for %v: %v %s", args, err, output.String())
		}
	}
}

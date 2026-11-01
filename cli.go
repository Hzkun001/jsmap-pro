package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

func parseCLI(args []string, output io.Writer) (config, error) {
	var c config
	fs := flag.NewFlagSet("jsmap", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&c.URL, "u", "", "Website URL or hostname (also accepted as a positional target)")
	fs.StringVar(&c.List, "l", "", "File containing JavaScript URLs")
	fs.StringVar(&c.HAR, "har", "", "Offline HTTP Archive file")
	fs.StringVar(&c.Directory, "d", "", "Offline directory of JavaScript files")
	fs.StringVar(&c.Output, "o", "jsmap-output", "New or empty output directory")
	fs.IntVar(&c.Concurrency, "c", 10, "Number of concurrent workers")
	fs.DurationVar(&c.Timeout, "timeout", 30*time.Second, "Timeout per HTTP request")
	fs.Usage = func() {
		fmt.Fprintln(output, "jsmap — JavaScript asset and source map scanner")
		fmt.Fprintln(output, "\nUsage: jsmap [options] [website]")
		fmt.Fprintln(output, "\nExamples:\n  jsmap example.com\n  jsmap example.com -o results\n  jsmap -l urls.txt\n  jsmap -har capture.har\n  jsmap -d ./assets")
		fmt.Fprintln(output, "\nOptions:")
		fs.PrintDefaults()
		fmt.Fprintln(output, "\nHostnames use HTTPS by default. Use http:// explicitly when needed.")
	}
	if len(args) == 0 {
		fs.Usage()
		return c, flag.ErrHelp
	}
	// Keep flag values together while allowing options on either side of the target.
	var options, targets []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			targets = append(targets, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			targets = append(targets, arg)
			continue
		}
		options = append(options, arg)
		name, _, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if fs.Lookup(name) != nil && !hasValue && i+1 < len(args) {
			i++
			options = append(options, args[i])
		}
	}
	if err := fs.Parse(options); err != nil {
		return c, err
	}
	if len(targets) > 1 {
		return c, fmt.Errorf("provide one website target; use -l for a URL list")
	}
	if len(targets) == 1 {
		if c.URL != "" || c.List != "" || c.HAR != "" || c.Directory != "" {
			return c, fmt.Errorf("a website target cannot be combined with another input mode")
		}
		c.URL = targets[0]
	}
	c.URL = strings.TrimSpace(c.URL)
	if c.URL != "" && !strings.Contains(c.URL, "://") {
		c.URL = "https://" + c.URL
	}
	return c, nil
}

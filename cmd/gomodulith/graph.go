package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

// runGraph implements `gomodulith graph`.
func runGraph(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("graph", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text, mermaid, json, d2")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: gomodulith graph [patterns...] [--format text|mermaid|json|d2]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	app, err := loadApp(ctx, fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith graph: %v\n", err)
		return 1
	}

	switch *format {
	case "text":
		g := app.BuildGraph()
		if g.String() == "" {
			fmt.Fprintln(stdout, "(no module dependencies)")
			return 0
		}
		fmt.Fprintln(stdout, g.String())
	case "mermaid":
		fmt.Fprint(stdout, app.ExportMermaid())
	case "d2":
		fmt.Fprint(stdout, app.ExportD2())
	case "json":
		model, err := app.Model(false)
		if err != nil {
			fmt.Fprintf(stderr, "gomodulith graph: %v\n", err)
			return 1
		}
		if err := writeJSON(stdout, model); err != nil {
			fmt.Fprintf(stderr, "gomodulith graph: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(stderr, "gomodulith graph: unknown format %q (want text, mermaid, json or d2)\n", *format)
		return 2
	}
	return 0
}

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// runContract implements `gomodulith contract`: it writes the architecture
// contract (a Markdown file that AI coding agents and humans read before
// touching the codebase) to .gomodulith/architecture.md by default.
func runContract(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("contract", flag.ExitOnError)
	outPath := fs.String("out", "", "output path (default .gomodulith/architecture.md)")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: gomodulith contract [patterns...] [--out <path>]\n")
		fmt.Fprint(stderr, "\nWrites the architecture contract to .gomodulith/architecture.md by default.\n")
		fmt.Fprint(stderr, "The contract describes every module's public API, dependency rules, events and\n")
		fmt.Fprint(stderr, "verification status, so AI coding agents can avoid violating the architecture.\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	app, err := loadApp(ctx, fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith contract: %v\n", err)
		return 1
	}
	doc, err := app.ExportContract()
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith contract: %v\n", err)
		return 1
	}

	path := *outPath
	if path == "" {
		dir := filepath.Join(".", ".gomodulith")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(stderr, "gomodulith contract: %v\n", err)
			return 1
		}
		path = filepath.Join(dir, "architecture.md")
	}
	if err := os.WriteFile(path, doc, 0o644); err != nil {
		fmt.Fprintf(stderr, "gomodulith contract: write %s: %v\n", path, err)
		return 1
	}
	fmt.Fprintf(stdout, "architecture contract written to %s\n", path)
	return 0
}

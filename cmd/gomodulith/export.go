package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/MouXiaoJun/gomodulith/modulith"
)

// runExport implements `gomodulith export`.
func runExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	format := fs.String("format", "json", "output format: json, sarif, mermaid, d2")
	verify := fs.Bool("verify", false, "include verification findings in the JSON/SARIF model")
	outFile := fs.String("out", "", "write output to this file instead of stdout")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: gomodulith export [patterns...] [--format json|sarif|mermaid|d2] [--verify] [--out <file>]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	app, err := loadApp(ctx, fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith export: %v\n", err)
		return 1
	}

	var output []byte
	switch *format {
	case "json":
		out, err := app.ExportJSON(*verify)
		if err != nil {
			fmt.Fprintf(stderr, "gomodulith export: %v\n", err)
			return 1
		}
		output = append(output, out...)
		output = append(output, '\n')
	case "sarif":
		if !*verify {
			fmt.Fprintln(stderr, "gomodulith export: --format sarif implies --verify; findings are included")
		}
		out, err := app.ExportSARIF()
		if err != nil {
			fmt.Fprintf(stderr, "gomodulith export: %v\n", err)
			return 1
		}
		output = append(output, out...)
		output = append(output, '\n')
	case "mermaid":
		output = []byte(app.ExportMermaid())
	case "d2":
		output = []byte(app.ExportD2())
	default:
		fmt.Fprintf(stderr, "gomodulith export: unknown format %q (want json, sarif, mermaid or d2)\n", *format)
		return 2
	}

	if *outFile != "" {
		if err := os.WriteFile(*outFile, output, 0o644); err != nil {
			fmt.Fprintf(stderr, "gomodulith export: write %s: %v\n", *outFile, err)
			return 1
		}
	} else {
		if _, err := stdout.Write(output); err != nil {
			fmt.Fprintf(stderr, "gomodulith export: %v\n", err)
			return 1
		}
	}
	return 0
}

// writeJSON writes v as indented JSON followed by a newline.
func writeJSON(w io.Writer, v any) error {
	data, err := marshalJSON(v)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w)
	return err
}

// marshalJSON returns v as indented JSON.
func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

var _ = modulith.Load

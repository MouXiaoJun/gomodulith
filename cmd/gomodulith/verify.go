package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/MouXiaoJun/gomodulith/modulith"
)

// runVerify implements `gomodulith verify`.
func runVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "emit the result as JSON")
	sarifOut := fs.Bool("sarif", false, "emit the findings as SARIF 2.1.0 (GitHub code scanning)")
	lspOut := fs.Bool("lsp", false, "emit the findings as LSP-style diagnostics")
	noCache := fs.Bool("no-cache", false, "disable the on-disk model cache")
	outFile := fs.String("out", "", "write output to this file instead of stdout")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: gomodulith verify [patterns...] [--json|--sarif|--lsp] [--no-cache] [--out <file>]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	app, err := loadAppIn(ctx, "", fs.Args(), !*noCache)
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith verify: %v\n", err)
		return 1
	}

	res, err := app.Verify()
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith verify: %v\n", err)
		return 1
	}

	var output []byte
	switch {
	case *lspOut:
		diags, err := app.ExportDiagnostics()
		if err != nil {
			fmt.Fprintf(stderr, "gomodulith verify: %v\n", err)
			return 1
		}
		data, err := marshalJSON(diags)
		if err != nil {
			fmt.Fprintf(stderr, "gomodulith verify: %v\n", err)
			return 1
		}
		output = append(output, data...)
		output = append(output, '\n')
	case *sarifOut:
		data, err := app.ExportSARIF()
		if err != nil {
			fmt.Fprintf(stderr, "gomodulith verify: %v\n", err)
			return 1
		}
		output = append(output, data...)
		output = append(output, '\n')
	case *jsonOut:
		report := map[string]any{
			"ok":       res.OK,
			"errors":   len(res.Errors()),
			"warnings": len(res.Warnings()),
			"issues":   issueModels(res),
			"cycles":   cycleModels(res),
		}
		data, err := marshalJSON(report)
		if err != nil {
			fmt.Fprintf(stderr, "gomodulith verify: %v\n", err)
			return 1
		}
		output = append(output, data...)
		output = append(output, '\n')
	default:
		output = []byte(modulith.Report(res))
	}

	if *outFile != "" {
		if err := os.WriteFile(*outFile, output, 0o644); err != nil {
			fmt.Fprintf(stderr, "gomodulith verify: write %s: %v\n", *outFile, err)
			return 1
		}
	} else {
		if _, err := stdout.Write(output); err != nil {
			fmt.Fprintf(stderr, "gomodulith verify: %v\n", err)
			return 1
		}
	}

	if !res.OK {
		return 1
	}
	return 0
}

func issueModels(res *modulith.Result) []modulith.IssueModel {
	out := make([]modulith.IssueModel, 0, len(res.Issues))
	for _, i := range res.Issues {
		out = append(out, modulith.IssueModel{
			Code:     string(i.Code),
			Severity: i.Severity.String(),
			Module:   i.Module,
			Message:  i.Message,
		})
	}
	return out
}

func cycleModels(res *modulith.Result) []modulith.CycleModel {
	out := make([]modulith.CycleModel, 0, len(res.Cycles))
	for _, c := range res.Cycles {
		out = append(out, modulith.CycleModel{Modules: c.Modules})
	}
	return out
}

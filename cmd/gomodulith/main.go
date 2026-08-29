// Command gomodulith verifies, inspects and exports the architecture of a Go
// codebase organized as a modular monolith.
//
// Usage:
//
//	gomodulith verify [patterns...]      verify module boundaries and rules
//	gomodulith graph  [--format text|mermaid|json|d2]   render the module graph
//	gomodulith explain <module>          explain a module's API and dependencies
//	gomodulith export  [--verify] [--format json|mermaid|d2]   export the architecture model
//	gomodulith diff <base.json> <head.json>   show architecture changes between two exports
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/MouXiaoJun/gomodulith/modulith"
)

const version = "0.6.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "verify":
		return runVerify(rest, stdout, stderr)
	case "graph":
		return runGraph(rest, stdout, stderr)
	case "explain":
		return runExplain(rest, stdout, stderr)
	case "export":
		return runExport(rest, stdout, stderr)
	case "contract":
		return runContract(rest, stdout, stderr)
	case "diff":
		return runDiff(rest, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "gomodulith %s\n", version)
		return 0
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "gomodulith: unknown command %q\n\n", cmd)
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `gomodulith - architecture verification toolkit for modular Go monoliths

Usage:
  gomodulith <command> [options]

Commands:
  verify    Verify module boundaries and dependency rules.
  graph     Render the discovered module graph.
  explain   Explain a module's public API, dependencies and dependents.
  export    Export an AI/CI-readable architecture model.
  contract  Write the architecture contract for AI coding agents.
  diff      Show architecture changes between two exported models.

Run "gomodulith <command> -h" for command-specific options.
`)
}

// loadApp loads the application for the current working directory, applying
// the project configuration (.gomodulith.yaml/.gomodulith.toml) when present,
// using the given patterns (defaulting to "./...").
func loadApp(ctx context.Context, patterns []string) (*modulith.Application, error) {
	return loadAppIn(ctx, "", patterns, false)
}

// loadAppIn is loadApp with an explicit working directory ("" for the current
// one) and an optional disk cache. It does not change the process working
// directory.
func loadAppIn(ctx context.Context, dir string, patterns []string, useCache bool) (*modulith.Application, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	if p := modulith.FindConfigFile(dir); p != "" {
		fc, err := modulith.LoadConfigFile(p)
		if err != nil {
			return nil, err
		}
		if useCache {
			return fc.BuildCached(ctx, dir, patterns, "")
		}
		return fc.Build(ctx, dir, patterns)
	}
	if useCache {
		return modulith.LoadCachedIn(ctx, dir, patterns, "")
	}
	return modulith.LoadIn(ctx, dir, patterns...)
}

// parsePatterns splits positional arguments after a flag.FlagSet has parsed.
func parsePatterns(fs *flag.FlagSet, args []string) []string {
	fs.Parse(args)
	return fs.Args()
}

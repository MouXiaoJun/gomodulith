package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/MouXiaoJun/gomodulith/modulith"
)

// runExplain implements `gomodulith explain <module>`.
func runExplain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: gomodulith explain <module> [patterns...]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pos := fs.Args()
	if len(pos) == 0 {
		fmt.Fprintln(stderr, "gomodulith explain: missing module name")
		fs.Usage()
		return 2
	}
	name := pos[0]

	ctx := context.Background()
	app, err := loadApp(ctx, pos[1:])
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith explain: %v\n", err)
		return 1
	}
	m := app.ModuleByName(name)
	if m == nil {
		names := app.Modules()
		modNames := make([]string, 0, len(names))
		for _, n := range names {
			modNames = append(modNames, n.Name)
		}
		sort.Strings(modNames)
		fmt.Fprintf(stderr, "gomodulith explain: module %q not found (modules: %v)\n", name, modNames)
		return 1
	}

	g := app.BuildGraph()
	explainModule(stdout, app, m, g)
	return 0
}

func explainModule(w io.Writer, app *modulith.Application, m *modulith.Module, g *modulith.Graph) {
	fmt.Fprintf(w, "module %s\n", m.Name)
	fmt.Fprintf(w, "  packages: %d\n", m.PackageCount())

	fmt.Fprintf(w, "  public API:\n")
	pub := m.PublicPackages
	sort.Strings(pub)
	if len(pub) == 0 {
		fmt.Fprintln(w, "    (none)")
	}
	for _, p := range pub {
		fmt.Fprintf(w, "    %s\n", p)
	}

	fmt.Fprintf(w, "  allowed dependencies: %v\n", m.AllowedDependencies)
	fmt.Fprintf(w, "  forbidden dependencies: %v\n", m.ForbiddenDependencies)

	fmt.Fprintln(w, "  depends on:")
	deps := g.DependenciesOf(m.Name)
	if len(deps) == 0 {
		fmt.Fprintln(w, "    (none)")
	}
	for _, d := range deps {
		status := "OK"
		if !d.Allowed {
			status = "VIOLATION"
		}
		fmt.Fprintf(w, "    %s  [%s]\n", d.To.Name, status)
		for _, imp := range d.Imports {
			fmt.Fprintf(w, "      %s\n", imp)
		}
	}

	fmt.Fprintln(w, "  depended on by:")
	dependents := g.DependentsOf(m.Name)
	if len(dependents) == 0 {
		fmt.Fprintln(w, "    (none)")
	}
	for _, d := range dependents {
		fmt.Fprintf(w, "    %s\n", d.From.Name)
	}
}

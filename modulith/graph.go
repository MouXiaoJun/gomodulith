package modulith

import (
	"fmt"
	"sort"
	"strings"
)

// Dependency is a directed dependency from one module to another, with the
// concrete package-level imports that establish it.
type Dependency struct {
	From *Module
	To   *Module

	// Imports lists the concrete package import edges as "fromPkg -> toPkg".
	Imports []string

	// Allowed reports whether the dependency is permitted by the declared
	// rules (or by the convention when no explicit rules are configured).
	Allowed bool
}

// Graph is the module-level dependency graph of the application.
type Graph struct {
	App *Application

	// Dependencies lists every module-to-module dependency, sorted by
	// (from, to).
	Dependencies []*Dependency

	byFrom map[string][]*Dependency
}

// ModuleForPackage returns the module that owns the given package path, or
// nil when the package is not part of any module.
func (a *Application) ModuleForPackage(id string) *Module {
	for _, m := range a.modules {
		if _, ok := m.pkgs[id]; ok {
			return m
		}
	}
	return nil
}

// isExternalImport reports whether the import path belongs to the standard
// library or a third-party module (outside the codebase being verified).
func isExternalImport(path string) bool {
	if path == "" {
		return true
	}
	// Standard library packages have no dot in the first path element.
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	if !strings.Contains(first, ".") {
		// std lib (e.g. "fmt", "net/http")
		return true
	}
	return false
}

// BuildGraph constructs the module dependency graph from the loaded packages.
// Imports of standard library and third-party packages are ignored.
func (a *Application) BuildGraph() *Graph {
	g := &Graph{
		App:    a,
		byFrom: map[string][]*Dependency{},
	}

	// Map from module name to module.
	type edge struct {
		from   *Module
		to     *Module
		fromPk string
		toPk   string
	}
	seen := map[string]bool{}
	var edges []edge

	for _, fromPkg := range a.Packages() {
		fromMod := a.ModuleForPackage(fromPkg.ID)
		for _, toPath := range fromPkg.Imports {
			if isExternalImport(toPath) {
				continue
			}
			toMod := a.ModuleForPackage(toPath)
			if toMod == nil {
				continue // package outside the module tree
			}
			if fromMod == nil {
				// Orphan package importing a module package: represented as
				// an "external -> module" edge. We record it as a package
				// import but not as a module dependency since the source is
				// not a module.
				continue
			}
			key := fromMod.Name + "\x00" + toMod.Name + "\x00" + fromPkg.ID + "\x00" + toPath
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, edge{
				from: fromMod, to: toMod,
				fromPk: fromPkg.ID, toPk: toPath,
			})
		}
	}

	// Aggregate into module-level dependencies.
	order := map[string]*Dependency{}
	for _, e := range edges {
		k := e.from.Name + "\x00" + e.to.Name
		d, ok := order[k]
		if !ok {
			d = &Dependency{From: e.from, To: e.to}
			order[k] = d
		}
		d.Imports = append(d.Imports, e.fromPk+" -> "+e.toPk)
	}

	names := sortedKeys(order)
	for _, k := range names {
		d := order[k]
		sort.Strings(d.Imports)
		d.Allowed = a.dependencyAllowed(d.From, d.To)
		g.Dependencies = append(g.Dependencies, d)
		g.byFrom[d.From.Name] = append(g.byFrom[d.From.Name], d)
	}
	return g
}

// dependencyAllowed reports whether a dependency between two modules is
// permitted by the configured rules. In explicit mode, a module without
// declared allowed-dependencies is closed (it may not depend on any module);
// in convention mode it is open (cross-module public imports are allowed).
func (a *Application) dependencyAllowed(from, to *Module) bool {
	if from == nil || to == nil {
		return true
	}
	// Explicit forbidden rules always win.
	if contains(from.ForbiddenDependencies, to.Name) {
		return false
	}
	if len(from.AllowedDependencies) > 0 {
		return contains(from.AllowedDependencies, to.Name)
	}
	if a.explicit {
		return false // explicit mode: closed by default
	}
	return true // convention mode: open by default
}

// DependenciesOf returns the outgoing dependencies of the given module,
// sorted by target module name.
func (g *Graph) DependenciesOf(name string) []*Dependency {
	out := append([]*Dependency(nil), g.byFrom[name]...)
	sort.Slice(out, func(i, j int) bool { return out[i].To.Name < out[j].To.Name })
	return out
}

// DependentsOf returns the modules that depend on the given module, sorted by
// source module name.
func (g *Graph) DependentsOf(name string) []*Dependency {
	var out []*Dependency
	for _, d := range g.Dependencies {
		if d.To.Name == name {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From.Name < out[j].From.Name })
	return out
}

// Edges returns the dependency edges as a sorted list of "from -> to" strings.
func (g *Graph) Edges() []string {
	var out []string
	for _, d := range g.Dependencies {
		out = append(out, d.From.Name+" -> "+d.To.Name)
	}
	return out
}

// String returns a compact textual representation of the graph.
func (g *Graph) String() string {
	var b strings.Builder
	for i, d := range g.Dependencies {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s -> %s", d.From.Name, d.To.Name)
	}
	return b.String()
}

package modulith

import (
	"context"
	"fmt"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// loadPackages loads all packages matching the given patterns using the
// go/packages driver, resolving patterns relative to dir ("" means the
// current working directory). It returns only the directly requested packages
// (not their transitively loaded dependencies).
func loadPackages(ctx context.Context, patterns []string) ([]*Package, error) {
	return loadPackagesIn(ctx, "", patterns)
}

// loadPackagesIn is loadPackages with an explicit working directory.
func loadPackagesIn(ctx context.Context, dir string, patterns []string) ([]*Package, error) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedCompiledGoFiles |
			packages.NeedTypes,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("modulith: load packages: %w", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("modulith: package loading reported errors")
	}

	// Collect only the root packages (patterns matched directly), not the
	// transitively loaded dependencies.
	var out []*Package
	seen := map[string]bool{}
	for _, p := range pkgs {
		if len(p.Errors) > 0 || p.PkgPath == "" || seen[p.PkgPath] {
			continue
		}
		seen[p.PkgPath] = true
		out = append(out, packageFrom(p, dir))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func packageFrom(p *packages.Package, baseDir string) *Package {
	out := &Package{
		ID:   p.PkgPath,
		Name: p.Name,
	}
	if len(p.GoFiles) > 0 {
		out.Dir = filepath.Dir(p.GoFiles[0])
		wd := baseDir
		if wd == "" {
			wd, _ = os.Getwd()
		}
		if rel, err := filepath.Rel(wd, out.Dir); err == nil {
			out.RelDir = rel
		} else {
			out.RelDir = out.Dir
		}
	}
	imports := map[string]bool{}
	for path := range p.Imports {
		imports[path] = true
	}
	out.Imports = sortedKeys(imports)
	out.GoFiles = append([]string(nil), p.GoFiles...)

	// Collect exported type names when type information is available.
	if p.Types != nil {
		scope := p.Types.Scope()
		var typeNames []string
		for _, name := range scope.Names() {
			if !isExported(name) {
				continue
			}
			if _, ok := scope.Lookup(name).(*types.TypeName); ok {
				typeNames = append(typeNames, name)
			}
		}
		sort.Strings(typeNames)
		out.ExportedTypes = typeNames
	}
	return out
}

// isExported reports whether a Go identifier is exported (starts with an
// uppercase letter).
func isExported(name string) bool {
	if name == "" {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// hasGoFiles reports whether dir contains (recursively) any .go files.
func hasGoFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != dir && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go") {
			found = true
		}
		return nil
	})
	return found
}

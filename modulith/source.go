package modulith

import (
	"bytes"
	"go/ast"
	"go/token"
	"go/types"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Positions are captured at load time and cached with the model. ByteColumn
// matches Go/terminal conventions; Range uses UTF-16 for LSP and SARIF.
type sourceLocation struct {
	File       string
	ByteColumn int
	Range      LSPRange
}

type packageSources struct {
	Package *sourceLocation
	Imports map[string]*sourceLocation
	Types   map[string]*sourceLocation
}

func collectPackageSources(p *packages.Package, contents map[string][]byte) *packageSources {
	out := &packageSources{Imports: map[string]*sourceLocation{}, Types: map[string]*sourceLocation{}}
	files := append([]*ast.File(nil), p.Syntax...)
	sort.Slice(files, func(i, j int) bool {
		return p.Fset.PositionFor(files[i].Pos(), false).Filename < p.Fset.PositionFor(files[j].Pos(), false).Filename
	})
	for _, f := range files {
		filename := p.Fset.PositionFor(f.Pos(), false).Filename
		data := contents[filename]
		locate := func(n ast.Node) *sourceLocation {
			start, end := p.Fset.PositionFor(n.Pos(), false), p.Fset.PositionFor(n.End(), false)
			return &sourceLocation{File: filename, ByteColumn: start.Column, Range: LSPRange{
				Start: sourcePosition(data, start), End: sourcePosition(data, end),
			}}
		}
		if out.Package == nil {
			out.Package = locate(f.Name)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err == nil && out.Imports[path] == nil {
				out.Imports[path] = locate(imp.Path)
			}
		}
		// Restrict searches to exported declarations, never function bodies.
		// For inferred or indirect types, the declaration itself is the source
		// of exposure, not an unrelated use of that type elsewhere in the file.
		record := func(name *ast.Ident, node ast.Node) {
			if !name.IsExported() {
				return
			}
			obj := p.TypesInfo.Defs[name]
			if obj == nil {
				return
			}
			if fn, ok := obj.(*types.Func); ok {
				sig := fn.Type().(*types.Signature)
				if recv := sig.Recv(); recv != nil {
					t := recv.Type()
					if ptr, ok := t.(*types.Pointer); ok {
						t = ptr.Elem()
					}
					if named, ok := t.(*types.Named); !ok || !named.Obj().Exported() {
						return
					}
				}
			}
			var refs []TypeRef
			seen, visited := map[string]bool{}, map[types.Type]bool{}
			// Methods are recorded at their own declarations, including methods
			// in other files, instead of at the receiver's type declaration.
			walkType(obj.Type(), &refs, seen, visited)
			if _, ok := obj.(*types.TypeName); ok {
				// Match collectAPITypeRefs: do not expand a referenced alias's
				// definition and misattribute a later, direct exposure to it.
				if named, ok := obj.Type().(*types.Named); ok {
					walkType(named.Underlying(), &refs, seen, visited)
				}
			}
			for _, ref := range refs {
				key := ref.Package + "." + ref.Name
				if out.Types[key] != nil {
					continue
				}
				location := locate(name)
				found := false
				ast.Inspect(node, func(n ast.Node) bool {
					if found {
						return false
					}
					id, ok := n.(*ast.Ident)
					if !ok {
						return true
					}
					tn, ok := p.TypesInfo.Uses[id].(*types.TypeName)
					if ok && tn.Pkg() != nil && tn.Pkg().Path() == ref.Package && tn.Name() == ref.Name {
						location, found = locate(id), true
					}
					return true
				})
				out.Types[key] = location
			}
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				record(d.Name, d.Type)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						record(s.Name, s)
					case *ast.ValueSpec:
						for _, name := range s.Names {
							record(name, s)
						}
					}
				}
			}
		}
	}
	return out
}

func sourcePosition(data []byte, pos token.Position) LSPPosition {
	lineStart := bytes.LastIndexByte(data[:pos.Offset], '\n') + 1
	column := 0
	for _, r := range string(data[lineStart:pos.Offset]) {
		column++
		if r > 0xffff {
			column++
		}
	}
	return LSPPosition{Line: pos.Line - 1, Character: column}
}

func (a *Application) locationForIssue(issue *Issue) *sourceLocation {
	if s := a.sources[issue.From]; s != nil {
		switch issue.Code {
		case CodeCrossModulePrivate, CodeEventDrivenViolation, CodeEventPackageMissing:
			return s.Imports[issue.To]
		case CodeCrossModuleTypeLeakage, CodeInternalAPITypeLeakage:
			return s.Types[issue.To]
		case CodeOrphanPackage:
			return s.Package
		}
	}
	switch issue.Code {
	case CodeForbiddenDependency, CodeUndeclaredDependency:
		return a.moduleImportLocation(issue.From, issue.To)
	case CodeCycle:
		// The first edge is a concrete cause of this cycle, not an arbitrary file.
		modules := strings.Split(issue.To, " -> ")
		if len(modules) >= 2 {
			return a.moduleImportLocation(modules[0], modules[1])
		}
	}
	// Missing declarations have no real Go source location. Keep them in the
	// text/SARIF report; LSP cannot represent a diagnostic without a document.
	return nil
}

func (a *Application) moduleImportLocation(from, to string) *sourceLocation {
	m := a.ModuleByName(from)
	if m == nil {
		return nil
	}
	for _, p := range m.Packages() {
		for _, imp := range p.Imports {
			if target := a.ModuleForPackage(imp); target != nil && target.Name == to {
				if s := a.sources[p.ID]; s != nil && s.Imports[imp] != nil {
					return s.Imports[imp]
				}
			}
		}
	}
	return nil
}

func (a *Application) sourcePath(file string) string {
	if rel, err := filepath.Rel(a.wd, file); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return file
}

// fileURI handles native paths and Windows drive/UNC paths, escaping URI
// delimiters instead of accidentally treating '#' or '?' as a fragment/query.
func fileURI(path string) string {
	if strings.HasPrefix(path, `\\`) || (len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/')) {
		path = strings.ReplaceAll(path, `\`, "/")
	}
	path = filepath.ToSlash(path)
	if strings.HasPrefix(path, "//") {
		host, rest, _ := strings.Cut(path[2:], "/")
		return (&url.URL{Scheme: "file", Host: host, Path: "/" + rest}).String()
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

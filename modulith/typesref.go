package modulith

import (
	"go/types"
	"sort"
)

// collectAPITypeRefs walks the exported API surface of a typed package and
// returns the de-duplicated set of types referenced by it. The API surface
// comprises exported functions (including methods and their type parameters),
// exported type declarations (expanded into their struct fields, interface
// methods and aliases) and exported variables and constants.
//
// Referenced types are recorded as defining package + name and are not
// expanded, except for the top-level type being declared. Generic arguments
// are part of the API surface and are walked, with type-identity cycle guards.
func collectAPITypeRefs(tp *types.Package) []TypeRef {
	if tp == nil {
		return nil
	}
	var out []TypeRef
	seen := map[string]bool{}
	visited := map[types.Type]bool{}
	walk := func(t types.Type) { walkType(t, &out, seen, visited) }

	scope := tp.Scope()
	for _, name := range scope.Names() {
		if !isExported(name) {
			continue
		}
		switch obj := scope.Lookup(name).(type) {
		case *types.Func:
			if sig, ok := obj.Type().(*types.Signature); ok {
				walkSignature(sig, walk)
			}
		case *types.TypeName:
			walkDeclared(obj.Type(), &out, seen, visited)
		case *types.Var:
			walk(obj.Type())
		case *types.Const:
			walk(obj.Type())
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// walkDeclared records a top-level exported type and expands its definition so
// that types used by its fields, methods and underlying type are captured.
func walkDeclared(t types.Type, out *[]TypeRef, seen map[string]bool, visited map[types.Type]bool) {
	walkType(t, out, seen, visited)
	expanded := map[string]bool{}
	expandNamed(t, out, seen, expanded, visited)
}

// expandNamed records the methods of a named type and descends into its
// underlying type. Each named type is expanded at most once per declaration,
// preventing infinite recursion through self-referential types.
func expandNamed(t types.Type, out *[]TypeRef, seen, expanded map[string]bool, visited map[types.Type]bool) {
	n, ok := t.(*types.Named)
	if !ok {
		return
	}
	obj := n.Obj()
	if obj == nil || obj.Pkg() == nil {
		return
	}
	key := obj.Pkg().Path() + ":" + obj.Name()
	if expanded[key] {
		return
	}
	expanded[key] = true

	walk := func(tt types.Type) { walkType(tt, out, seen, visited) }
	for i := 0; i < n.NumMethods(); i++ {
		m := n.Method(i)
		if !isExported(m.Name()) {
			continue
		}
		if sig, ok := m.Type().(*types.Signature); ok {
			walkSignature(sig, walk)
		}
	}
	walk(n.Underlying())
}

// walkType appends every *types.Named referenced by t to out, recording each
// defining package/name at most once. It descends through structural types but
// named type definitions are not expanded, but their arguments are traversed.
func walkType(t types.Type, out *[]TypeRef, seen map[string]bool, visited map[types.Type]bool) {
	if t == nil || visited[t] {
		return
	}
	visited[t] = true
	walk := func(t types.Type) { walkType(t, out, seen, visited) }
	switch tt := t.(type) {
	case *types.Named:
		obj := tt.Obj()
		if obj == nil || obj.Pkg() == nil {
			return
		}
		key := obj.Pkg().Path() + ":" + obj.Name()
		if !seen[key] {
			seen[key] = true
			*out = append(*out, TypeRef{Package: obj.Pkg().Path(), Name: obj.Name()})
		}
		// Do not descend into the referenced type's definition.
		for i := 0; i < tt.TypeArgs().Len(); i++ {
			walk(tt.TypeArgs().At(i))
		}
	case *types.Pointer:
		walk(tt.Elem())
	case *types.Slice:
		walk(tt.Elem())
	case *types.Array:
		walk(tt.Elem())
	case *types.Map:
		walk(tt.Key())
		walk(tt.Elem())
	case *types.Chan:
		walk(tt.Elem())
	case *types.Signature:
		walkSignature(tt, walk)
	case *types.Interface:
		for i := 0; i < tt.NumMethods(); i++ {
			walk(tt.Method(i).Type())
		}
	case *types.Struct:
		for i := 0; i < tt.NumFields(); i++ {
			walk(tt.Field(i).Type())
		}
	case *types.TypeParam:
		walk(tt.Constraint())
	case *types.Union:
		for i := 0; i < tt.Len(); i++ {
			walk(tt.Term(i).Type())
		}
	case *types.Alias:
		walk(types.Unalias(tt))
	case *types.Tuple:
		for i := 0; i < tt.Len(); i++ {
			walk(tt.At(i).Type())
		}
	}
}

func walkSignature(sig *types.Signature, walk func(types.Type)) {
	if sig == nil {
		return
	}
	if params := sig.Params(); params != nil {
		walk(params)
	}
	if results := sig.Results(); results != nil {
		walk(results)
	}
	if tps := sig.TypeParams(); tps != nil {
		for i := 0; i < tps.Len(); i++ {
			walk(tps.At(i))
		}
	}
	if recv := sig.Recv(); recv != nil {
		walk(recv.Type())
	}
}

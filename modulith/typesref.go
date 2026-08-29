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
// expanded: the walk stops at every *types.Named except the top-level type
// being declared. This keeps the result bounded and free of infinite recursion
// for self-referential types.
func collectAPITypeRefs(tp *types.Package) []TypeRef {
	if tp == nil {
		return nil
	}
	var out []TypeRef
	seen := map[string]bool{}
	walk := func(t types.Type) { walkType(t, &out, seen) }

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
			walkDeclared(obj.Type(), &out, seen)
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
func walkDeclared(t types.Type, out *[]TypeRef, seen map[string]bool) {
	walkType(t, out, seen)
	expanded := map[string]bool{}
	expandNamed(t, out, seen, expanded)
}

// expandNamed records the methods of a named type and descends into its
// underlying type. Each named type is expanded at most once per declaration,
// preventing infinite recursion through self-referential types.
func expandNamed(t types.Type, out *[]TypeRef, seen, expanded map[string]bool) {
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

	walk := func(tt types.Type) { walkType(tt, out, seen) }
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
// stops at named types (their internals belong to their own package).
func walkType(t types.Type, out *[]TypeRef, seen map[string]bool) {
	if t == nil {
		return
	}
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
	case *types.Pointer:
		walkType(tt.Elem(), out, seen)
	case *types.Slice:
		walkType(tt.Elem(), out, seen)
	case *types.Array:
		walkType(tt.Elem(), out, seen)
	case *types.Map:
		walkType(tt.Key(), out, seen)
		walkType(tt.Elem(), out, seen)
	case *types.Chan:
		walkType(tt.Elem(), out, seen)
	case *types.Signature:
		walkSignature(tt, func(t types.Type) { walkType(t, out, seen) })
	case *types.Interface:
		for i := 0; i < tt.NumMethods(); i++ {
			walkType(tt.Method(i).Type(), out, seen)
		}
	case *types.Struct:
		for i := 0; i < tt.NumFields(); i++ {
			walkType(tt.Field(i).Type(), out, seen)
		}
	case *types.TypeParam:
		walkType(tt.Constraint(), out, seen)
	case *types.Union:
		for i := 0; i < tt.Len(); i++ {
			walkType(tt.Term(i).Type(), out, seen)
		}
	case *types.Alias:
		walkType(types.Unalias(tt), out, seen)
	case *types.Tuple:
		for i := 0; i < tt.Len(); i++ {
			walkType(tt.At(i).Type(), out, seen)
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

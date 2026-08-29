package modulith

import (
	"fmt"
)

// checkTypeLeakage reports symbol-level API defects: a module's public surface
// (public API packages and event packages) exposing types that consumers cannot
// use without reaching into private packages.
//
// Two flavours:
//
//   - internal-api-leakage: the public surface references a type defined in the
//     module's own private packages. The internal type is coupled into the
//     public contract, so external consumers can neither name it nor change it
//     freely.
//   - cross-module-type-leakage: the public surface references a type defined in
//     another module's private packages. External consumers cannot name that
//     type without importing the other module's internals, so those API
//     elements are unusable from outside.
//
// Unlike the package-level import checks, this operates on the type level: it
// catches exposure through public packages and event packages that import no
// private package at all.
func (a *Application) checkTypeLeakage() []*Issue {
	var issues []*Issue
	for _, m := range a.modules {
		for _, p := range m.Packages() {
			if !a.isPublicSurface(m, p.ID) {
				continue
			}
			for _, ref := range p.APITypeRefs {
				if ref.Package == "" || ref.Name == "" {
					continue
				}
				if isExternalImport(ref.Package) {
					continue
				}
				owner := a.ModuleForPackage(ref.Package)
				if owner == nil {
					continue
				}
				typeName := ref.Package + "." + ref.Name
				if owner == m {
					if !a.isPublicSurface(m, ref.Package) {
						issues = append(issues, &Issue{
							Code:     CodeInternalAPITypeLeakage,
							Severity: SeverityError,
							Module:   m.Name,
							Message: fmt.Sprintf(
								"public surface package %q of module %q exposes internal type %q; external consumers cannot name it",
								p.ID, m.Name, typeName),
							From: p.ID,
							To:   typeName,
						})
					}
					continue
				}
				if !a.isPublicSurface(owner, ref.Package) {
					issues = append(issues, &Issue{
						Code:     CodeCrossModuleTypeLeakage,
						Severity: SeverityError,
						Module:   m.Name,
						Message: fmt.Sprintf(
							"public surface package %q of module %q exposes type %q defined in module %q's private package",
							p.ID, m.Name, typeName, owner.Name),
						From: p.ID,
						To:   typeName,
					})
				}
			}
		}
	}
	return issues
}

package modulith

import (
	"fmt"
	"sort"
	"strings"
)

// Severity classifies an issue.
type Severity int

const (
	// SeverityWarning is informational; it does not fail verification.
	SeverityWarning Severity = iota
	// SeverityError fails verification.
	SeverityError
)

func (s Severity) String() string {
	if s == SeverityError {
		return "error"
	}
	return "warning"
}

// IssueCode identifies the kind of architecture issue.
type IssueCode string

const (
	// CodeCrossModulePrivate is a cross-module import that reaches into a
	// module's private (non-API) packages.
	CodeCrossModulePrivate IssueCode = "cross-module-private-access"
	// CodeUndeclaredDependency is a module dependency not declared in the
	// source module's allowed-dependencies rules.
	CodeUndeclaredDependency IssueCode = "undeclared-dependency"
	// CodeForbiddenDependency is a module dependency explicitly forbidden by
	// the source module's rules.
	CodeForbiddenDependency IssueCode = "forbidden-dependency"
	// CodeCycle is a cyclic module dependency.
	CodeCycle IssueCode = "cycle"
	// CodeOrphanPackage is a loaded package that belongs to no module.
	CodeOrphanPackage IssueCode = "orphan-package"
	// CodeMissingPublicAPI is a module without any public API package.
	CodeMissingPublicAPI IssueCode = "missing-public-api"
	// CodeInvalidPublicAPI is a declared public API package that does not
	// exist or does not belong to the module.
	CodeInvalidPublicAPI IssueCode = "invalid-public-api"
)

// Issue is a single architecture finding.
type Issue struct {
	Code     IssueCode
	Severity Severity
	Module   string // related module (empty when not module-specific)
	Message  string // human-readable description
	From     string // source package or module, when applicable
	To       string // target package or module, when applicable
}

// String returns a compact one-line representation of the issue.
func (i *Issue) String() string {
	loc := i.Module
	if i.From != "" {
		if loc != "" {
			loc = loc + " · " + i.From
		} else {
			loc = i.From
		}
	}
	prefix := ""
	if loc != "" {
		prefix = loc + ": "
	}
	return fmt.Sprintf("%s [%s] %s", prefix, i.Code, i.Message)
}

// Result is the outcome of a verification run.
type Result struct {
	// OK reports whether no error-severity issues were found.
	OK bool

	// Issues lists every finding, sorted by severity then code then message.
	Issues []*Issue

	// Graph is the module dependency graph that was verified.
	Graph *Graph

	// Cycles lists the detected cyclic dependencies.
	Cycles []*Cycle
}

// Errors returns only the error-severity issues.
func (r *Result) Errors() []*Issue {
	var out []*Issue
	for _, i := range r.Issues {
		if i.Severity == SeverityError {
			out = append(out, i)
		}
	}
	return out
}

// Warnings returns only the warning-severity issues.
func (r *Result) Warnings() []*Issue {
	var out []*Issue
	for _, i := range r.Issues {
		if i.Severity == SeverityWarning {
			out = append(out, i)
		}
	}
	return out
}

// HasCode reports whether the result contains an issue with the given code.
func (r *Result) HasCode(code IssueCode) bool {
	for _, i := range r.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// Verify runs every configured architecture check against the loaded
// application and returns the result. The returned error is nil unless
// verification itself failed to run (e.g. the application was not loaded).
func (a *Application) Verify() (*Result, error) {
	if !a.loaded {
		return nil, fmt.Errorf("modulith: application not loaded; call Load, LoadExplicit or Scan first")
	}
	g := a.BuildGraph()
	cycles := g.Cycles()
	var issues []*Issue

	issues = append(issues, a.checkCrossModulePrivateAccess()...)
	issues = append(issues, checkDependencyRules(g)...)
	issues = append(issues, a.checkPublicAPIDeclarations()...)
	issues = append(issues, a.checkMissingPublicAPI()...)
	issues = append(issues, a.checkOrphans()...)

	for _, c := range cycles {
		issues = append(issues, &Issue{
			Code:     CodeCycle,
			Severity: SeverityError,
			Message:  "cyclic module dependency: " + c.String(),
			To:       c.String(),
		})
	}

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Severity != issues[j].Severity {
			return issues[i].Severity > issues[j].Severity // errors first
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].String() < issues[j].String()
	})

	ok := true
	for _, i := range issues {
		if i.Severity == SeverityError {
			ok = false
			break
		}
	}
	return &Result{OK: ok, Issues: issues, Graph: g, Cycles: cycles}, nil
}

// checkCrossModulePrivateAccess reports imports from one module into another
// module's private (non-public-API) packages.
func (a *Application) checkCrossModulePrivateAccess() []*Issue {
	var issues []*Issue
	for _, fromPkg := range a.Packages() {
		fromMod := a.ModuleForPackage(fromPkg.ID)
		for _, toPath := range fromPkg.Imports {
			if isExternalImport(toPath) {
				continue
			}
			toMod := a.ModuleForPackage(toPath)
			if toMod == nil {
				continue
			}
			if fromMod == nil {
				// An unclassified package reaching into a module's private
				// packages is a warning; its public packages are acceptable
				// entry points.
				if !toMod.IsPublicPackage(toPath) {
					issues = append(issues, &Issue{
						Code:     CodeCrossModulePrivate,
						Severity: SeverityWarning,
						Module:   toMod.Name,
						Message:  fmt.Sprintf("unclassified package %q imports module's private package %q", fromPkg.ID, toPath),
						From:     fromPkg.ID,
						To:       toPath,
					})
				}
				continue
			}
			if fromMod == toMod {
				continue // intra-module
			}
			if !toMod.IsPublicPackage(toPath) {
				issues = append(issues, &Issue{
					Code:     CodeCrossModulePrivate,
					Severity: SeverityError,
					Module:   fromMod.Name,
					Message: fmt.Sprintf(
						"module %q imports private package %q of module %q; import the public API instead",
						fromMod.Name, toPath, toMod.Name),
					From: fromPkg.ID,
					To:   toPath,
				})
			}
		}
	}
	return issues
}

// checkDependencyRules reports undeclared and forbidden module dependencies.
func checkDependencyRules(g *Graph) []*Issue {
	var issues []*Issue
	for _, d := range g.Dependencies {
		if contains(d.From.ForbiddenDependencies, d.To.Name) {
			issues = append(issues, &Issue{
				Code:     CodeForbiddenDependency,
				Severity: SeverityError,
				Module:   d.From.Name,
				Message:  fmt.Sprintf("module %q must not depend on module %q", d.From.Name, d.To.Name),
				From:     d.From.Name,
				To:       d.To.Name,
			})
			continue
		}
		if !d.Allowed {
			allowed := d.From.AllowedDependencies
			detail := "it is not in the allowed dependencies"
			if len(allowed) > 0 {
				detail = fmt.Sprintf("it is not in the allowed dependencies (%s)", strings.Join(allowed, ", "))
			}
			issues = append(issues, &Issue{
				Code:     CodeUndeclaredDependency,
				Severity: SeverityError,
				Module:   d.From.Name,
				Message:  fmt.Sprintf("module %q depends on module %q but %s", d.From.Name, d.To.Name, detail),
				From:     d.From.Name,
				To:       d.To.Name,
			})
		}
	}
	return issues
}

// checkPublicAPIDeclarations validates explicitly declared public API
// packages: they must exist in the loaded packages and belong to the module.
func (a *Application) checkPublicAPIDeclarations() []*Issue {
	var issues []*Issue
	for _, m := range a.modules {
		for _, path := range m.PublicPackages {
			pkg, ok := a.pkgs[path]
			if !ok {
				issues = append(issues, &Issue{
					Code:     CodeInvalidPublicAPI,
					Severity: SeverityError,
					Module:   m.Name,
					Message:  fmt.Sprintf("declared public API package %q does not exist", path),
					From:     m.Name,
					To:       path,
				})
				continue
			}
			if _, inMod := m.pkgs[path]; !inMod {
				issues = append(issues, &Issue{
					Code:     CodeInvalidPublicAPI,
					Severity: SeverityError,
					Module:   m.Name,
					Message:  fmt.Sprintf("declared public API package %q does not belong to module %q", path, m.Name),
					From:     m.Name,
					To:       path,
				})
			}
			_ = pkg
		}
	}
	return issues
}

// checkMissingPublicAPI reports modules without any public API package.
func (a *Application) checkMissingPublicAPI() []*Issue {
	var issues []*Issue
	for _, m := range a.modules {
		if len(m.PublicPackages) == 0 {
			sev := SeverityWarning
			if a.config.PublicAPIRequired {
				sev = SeverityError
			}
			issues = append(issues, &Issue{
				Code:     CodeMissingPublicAPI,
				Severity: sev,
				Module:   m.Name,
				Message:  fmt.Sprintf("module %q has no public API package", m.Name),
			})
		}
	}
	return issues
}

// checkOrphans reports unclassified packages when configured to do so.
func (a *Application) checkOrphans() []*Issue {
	var issues []*Issue
	if !a.config.ReportOrphans {
		return issues
	}
	for _, p := range a.orphanPkgs {
		issues = append(issues, &Issue{
			Code:     CodeOrphanPackage,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("package %q is not part of any module", p.ID),
			From:     p.ID,
		})
	}
	return issues
}

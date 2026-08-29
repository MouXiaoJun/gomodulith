package modulith

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ArchitectureModel is the machine-readable, AI/CI-consumable architecture
// contract of the application.
type ArchitectureModel struct {
	// Version is the schema version of the export format.
	Version string `json:"version"`

	// ModulePath is the Go module path, when detectable.
	ModulePath string `json:"module_path,omitempty"`

	// Modules lists every module with its public API and dependency rules.
	Modules []ModuleModel `json:"modules"`

	// Dependencies lists every module-to-module dependency edge.
	Dependencies []DependencyModel `json:"dependencies"`

	// Rules declares the global architecture rules.
	Rules RulesModel `json:"rules"`

	// Issues lists the verification findings, when the model was built from
	// a verification result.
	Issues []IssueModel `json:"issues,omitempty"`

	// Cycles lists detected cyclic dependencies.
	Cycles []CycleModel `json:"cycles,omitempty"`
}

// ModuleModel is the machine-readable description of a single module.
type ModuleModel struct {
	Name                  string   `json:"name"`
	PublicPackages        []string `json:"public_packages,omitempty"`
	PrivatePackages       []string `json:"private_packages,omitempty"`
	AllowedDependencies   []string `json:"allowed_dependencies,omitempty"`
	ForbiddenDependencies []string `json:"forbidden_dependencies,omitempty"`
}

// DependencyModel is a module-to-module dependency edge.
type DependencyModel struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Allowed bool     `json:"allowed"`
	Via     []string `json:"via,omitempty"`
}

// RulesModel declares the global architecture rules.
type RulesModel struct {
	// Cycles is "forbidden" when cyclic module dependencies are not allowed.
	Cycles string `json:"cycles"`

	// CrossModulePrivateImports is "forbidden" when cross-module imports of
	// private packages are not allowed.
	CrossModulePrivateImports string `json:"cross_module_private_imports"`
}

// IssueModel is a machine-readable verification finding.
type IssueModel struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Module   string `json:"module,omitempty"`
	Message  string `json:"message"`
}

// CycleModel is a machine-readable cyclic dependency.
type CycleModel struct {
	Modules []string `json:"modules"`
}

// Model builds the machine-readable architecture model. When verify is true,
// the model also includes the verification findings.
func (a *Application) Model(verify bool) (*ArchitectureModel, error) {
	g := a.BuildGraph()
	cycles := g.Cycles()

	m := &ArchitectureModel{
		Version:    "1",
		ModulePath: detectModulePath(a),
		Rules: RulesModel{
			Cycles:                    "forbidden",
			CrossModulePrivateImports: "forbidden",
		},
	}

	for _, mod := range a.modules {
		mm := ModuleModel{Name: mod.Name}
		publicSet := mod.PublicPackageSet()
		for _, p := range mod.Packages() {
			if publicSet[p.ID] {
				mm.PublicPackages = append(mm.PublicPackages, p.ID)
			} else {
				mm.PrivatePackages = append(mm.PrivatePackages, p.ID)
			}
		}
		sort.Strings(mm.PublicPackages)
		sort.Strings(mm.PrivatePackages)
		mm.AllowedDependencies = append([]string(nil), mod.AllowedDependencies...)
		mm.ForbiddenDependencies = append([]string(nil), mod.ForbiddenDependencies...)
		m.Modules = append(m.Modules, mm)
	}

	for _, d := range g.Dependencies {
		dm := DependencyModel{From: d.From.Name, To: d.To.Name, Allowed: d.Allowed}
		if len(d.Imports) <= 6 {
			dm.Via = append([]string(nil), d.Imports...)
		}
		m.Dependencies = append(m.Dependencies, dm)
	}

	for _, c := range cycles {
		m.Cycles = append(m.Cycles, CycleModel{Modules: c.Modules})
	}

	if verify {
		res, err := a.Verify()
		if err != nil {
			return nil, err
		}
		for _, i := range res.Issues {
			m.Issues = append(m.Issues, IssueModel{
				Code:     string(i.Code),
				Severity: i.Severity.String(),
				Module:   i.Module,
				Message:  i.Message,
			})
		}
	}
	return m, nil
}

// ExportJSON returns the architecture model as indented JSON.
func (a *Application) ExportJSON(verify bool) ([]byte, error) {
	m, err := a.Model(verify)
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("modulith: marshal architecture model: %w", err)
	}
	return out, nil
}

// ExportMermaid renders the module graph as a Mermaid flowchart.
func (a *Application) ExportMermaid() string {
	g := a.BuildGraph()
	var b strings.Builder
	b.WriteString("graph TD\n")
	// Nodes first (so standalone modules render too).
	for _, m := range a.modules {
		fmt.Fprintf(&b, "    %s[%s]\n", sanitizeID(m.Name), m.Name)
	}
	for _, d := range g.Dependencies {
		fmt.Fprintf(&b, "    %s --> %s\n", sanitizeID(d.From.Name), sanitizeID(d.To.Name))
	}
	return b.String()
}

// ExportD2 renders the module graph in D2 syntax.
func (a *Application) ExportD2() string {
	g := a.BuildGraph()
	var b strings.Builder
	for _, m := range a.modules {
		fmt.Fprintf(&b, "%s: { shape: rectangle }\n", sanitizeID(m.Name))
	}
	for _, d := range g.Dependencies {
		fmt.Fprintf(&b, "%s -> %s\n", sanitizeID(d.From.Name), sanitizeID(d.To.Name))
	}
	return b.String()
}

// detectModulePath returns the Go module path from the go.mod in the working
// directory, or "" when it cannot be determined.
func detectModulePath(a *Application) string {
	mod := a.findGoMod()
	if mod == "" {
		return ""
	}
	mod = strings.TrimSpace(mod)
	// The first line is "module <path>".
	for _, line := range strings.Split(mod, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			if i := strings.IndexByte(path, ' '); i >= 0 {
				path = path[:i]
			}
			return strings.Trim(path, `"`)
		}
	}
	return ""
}

// detectModulePath reads go.mod from the application working directory.
func (a *Application) findGoMod() string {
	return readGoMod(a.wd)
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '-' || r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

package modulith

import (
	"path/filepath"
	"sort"
	"strings"
)

// DiagnosticSeverity mirrors the LSP diagnostic severity levels
// (DiagnosticSeverity in the Language Server Protocol).
type DiagnosticSeverity int

const (
	// SeverityErrorLSP = 1 (LSP DiagnosticSeverity.Error).
	SeverityErrorLSP DiagnosticSeverity = 1
	// SeverityWarningLSP = 2 (LSP DiagnosticSeverity.Warning).
	SeverityWarningLSP DiagnosticSeverity = 2
)

// Diagnostic is a violation located at a file, in a form that editors and
// language servers can consume (LSP Diagnostic JSON).
type Diagnostic struct {
	// URI is the file:// URI of the file the diagnostic is attached to.
	URI string `json:"uri"`
	// Range is a zero-based character range (LSP Position is 0-based).
	Range LSPRange `json:"range"`
	// Severity: 1 = error, 2 = warning.
	Severity DiagnosticSeverity `json:"severity"`
	// Code is the architecture issue code, e.g. "cross-module-private-access".
	Code string `json:"code"`
	// Source is the diagnostic producer.
	Source string `json:"source"`
	// Message is the human-readable description.
	Message string `json:"message"`
}

// LSPRange is an LSP Range: a start and end position. Character offsets are
// UTF-16 code units, matching the LSP spec.
type LSPRange struct {
	Start LSPPosition `json:"start"`
	End   LSPPosition `json:"end"`
}

// LSPPosition is an LSP Position (0-based line and UTF-16 character).
type LSPPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// ExportDiagnostics returns the verification findings as LSP-style
// diagnostics, each located at the first source file of the package it
// concerns. When an issue cannot be attached to a file, it is attached to the
// first file of its module (or omitted if none exists).
func (a *Application) ExportDiagnostics() ([]*Diagnostic, error) {
	res, err := a.Verify()
	if err != nil {
		return nil, err
	}
	out := make([]*Diagnostic, 0, len(res.Issues))
	for _, issue := range res.Issues {
		d := &Diagnostic{
			Severity: SeverityWarningLSP,
			Code:     string(issue.Code),
			Source:   "gomodulith",
			Message:  issue.Message,
		}
		if issue.Severity == SeverityError {
			d.Severity = SeverityErrorLSP
		}
		if f := a.fileForIssue(issue); f != "" {
			d.URI = fileURI(f)
			d.Range = LSPRange{
				Start: LSPPosition{Line: 0, Character: 0},
				End:   LSPPosition{Line: 0, Character: 0},
			}
			out = append(out, d)
			continue
		}
		// No file available (e.g. a cycle or module-level issue): attach to the
		// module's first file when possible.
		if f := a.firstModuleFile(issue.Module); f != "" {
			d.URI = fileURI(f)
			d.Range = LSPRange{
				Start: LSPPosition{Line: 0, Character: 0},
				End:   LSPPosition{Line: 0, Character: 0},
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out, nil
}

// fileForIssue returns the first Go file of the source package of the issue.
func (a *Application) fileForIssue(issue *Issue) string {
	if issue.From == "" {
		return ""
	}
	if p := a.pkgs[issue.From]; p != nil {
		return firstGoFile(p)
	}
	return ""
}

// firstModuleFile returns the first Go file of any package of the module.
func (a *Application) firstModuleFile(module string) string {
	m := a.byName[module]
	if m == nil {
		return ""
	}
	for _, p := range m.Packages() {
		if f := firstGoFile(p); f != "" {
			return f
		}
	}
	return ""
}

func firstGoFile(p *Package) string {
	for _, f := range p.GoFiles {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		return f
	}
	if p.Dir != "" {
		return p.Dir
	}
	return ""
}

// fileURI converts an absolute path to a file:// URI.
func fileURI(path string) string {
	return "file://" + filepath.ToSlash(path)
}

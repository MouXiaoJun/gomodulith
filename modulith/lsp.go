package modulith

import (
	"sort"
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
// diagnostics at the actual import or API declaration. Findings without a
// source location (such as missing configuration declarations) are omitted;
// they remain available through Verify, Report and ExportSARIF.
func (a *Application) ExportDiagnostics() ([]*Diagnostic, error) {
	res, err := a.Verify()
	if err != nil {
		return nil, err
	}
	out := make([]*Diagnostic, 0, len(res.Issues))
	for _, issue := range res.Issues {
		loc := a.locationForIssue(issue)
		if loc == nil {
			continue
		}
		d := &Diagnostic{
			URI:      fileURI(loc.File),
			Range:    loc.Range,
			Severity: SeverityWarningLSP,
			Code:     string(issue.Code),
			Source:   "gomodulith",
			Message:  issue.Message,
		}
		if issue.Severity == SeverityError {
			d.Severity = SeverityErrorLSP
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].URI != out[j].URI {
			return out[i].URI < out[j].URI
		}
		if out[i].Range.Start.Line != out[j].Range.Start.Line {
			return out[i].Range.Start.Line < out[j].Range.Start.Line
		}
		return out[i].Range.Start.Character < out[j].Range.Start.Character
	})
	return out, nil
}

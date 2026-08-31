package modulith

import (
	"strings"
	"testing"
)

func TestExportDiagnostics(t *testing.T) {
	writeFixture(t, defaultFixture()) // has order.domain -> user.domain violation
	app := mustLoad(t, "./...")

	diags, err := app.ExportDiagnostics()
	if err != nil {
		t.Fatalf("ExportDiagnostics: %v", err)
	}
	if len(diags) == 0 {
		t.Fatal("expected diagnostics")
	}
	found := false
	for _, d := range diags {
		if d.Code != "cross-module-private-access" {
			continue
		}
		found = true
		if d.Severity != SeverityErrorLSP {
			t.Errorf("severity = %d, want %d", d.Severity, SeverityErrorLSP)
		}
		if d.Source != "gomodulith" {
			t.Errorf("source = %q", d.Source)
		}
		if !strings.HasPrefix(d.URI, "file://") {
			t.Errorf("uri = %q, want file:// prefix", d.URI)
		}
		if !strings.Contains(d.URI, "/internal/order/domain/") {
			t.Errorf("uri should point at the offending package, got %q", d.URI)
		}
		if d.Range.Start.Line != 2 {
			t.Errorf("start line = %d, want import line 2", d.Range.Start.Line)
		}
	}
	if !found {
		t.Fatalf("no cross-module-private-access diagnostic:\n%+v", diags)
	}
}

func TestExportDiagnosticsHealthy(t *testing.T) {
	files := defaultFixture()
	files["internal/order/domain/dom.go"] = goFile("domain", []string{fixtureModule + "/internal/user/api"})
	writeFixture(t, files)
	app := mustLoad(t, "./...")

	diags, err := app.ExportDiagnostics()
	if err != nil {
		t.Fatalf("ExportDiagnostics: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %d: %+v", len(diags), diags)
	}
}

func TestExportDiagnosticsModuleLevelIssue(t *testing.T) {
	// Missing configuration has no source location: do not blame an arbitrary file.
	files := map[string]string{
		"internal/user/domain/dom.go": goFile("domain", nil),
	}
	writeFixture(t, files)
	app := mustLoad(t, "./...")
	diags, err := app.ExportDiagnostics()
	if err != nil {
		t.Fatalf("ExportDiagnostics: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("unexpected fabricated locations: %+v", diags)
	}
	res, err := app.Verify()
	if err != nil || !res.HasCode(CodeMissingPublicAPI) {
		t.Fatalf("missing-public-api finding must remain available: %v, %v", res, err)
	}
}

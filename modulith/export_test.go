package modulith

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExportJSONModel(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")

	data, err := app.ExportJSON(true)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var m ArchitectureModel
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m.Version != "1" {
		t.Errorf("version = %q, want 1", m.Version)
	}
	if len(m.Modules) != 3 {
		t.Fatalf("modules = %d, want 3", len(m.Modules))
	}
	// Verify issues are included (the fixture has a cross-module violation).
	if len(m.Issues) == 0 {
		t.Fatalf("expected issues in verified export:\n%s", data)
	}
	// Module path detection: the fixture module path.
	if !strings.Contains(m.ModulePath, fixtureModule) {
		t.Errorf("module_path = %q, want to contain %q", m.ModulePath, fixtureModule)
	}
}

func TestExportJSONNoIssuesWhenNotVerified(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	data, err := app.ExportJSON(false)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var m ArchitectureModel
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m.Issues) != 0 {
		t.Fatalf("unexpected issues: %v", m.Issues)
	}
	if len(m.Cycles) != 0 {
		t.Fatalf("unexpected cycles: %v", m.Cycles)
	}
}

func TestExportMermaid(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	out := app.ExportMermaid()
	for _, want := range []string{
		"graph TD",
		"order[order]",
		"payment[payment]",
		"user[user]",
		"order --> user",
		"payment --> order",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mermaid output missing %q:\n%s", want, out)
		}
	}
}

func TestExportD2(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	out := app.ExportD2()
	for _, want := range []string{
		"order: { shape: rectangle }",
		"user: { shape: rectangle }",
		"order -> user",
		"payment -> order",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("d2 output missing %q:\n%s", want, out)
		}
	}
}

func TestModelPublicAndPrivatePackages(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	m, err := app.Model(false)
	if err != nil {
		t.Fatalf("Model: %v", err)
	}
	var user ModuleModel
	for _, mm := range m.Modules {
		if mm.Name == "user" {
			user = mm
		}
	}
	if len(user.PublicPackages) != 1 ||
		user.PublicPackages[0] != fixtureModule+"/internal/user/api" {
		t.Errorf("user public = %v", user.PublicPackages)
	}
	if len(user.PrivatePackages) != 1 ||
		user.PrivatePackages[0] != fixtureModule+"/internal/user/domain" {
		t.Errorf("user private = %v", user.PrivatePackages)
	}
}

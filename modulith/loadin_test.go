package modulith

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLoadInScansDirectory(t *testing.T) {
	fixtureDir := writeFixture(t, defaultFixture())

	// Move to an unrelated directory to prove LoadIn does not depend on cwd.
	other := t.TempDir()
	chdir(t, other)

	app, err := LoadIn(context.Background(), fixtureDir, "./...")
	if err != nil {
		t.Fatalf("LoadIn: %v", err)
	}
	if app.WorkingDir() != fixtureDir {
		t.Fatalf("WorkingDir = %q, want %q", app.WorkingDir(), fixtureDir)
	}
	if app.ModuleByName("user") == nil {
		t.Fatal("module user should be discovered")
	}
	if n := app.ModuleByName("user").PackageCount(); n != 2 {
		t.Fatalf("user packages = %d, want 2", n)
	}
	// RelDirs must be relative to the scanned directory.
	for _, p := range app.Packages() {
		if p.RelDir == "" {
			t.Errorf("package %s has empty RelDir", p.ID)
		}
	}
}

func TestLoadInKeepsCWD(t *testing.T) {
	writeFixture(t, defaultFixture())
	wd, _ := filepath.Abs(".")

	// Loading a directory without go.mod fails, but cwd must be preserved.
	empty := t.TempDir()
	if _, err := LoadIn(context.Background(), empty, "./..."); err == nil {
		t.Fatal("expected error loading directory without go.mod")
	}
	now, _ := filepath.Abs(".")
	if now != wd {
		t.Fatalf("cwd changed: was %q now %q", wd, now)
	}
}

func TestLoadExplicitIn(t *testing.T) {
	fixtureDir := writeFixture(t, defaultFixture())
	chdir(t, t.TempDir())

	app := New().
		Module("user", "./internal/user/...").
		Module("order", "./internal/order/...")
	if err := LoadExplicitIn(context.Background(), fixtureDir, app, "./..."); err != nil {
		t.Fatalf("LoadExplicitIn: %v", err)
	}
	if n := app.ModuleByName("user").PackageCount(); n != 2 {
		t.Fatalf("user packages = %d, want 2", n)
	}
}

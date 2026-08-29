package modulith

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigYAML(t *testing.T) {
	data := []byte(`
module_root: modules
api_element: api
report_orphans: true
public_api_required: true
patterns:
  - "./..."
modules:
  user:
    patterns: ["./internal/user/..."]
    allowed: [order]
    forbidden: [billing]
  order:
    patterns: ["./internal/order/..."]
    public: ["./internal/order/api"]
`)
	fc, err := ParseConfig(data, ConfigYAML)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if fc.ModuleRoot != "modules" || fc.APIElement != "api" {
		t.Errorf("ModuleRoot=%q APIElement=%q", fc.ModuleRoot, fc.APIElement)
	}
	if !fc.ReportOrphans || !fc.PublicAPIRequired {
		t.Errorf("flags: %+v", fc)
	}
	if len(fc.Patterns) != 1 || fc.Patterns[0] != "./..." {
		t.Errorf("patterns = %v", fc.Patterns)
	}
	user, ok := fc.Modules["user"]
	if !ok {
		t.Fatal("user module missing")
	}
	if len(user.Allowed) != 1 || user.Allowed[0] != "order" {
		t.Errorf("user allowed = %v", user.Allowed)
	}
	if len(user.Forbidden) != 1 || user.Forbidden[0] != "billing" {
		t.Errorf("user forbidden = %v", user.Forbidden)
	}
	order := fc.Modules["order"]
	if len(order.Public) != 1 || order.Public[0] != "./internal/order/api" {
		t.Errorf("order public = %v", order.Public)
	}
}

func TestParseConfigTOML(t *testing.T) {
	data := []byte(`
module_root = "internal"
report_orphans = true
patterns = ["./..."]

[modules.user]
patterns = ["./internal/user/..."]
allowed = ["order"]
`)
	fc, err := ParseConfig(data, ConfigTOML)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if fc.ModuleRoot != "internal" {
		t.Errorf("ModuleRoot = %q", fc.ModuleRoot)
	}
	if !fc.ReportOrphans {
		t.Error("ReportOrphans should be true")
	}
	user, ok := fc.Modules["user"]
	if !ok || len(user.Allowed) != 1 || user.Allowed[0] != "order" {
		t.Errorf("user module = %+v", user)
	}
}

func TestParseConfigEmptyNameRejected(t *testing.T) {
	data := []byte("modules:\n  \"\":\n    patterns: [\"./...\"]\n")
	if _, err := ParseConfig(data, ConfigYAML); err == nil {
		t.Fatal("expected error for empty module name")
	}
}

func TestFindConfigFile(t *testing.T) {
	dir := t.TempDir()
	if p := FindConfigFile(dir); p != "" {
		t.Fatalf("unexpected config in empty dir: %s", p)
	}
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// No config anywhere.
	if p := FindConfigFile(sub); p != "" {
		t.Fatalf("unexpected config: %s", p)
	}
	// Place a .gomodulith.yaml in a parent and verify upward discovery.
	yamlPath := filepath.Join(dir, ".gomodulith.yaml")
	if err := os.WriteFile(yamlPath, []byte("report_orphans: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := FindConfigFile(sub); p != yamlPath {
		t.Fatalf("FindConfigFile = %q, want %q", p, yamlPath)
	}
	// TOML takes precedence when placed in the same directory as discovery.
	tomlPath := filepath.Join(sub, ".gomodulith.toml")
	if err := os.WriteFile(tomlPath, []byte("report_orphans = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := FindConfigFile(sub); p != tomlPath {
		t.Fatalf("FindConfigFile = %q, want %q (nearest wins)", p, tomlPath)
	}
}

func TestFileConfigBuildConventionMode(t *testing.T) {
	files := map[string]string{
		"myroot/user/api/api.go": goFile("api", nil),
	}
	dir := writeFixture(t, files)
	chdir(t, t.TempDir())

	fc := &FileConfig{ModuleRoot: "myroot"}
	app, err := fc.Build(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if app.ModuleByName("user") == nil {
		t.Fatal("user module should be discovered under myroot")
	}
}

func TestFileConfigBuildExplicitMode(t *testing.T) {
	files := map[string]string{
		"internal/user/api/api.go":    goFile("api", nil),
		"internal/order/api/api.go":   goFile("api", []string{fixtureModule + "/internal/user/api"}),
		"internal/payment/api/api.go": goFile("api", []string{fixtureModule + "/internal/user/api"}),
	}
	dir := writeFixture(t, files)
	chdir(t, t.TempDir())

	fc := &FileConfig{Modules: map[string]ModuleConfig{
		"user":    {Patterns: []string{"./internal/user/..."}},
		"order":   {Patterns: []string{"./internal/order/..."}, Allowed: []string{"user"}},
		"payment": {Patterns: []string{"./internal/payment/..."}},
	}}
	app, err := fc.Build(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// payment has no allowed deps → payment->user is undeclared in explicit
	// mode; order->user is declared.
	if !res.HasCode(CodeUndeclaredDependency) {
		t.Fatalf("expected undeclared dependency, got:\n%s", Report(res))
	}
	for _, i := range res.Issues {
		if i.Code == CodeUndeclaredDependency && i.From != "payment" {
			t.Errorf("undeclared dep should be payment, got %s", i.From)
		}
	}
}

func TestLoadConfigFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gomodulith.yaml")
	content := "module_root: internal\nreport_orphans: true\nmodules:\n  user:\n    patterns: [\"./internal/user/...\"]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fc, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if fc.ModuleRoot != "internal" || !fc.ReportOrphans {
		t.Errorf("fc = %+v", fc)
	}
	if _, ok := fc.Modules["user"]; !ok {
		t.Error("user module missing")
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	if _, err := LoadConfigFile(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestConfigLoadPatterns(t *testing.T) {
	fc := &FileConfig{Patterns: []string{"./..."}}
	if got := fc.LoadPatterns(nil); len(got) != 1 || got[0] != "./..." {
		t.Errorf("LoadPatterns(nil) = %v", got)
	}
	if got := fc.LoadPatterns([]string{"./internal/user/..."}); len(got) != 1 || got[0] != "./internal/user/..." {
		t.Errorf("LoadPatterns(explicit) = %v", got)
	}
	empty := &FileConfig{}
	if got := empty.LoadPatterns(nil); len(got) != 1 || got[0] != "./..." {
		t.Errorf("LoadPatterns(empty) = %v", got)
	}
}

func TestParseConfigInvalidYAML(t *testing.T) {
	if _, err := ParseConfig([]byte(": : : not yaml"), ConfigYAML); err == nil {
		t.Fatal("expected parse error")
	}
}

var _ = strings.TrimSpace

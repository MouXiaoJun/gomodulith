package modulith

import (
	"context"
	"testing"
)

func TestVerifyDetectsViolationInDefaultFixture(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")

	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// The default fixture contains order.domain -> user.domain, which is a
	// cross-module private violation, so verification must fail.
	if res.OK {
		t.Fatalf("expected failure, got:\n%s", Report(res))
	}
	if !res.HasCode(CodeCrossModulePrivate) {
		t.Fatalf("expected cross-module-private issue, got:\n%s", Report(res))
	}
}

func TestCrossModulePrivateAccessReported(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	res, _ := app.Verify()

	var found bool
	for _, i := range res.Issues {
		if i.Code == CodeCrossModulePrivate &&
			i.From == fixtureModule+"/internal/order/domain" &&
			i.To == fixtureModule+"/internal/user/domain" {
			found = true
			if i.Severity != SeverityError {
				t.Errorf("severity = %v, want error", i.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("cross-module private issue not found:\n%s", Report(res))
	}
}

func TestUndeclaredDependencyDetected(t *testing.T) {
	// order depends on user but only declares user; payment is undeclared.
	files := map[string]string{
		"internal/user/api/api.go":    goFile("api", nil),
		"internal/order/api/api.go":   goFile("api", []string{fixtureModule + "/internal/user/api"}),
		"internal/payment/api/api.go": goFile("api", []string{fixtureModule + "/internal/order/api"}),
	}
	writeFixture(t, files)

	app := New().
		Module("user", "./internal/user/...").
		Module("order", "./internal/order/...").
		Module("payment", "./internal/payment/...")
	app.ModuleRules("order").AllowDependencies("user")
	if err := LoadExplicit(context.Background(), app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}

	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK {
		t.Fatal("expected failure")
	}
	if !res.HasCode(CodeUndeclaredDependency) {
		t.Fatalf("expected undeclared-dependency, got:\n%s", Report(res))
	}
	if res.HasCode(CodeCrossModulePrivate) {
		t.Fatalf("public API imports must not be private-access violations:\n%s", Report(res))
	}
}

func TestForbiddenDependencyDetected(t *testing.T) {
	files := map[string]string{
		"internal/user/api/api.go":  goFile("api", nil),
		"internal/order/api/api.go": goFile("api", []string{fixtureModule + "/internal/user/api"}),
	}
	writeFixture(t, files)

	app := New().
		Module("user", "./internal/user/...").
		Module("order", "./internal/order/...")
	app.ModuleRules("order").ForbidDependencies("user")
	if err := LoadExplicit(context.Background(), app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}

	res, _ := app.Verify()
	if !res.HasCode(CodeForbiddenDependency) {
		t.Fatalf("expected forbidden-dependency, got:\n%s", Report(res))
	}
}

func TestInvalidPublicAPIDeclaration(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := New().
		Module("user", "./internal/user/...").
		Module("order", "./internal/order/...")
	app.ModuleRules("user").Public(fixtureModule + "/internal/user/does-not-exist")
	if err := LoadExplicit(context.Background(), app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}
	res, _ := app.Verify()
	if !res.HasCode(CodeInvalidPublicAPI) {
		t.Fatalf("expected invalid-public-api, got:\n%s", Report(res))
	}
}

func TestOrphanWarningWhenEnabled(t *testing.T) {
	// A main package outside the module tree plus a healthy module tree.
	files := defaultFixture()
	files["main.go"] = goFile("main", nil)
	writeFixture(t, files)

	cfg := NewConfig()
	cfg.ReportOrphans = true
	app := NewWithConfig(cfg)
	if err := app.load(context.Background(), []string{"./..."}); err != nil {
		t.Fatalf("load: %v", err)
	}
	res, _ := app.Verify()
	if !res.HasCode(CodeOrphanPackage) {
		t.Fatalf("expected orphan-package, got:\n%s", Report(res))
	}
	for _, i := range res.Issues {
		if i.Code == CodeOrphanPackage && i.Severity != SeverityWarning {
			t.Errorf("orphan severity = %v, want warning", i.Severity)
		}
	}
}

func TestMissingPublicAPIWarning(t *testing.T) {
	// Module with only a private package, no api.
	files := map[string]string{
		"internal/user/impl/impl.go": goFile("impl", nil),
	}
	writeFixture(t, files)

	app := mustLoad(t, "./...")
	res, _ := app.Verify()
	if !res.HasCode(CodeMissingPublicAPI) {
		t.Fatalf("expected missing-public-api, got:\n%s", Report(res))
	}
	if !res.OK {
		t.Fatal("missing-public-api is only a warning; verification should still pass")
	}
}

func TestMissingPublicAPIRequired(t *testing.T) {
	files := map[string]string{
		"internal/user/impl/impl.go": goFile("impl", nil),
	}
	writeFixture(t, files)

	cfg := NewConfig()
	cfg.PublicAPIRequired = true
	app := NewWithConfig(cfg)
	if err := app.load(context.Background(), []string{"./..."}); err != nil {
		t.Fatalf("load: %v", err)
	}
	res, _ := app.Verify()
	if res.OK {
		t.Fatal("expected failure when PublicAPIRequired is set")
	}
	if !res.HasCode(CodeMissingPublicAPI) {
		t.Fatalf("expected missing-public-api, got:\n%s", Report(res))
	}
}

func mustLoad(t *testing.T, patterns ...string) *Application {
	t.Helper()
	app, err := Load(context.Background(), patterns...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return app
}

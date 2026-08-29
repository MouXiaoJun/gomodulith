package modulith

import (
	"context"
	"testing"
)

func TestLoadDiscoversModulesFromConvention(t *testing.T) {
	writeFixture(t, defaultFixture())

	app, err := Load(context.Background(), "./...")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	names := moduleNames(app)
	want := []string{"order", "payment", "user"}
	if len(names) != len(want) {
		t.Fatalf("modules = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("modules = %v, want %v", names, want)
		}
	}

	user := app.ModuleByName("user")
	if user == nil {
		t.Fatal("module user not found")
	}
	if user.PackageCount() != 2 {
		t.Errorf("user package count = %d, want 2", user.PackageCount())
	}
	// api packages must be public.
	pub := user.PublicPackageSet()
	if !pub[fixtureModule+"/internal/user/api"] {
		t.Errorf("user/api should be public, got public=%v", user.PublicPackages)
	}
	if pub[fixtureModule+"/internal/user/domain"] {
		t.Errorf("user/domain should be private, got public=%v", user.PublicPackages)
	}
}

func TestLoadNoModuleRoot(t *testing.T) {
	writeFixture(t, map[string]string{
		"main.go": goFile("main", nil),
	})
	app, err := Load(context.Background(), "./...")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(app.Modules()) != 0 {
		t.Fatalf("modules = %v, want none", moduleNames(app))
	}
	if len(app.OrphanPackages()) != 1 {
		t.Fatalf("orphans = %v, want 1", app.OrphanPackages())
	}
}

func TestLoadRootPackagePublicWhenNoAPI(t *testing.T) {
	writeFixture(t, map[string]string{
		"internal/user/user.go":      goFile("user", nil),
		"internal/user/impl/impl.go": goFile("impl", nil),
	})
	app, err := Load(context.Background(), "./...")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	user := app.ModuleByName("user")
	if user == nil {
		t.Fatal("module user not found")
	}
	// The module root package itself is the public API when there is no api
	// sub-package.
	if !user.IsPublicPackage(fixtureModule + "/internal/user") {
		t.Errorf("internal/user should be public, got %v", user.PublicPackages)
	}
	if user.IsPublicPackage(fixtureModule + "/internal/user/impl") {
		t.Errorf("internal/user/impl should be private, got %v", user.PublicPackages)
	}
}

func TestExplicitModuleDefinition(t *testing.T) {
	writeFixture(t, defaultFixture())

	app := New().
		Module("user", "./internal/user/...").
		Module("order", "./internal/order/...").
		Module("payment", "./internal/payment/...")
	if err := LoadExplicit(context.Background(), app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}

	if len(app.Modules()) != 3 {
		t.Fatalf("modules = %v, want 3", moduleNames(app))
	}
	if app.ModuleByName("user").PackageCount() != 2 {
		t.Errorf("user packages = %d, want 2", app.ModuleByName("user").PackageCount())
	}
}

func TestLoadExplicitRejectsDoubleLoad(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := New().Module("user", "./internal/user/...")
	if err := LoadExplicit(context.Background(), app, "./..."); err != nil {
		t.Fatalf("first LoadExplicit: %v", err)
	}
	if err := LoadExplicit(context.Background(), app, "./..."); err == nil {
		t.Fatal("second LoadExplicit should fail")
	}
}

func TestModuleRulesChaining(t *testing.T) {
	app := New().
		Module("user").
		Module("order")
	app.ModuleRules("order").AllowDependencies("user").ForbidDependencies("payment")
	order := app.ModuleRules("order")
	if len(order.AllowedDependencies) != 1 || order.AllowedDependencies[0] != "user" {
		t.Errorf("allowed = %v, want [user]", order.AllowedDependencies)
	}
	if len(order.ForbiddenDependencies) != 1 || order.ForbiddenDependencies[0] != "payment" {
		t.Errorf("forbidden = %v, want [payment]", order.ForbiddenDependencies)
	}
	// Duplicate declarations are de-duplicated.
	order.AllowDependencies("user")
	if len(order.AllowedDependencies) != 1 {
		t.Errorf("allowed after dup = %v, want [user]", order.AllowedDependencies)
	}
}

func TestVerifyBeforeLoadFails(t *testing.T) {
	app := New()
	if _, err := app.Verify(); err == nil {
		t.Fatal("Verify on unloaded application should fail")
	}
}

func moduleNames(app *Application) []string {
	var out []string
	for _, m := range app.Modules() {
		out = append(out, m.Name)
	}
	return out
}

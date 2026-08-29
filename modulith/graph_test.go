package modulith

import (
	"context"
	"testing"
)

func TestGraphEdges(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	g := app.BuildGraph()

	want := []string{
		"order -> user",
		"payment -> order",
	}
	got := g.Edges()
	if len(got) != len(want) {
		t.Fatalf("edges = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("edges = %v, want %v", got, want)
		}
	}

	// order -> user must carry the concrete package imports (both the api and
	// the domain import edges, sorted).
	orderDeps := g.DependenciesOf("order")
	if len(orderDeps) != 1 || orderDeps[0].To.Name != "user" {
		t.Fatalf("order deps = %+v", orderDeps)
	}
	d := orderDeps[0]
	wantImports := []string{
		fixtureModule + "/internal/order/api -> " + fixtureModule + "/internal/user/api",
		fixtureModule + "/internal/order/domain -> " + fixtureModule + "/internal/user/domain",
	}
	if len(d.Imports) != 2 || d.Imports[0] != wantImports[0] || d.Imports[1] != wantImports[1] {
		t.Fatalf("order->user imports = %v, want %v", d.Imports, wantImports)
	}

	// dependents
	dependents := g.DependentsOf("order")
	if len(dependents) != 1 || dependents[0].From.Name != "payment" {
		t.Fatalf("dependents of order = %+v", dependents)
	}
}

func TestGraphIgnoresExternalAndOrphanEdges(t *testing.T) {
	files := map[string]string{
		// A module importing only stdlib, and a main package importing a
		// module.
		"internal/user/api/api.go": goFile("api", []string{"fmt"}),
		"main.go":                  goFile("main", []string{fixtureModule + "/internal/user/api"}),
	}
	writeFixture(t, files)
	app := mustLoad(t, "./...")
	g := app.BuildGraph()
	if len(g.Dependencies) != 0 {
		t.Fatalf("dependencies = %v, want none (stdlib import and orphan source are ignored)", g.Edges())
	}
}

func TestGraphAllowedByConvention(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	g := app.BuildGraph()
	// Convention mode (no explicit rules): all module dependencies allowed.
	for _, d := range g.Dependencies {
		if !d.Allowed {
			t.Errorf("dependency %s->%s should be allowed by convention", d.From.Name, d.To.Name)
		}
	}
}

func TestGraphAllowedByRules(t *testing.T) {
	// order and payment depend on user; both declare user as allowed.
	// payment also depends on order but does NOT declare order, so that edge
	// must be disallowed while payment -> user stays allowed.
	files := map[string]string{
		"internal/user/api/api.go":  goFile("api", nil),
		"internal/order/api/api.go": goFile("api", []string{fixtureModule + "/internal/user/api"}),
		"internal/payment/api/api.go": goFile("api", []string{
			fixtureModule + "/internal/user/api",
			fixtureModule + "/internal/order/api",
		}),
	}
	writeFixture(t, files)
	app := New().
		Module("user", "./internal/user/...").
		Module("order", "./internal/order/...").
		Module("payment", "./internal/payment/...")
	app.ModuleRules("order").AllowDependencies("user")
	app.ModuleRules("payment").AllowDependencies("user")
	if err := LoadExplicit(context.Background(), app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}
	g := app.BuildGraph()

	paymentOrder := findDep(g, "payment", "order")
	if paymentOrder == nil || paymentOrder.Allowed {
		t.Fatalf("payment->order should be disallowed (undeclared)")
	}
	paymentUser := findDep(g, "payment", "user")
	if paymentUser == nil || !paymentUser.Allowed {
		t.Fatalf("payment->user should be allowed")
	}
	orderUser := findDep(g, "order", "user")
	if orderUser == nil || !orderUser.Allowed {
		t.Fatalf("order->user should be allowed")
	}
}

func findDep(g *Graph, from, to string) *Dependency {
	for _, d := range g.Dependencies {
		if d.From.Name == from && d.To.Name == to {
			return d
		}
	}
	return nil
}

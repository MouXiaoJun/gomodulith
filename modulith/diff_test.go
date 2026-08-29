package modulith

import (
	"context"
	"testing"
)

func TestDiffModuleAndDependencyChanges(t *testing.T) {
	writeFixture(t, defaultFixture())

	base := mustLoad(t, "./...")
	baseModel, err := base.Model(false)
	if err != nil {
		t.Fatalf("base Model: %v", err)
	}

	// Add a new module (shipping) and a dependency from it to user.
	files := defaultFixture()
	files["internal/shipping/api/api.go"] = goFile("api", []string{fixtureModule + "/internal/user/api"})
	writeFixture(t, files)
	head := mustLoad(t, "./...")
	headModel, err := head.Model(false)
	if err != nil {
		t.Fatalf("head Model: %v", err)
	}

	changes := Diff(baseModel, headModel)
	var addedModule, addedDep bool
	for _, c := range changes {
		if c.Kind == DiffModuleAdded && c.Module == "shipping" {
			addedModule = true
		}
		if c.Kind == DiffDependencyAdded && c.Module == "shipping" && c.Detail == "shipping -> user" {
			addedDep = true
		}
	}
	if !addedModule {
		t.Errorf("expected module-added for shipping, changes = %v", changes)
	}
	if !addedDep {
		t.Errorf("expected dependency-added shipping->user, changes = %v", changes)
	}
}

func TestDiffReverseDirection(t *testing.T) {
	writeFixture(t, defaultFixture())
	full := mustLoad(t, "./...")
	fullModel, err := full.Model(false)
	if err != nil {
		t.Fatalf("full Model: %v", err)
	}

	// Remove a module by using a smaller fixture.
	writeFixture(t, map[string]string{
		"internal/user/api/api.go":  goFile("api", nil),
		"internal/order/api/api.go": goFile("api", []string{fixtureModule + "/internal/user/api"}),
	})
	small := mustLoad(t, "./...")
	smallModel, err := small.Model(false)
	if err != nil {
		t.Fatalf("small Model: %v", err)
	}

	changes := Diff(fullModel, smallModel)
	var removedModule bool
	for _, c := range changes {
		if c.Kind == DiffModuleRemoved && c.Module == "payment" {
			removedModule = true
		}
	}
	if !removedModule {
		t.Errorf("expected module-removed for payment, changes = %v", changes)
	}
}

func TestDiffIdenticalModels(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	m1, err := app.Model(false)
	if err != nil {
		t.Fatalf("Model: %v", err)
	}
	m2, err := app.Model(false)
	if err != nil {
		t.Fatalf("Model: %v", err)
	}
	if changes := Diff(m1, m2); len(changes) != 0 {
		t.Fatalf("identical models should have no changes, got %v", changes)
	}
}

func TestDiffCycleChanges(t *testing.T) {
	// Base: user -> order (acyclic). Head: user <-> order module cycle, using
	// distinct package pairs so no package import cycle is formed.
	baseFiles := map[string]string{
		"internal/user/api/api.go":  goFile("api", []string{fixtureModule + "/internal/order/api"}),
		"internal/order/api/api.go": goFile("api", nil),
	}
	writeFixture(t, baseFiles)
	base := mustLoad(t, "./...")
	baseModel, _ := base.Model(false)

	headFiles := map[string]string{
		"internal/user/api/api.go":     goFile("api", []string{fixtureModule + "/internal/order/domain"}),
		"internal/order/api/api.go":    goFile("api", []string{fixtureModule + "/internal/user/domain"}),
		"internal/order/domain/dom.go": goFile("domain", nil),
		"internal/user/domain/dom.go":  goFile("domain", nil),
	}
	writeFixture(t, headFiles)
	head := mustLoad(t, "./...")
	headModel, _ := head.Model(false)

	changes := Diff(baseModel, headModel)
	var cycleAdded bool
	for _, c := range changes {
		if c.Kind == DiffCycleAdded {
			cycleAdded = true
		}
	}
	if !cycleAdded {
		t.Errorf("expected cycle-added, changes = %v", changes)
	}
}

func TestDiffNilModels(t *testing.T) {
	// Diffing nil against a model treats nil as empty.
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	m, _ := app.Model(false)
	changes := Diff(nil, m)
	if len(changes) == 0 {
		t.Fatal("expected changes when diffing nil base")
	}
}

var _ = context.Background

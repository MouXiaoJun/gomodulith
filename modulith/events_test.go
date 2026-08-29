package modulith

import (
	"context"
	"strings"
	"testing"
)

// ctx is a shared background context for loading fixtures.
var ctx = context.Background()

// eventFile returns a Go file declaring exported event types in the given
// package.
func eventFile(pkg string, events []string, imports []string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	for _, imp := range imports {
		b.WriteString("import _ \"" + imp + "\"\n")
	}
	if len(imports) > 0 {
		b.WriteString("\n")
	}
	for _, e := range events {
		b.WriteString("type " + e + " struct{}\n")
	}
	return b.String()
}

func eventsFixture() map[string]string {
	return map[string]string{
		// user publishes UserRegistered.
		"internal/user/api/api.go":       goFile("api", nil),
		"internal/user/events/events.go": eventFile("events", []string{"UserRegistered"}, nil),
		"internal/notifications/api/api.go": goFile("api", []string{
			fixtureModule + "/internal/user/events",
		}),
		"internal/notifications/domain/dom.go": goFile("domain", []string{
			fixtureModule + "/internal/user/api",
		}),
	}
}

func TestPublishEventsHealthy(t *testing.T) {
	writeFixture(t, eventsFixture())
	app := New().
		Module("user", "./internal/user/...").
		Module("notifications", "./internal/notifications/...")
	app.ModuleRules("user").PublishEvents("UserRegistered")
	if err := LoadExplicit(ctx, app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.HasCode(CodeMissingPublishedEvent) {
		t.Fatalf("unexpected missing-published-event:\n%s", Report(res))
	}
}

func TestPublishEventsMissing(t *testing.T) {
	writeFixture(t, eventsFixture())
	app := New().
		Module("user", "./internal/user/...").
		Module("notifications", "./internal/notifications/...")
	app.ModuleRules("user").PublishEvents("UserRegistered", "GhostEvent")
	if err := LoadExplicit(ctx, app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.HasCode(CodeMissingPublishedEvent) {
		t.Fatalf("expected missing-published-event:\n%s", Report(res))
	}
	for _, i := range res.Issues {
		if i.Code == CodeMissingPublishedEvent && i.Module != "user" {
			t.Errorf("issue module = %q, want user", i.Module)
		}
	}
}

func TestEventDrivenFromHealthy(t *testing.T) {
	files := eventsFixture()
	// notifications imports user/events (event package): allowed.
	delete(files, "internal/notifications/domain/dom.go")
	writeFixture(t, files)
	app := New().
		Module("user", "./internal/user/...").
		Module("notifications", "./internal/notifications/...")
	app.ModuleRules("notifications").AllowDependencies("user").EventDrivenFrom("user")
	if err := LoadExplicit(ctx, app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.HasCode(CodeEventDrivenViolation) || res.HasCode(CodeEventPackageMissing) {
		t.Fatalf("unexpected event issues:\n%s", Report(res))
	}
}

func TestEventDrivenFromViolation(t *testing.T) {
	writeFixture(t, eventsFixture())
	app := New().
		Module("user", "./internal/user/...").
		Module("notifications", "./internal/notifications/...")
	app.ModuleRules("notifications").AllowDependencies("user").EventDrivenFrom("user")
	if err := LoadExplicit(ctx, app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.HasCode(CodeEventDrivenViolation) {
		t.Fatalf("expected event-driven-violation:\n%s", Report(res))
	}
	// The non-event import is notifications.domain -> user.api.
	found := false
	for _, i := range res.Issues {
		if i.Code == CodeEventDrivenViolation && i.From == fixtureModule+"/internal/notifications/domain" && i.To == fixtureModule+"/internal/user/api" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected violation on notifications.domain -> user.api:\n%s", Report(res))
	}
}

func TestEventPackageMissing(t *testing.T) {
	// notifications is event-driven towards billing, which has no events package.
	files := map[string]string{
		"internal/notifications/api/api.go": goFile("api", []string{fixtureModule + "/internal/billing/api"}),
		"internal/billing/api/api.go":       goFile("api", nil),
	}
	writeFixture(t, files)
	app := New().
		Module("notifications", "./internal/notifications/...").
		Module("billing", "./internal/billing/...")
	app.ModuleRules("notifications").AllowDependencies("billing").EventDrivenFrom("billing")
	if err := LoadExplicit(ctx, app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.HasCode(CodeEventPackageMissing) {
		t.Fatalf("expected event-package-missing warning:\n%s", Report(res))
	}
}

func TestEventPackagesArePublicSurface(t *testing.T) {
	// Cross-module import of an event package must not be flagged as
	// cross-module-private-access in convention mode.
	writeFixture(t, eventsFixture())
	app := mustLoad(t, "./...")
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.HasCode(CodeCrossModulePrivate) {
		t.Fatalf("event package should be public surface:\n%s", Report(res))
	}
}

func TestEventElementConfigOverride(t *testing.T) {
	files := map[string]string{
		"internal/user/api/api.go":          goFile("api", nil),
		"internal/user/evt/evt.go":          eventFile("evt", []string{"UserRegistered"}, nil),
		"internal/notifications/api/api.go": goFile("api", []string{fixtureModule + "/internal/user/evt"}),
	}
	writeFixture(t, files)
	app := NewWithConfig(Config{EventElement: "evt"}).
		Module("user", "./internal/user/...").
		Module("notifications", "./internal/notifications/...")
	app.ModuleRules("notifications").AllowDependencies("user").EventDrivenFrom("user")
	if err := LoadExplicit(ctx, app, "./..."); err != nil {
		t.Fatalf("LoadExplicit: %v", err)
	}
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.HasCode(CodeEventDrivenViolation) || res.HasCode(CodeCrossModulePrivate) {
		t.Fatalf("unexpected issues with custom event element:\n%s", Report(res))
	}
}

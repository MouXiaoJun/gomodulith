package modulith

import (
	"strings"
	"testing"
)

// leakFile returns a Go source file that declares an exported symbol referencing
// a type from the given import. body may contain declarations such as
// "func Create(o domain.Order) {}" or "type DTO struct{ ID domain.UserID }".
func leakFile(pkg string, imports []string, body string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	for _, imp := range imports {
		b.WriteString("import \"" + imp + "\"\n")
	}
	b.WriteString("\n")
	b.WriteString(body)
	b.WriteString("\n")
	return b.String()
}

func TestInternalAPITypeLeakage(t *testing.T) {
	files := map[string]string{
		"internal/user/api/api.go":       goFile("api", nil),
		"internal/user/domain/dom.go":    goFile("domain", nil),
		"internal/order/api/api.go":      goFile("api", nil),
		"internal/order/domain/dom.go":   "package domain\n\ntype Order struct{}\n",
		"internal/payment/api/api.go":    goFile("api", []string{fixtureModule + "/internal/order/api"}),
		"internal/payment/domain/dom.go": goFile("domain", nil),
	}
	// order.api exposes order/domain.Order in its public API.
	files["internal/order/api/api.go"] = leakFile("api", []string{fixtureModule + "/internal/order/domain"}, "type OrderDTO struct {\n\tOrder domain.Order\n}\n")
	writeFixture(t, files)
	app := mustLoad(t, "./...")

	res, err := app.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.HasCode(CodeInternalAPITypeLeakage) {
		t.Fatalf("expected internal-api-leakage:\n%s", Report(res))
	}
	if res.HasCode(CodeCrossModuleTypeLeakage) {
		t.Errorf("unexpected cross-module-type-leakage:\n%s", Report(res))
	}
	// The leak message should identify the internal type.
	for _, i := range res.Issues {
		if i.Code == CodeInternalAPITypeLeakage {
			if !strings.Contains(i.Message, fixtureModule+"/internal/order/domain.Order") {
				t.Errorf("message should name the leaked type:\n%s", i.Message)
			}
			if i.From != fixtureModule+"/internal/order/api" {
				t.Errorf("From = %q", i.From)
			}
		}
	}
}

func TestCrossModuleTypeLeakage(t *testing.T) {
	// user/domain exposes no public surface type to order; order's event package
	// references user's private type in an event payload.
	files := map[string]string{
		"internal/user/api/api.go":        goFile("api", nil),
		"internal/user/domain/dom.go":     "package domain\n\ntype UserID string\n",
		"internal/order/api/api.go":       goFile("api", nil),
		"internal/order/domain/dom.go":    goFile("domain", nil),
		"internal/order/events/events.go": leakFile("events", []string{fixtureModule + "/internal/user/domain"}, "type OrderPlaced struct {\n\tBy domain.UserID\n}\n"),
		"internal/payment/api/api.go":     goFile("api", []string{fixtureModule + "/internal/order/api"}),
		"internal/payment/domain/dom.go":  goFile("domain", nil),
	}
	writeFixture(t, files)
	app := mustLoad(t, "./...")

	res, err := app.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.HasCode(CodeCrossModuleTypeLeakage) {
		t.Fatalf("expected cross-module-type-leakage:\n%s", Report(res))
	}
	// The same exposure through an event package must not be flagged as an
	// internal leak of order itself.
	if res.HasCode(CodeInternalAPITypeLeakage) {
		t.Errorf("unexpected internal-api-leakage:\n%s", Report(res))
	}
}

func TestNoTypeLeakageHealthy(t *testing.T) {
	writeFixture(t, defaultFixture())
	app := mustLoad(t, "./...")
	res, err := app.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.HasCode(CodeInternalAPITypeLeakage) || res.HasCode(CodeCrossModuleTypeLeakage) {
		t.Fatalf("unexpected leakage:\n%s", Report(res))
	}
}

func TestEventSurfaceInternalLeak(t *testing.T) {
	// An event package of order references order's own private type: external
	// subscribers cannot construct the event without naming a private type.
	files := map[string]string{
		"internal/user/api/api.go":        goFile("api", nil),
		"internal/user/domain/dom.go":     goFile("domain", nil),
		"internal/order/api/api.go":       goFile("api", nil),
		"internal/order/domain/dom.go":    "package domain\n\ntype OrderID string\n",
		"internal/order/events/events.go": leakFile("events", []string{fixtureModule + "/internal/order/domain"}, "type OrderPlaced struct {\n\tID domain.OrderID\n}\n"),
		"internal/payment/api/api.go":     goFile("api", []string{fixtureModule + "/internal/order/events"}),
		"internal/payment/domain/dom.go":  goFile("domain", nil),
	}
	writeFixture(t, files)
	app := mustLoad(t, "./...")

	res, err := app.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.HasCode(CodeInternalAPITypeLeakage) {
		t.Fatalf("expected internal-api-leakage on event surface:\n%s", Report(res))
	}
}

func TestPublicSurfaceReferenceAllowed(t *testing.T) {
	// Referencing another module's public API type is a normal, allowed use.
	files := map[string]string{
		"internal/user/api/api.go":     leakFile("api", nil, "type User struct{}\n"),
		"internal/user/domain/dom.go":  goFile("domain", nil),
		"internal/order/api/api.go":    leakFile("api", []string{fixtureModule + "/internal/user/api"}, "func Find(u api.User) {}\n"),
		"internal/order/domain/dom.go": goFile("domain", nil),
	}
	writeFixture(t, files)
	app := mustLoad(t, "./...")

	res, err := app.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.HasCode(CodeInternalAPITypeLeakage) || res.HasCode(CodeCrossModuleTypeLeakage) {
		t.Fatalf("unexpected leakage:\n%s", Report(res))
	}
}

func TestUnicodeAPITypeLeakage(t *testing.T) {
	for _, body := range []string{
		"func Éxport() domain.Order { return domain.Order{} }",
		"type Évent struct { Order domain.Order }",
		"type Service struct{}; func (Service) Éxport() domain.Order { return domain.Order{} }",
	} {
		t.Run(body, func(t *testing.T) {
			writeFixture(t, map[string]string{
				"internal/order/domain/dom.go": "package domain\ntype Order struct{}\n",
				"internal/order/api/api.go":    leakFile("api", []string{fixtureModule + "/internal/order/domain"}, body),
			})
			result, err := mustLoad(t, "./...").Verify()
			if err != nil {
				t.Fatal(err)
			}
			if !result.HasCode(CodeInternalAPITypeLeakage) {
				t.Fatalf("Unicode exported symbol hid private type: %s", Report(result))
			}
		})
	}
}

func TestGenericAPITypeArguments(t *testing.T) {
	writeFixture(t, map[string]string{
		"internal/order/domain/dom.go": "package domain\ntype Order struct{}\ntype ID string\n",
		"internal/order/api/api.go": leakFile("api", []string{fixtureModule + "/internal/order/domain"}, `
type Box[T any] struct { Value T }
func First() Box[domain.Order] { return Box[domain.Order]{} }
func Second() Box[[]domain.ID] { return Box[[]domain.ID]{} }
`),
	})
	result, err := mustLoad(t, "./...").Verify()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Order", "ID"} {
		found := false
		for _, issue := range result.Issues {
			if issue.Code == CodeInternalAPITypeLeakage && strings.Contains(issue.Message, "domain."+name) {
				found = true
			}
		}
		if !found {
			t.Errorf("generic argument domain.%s not reported: %s", name, Report(result))
		}
	}
}

func TestUnicodePublishedEvent(t *testing.T) {
	writeFixture(t, map[string]string{
		"internal/order/events/event.go": "package events\ntype Évent struct{}\n",
	})
	app := mustLoad(t, "./...")
	app.ModuleRules("order").PublishEvents("Évent")
	result, err := app.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if result.HasCode(CodeMissingPublishedEvent) {
		t.Fatalf("Unicode event treated as missing: %s", Report(result))
	}
}

func TestGenericAPIRecursiveConstraint(t *testing.T) {
	writeFixture(t, map[string]string{
		"internal/order/api/api.go": `package api
type Node[T any] interface { Next() T }
type Box[T Node[T]] struct { Value T }
func Read[T Node[T]](box Box[T]) T { return box.Value }
`,
	})
	result, err := mustLoad(t, "./...").Verify()
	if err != nil {
		t.Fatal(err)
	}
	if result.HasCode(CodeInternalAPITypeLeakage) {
		t.Fatalf("public recursive generic must remain valid: %s", Report(result))
	}
}

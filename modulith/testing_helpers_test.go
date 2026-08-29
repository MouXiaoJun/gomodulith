package modulith

import (
	"fmt"
	"strings"
	"testing"
)

func TestScanAndVerifyTestPass(t *testing.T) {
	files := defaultFixture()
	// Make the fixture healthy by fixing the private access.
	files["internal/order/domain/dom.go"] = goFile("domain", []string{fixtureModule + "/internal/user/api"})
	writeFixture(t, files)

	app := Scan(t, "./...")
	app.VerifyTest(t) // must not fail
}

func TestScanFailsOnLoadError(t *testing.T) {
	writeFixture(t, map[string]string{
		// A package with invalid Go source causes loading to fail.
		"internal/user/api/api.go": "package api\n\nthis is not valid go\n",
	})
	sub := newFakeTB()
	app := Scan(sub, "./...")
	if !sub.failed {
		t.Fatal("Scan should have failed the test on load error")
	}
	if app != nil {
		t.Fatal("Scan should return nil after failing")
	}
}

func TestNewTestExplicit(t *testing.T) {
	writeFixture(t, defaultFixture())

	app := NewTest(t, "./...").
		Module("user", "./internal/user/...").
		Module("order", "./internal/order/...").
		Module("payment", "./internal/payment/...")
	if len(app.Modules()) != 3 {
		t.Fatalf("modules = %v, want 3", moduleNames(app))
	}
}

func TestVerifyTestReportsReport(t *testing.T) {
	writeFixture(t, defaultFixture()) // has a cross-module violation
	app := Scan(t, "./...")

	sub := newFakeTB()
	app.VerifyTest(sub)
	if !sub.failed {
		t.Fatal("VerifyTest should have failed the test")
	}
	if !strings.Contains(sub.logs.String(), "cross-module-private-access") {
		t.Errorf("report should contain the violation, got:\n%s", sub.logs.String())
	}
}

func TestScanWithConfig(t *testing.T) {
	files := map[string]string{
		"myroot/user/api/api.go": goFile("api", nil),
	}
	writeFixture(t, files)
	cfg := NewConfig()
	cfg.ModuleRoot = "myroot"
	app := ScanWithConfig(t, cfg, "./...")
	if app.ModuleByName("user") == nil {
		t.Fatal("module user should be discovered under custom module root")
	}
}

// fakeTB is a minimal testing.TB that records failures instead of aborting the
// test process. The embedded testing.TB provides the full interface surface;
// only the methods used by the test-facing helpers (Scan, VerifyTest, NewTest)
// are overridden. Any other method called on the embedded nil interface would
// panic, which is acceptable because the helpers only call Helper, Fatal,
// Fatalf and Log.
type fakeTB struct {
	testing.TB
	failed bool
	logs   strings.Builder
}

func newFakeTB() *fakeTB { return &fakeTB{} }

func (f *fakeTB) Helper()                           {}
func (f *fakeTB) Error(args ...any)                 { f.failed = true }
func (f *fakeTB) Errorf(format string, args ...any) { f.failed = true }
func (f *fakeTB) Fail()                             { f.failed = true }
func (f *fakeTB) FailNow()                          { f.failed = true }
func (f *fakeTB) Failed() bool                      { return f.failed }
func (f *fakeTB) Fatal(args ...any)                 { f.failed = true }
func (f *fakeTB) Fatalf(string, ...any)             { f.failed = true }
func (f *fakeTB) Log(args ...any) {
	f.logs.WriteString(fmt.Sprint(args...))
	f.logs.WriteString("\n")
}
func (f *fakeTB) Logf(format string, args ...any) {
	f.logs.WriteString(fmt.Sprintf(format, args...))
	f.logs.WriteString("\n")
}

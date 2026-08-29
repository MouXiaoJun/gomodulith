package modulith

import (
	"context"
	"testing"
)

// Scan loads the application using convention-based discovery and fails the
// test when loading reports an error. It is the idiomatic entry point for
// architecture tests:
//
//	func TestArchitecture(t *testing.T) {
//		app := modulith.Scan(t, "./...")
//		app.VerifyTest(t)
//	}
func Scan(t testing.TB, patterns ...string) *Application {
	t.Helper()
	app, err := Load(context.Background(), patterns...)
	if err != nil {
		t.Fatalf("modulith: %v", err)
	}
	return app
}

// ScanWithConfig is like Scan but uses the supplied configuration.
func ScanWithConfig(t testing.TB, cfg Config, patterns ...string) *Application {
	t.Helper()
	app := NewWithConfig(cfg)
	if err := app.load(context.Background(), patterns); err != nil {
		t.Fatalf("modulith: %v", err)
	}
	return app
}

// NewTest returns an explicit-mode Application and loads it with the given
// patterns, failing the test on error:
//
//	app := modulith.NewTest(t, "./...").
//		Module("user", "./internal/user/...").
//		Module("order", "./internal/order/...")
//	app.VerifyTest(t)
func NewTest(t testing.TB, patterns ...string) *Application {
	t.Helper()
	app := New()
	if err := LoadExplicit(context.Background(), app, patterns...); err != nil {
		t.Fatalf("modulith: %v", err)
	}
	return app
}

// VerifyTest runs verification and fails the test when any error-severity
// issue is found, printing the full report to the test log.
func (a *Application) VerifyTest(t testing.TB) {
	t.Helper()
	if !a.loaded {
		t.Fatal("modulith: application not loaded")
	}
	res, err := a.Verify()
	if err != nil {
		t.Fatalf("modulith: verify: %v", err)
	}
	t.Log("\n" + Report(res))
	if !res.OK {
		t.Fatalf("modulith: architecture violations found (%d error(s), %d warning(s))",
			len(res.Errors()), len(res.Warnings()))
	}
}

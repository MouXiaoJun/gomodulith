package modulith

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cacheFileModTime(t *testing.T, cacheDir string) time.Time {
	t.Helper()
	st, err := os.Stat(filepath.Join(cacheDir, "model.json"))
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	return st.ModTime()
}

func TestLoadCachedInHit(t *testing.T) {
	fixtureDir := writeFixture(t, defaultFixture())
	cacheDir := filepath.Join(fixtureDir, DefaultCacheDir)
	ctx := context.Background()

	app1, err := LoadCachedIn(ctx, fixtureDir, []string{"./..."}, cacheDir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	res1, _ := app1.Verify()
	m1 := cacheFileModTime(t, cacheDir)

	// Second load must hit the cache: same model, cache file not rewritten.
	app2, err := LoadCachedIn(ctx, fixtureDir, []string{"./..."}, cacheDir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	res2, _ := app2.Verify()
	if m2 := cacheFileModTime(t, cacheDir); !m2.Equal(m1) {
		t.Errorf("cache rewritten on hit: %v -> %v", m1, m2)
	}
	if len(res1.Issues) != len(res2.Issues) {
		t.Fatalf("issue count differs: %d vs %d", len(res1.Issues), len(res2.Issues))
	}
	if app2.WorkingDir() != fixtureDir {
		t.Errorf("WorkingDir = %q", app2.WorkingDir())
	}
}

func TestLoadCachedInInvalidatesOnFileChange(t *testing.T) {
	fixtureDir := writeFixture(t, defaultFixture())
	cacheDir := filepath.Join(fixtureDir, DefaultCacheDir)
	ctx := context.Background()

	app1, err := LoadCachedIn(ctx, fixtureDir, []string{"./..."}, cacheDir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	res1, _ := app1.Verify()
	if !res1.HasCode(CodeCrossModulePrivate) {
		t.Fatalf("fixture should have a violation:\n%s", Report(res1))
	}

	// Fix the violation and give the file a fresh mtime.
	domPath := filepath.Join(fixtureDir, "internal/order/domain/dom.go")
	if err := os.WriteFile(domPath, []byte(goFile("domain", []string{fixtureModule + "/internal/user/api"})), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(domPath, now, now); err != nil {
		t.Fatal(err)
	}

	app2, err := LoadCachedIn(ctx, fixtureDir, []string{"./..."}, cacheDir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	res2, _ := app2.Verify()
	if res2.HasCode(CodeCrossModulePrivate) {
		t.Fatalf("cache should have invalidated:\n%s", Report(res2))
	}
}

func TestLoadExplicitCachedPreservesRules(t *testing.T) {
	fixtureDir := writeFixture(t, defaultFixture())
	cacheDir := filepath.Join(fixtureDir, DefaultCacheDir)
	ctx := context.Background()

	build := func() (*Application, error) {
		app := New().
			Module("user", "./internal/user/...").
			Module("order", "./internal/order/...").
			Module("payment", "./internal/payment/...")
		app.ModuleRules("user").PublishEvents("UserRegistered")
		app.ModuleRules("order").AllowDependencies("user")
		if err := LoadExplicitCached(ctx, fixtureDir, app, []string{"./..."}, cacheDir); err != nil {
			return nil, err
		}
		return app, nil
	}

	app1, err := build()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	res1, _ := app1.Verify()
	// user publishes an event that does not exist -> error.
	if !res1.HasCode(CodeMissingPublishedEvent) {
		t.Fatalf("expected missing-published-event:\n%s", Report(res1))
	}

	app2, err := build()
	if err != nil {
		t.Fatalf("cached load: %v", err)
	}
	res2, _ := app2.Verify()
	if !res2.HasCode(CodeMissingPublishedEvent) {
		t.Fatalf("cached rules lost:\n%s", Report(res2))
	}
	if len(app2.Modules()) != 3 {
		t.Fatalf("modules = %d, want 3", len(app2.Modules()))
	}
}

func TestCacheDisabledWhenNoChanges(t *testing.T) {
	// A cache miss must not be an error: missing cache dir falls back to a
	// normal load.
	fixtureDir := writeFixture(t, defaultFixture())
	app, err := LoadCachedIn(context.Background(), fixtureDir, []string{"./..."}, filepath.Join(fixtureDir, "nested", "cache"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestCacheIgnoresUnreadableCache(t *testing.T) {
	fixtureDir := writeFixture(t, defaultFixture())
	cacheDir := filepath.Join(fixtureDir, DefaultCacheDir)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Corrupt cache file must be treated as a miss, not an error.
	if err := os.WriteFile(filepath.Join(cacheDir, "model.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := LoadCachedIn(context.Background(), fixtureDir, []string{"./..."}, cacheDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestConfigStringStable(t *testing.T) {
	a := NewConfig()
	b := NewConfig()
	if a.String() != b.String() {
		t.Errorf("config String() not stable: %q vs %q", a.String(), b.String())
	}
	c := a
	c.ReportOrphans = true
	if a.String() == c.String() {
		t.Error("config String() should reflect changes")
	}
	if !strings.Contains(c.String(), "report_orphans=true") {
		t.Errorf("unexpected String(): %q", c.String())
	}
}

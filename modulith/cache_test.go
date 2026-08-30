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

func TestLoadCachedInBuildEnvironment(t *testing.T) {
	for _, tt := range []struct{ name, before, after, tag string }{
		{"GOFLAGS", "", "-tags=cache_extra", "cache_extra"},
		{"GOOS", "darwin", "linux", "linux"},
		{"GOARCH", "amd64", "arm64", "arm64"},
		{"CGO_ENABLED", "0", "1", "cgo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.name, tt.before)
			files := defaultFixture()
			files["internal/order/domain/dom.go"] = goFile("domain", nil)
			files["internal/order/domain/tagged.go"] = "//go:build " + tt.tag + "\n\n" + goFile("domain", []string{fixtureModule + "/internal/user/domain"})
			dir := writeFixture(t, files)
			before, err := LoadCachedIn(context.Background(), dir, []string{"./..."}, "")
			if err != nil {
				t.Fatal(err)
			}
			result, _ := before.Verify()
			if result.HasCode(CodeCrossModulePrivate) {
				t.Fatal("tagged violation must be inactive before environment change")
			}
			t.Setenv(tt.name, tt.after)
			after, err := LoadCachedIn(context.Background(), dir, []string{"./..."}, "")
			if err != nil {
				t.Fatal(err)
			}
			result, _ = after.Verify()
			if !result.HasCode(CodeCrossModulePrivate) {
				t.Fatalf("cache hid violation after %s changed", tt.name)
			}
		})
	}
}

func TestLoadCachedInContentChangeWithSameMetadata(t *testing.T) {
	dir := writeFixture(t, defaultFixture())
	path := filepath.Join(dir, "internal/order/domain/dom.go")
	before := goFile("domain", []string{fixtureModule + "/internal/user/api"}) + "   "
	after := goFile("domain", []string{fixtureModule + "/internal/user/domain"})
	if len(before) != len(after) {
		t.Fatal("fixture must preserve file size")
	}
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCachedIn(context.Background(), dir, []string{"./..."}, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	app, err := LoadCachedIn(context.Background(), dir, []string{"./..."}, "")
	if err != nil {
		t.Fatal(err)
	}
	result, _ := app.Verify()
	if !result.HasCode(CodeCrossModulePrivate) {
		t.Fatal("same-size content change with restored mtime reused stale model")
	}
}

func TestLoadCachedInExternalLocalSources(t *testing.T) {
	for _, workspace := range []bool{false, true} {
		name := "replace"
		if workspace {
			name = "workspace"
		}
		t.Run(name, func(t *testing.T) {
			shared := t.TempDir()
			if err := os.WriteFile(filepath.Join(shared, "go.mod"), []byte("module example.com/shared\ngo 1.21\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(shared, "shared.go")
			if err := os.WriteFile(path, []byte("package shared\ntype Item struct{}\nfunc New() Item { return Item{} }\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mod := "module " + fixtureModule + "\ngo 1.21\nrequire example.com/shared v0.0.0\n"
			if !workspace {
				mod += "replace example.com/shared => " + shared + "\n"
			}
			dir := writeFixture(t, map[string]string{
				"go.mod":                    mod,
				"internal/order/api/api.go": "package api\nimport \"example.com/shared\"\nvar Value = shared.New()\n",
			})
			if workspace {
				work := filepath.Join(dir, "go.work")
				if err := os.WriteFile(work, []byte("go 1.23\nuse (\n.\n"+shared+"\n)\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("GOWORK", work)
			}
			if _, err := LoadCachedIn(context.Background(), dir, []string{"./..."}, ""); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("package shared\ntype Other struct{}\nfunc New() Other { return Other{} }\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			app, err := LoadCachedIn(context.Background(), dir, []string{"./..."}, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range app.Packages() {
				for _, ref := range p.APITypeRefs {
					if ref.Package == "example.com/shared" && ref.Name == "Other" {
						return
					}
				}
			}
			t.Fatal("cache hid changed type from external local module")
		})
	}
}

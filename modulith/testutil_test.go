package modulith

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureModule is the module path used for dynamically created fixtures.
const fixtureModule = "example.com/app"

// writeFixture creates a temporary Go module tree from a map of
// slash-relative file paths to contents, changes into it for the duration of
// the test, and returns the directory. It writes a go.mod declaring
// fixtureModule when the map does not already provide one.
func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module " + fixtureModule + "\n\ngo 1.21\n"
	}
	for path, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("writeFixture: mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("writeFixture: write %s: %v", p, err)
		}
	}
	chdir(t, dir)
	return dir
}

// chdir changes the process working directory to dir for the remainder of the
// test. The test package must not call t.Parallel() because the working
// directory is process-global.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("chdir: getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

// goFile returns a minimal valid Go source file for the given package name
// and imports. Imports are emitted as blank imports so the file compiles while
// still creating a real import edge that go/packages reports.
func goFile(pkg string, imports []string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	for _, imp := range imports {
		b.WriteString("import _ \"" + imp + "\"\n")
	}
	if len(imports) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("var _ = struct{}{}\n")
	return b.String()
}

// defaultFixture returns a healthy modular monolith used by many tests:
//
//	internal/user/{api,domain}
//	internal/order/{api,domain}
//	internal/payment/{api,domain}
//
// order.api -> user.api (allowed)
// payment.api -> order.api (allowed)
// order.domain -> user.domain (cross-module private violation)
func defaultFixture() map[string]string {
	return map[string]string{
		"internal/user/api/api.go":       goFile("api", nil),
		"internal/user/domain/dom.go":    goFile("domain", nil),
		"internal/order/api/api.go":      goFile("api", []string{fixtureModule + "/internal/user/api"}),
		"internal/order/domain/dom.go":   goFile("domain", []string{fixtureModule + "/internal/user/domain"}),
		"internal/payment/api/api.go":    goFile("api", []string{fixtureModule + "/internal/order/api"}),
		"internal/payment/domain/dom.go": goFile("domain", nil),
	}
}

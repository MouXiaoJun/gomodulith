package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const cliFixtureModule = "example.com/app"

// writeCLIFixture creates a temp Go module tree and chdirs into it.
func writeCLIFixture(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module " + cliFixtureModule + "\n\ngo 1.21\n"
	}
	for path, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func cliFile(pkg string, imports []string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	for _, imp := range imports {
		b.WriteString("import _ \"" + imp + "\"\n")
	}
	b.WriteString("\nvar _ = struct{}{}\n")
	return b.String()
}

// cliHealthyFixture is a clean modular monolith.
func cliHealthyFixture() map[string]string {
	return map[string]string{
		"internal/user/api/api.go":    cliFile("api", nil),
		"internal/order/api/api.go":   cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
		"internal/payment/api/api.go": cliFile("api", []string{cliFixtureModule + "/internal/order/api"}),
	}
}

func runCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := run(args, &out, &errBuf)
	if errBuf.Len() > 0 {
		out.WriteString("STDERR: " + errBuf.String())
	}
	return out.String(), code
}

func TestRunVersion(t *testing.T) {
	out, code := runCLI(t, "version")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "gomodulith") {
		t.Errorf("output = %q", out)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	_, code := runCLI(t, "frobnicate")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunNoCommand(t *testing.T) {
	_, code := runCLI(t)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunVerifyHealthy(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	out, code := runCLI(t, "verify")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "architecture OK") {
		t.Errorf("missing success marker:\n%s", out)
	}
}

func TestRunVerifyViolation(t *testing.T) {
	writeCLIFixture(t, map[string]string{
		"internal/user/api/api.go":     cliFile("api", nil),
		"internal/user/domain/dom.go":  cliFile("domain", nil),
		"internal/order/api/api.go":    cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
		"internal/order/domain/dom.go": cliFile("domain", []string{cliFixtureModule + "/internal/user/domain"}),
	})
	out, code := runCLI(t, "verify")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "cross-module-private-access") {
		t.Errorf("missing violation detail:\n%s", out)
	}
}

func TestRunVerifyJSON(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	out, code := runCLI(t, "verify", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, `"ok": true`) {
		t.Errorf("expected ok:true in JSON:\n%s", out)
	}
}

func TestRunGraphFormats(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	cases := []struct {
		format string
		want   string
	}{
		{"text", "order -> user"},
		{"mermaid", "graph TD"},
		{"d2", "order -> user"},
		{"json", `"version"`},
	}
	for _, c := range cases {
		out, code := runCLI(t, "graph", "--format", c.format)
		if code != 0 {
			t.Fatalf("%s: exit = %d\n%s", c.format, code, out)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: missing %q:\n%s", c.format, c.want, out)
		}
	}
}

func TestRunGraphUnknownFormat(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	_, code := runCLI(t, "graph", "--format", "bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunExplain(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	out, code := runCLI(t, "explain", "order")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "module order") {
		t.Errorf("missing module header:\n%s", out)
	}
	if !strings.Contains(out, "user") {
		t.Errorf("missing dependency:\n%s", out)
	}
}

func TestRunExplainMissingModule(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	_, code := runCLI(t, "explain", "nope")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestRunExportJSON(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	out, code := runCLI(t, "export")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, cliFixtureModule) {
		t.Errorf("missing module path:\n%s", out)
	}
	if !strings.Contains(out, `"order"`) {
		t.Errorf("missing order module:\n%s", out)
	}
}

func TestRunExportMermaid(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	out, code := runCLI(t, "export", "--format", "mermaid")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "graph TD") {
		t.Errorf("missing mermaid header:\n%s", out)
	}
}

func TestRunDiff(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	base, _ := runCLI(t, "export")

	// Add a module.
	writeCLIFixture(t, map[string]string{
		"internal/user/api/api.go":     cliFile("api", nil),
		"internal/order/api/api.go":    cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
		"internal/shipping/api/api.go": cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
	})
	head, _ := runCLI(t, "export")

	basePath := filepath.Join(t.TempDir(), "base.json")
	headPath := filepath.Join(t.TempDir(), "head.json")
	if err := os.WriteFile(basePath, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headPath, []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, "diff", basePath, headPath)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "module-added (shipping)") {
		t.Errorf("missing module-added change:\n%s", out)
	}
}

func TestRunDiffMissingArgs(t *testing.T) {
	_, code := runCLI(t, "diff", "only-one")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunDiffBadFile(t *testing.T) {
	_, code := runCLI(t, "diff", "/nonexistent/a.json", "/nonexistent/b.json")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestRunVerifySARIF(t *testing.T) {
	writeCLIFixture(t, map[string]string{
		"internal/user/api/api.go":     cliFile("api", nil),
		"internal/user/domain/dom.go":  cliFile("domain", nil),
		"internal/order/api/api.go":    cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
		"internal/order/domain/dom.go": cliFile("domain", []string{cliFixtureModule + "/internal/user/domain"}),
	})
	out, code := runCLI(t, "verify", "--sarif")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (violations present)\n%s", code, out)
	}
	if !strings.Contains(out, `"version": "2.1.0"`) {
		t.Errorf("missing sarif version:\n%s", out)
	}
	if !strings.Contains(out, `"ruleId": "cross-module-private-access"`) {
		t.Errorf("missing rule result:\n%s", out)
	}
}

func TestRunVerifyOutFile(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	outPath := filepath.Join(t.TempDir(), "verify.txt")
	_, code := runCLI(t, "verify", "--out", outPath)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out file: %v", err)
	}
	if !strings.Contains(string(data), "architecture OK") {
		t.Errorf("out file content:\n%s", data)
	}
}

func TestRunExportSARIF(t *testing.T) {
	writeCLIFixture(t, map[string]string{
		"internal/user/api/api.go":     cliFile("api", nil),
		"internal/user/domain/dom.go":  cliFile("domain", nil),
		"internal/order/api/api.go":    cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
		"internal/order/domain/dom.go": cliFile("domain", []string{cliFixtureModule + "/internal/user/domain"}),
	})
	out, code := runCLI(t, "export", "--format", "sarif")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, `"$schema"`) {
		t.Errorf("missing sarif schema:\n%s", out)
	}
}

func TestRunExportOutFile(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	outPath := filepath.Join(t.TempDir(), "arch.json")
	_, code := runCLI(t, "export", "--out", outPath)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out file: %v", err)
	}
	if !strings.Contains(string(data), cliFixtureModule) {
		t.Errorf("out file content:\n%s", data)
	}
}

// initGitRepo creates a git repository with two commits. The first commit
// contains baseFiles; the second commit (only when headFiles is non-empty)
// adds headFiles on top.
func initGitRepo(t *testing.T, baseFiles, headFiles map[string]string) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, baseFiles)
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "base")
	if len(headFiles) > 0 {
		writeFiles(t, dir, headFiles)
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-q", "-m", "head")
	}
	chdirTo(t, dir)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func chdirTo(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestRunDiffGitRefs(t *testing.T) {
	baseFiles := map[string]string{
		"go.mod":                    "module " + cliFixtureModule + "\n\ngo 1.21\n",
		"internal/user/api/api.go":  cliFile("api", nil),
		"internal/order/api/api.go": cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
	}
	headFiles := map[string]string{
		"internal/shipping/api/api.go": cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
	}
	initGitRepo(t, baseFiles, headFiles)

	out, code := runCLI(t, "diff", "HEAD~1", "HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "module-added (shipping)") {
		t.Errorf("missing module-added change:\n%s", out)
	}
	if !strings.Contains(out, "dependency-added (shipping)") {
		t.Errorf("missing dependency-added change:\n%s", out)
	}
}

func TestRunDiffGitRefHeadOnly(t *testing.T) {
	baseFiles := map[string]string{
		"go.mod":                   "module " + cliFixtureModule + "\n\ngo 1.21\n",
		"internal/user/api/api.go": cliFile("api", nil),
	}
	initGitRepo(t, baseFiles, nil)

	out, code := runCLI(t, "diff", "HEAD", "HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "no architecture changes") {
		t.Errorf("expected no changes, got:\n%s", out)
	}
}

func TestRunVerifyWithConfig(t *testing.T) {
	// Explicit-mode rules via .gomodulith.yaml: order may only depend on user;
	// payment->order becomes undeclared.
	files := map[string]string{
		"internal/user/api/api.go":    cliFile("api", nil),
		"internal/order/api/api.go":   cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
		"internal/payment/api/api.go": cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
		".gomodulith.yaml": `module_root: internal
modules:
  user:
    patterns: ["./internal/user/..."]
  order:
    patterns: ["./internal/order/..."]
    allowed: ["user"]
  payment:
    patterns: ["./internal/payment/..."]
`,
	}
	writeCLIFixture(t, files)

	out, code := runCLI(t, "verify")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "undeclared-dependency") {
		t.Errorf("expected undeclared-dependency from config rules:\n%s", out)
	}
}

func TestRunVerifyWithTOMLConfig(t *testing.T) {
	files := map[string]string{
		"internal/user/api/api.go": cliFile("api", nil),
		".gomodulith.toml": `module_root = "internal"
report_orphans = true
`,
	}
	writeCLIFixture(t, files)
	// report_orphans=true turns the config discovery on and the orphan check
	// should not fail verification (warning only).
	out, code := runCLI(t, "verify")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "architecture OK") {
		t.Errorf("output:\n%s", out)
	}
}

func TestRunContract(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	out, code := runCLI(t, "contract")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "architecture contract written to") {
		t.Errorf("output:\n%s", out)
	}
	doc, err := os.ReadFile(".gomodulith/architecture.md")
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	for _, want := range []string{"# Architecture Contract", "### user", "### order", "## Rules", "## Verification status"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("contract missing %q:\n%s", want, doc)
		}
	}
}

func TestRunContractOut(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	outPath := filepath.Join(t.TempDir(), "custom.md")
	out, code := runCLI(t, "contract", "--out", outPath)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "# Architecture Contract") {
		t.Errorf("content:\n%s", data)
	}
}

func TestRunExportDot(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	out, code := runCLI(t, "export", "--format", "dot")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "digraph modules {") {
		t.Errorf("dot output:\n%s", out)
	}
	if !strings.Contains(out, `"user" [label="user"];`) {
		t.Errorf("dot output:\n%s", out)
	}
}

func TestRunVerifyEventConfig(t *testing.T) {
	// order publishes an event that does not exist -> missing-published-event
	// via .gomodulith.yaml rules.
	files := map[string]string{
		"internal/order/api/api.go": cliFile("api", nil),
		".gomodulith.yaml": `modules:
  order:
    patterns: ["./internal/order/..."]
    publish_events: ["OrderPlaced"]
`,
	}
	writeCLIFixture(t, files)
	out, code := runCLI(t, "verify")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "missing-published-event") {
		t.Errorf("expected missing-published-event:\n%s", out)
	}
}

func TestRunVerifyLSP(t *testing.T) {
	writeCLIFixture(t, map[string]string{
		"internal/user/api/api.go":     cliFile("api", nil),
		"internal/user/domain/dom.go":  cliFile("domain", nil),
		"internal/order/api/api.go":    cliFile("api", []string{cliFixtureModule + "/internal/user/api"}),
		"internal/order/domain/dom.go": cliFile("domain", []string{cliFixtureModule + "/internal/user/domain"}),
	})
	out, code := runCLI(t, "verify", "--lsp")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, `"uri": "file://`) {
		t.Errorf("missing file uri:\n%s", out)
	}
	if !strings.Contains(out, `"code": "cross-module-private-access"`) {
		t.Errorf("missing code:\n%s", out)
	}
	if !strings.Contains(out, `"severity": 1`) {
		t.Errorf("missing severity 1:\n%s", out)
	}
}

func TestRunVerifyCacheHit(t *testing.T) {
	writeCLIFixture(t, cliHealthyFixture())
	// First run populates .gomodulith/cache.
	out1, code1 := runCLI(t, "verify")
	if code1 != 0 {
		t.Fatalf("first run exit = %d\n%s", code1, out1)
	}
	cachePath := ".gomodulith/cache/model.json"
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache not created: %v", err)
	}
	st1, _ := os.Stat(cachePath)
	// Second run should hit the cache (cache file unchanged).
	out2, code2 := runCLI(t, "verify")
	if code2 != 0 {
		t.Fatalf("second run exit = %d\n%s", code2, out2)
	}
	st2, _ := os.Stat(cachePath)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Errorf("cache rewritten on hit: %v -> %v", st1.ModTime(), st2.ModTime())
	}
	// --no-cache must still produce a correct result.
	out3, code3 := runCLI(t, "verify", "--no-cache")
	if code3 != 0 {
		t.Fatalf("no-cache run exit = %d\n%s", code3, out3)
	}
	if !strings.Contains(out3, "architecture OK") {
		t.Errorf("no-cache output:\n%s", out3)
	}
}

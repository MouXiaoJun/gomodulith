package modulith

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/tools/go/packages"
)

func TestSourceLocationsAcrossOutputsAndCache(t *testing.T) {
	const path = "internal/order/api/z # source.go"
	const imp = `"example.com/app/internal/user/domain"`
	const code = "package api\r\n\r\nimport d " + imp + "\r\n\r\n//line pretend.go:800\r\nfunc Leak(/* 中文😀 */ value d.Secret) d.Secret { return value }\r\n"
	dir := writeFixture(t, map[string]string{
		"internal/user/api/api.go":      "package api\n",
		"internal/user/domain/dom.go":   "package domain\ntype Secret string\n",
		"internal/order/api/a.go":       "package api\n// The first file does not cause the finding.\n",
		path:                            code,
		"internal/order/api/ignored.go": "//go:build never_modulith_fixture\n\npackage api\nimport _ \"example.com/app/internal/user/domain\"\n",
	})
	cacheDir := filepath.Join(dir, ".cache")
	app, err := LoadCachedIn(context.Background(), dir, []string{"./..."}, cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	app.ModuleRules("order").ForbidDependencies("user").EventDrivenFrom("user")
	res, err := app.Verify()
	if err != nil {
		t.Fatal(err)
	}
	diags, err := app.ExportDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	sarif, err := app.ExportSARIF()
	if err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	if err := json.Unmarshal(sarif, &log); err != nil {
		t.Fatal(err)
	}
	if log.Runs[0].ColumnKind != "utf16CodeUnits" {
		t.Fatal("SARIF column encoding is not declared")
	}
	checked := map[IssueCode]bool{}
	for _, issue := range res.Issues {
		token := imp
		if issue.Code == CodeCrossModuleTypeLeakage {
			token = "Secret"
		}
		offset := strings.Index(code, token)
		prefix := code[:offset]
		line := strings.Count(prefix, "\n")
		columnText := prefix[strings.LastIndex(prefix, "\n")+1:]
		column := len(utf16.Encode([]rune(columnText)))
		want := LSPRange{Start: LSPPosition{line, column}, End: LSPPosition{line, column + len(token)}}
		loc := app.locationForIssue(issue)
		if loc == nil || loc.File != filepath.Join(dir, filepath.FromSlash(path)) || loc.Range != want {
			t.Fatalf("%s location = %+v, want %s %+v", issue.Code, loc, path, want)
		}
		textPos := fmt.Sprintf("%s:%d:%d", path, line+1, len(columnText)+1)
		if !strings.Contains(Report(res), textPos) {
			t.Fatalf("terminal lacks %s", textPos)
		}
		found := false
		for _, d := range diags {
			if d.Code != string(issue.Code) {
				continue
			}
			found = true
			if d.URI != fileURI(loc.File) || d.Range != want {
				t.Fatalf("LSP mismatch: %+v", d)
			}
		}
		if !found {
			t.Fatalf("LSP missing %s", issue.Code)
		}
		found = false
		for _, r := range log.Runs[0].Results {
			if r.RuleID != string(issue.Code) {
				continue
			}
			found = true
			if len(r.Locations) != 1 {
				t.Fatalf("SARIF location count: %+v", r)
			}
			p := r.Locations[0].PhysicalLocation
			uri := (&url.URL{Path: path}).String()
			if p == nil || p.ArtifactLocation.URI != uri || p.Region == nil ||
				p.Region.StartLine != line+1 || p.Region.StartColumn != column+1 ||
				p.Region.EndLine != line+1 || p.Region.EndColumn != want.End.Character+1 {
				t.Fatalf("SARIF mismatch: %+v", p)
			}
		}
		if !found {
			t.Fatalf("SARIF missing %s", issue.Code)
		}
		checked[issue.Code] = true
	}
	for _, code := range []IssueCode{CodeCrossModulePrivate, CodeCrossModuleTypeLeakage, CodeForbiddenDependency, CodeEventPackageMissing} {
		if !checked[code] {
			t.Fatalf("fixture did not exercise %s", code)
		}
	}
	// Verify actual serialization/hit, not a fresh load that happens to agree.
	cached, hit := tryLoadCache(context.Background(), cacheDir, dir, NewConfig(), []string{"./..."}, nil)
	if !hit {
		t.Fatal("expected cache hit")
	}
	if !reflect.DeepEqual(app.sources, cached.sources) {
		t.Fatal("cache lost locations")
	}
	// Location data must be an immutable part of the loaded model.
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(path))); err != nil {
		t.Fatal(err)
	}
	again, err := app.ExportDiagnostics()
	if err != nil || !reflect.DeepEqual(diags, again) {
		t.Fatalf("output re-read changed source: %v", err)
	}
}

func TestAPIExposureLocations(t *testing.T) {
	files := map[string]string{
		"internal/user/api/a.go":      "package api\ntype Receiver struct{}\n",
		"internal/user/domain/dom.go": "package domain\ntype Secret string\ntype Dot string\ntype Method string\ntype Inferred string\nfunc New() Inferred { return 0 }\n",
		"internal/user/api/b.go":      "package api\nimport d \"example.com/app/internal/user/domain\"\nfunc hidden() { var _ d.Secret }\nfunc Exported() d.Secret { return 0 }\n",
		"internal/user/api/c.go":      "package api\nimport . \"example.com/app/internal/user/domain\"\ntype Alias = Dot\n",
		"internal/user/api/d.go":      "package api\nimport d \"example.com/app/internal/user/domain\"\nfunc (*Receiver) Exported() d.Method { return 0 }\n",
		"internal/user/api/e.go":      "package api\nimport d \"example.com/app/internal/user/domain\"\nvar ExportedValue = d.New()\n",
		"internal/user/api/aa.go":     "package api\nimport p \"example.com/app/internal/other/api\"\ntype Proxy = p.Public\n",
		"internal/other/api/a.go":     "package api\nimport d \"example.com/app/internal/user/domain\"\ntype Public struct { Field d.Secret }\n",
	}
	// Use integer underlying types so all zero-value return expressions compile.
	files["internal/user/domain/dom.go"] = strings.ReplaceAll(files["internal/user/domain/dom.go"], " string", " int")
	writeFixture(t, files)
	app := mustLoad(t, "./...")
	res, err := app.Verify()
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]struct {
		file, token string
		line        int
	}{
		"Secret": {"b.go", "Secret", 3}, "Dot": {"c.go", "Dot", 2},
		"Method": {"d.go", "Method", 2}, "Inferred": {"e.go", "ExportedValue", 2},
	}
	for _, issue := range res.Issues {
		if issue.Code != CodeInternalAPITypeLeakage {
			continue
		}
		name := strings.TrimPrefix(issue.To, fixtureModule+"/internal/user/domain.")
		want, ok := wants[name]
		if !ok {
			t.Fatalf("unexpected type: %s", issue.To)
		}
		loc := app.locationForIssue(issue)
		if loc == nil || filepath.Base(loc.File) != want.file || loc.Range.Start.Line != want.line {
			t.Fatalf("%s at %+v", name, loc)
		}
		line := strings.Split(files["internal/user/api/"+want.file], "\n")[want.line]
		if loc.Range.Start.Character != strings.Index(line, want.token) {
			t.Fatalf("%s column: %+v", name, loc)
		}
		delete(wants, name)
	}
	if len(wants) != 0 {
		t.Fatalf("missing exposure cases: %v", wants)
	}
}

func TestFileURI(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/tmp/a b#c?.go", "file:///tmp/a%20b%23c%3F.go"},
		{`C:\work\a b#c.go`, "file:///C:/work/a%20b%23c.go"},
		{"C:/work/a.go", "file:///C:/work/a.go"},
		{`\\server\share\a b.go`, "file://server/share/a%20b.go"},
		{"/tmp/中文.go", "file:///tmp/%E4%B8%AD%E6%96%87.go"},
	} {
		if got := fileURI(tc.path); got != tc.want {
			t.Errorf("fileURI(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestModuleAndOrphanLocations(t *testing.T) {
	files := map[string]string{
		"internal/a/api/a.go":     "package api\n",
		"internal/a/api/z.go":     goFile("api", []string{fixtureModule + "/internal/b/api"}),
		"internal/b/api/b.go":     "package api\n",
		"internal/b/service/z.go": goFile("service", []string{fixtureModule + "/internal/a/api"}),
		"internal/b/events/e.go":  "package events\n",
		"cmd/other.go":            "package cmd\n",
	}
	dir := writeFixture(t, files)
	app := NewWithConfig(Config{ReportOrphans: true})
	app.Module("a", "./internal/a/...").Module("b", "./internal/b/...")
	app.ModuleRules("a").EventDrivenFrom("b")
	cacheDir := filepath.Join(dir, ".cache")
	if err := LoadExplicitCached(context.Background(), dir, app, []string{"./..."}, cacheDir); err != nil {
		t.Fatal(err)
	}
	res, err := app.Verify()
	if err != nil {
		t.Fatal(err)
	}
	checked := map[IssueCode]bool{}
	for _, issue := range res.Issues {
		loc := app.locationForIssue(issue)
		if loc == nil {
			t.Fatalf("missing location for %s", issue.String())
		}
		if issue.Code == CodeOrphanPackage {
			if filepath.Base(loc.File) != "other.go" || loc.Range.Start != (LSPPosition{0, 8}) {
				t.Fatalf("orphan: %+v", loc)
			}
		} else if filepath.Base(loc.File) != "z.go" || loc.Range.Start.Line != 2 {
			t.Fatalf("%s: %+v", issue.Code, loc)
		}
		checked[issue.Code] = true
	}
	for _, code := range []IssueCode{CodeCycle, CodeUndeclaredDependency, CodeEventDrivenViolation, CodeOrphanPackage} {
		if !checked[code] {
			t.Fatalf("missing case %s", code)
		}
	}
	copy := NewWithConfig(Config{ReportOrphans: true})
	copy.Module("a", "./internal/a/...").Module("b", "./internal/b/...")
	copy.ModuleRules("a").EventDrivenFrom("b")
	if _, hit := tryLoadCache(context.Background(), cacheDir, dir, copy.config, []string{"./..."}, copy.ruleDefs()); !hit {
		t.Fatal("explicit cache did not hit")
	}
	if err := LoadExplicitCached(context.Background(), dir, copy, []string{"./..."}, cacheDir); err != nil {
		t.Fatal(err)
	}
	a, err := app.ExportDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	b, err := copy.ExportDiagnostics()
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("explicit cache restore lost positions: %v", err)
	}
}

func TestSourceLocationsUseParsedBytes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.go")
	// The parsed input need not be on disk: never re-read a different version
	// to convert the positions. Test the collector, not GOFLAGS overlay support
	// (go/packages requires Config.Overlay rather than merely GOFLAGS).
	src := "package api\n\n/* 中文😀 */ import _ \"example.com/app/internal/b/domain\"\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := &packages.Package{Fset: fset, Syntax: []*ast.File{f}, TypesInfo: &types.Info{}}
	collected := collectPackageSources(p, map[string][]byte{file: []byte(src)})
	loc := collected.Imports["example.com/app/internal/b/domain"]
	prefix := "/* 中文😀 */ import _ "
	if loc == nil || loc.File != file || loc.Range.Start != (LSPPosition{2, len(utf16.Encode([]rune(prefix)))}) {
		t.Fatalf("positions did not use parsed bytes: %+v", loc)
	}
}

package modulith

import (
	"reflect"
	"testing"
)

// cycleFixture builds a module-level cycle WITHOUT a package-level cycle:
// every module edge "x -> y" is materialized by a unique source package in x
// importing a unique target package in y. Because no package is ever both a
// source and a target, no package import cycle is formed and the code
// compiles while the module graph cycles.
func cycleFixture(edges map[string][]string) map[string]string {
	files := map[string]string{}
	modules := map[string]bool{}
	for from, tos := range edges {
		modules[from] = true
		for _, to := range tos {
			modules[to] = true
		}
	}
	for mod := range modules {
		files["internal/"+mod+"/base/base.go"] = goFile("base", nil)
	}

	i := 0
	for from, tos := range edges {
		for _, to := range tos {
			src := "internal/" + from + "/s" + itoa(i)
			tgt := "internal/" + to + "/t" + itoa(i)
			files[src+"/s.go"] = goFile("s"+itoa(i), []string{fixtureModule + "/" + tgt})
			files[tgt+"/t.go"] = goFile("t"+itoa(i), nil)
			i++
		}
	}
	return files
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestCycleDetection(t *testing.T) {
	// Module cycle user -> order -> payment -> user, without package cycles.
	files := cycleFixture(map[string][]string{
		"user":    {"order"},
		"order":   {"payment"},
		"payment": {"user"},
	})
	writeFixture(t, files)

	app := mustLoad(t, "./...")
	g := app.BuildGraph()
	cycles := g.Cycles()
	if len(cycles) != 1 {
		t.Fatalf("cycles = %v, want exactly 1", cycleStrings(cycles))
	}
	got := cycles[0].Modules
	want := []string{"order", "payment", "user", "order"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cycle = %v, want %v", got, want)
	}

	// Verify reports the cycle as an error.
	res, _ := app.Verify()
	if res.OK {
		t.Fatal("expected verification failure on cycle")
	}
	if !res.HasCode(CodeCycle) {
		t.Fatalf("expected cycle issue, got:\n%s", Report(res))
	}
}

func TestNoFalseCycleOnDAG(t *testing.T) {
	// Module chain user -> order -> payment (no cycle).
	files := cycleFixture(map[string][]string{
		"user":  {"order"},
		"order": {"payment"},
	})
	writeFixture(t, files)
	app := mustLoad(t, "./...")
	if cycles := app.BuildGraph().Cycles(); len(cycles) != 0 {
		t.Fatalf("cycles = %v, want none", cycleStrings(cycles))
	}
}

func TestMultipleElementaryCycles(t *testing.T) {
	// Module graph: a -> b, a -> c, b -> a, b -> c, c -> a
	// Elementary module cycles: a-b-a, a-c-a, a-b-c-a.
	files := cycleFixture(map[string][]string{
		"a": {"b", "c"},
		"b": {"a", "c"},
		"c": {"a"},
	})
	writeFixture(t, files)
	app := mustLoad(t, "./...")
	cycles := app.BuildGraph().Cycles()

	want := map[string]bool{
		"a -> b -> a":      true,
		"a -> c -> a":      true,
		"a -> b -> c -> a": true,
	}
	if len(cycles) != len(want) {
		t.Fatalf("cycles = %v, want %d distinct", cycleStrings(cycles), len(want))
	}
	for _, c := range cycles {
		if !want[c.String()] {
			t.Errorf("unexpected cycle %q", c.String())
		}
	}
}

// TestCyclesPureGraph verifies the cycle enumeration on a synthetic graph
// without going through package loading, covering edge cases such as a
// self-loop being ignored and a single 2-node cycle.
func TestCyclesPureGraph(t *testing.T) {
	user := &Module{Name: "user"}
	order := &Module{Name: "order"}
	payment := &Module{Name: "payment"}
	g := &Graph{Dependencies: []*Dependency{
		{From: user, To: order},
		{From: order, To: payment},
		{From: payment, To: user},
	}}
	cycles := g.Cycles()
	if len(cycles) != 1 {
		t.Fatalf("cycles = %v, want 1", cycleStrings(cycles))
	}
	if cycles[0].String() != "order -> payment -> user -> order" {
		t.Fatalf("cycle = %q", cycles[0].String())
	}

	// Self-loop must not be reported as a cycle.
	g2 := &Graph{Dependencies: []*Dependency{
		{From: user, To: user},
		{From: order, To: order},
	}}
	if cycles := g2.Cycles(); len(cycles) != 0 {
		t.Fatalf("self-loops must not be cycles, got %v", cycleStrings(cycles))
	}
}

func cycleStrings(cycles []*Cycle) []string {
	var out []string
	for _, c := range cycles {
		out = append(out, c.String())
	}
	return out
}

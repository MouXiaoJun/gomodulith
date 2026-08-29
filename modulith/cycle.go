package modulith

import (
	"sort"
	"strings"
)

// Cycle is a cyclic dependency between modules.
type Cycle struct {
	// Modules is the cycle as a sequence of module names where the last
	// module depends back on the first, e.g. ["user", "order", "user"].
	Modules []string
}

// String returns "user -> order -> user".
func (c *Cycle) String() string {
	var b strings.Builder
	for i, m := range c.Modules {
		if i > 0 {
			b.WriteString(" -> ")
		}
		b.WriteString(m)
	}
	return b.String()
}

// Cycles detects every distinct elementary cycle in the module dependency
// graph. Each elementary cycle has a unique minimum vertex (lexicographically
// smallest module name), so starting a depth-first search from that vertex and
// only visiting vertices no smaller than it enumerates every cycle exactly
// once. Self-loops are not reported as cycles.
func (g *Graph) Cycles() []*Cycle {
	// Adjacency list over all module names, sorted for determinism.
	adj := map[string][]string{}
	has := map[string]bool{}
	for _, d := range g.Dependencies {
		has[d.From.Name] = true
		has[d.To.Name] = true
		if d.From.Name != d.To.Name {
			adj[d.From.Name] = append(adj[d.From.Name], d.To.Name)
		}
	}
	names := make([]string, 0, len(has))
	for n := range has {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sort.Strings(adj[n])
	}

	// A hard cap keeps pathological graphs bounded; architecture graphs are
	// expected to stay far below this.
	const maxPath = 256

	var cycles []*Cycle
	unique := map[string]bool{}

	for _, start := range names {
		if len(adj[start]) == 0 {
			continue
		}
		path := []string{start}
		onPath := map[string]bool{start: true}

		var dfs func(v string)
		dfs = func(v string) {
			if len(path) > maxPath {
				return
			}
			for _, w := range adj[v] {
				if w == start {
					cycle := append([]string(nil), path...)
					cycle = append(cycle, start)
					key := strings.Join(cycle, "->")
					if !unique[key] {
						unique[key] = true
						cycles = append(cycles, &Cycle{Modules: cycle})
					}
					continue
				}
				if w < start || onPath[w] {
					continue
				}
				onPath[w] = true
				path = append(path, w)
				dfs(w)
				path = path[:len(path)-1]
				delete(onPath, w)
			}
		}
		dfs(start)
	}

	sort.Slice(cycles, func(i, j int) bool { return cycles[i].String() < cycles[j].String() })
	return cycles
}

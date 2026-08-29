package modulith

import (
	"fmt"
	"sort"
)

// DiffChangeKind identifies the type of an architecture change between two
// models.
type DiffChangeKind string

const (
	// DiffModuleAdded: a module appeared in the head model.
	DiffModuleAdded DiffChangeKind = "module-added"
	// DiffModuleRemoved: a module disappeared from the head model.
	DiffModuleRemoved DiffChangeKind = "module-removed"
	// DiffDependencyAdded: a module dependency appeared.
	DiffDependencyAdded DiffChangeKind = "dependency-added"
	// DiffDependencyRemoved: a module dependency disappeared.
	DiffDependencyRemoved DiffChangeKind = "dependency-removed"
	// DiffPublicAPIAdded: a public API package appeared.
	DiffPublicAPIAdded DiffChangeKind = "public-api-added"
	// DiffPublicAPIRemoved: a public API package disappeared.
	DiffPublicAPIRemoved DiffChangeKind = "public-api-removed"
	// DiffRuleChanged: a dependency rule changed.
	DiffRuleChanged DiffChangeKind = "rule-changed"
	// DiffCycleAdded: a new cyclic dependency appeared.
	DiffCycleAdded DiffChangeKind = "cycle-added"
	// DiffCycleRemoved: a cyclic dependency disappeared.
	DiffCycleRemoved DiffChangeKind = "cycle-removed"
)

// DiffChange is a single architecture change between two models.
type DiffChange struct {
	Kind   DiffChangeKind
	Module string // related module name, when applicable
	Detail string // human-readable detail
}

// String returns a one-line description of the change.
func (c DiffChange) String() string {
	prefix := string(c.Kind)
	if c.Module != "" {
		prefix += " (" + c.Module + ")"
	}
	if c.Detail != "" {
		return prefix + ": " + c.Detail
	}
	return prefix
}

// Diff computes the architecture changes between the base and head models.
// The comparison covers modules, dependencies, public API packages, rules and
// cycles. Both models may be nil (treated as empty), which makes it safe to
// diff an initial export against a later one.
func Diff(base, head *ArchitectureModel) []DiffChange {
	var changes []DiffChange
	if base == nil {
		base = &ArchitectureModel{}
	}
	if head == nil {
		head = &ArchitectureModel{}
	}

	baseMods := map[string]ModuleModel{}
	for _, m := range base.Modules {
		baseMods[m.Name] = m
	}
	headMods := map[string]ModuleModel{}
	for _, m := range head.Modules {
		headMods[m.Name] = m
	}

	// Modules.
	for name := range headMods {
		if _, ok := baseMods[name]; !ok {
			changes = append(changes, DiffChange{Kind: DiffModuleAdded, Module: name})
		}
	}
	for name := range baseMods {
		if _, ok := headMods[name]; !ok {
			changes = append(changes, DiffChange{Kind: DiffModuleRemoved, Module: name})
		}
	}

	// Dependencies.
	depKey := func(d DependencyModel) string { return d.From + " -> " + d.To }
	baseDeps := map[string]DependencyModel{}
	for _, d := range base.Dependencies {
		baseDeps[depKey(d)] = d
	}
	headDeps := map[string]DependencyModel{}
	for _, d := range head.Dependencies {
		headDeps[depKey(d)] = d
	}
	for k, d := range headDeps {
		if _, ok := baseDeps[k]; !ok {
			changes = append(changes, DiffChange{Kind: DiffDependencyAdded, Module: d.From, Detail: k})
		}
	}
	for k, d := range baseDeps {
		if _, ok := headDeps[k]; !ok {
			changes = append(changes, DiffChange{Kind: DiffDependencyRemoved, Module: d.From, Detail: k})
		}
	}

	// Public API and rules per module.
	for name, hm := range headMods {
		bm, ok := baseMods[name]
		if !ok {
			continue
		}
		basePub := strSet(bm.PublicPackages)
		headPub := strSet(hm.PublicPackages)
		for p := range headPub {
			if !basePub[p] {
				changes = append(changes, DiffChange{Kind: DiffPublicAPIAdded, Module: name, Detail: p})
			}
		}
		for p := range basePub {
			if !headPub[p] {
				changes = append(changes, DiffChange{Kind: DiffPublicAPIRemoved, Module: name, Detail: p})
			}
		}
		if !sameRules(bm, hm) {
			changes = append(changes, DiffChange{
				Kind:   DiffRuleChanged,
				Module: name,
				Detail: fmt.Sprintf("allowed=%v forbidden=%v", hm.AllowedDependencies, hm.ForbiddenDependencies),
			})
		}
	}

	// Cycles.
	baseCycle := cycleSet(base.Cycles)
	headCycle := cycleSet(head.Cycles)
	for c := range headCycle {
		if !baseCycle[c] {
			changes = append(changes, DiffChange{Kind: DiffCycleAdded, Detail: c})
		}
	}
	for c := range baseCycle {
		if !headCycle[c] {
			changes = append(changes, DiffChange{Kind: DiffCycleRemoved, Detail: c})
		}
	}

	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].String() < changes[j].String()
	})
	return changes
}

func strSet(xs []string) map[string]bool {
	out := map[string]bool{}
	for _, x := range xs {
		out[x] = true
	}
	return out
}

func sameRules(a, b ModuleModel) bool {
	if len(a.AllowedDependencies) != len(b.AllowedDependencies) {
		return false
	}
	if len(a.ForbiddenDependencies) != len(b.ForbiddenDependencies) {
		return false
	}
	sa := strSet(a.AllowedDependencies)
	sb := strSet(b.AllowedDependencies)
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	fa := strSet(a.ForbiddenDependencies)
	fb := strSet(b.ForbiddenDependencies)
	for k := range fa {
		if !fb[k] {
			return false
		}
	}
	return true
}

func cycleSet(cycles []CycleModel) map[string]bool {
	out := map[string]bool{}
	for _, c := range cycles {
		key := ""
		for i, m := range c.Modules {
			if i > 0 {
				key += " -> "
			}
			key += m
		}
		out[key] = true
	}
	return out
}

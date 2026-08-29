package modulith

import (
	"fmt"
	"strings"
)

// Report renders the verification result as a human-readable text report in
// the spirit of the README examples:
//
//	✓ user
//	✓ order
//	✗ payment
//
//	order -> user/api         OK
//	order -> user/internal    VIOLATION
//
//	cycle detected:
//	user -> order -> user
func Report(r *Result) string {
	var b strings.Builder

	// Module summary.
	for _, m := range r.Graph.App.Modules() {
		mark := "✓"
		if moduleHasError(r, m.Name) {
			mark = "✗"
		}
		fmt.Fprintf(&b, "%s %s\n", mark, m.Name)
	}

	// Dependency edges.
	if len(r.Graph.Dependencies) > 0 {
		b.WriteString("\n")
		edges := r.Graph.Dependencies
		maxFrom := 0
		for _, d := range edges {
			if len(d.From.Name) > maxFrom {
				maxFrom = len(d.From.Name)
			}
		}
		for _, d := range edges {
			status := "OK"
			if !d.Allowed {
				status = "VIOLATION"
			}
			fmt.Fprintf(&b, "%-*s -> %-12s %s\n", maxFrom, d.From.Name, d.To.Name, status)
			for _, imp := range d.Imports {
				fmt.Fprintf(&b, "    %s\n", imp)
			}
		}
	}

	// Issues.
	issues := append([]*Issue(nil), r.Issues...)
	// keep only issues that add value on top of the graph view, dedupe.
	seen := map[string]bool{}
	var details []*Issue
	for _, i := range issues {
		key := string(i.Code) + "\x00" + i.From + "\x00" + i.To
		if seen[key] {
			continue
		}
		seen[key] = true
		details = append(details, i)
	}
	if len(details) > 0 {
		b.WriteString("\n")
		if len(r.Cycles) > 0 {
			b.WriteString("cycle detected:\n")
			for _, c := range r.Cycles {
				fmt.Fprintf(&b, "%s\n", c.String())
			}
		}
		for _, i := range details {
			if i.Code == CodeCycle {
				continue // already shown above
			}
			fmt.Fprintf(&b, "%s\n", i.String())
		}
	}

	b.WriteString("\n")
	if r.OK {
		b.WriteString("architecture OK\n")
	} else {
		nerr := len(r.Errors())
		nwarn := len(r.Warnings())
		b.WriteString(fmt.Sprintf("architecture violations: %d error(s), %d warning(s)\n", nerr, nwarn))
	}
	return b.String()
}

// moduleHasError reports whether the module is involved in an error issue.
func moduleHasError(r *Result, name string) bool {
	for _, i := range r.Issues {
		if i.Severity != SeverityError {
			continue
		}
		if i.Module == name || i.From == name {
			return true
		}
	}
	return false
}

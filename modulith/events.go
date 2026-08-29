package modulith

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// eventPackageOf returns the module's event packages — packages whose
// directory element equals the configured event element (default "events").
func (m *Module) eventPackageOf(cfg Config) map[string]bool {
	eventRel := m.RootDir + "/" + cfg.EventElement
	out := map[string]bool{}
	for id, p := range m.pkgs {
		rel := filepath.ToSlash(p.RelDir)
		if rel == eventRel || strings.HasPrefix(rel, eventRel+"/") {
			out[id] = true
		}
	}
	return out
}

// isEventPackage reports whether the given package path is one of the module's
// event packages.
func (m *Module) isEventPackage(cfg Config, id string) bool {
	return m.eventPackageOf(cfg)[id]
}

// isPublicSurface reports whether the package is a public interaction surface
// of the module: a declared public API package or an event package. Event
// packages are public by design because other modules subscribe to events
// through them.
func (a *Application) isPublicSurface(m *Module, id string) bool {
	if m == nil {
		return false
	}
	return m.IsPublicPackage(id) || m.isEventPackage(a.config, id)
}

// exportedTypes returns the set of exported type names declared anywhere in the
// module.
func (m *Module) exportedTypes() map[string]bool {
	out := map[string]bool{}
	for _, p := range m.pkgs {
		for _, name := range p.ExportedTypes {
			out[name] = true
		}
	}
	return out
}

// checkEventRules validates event-driven interaction rules:
//
//   - every declared published event type must exist in the publishing module
//     (missing-published-event),
//   - a module with event-driven rules towards another module may only import
//     that module's event packages (event-driven-violation),
//   - depending on a module that has no event packages while declaring it
//     event-driven is flagged (event-package-missing).
func (a *Application) checkEventRules() []*Issue {
	var issues []*Issue
	config := a.config

	for _, m := range a.modules {
		// Published events must exist as exported types in the module.
		if len(m.PublishedEvents) > 0 {
			types := m.exportedTypes()
			for _, ev := range m.PublishedEvents {
				if !types[ev] {
					issues = append(issues, &Issue{
						Code:     CodeMissingPublishedEvent,
						Severity: SeverityError,
						Module:   m.Name,
						Message: fmt.Sprintf(
							"module %q declares published event %q but no such exported type exists in the module",
							m.Name, ev),
						From: m.Name,
					})
				}
			}
		}

		// Event-driven dependencies: only event-package imports are allowed.
		if len(m.EventDrivenModules) == 0 {
			continue
		}
		eventDriven := map[string]bool{}
		for _, n := range m.EventDrivenModules {
			eventDriven[n] = true
		}
		for _, fromPkg := range m.Packages() {
			for _, toPath := range fromPkg.Imports {
				if isExternalImport(toPath) {
					continue
				}
				toMod := a.ModuleForPackage(toPath)
				if toMod == nil || !eventDriven[toMod.Name] {
					continue
				}
				eventPkgs := toMod.eventPackageOf(config)
				if len(eventPkgs) == 0 {
					// Target module has no event packages: any interaction is
					// impossible to express as events.
					issues = append(issues, &Issue{
						Code:     CodeEventPackageMissing,
						Severity: SeverityWarning,
						Module:   m.Name,
						Message: fmt.Sprintf(
							"module %q is declared event-driven towards %q but %q has no %q package",
							m.Name, toMod.Name, toMod.Name, config.EventElement),
						From: fromPkg.ID,
						To:   toPath,
					})
					continue
				}
				if !eventPkgs[toPath] {
					issues = append(issues, &Issue{
						Code:     CodeEventDrivenViolation,
						Severity: SeverityError,
						Module:   m.Name,
						Message: fmt.Sprintf(
							"module %q is event-driven towards %q but imports non-event package %q; import only %q packages",
							m.Name, toMod.Name, toPath, config.EventElement),
						From: fromPkg.ID,
						To:   toPath,
					})
				}
			}
		}
	}

	// Deterministic order.
	sort.SliceStable(issues, func(i, j int) bool {
		return issues[i].String() < issues[j].String()
	})
	return issues
}

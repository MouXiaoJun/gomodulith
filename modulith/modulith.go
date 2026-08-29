// Package modulith provides architecture verification and a modular monolith
// toolkit for Go, inspired by Spring Modulith and architecture-testing tools.
//
// The package is designed around Go's package model, internal visibility rules,
// go/packages, and idiomatic go test workflows.
package modulith

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModuleRootDefault is the default directory (relative to the working
// directory) that is scanned for modules during convention-based discovery.
const ModuleRootDefault = "internal"

// Config controls module discovery and verification behaviour.
type Config struct {
	// ModuleRoot is the directory scanned for modules using the default
	// convention. It defaults to "internal".
	ModuleRoot string

	// APIElement is the name of the sub-directory (or sub-package element)
	// that marks a module's public API packages. It defaults to "api".
	APIElement string

	// InternalElement is the name of the sub-directory (or sub-package
	// element) that marks a module's private implementation packages. It
	// defaults to "internal".
	InternalElement string

	// EventElement is the name of the sub-directory (or sub-package element)
	// that marks a module's event packages — the packages through which
	// event-driven interaction is allowed. It defaults to "events".
	EventElement string

	// ReportOrphans enables the "orphan package" warning for packages that
	// are not classified into any module. Disabled by default because many
	// projects intentionally keep packages such as cmd/ or pkg/ outside of
	// the module tree.
	ReportOrphans bool

	// PublicAPIRequired, when true, makes the absence of a public API package
	// in a discovered module a verification error instead of a warning.
	PublicAPIRequired bool
}

// NewConfig returns a Config populated with the default values.
func NewConfig() Config {
	return Config{
		ModuleRoot:      ModuleRootDefault,
		APIElement:      "api",
		InternalElement: "internal",
		EventElement:    "events",
		ReportOrphans:   false,
	}
}

// Package is a single Go package participating in the architecture model.
type Package struct {
	ID      string   // import path
	Name    string   // Go package name
	Dir     string   // source directory on disk
	RelDir  string   // directory relative to the working directory
	Imports []string // sorted, de-duplicated import paths

	// GoFiles lists the absolute paths of the package's Go source files
	// (excluding test files), as reported by go/packages.
	GoFiles []string

	// ExportedTypes lists the exported type names declared by the package,
	// sorted. Populated when type information is available.
	ExportedTypes []string
}

// Module is a single application module in the architecture model.
type Module struct {
	// Name is the module's identifier, e.g. "user".
	Name string

	// Patterns are the package patterns that belong to this module, e.g.
	// "./internal/user/...". Empty in convention mode where membership is
	// derived from the module root.
	Patterns []string

	// RootDir is the module's directory relative to the working directory,
	// e.g. "internal/user". Set in convention mode.
	RootDir string

	// PublicPackages are the package import paths that form the module's
	// public API.
	PublicPackages []string

	// AllowedDependencies are the names of modules this module may depend on.
	// When non-empty, undeclared module dependencies become violations.
	AllowedDependencies []string

	// ForbiddenDependencies are the names of modules this module may not
	// depend on.
	ForbiddenDependencies []string

	// PublishedEvents are the names of the event types the module publishes.
	// Each must exist as an exported type within the module.
	PublishedEvents []string

	// EventDrivenModules are the names of modules with which this module may
	// interact only through events: imports into those modules must target
	// their event packages.
	EventDrivenModules []string

	pkgs map[string]*Package
}

// Packages returns the packages that belong to this module, sorted by path.
func (m *Module) Packages() []*Package {
	out := make([]*Package, 0, len(m.pkgs))
	for _, p := range m.pkgs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PackageCount returns the number of packages in the module.
func (m *Module) PackageCount() int { return len(m.pkgs) }

// PublicPackageSet returns the set of public package paths for the module.
func (m *Module) PublicPackageSet() map[string]bool {
	set := map[string]bool{}
	for _, p := range m.PublicPackages {
		set[p] = true
	}
	return set
}

// IsPublicPackage reports whether the given package path is part of the
// module's public API.
func (m *Module) IsPublicPackage(id string) bool {
	_, ok := m.PublicPackageSet()[id]
	return ok
}

// Application is the architecture model for the whole codebase.
type Application struct {
	config Config
	wd     string

	// modules keeps insertion order for deterministic output.
	modules []*Module
	byName  map[string]*Module

	// pkgs indexes every loaded package by import path.
	pkgs map[string]*Package

	// orphanPkgs are loaded packages that were not classified into a module.
	orphanPkgs []*Package

	// explicit marks that modules were defined explicitly (via Module +
	// LoadExplicit) rather than discovered by convention. It changes the
	// default for modules without declared dependency rules: explicit mode is
	// closed by default, convention mode is open by default.
	explicit bool

	loaded bool
}

// Config returns a copy of the configuration used by the application.
func (a *Application) Config() Config { return a.config }

// String returns a stable canonical representation of the configuration,
// suitable for cache keys and diffing.
func (c Config) String() string {
	return strings.Join([]string{
		"module_root=" + c.ModuleRoot,
		"api_element=" + c.APIElement,
		"internal_element=" + c.InternalElement,
		"event_element=" + c.EventElement,
		fmt.Sprintf("report_orphans=%t", c.ReportOrphans),
		fmt.Sprintf("public_api_required=%t", c.PublicAPIRequired),
	}, "|")
}

// WorkingDir returns the working directory the application was loaded from.
func (a *Application) WorkingDir() string { return a.wd }

// Modules returns the modules in insertion order.
func (a *Application) Modules() []*Module {
	out := make([]*Module, len(a.modules))
	copy(out, a.modules)
	return out
}

// ModuleByName returns the module with the given name, or nil.
func (a *Application) ModuleByName(name string) *Module {
	return a.byName[name]
}

// Packages returns every loaded package, sorted by import path.
func (a *Application) Packages() []*Package {
	out := make([]*Package, 0, len(a.pkgs))
	for _, p := range a.pkgs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// OrphanPackages returns packages that were not classified into any module.
func (a *Application) OrphanPackages() []*Package {
	out := make([]*Package, len(a.orphanPkgs))
	copy(out, a.orphanPkgs)
	return out
}

// PackageByID returns the loaded package with the given import path, or nil.
func (a *Application) PackageByID(id string) *Package { return a.pkgs[id] }

// New returns an empty Application for explicit, rule-driven module
// definitions. Use Load for convention-based discovery.
func New() *Application {
	return NewWithConfig(NewConfig())
}

// NewWithConfig returns an empty Application using the supplied configuration.
func NewWithConfig(cfg Config) *Application {
	if cfg.ModuleRoot == "" {
		cfg.ModuleRoot = ModuleRootDefault
	}
	if cfg.APIElement == "" {
		cfg.APIElement = "api"
	}
	if cfg.InternalElement == "" {
		cfg.InternalElement = "internal"
	}
	if cfg.EventElement == "" {
		cfg.EventElement = "events"
	}
	wd, _ := os.Getwd()
	return &Application{
		config: cfg,
		wd:     wd,
		byName: map[string]*Module{},
		pkgs:   map[string]*Package{},
	}
}

// Module defines (or redefines) a module with the given name and package
// patterns. It returns the application so calls can be chained:
//
//	app := modulith.New().
//		Module("user", "./internal/user/...").
//		Module("order", "./internal/order/...")
//
// When patterns is empty and the module already exists, Module returns the
// application unchanged.
func (a *Application) Module(name string, patterns ...string) *Application {
	m, ok := a.byName[name]
	if !ok {
		m = &Module{
			Name:     name,
			Patterns: append([]string(nil), patterns...),
			pkgs:     map[string]*Package{},
		}
		a.byName[name] = m
		a.modules = append(a.modules, m)
	} else if len(patterns) > 0 {
		m.Patterns = append([]string(nil), patterns...)
	}
	// Derive the module root directory from the first pattern so that the
	// api-element convention can still be applied in explicit mode.
	if m.RootDir == "" {
		for _, p := range patterns {
			pat := filepath.ToSlash(strings.TrimPrefix(p, "./"))
			pat = strings.TrimSuffix(pat, "/...")
			if pat != "" && pat != "." {
				m.RootDir = pat
				break
			}
		}
	}
	return a
}

// ModuleRules returns the module with the given name so that dependency rules
// can be configured, or nil when the module does not exist.
func (a *Application) ModuleRules(name string) *Module {
	return a.byName[name]
}

// AllowDependencies declares that the module may depend on the named modules.
// Returns the module for chaining. When a module has allowed dependencies,
// undeclared module dependencies become verification violations.
func (m *Module) AllowDependencies(names ...string) *Module {
	for _, n := range names {
		if !contains(m.AllowedDependencies, n) {
			m.AllowedDependencies = append(m.AllowedDependencies, n)
		}
	}
	return m
}

// ForbidDependencies declares that the module must not depend on the named
// modules. Returns the module for chaining.
func (m *Module) ForbidDependencies(names ...string) *Module {
	for _, n := range names {
		if !contains(m.ForbiddenDependencies, n) {
			m.ForbiddenDependencies = append(m.ForbiddenDependencies, n)
		}
	}
	return m
}

// Public declares the public API packages of the module. Returns the module
// for chaining.
func (m *Module) Public(paths ...string) *Module {
	for _, p := range paths {
		if !contains(m.PublicPackages, p) {
			m.PublicPackages = append(m.PublicPackages, p)
		}
	}
	return m
}

// PublishEvents declares the names of the event types this module publishes.
// Each name must exist as an exported type within the module; otherwise the
// verification reports a missing-published-event error. Returns the module for
// chaining.
func (m *Module) PublishEvents(names ...string) *Module {
	for _, n := range names {
		if !contains(m.PublishedEvents, n) {
			m.PublishedEvents = append(m.PublishedEvents, n)
		}
	}
	return m
}

// EventDrivenFrom declares that this module may interact with the named
// modules only through events: every import into those modules must target one
// of their event packages. Returns the module for chaining.
func (m *Module) EventDrivenFrom(modules ...string) *Module {
	for _, n := range modules {
		if !contains(m.EventDrivenModules, n) {
			m.EventDrivenModules = append(m.EventDrivenModules, n)
		}
	}
	return m
}

// Load loads all packages matching the given patterns, discovers modules using
// the default convention, and returns the populated Application.
//
// The patterns use go/packages semantics, e.g. "./..." or
// "./internal/user/...". Use New (or LoadExplicit) when modules do not follow
// the default convention.
func Load(ctx context.Context, patterns ...string) (*Application, error) {
	return LoadIn(ctx, "", patterns...)
}

// LoadIn is like Load but resolves patterns and the module root relative to
// dir instead of the process working directory. It does not change the
// process working directory. Use it to analyse another checkout, such as a
// different git revision.
func LoadIn(ctx context.Context, dir string, patterns ...string) (*Application, error) {
	a := New()
	if err := a.loadIn(ctx, dir, patterns); err != nil {
		return nil, err
	}
	return a, nil
}

// LoadExplicit loads packages matching the given patterns and attaches them to
// the modules previously declared with Module. Packages that do not match any
// module are recorded as orphans.
func LoadExplicit(ctx context.Context, a *Application, patterns ...string) error {
	return LoadExplicitIn(ctx, "", a, patterns...)
}

// LoadExplicitIn is like LoadExplicit but resolves patterns relative to dir.
func LoadExplicitIn(ctx context.Context, dir string, a *Application, patterns ...string) error {
	if a == nil {
		return fmt.Errorf("modulith: application is nil")
	}
	if a.loaded {
		return fmt.Errorf("modulith: application already loaded")
	}
	all, err := loadPackagesIn(ctx, dir, patterns)
	if err != nil {
		return err
	}
	a.pkgs = map[string]*Package{}
	for _, p := range all {
		a.pkgs[p.ID] = p
	}
	if dir != "" {
		a.wd = dir
	}
	a.explicit = true
	a.classifyExplicit()
	a.loaded = true
	return nil
}

// classifyExplicit assigns packages to explicitly declared modules using their
// patterns converted to directory-prefix matchers.
func (a *Application) classifyExplicit() {
	type rule struct {
		module *Module
		dir    string // directory prefix relative to wd
		exact  bool
	}
	var rules []rule
	for _, m := range a.modules {
		for _, pat := range m.Patterns {
			pat = filepath.ToSlash(pat)
			if strings.HasSuffix(pat, "/...") {
				prefix := strings.TrimSuffix(pat, "/...")
				prefix = strings.TrimPrefix(prefix, "./")
				rules = append(rules, rule{module: m, dir: prefix})
			} else {
				pat = strings.TrimPrefix(pat, "./")
				rules = append(rules, rule{module: m, dir: pat, exact: true})
			}
		}
	}

	remaining := make(map[string]*Package, len(a.pkgs))
	for id, p := range a.pkgs {
		remaining[id] = p
	}

	for _, r := range rules {
		ids := make([]string, 0, len(remaining))
		for id := range remaining {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			p := remaining[id]
			rel := filepath.ToSlash(p.RelDir)
			if r.exact {
				if rel == r.dir {
					r.module.pkgs[id] = p
					delete(remaining, id)
				}
				continue
			}
			if r.dir == "" || rel == r.dir || strings.HasPrefix(rel, r.dir+"/") {
				r.module.pkgs[id] = p
				delete(remaining, id)
			}
		}
	}

	for _, id := range sortedKeys(remaining) {
		a.orphanPkgs = append(a.orphanPkgs, remaining[id])
	}

	// Explicitly defined modules still follow the api-element convention for
	// their public API packages (unless the caller declared Public() paths,
	// which are merged in later).
	for _, m := range a.modules {
		m.publicFromConvention(a.config)
	}
}

// load discovers modules from the convention and loads all packages relative
// to the process working directory.
func (a *Application) load(ctx context.Context, patterns []string) error {
	return a.loadIn(ctx, "", patterns)
}

// loadIn is load with an explicit working directory.
func (a *Application) loadIn(ctx context.Context, dir string, patterns []string) error {
	all, err := loadPackagesIn(ctx, dir, patterns)
	if err != nil {
		return err
	}
	a.pkgs = map[string]*Package{}
	for _, p := range all {
		a.pkgs[p.ID] = p
	}
	if dir != "" {
		a.wd = dir
	}
	if err := a.discoverModules(); err != nil {
		return err
	}
	a.classifyDiscovered()
	a.loaded = true
	return nil
}

// discoverModules builds Module entries from the default convention: every
// direct child directory of the module root (internal by default) that
// contains Go source files becomes a module.
func (a *Application) discoverModules() error {
	absRoot := a.config.ModuleRoot
	if !filepath.IsAbs(absRoot) {
		absRoot = filepath.Join(a.wd, absRoot)
	}
	dirs, err := os.ReadDir(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no module root: nothing to discover
		}
		return fmt.Errorf("modulith: read module root %q: %w", absRoot, err)
	}

	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		dir := filepath.Join(absRoot, name)
		if !hasGoFiles(dir) {
			continue
		}
		rel, err := filepath.Rel(a.wd, dir)
		if err != nil {
			continue
		}
		m := &Module{
			Name:    name,
			RootDir: filepath.ToSlash(rel),
			pkgs:    map[string]*Package{},
		}
		a.byName[name] = m
		a.modules = append(a.modules, m)
	}
	return nil
}

// classifyDiscovered assigns loaded packages to discovered modules using
// their on-disk directory relative to the working directory, and derives
// public API packages from the api-element convention.
func (a *Application) classifyDiscovered() {
	for _, m := range a.modules {
		prefix := m.RootDir
		for _, p := range a.pkgs {
			rel := filepath.ToSlash(p.RelDir)
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				m.pkgs[p.ID] = p
			}
		}
		m.publicFromConvention(a.config)
	}

	assigned := map[string]bool{}
	for _, m := range a.modules {
		for id := range m.pkgs {
			assigned[id] = true
		}
	}
	for _, id := range sortedKeys(a.pkgs) {
		if !assigned[id] {
			a.orphanPkgs = append(a.orphanPkgs, a.pkgs[id])
		}
	}
}

// publicFromConvention derives the public API packages of the module from the
// api-element convention: <root>/<api>/... packages are public; if none exist,
// the module root package itself (when present) is treated as public.
func (m *Module) publicFromConvention(cfg Config) {
	apiRel := m.RootDir + "/" + cfg.APIElement
	var hasAPI bool
	for id, p := range m.pkgs {
		rel := filepath.ToSlash(p.RelDir)
		if rel == apiRel || strings.HasPrefix(rel, apiRel+"/") {
			m.PublicPackages = append(m.PublicPackages, id)
			hasAPI = true
		}
	}
	sort.Strings(m.PublicPackages)
	if !hasAPI {
		for id, p := range m.pkgs {
			if filepath.ToSlash(p.RelDir) == m.RootDir {
				m.PublicPackages = append(m.PublicPackages, id)
			}
		}
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

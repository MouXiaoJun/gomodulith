package modulith

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/modfile"
)

// cacheSchemaVersion is bumped whenever the on-disk cache format changes.
const cacheSchemaVersion = 2

// DefaultCacheDir is the directory, relative to the working directory, used
// for the on-disk model cache when no explicit cache directory is given.
const DefaultCacheDir = ".gomodulith/cache"

// cachedModule is the serializable description of a module used to rebuild an
// Application from the cache.
type cachedModule struct {
	Name                  string   `json:"name"`
	RootDir               string   `json:"root_dir,omitempty"`
	Patterns              []string `json:"patterns,omitempty"`
	PublicPackages        []string `json:"public_packages,omitempty"`
	AllowedDependencies   []string `json:"allowed_dependencies,omitempty"`
	ForbiddenDependencies []string `json:"forbidden_dependencies,omitempty"`
	PublishedEvents       []string `json:"published_events,omitempty"`
	EventDrivenModules    []string `json:"event_driven_modules,omitempty"`
	PackageIDs            []string `json:"package_ids"`
}

// cachedModel is the serializable state of a loaded Application.
type cachedModel struct {
	Schema      int            `json:"schema"`
	Fingerprint string         `json:"fingerprint"`
	Config      Config         `json:"config"`
	WD          string         `json:"wd"`
	Explicit    bool           `json:"explicit"`
	Packages    []*Package     `json:"packages"`
	Modules     []cachedModule `json:"modules"`
	Orphans     []string       `json:"orphans,omitempty"`
}

// LoadCachedIn is like LoadIn but uses a disk cache under cacheDir (default
// ".gomodulith/cache" relative to the working directory). When the cache
// fingerprint matches the current source tree, the expensive go/packages load
// is skipped and the cached model is reused. The key includes source contents
// and the effective Go build environment. Unsupported build contexts (including
// workspaces and local replacements) fall back to an uncached load.
func LoadCachedIn(ctx context.Context, dir string, patterns []string, cacheDir string) (*Application, error) {
	return LoadCachedWithConfigIn(ctx, dir, NewConfig(), patterns, cacheDir)
}

// LoadCachedWithConfigIn is LoadCachedIn with an explicit configuration.
func LoadCachedWithConfigIn(ctx context.Context, dir string, cfg Config, patterns []string, cacheDir string) (*Application, error) {
	dir, cacheDir = resolveCacheDirs(dir, cacheDir)
	if app, ok := tryLoadCache(ctx, cacheDir, dir, cfg, patterns, nil); ok {
		return app, nil
	}
	app := NewWithConfig(cfg)
	if err := app.loadIn(ctx, dir, patterns); err != nil {
		return nil, err
	}
	saveCache(ctx, app, cacheDir, patterns, nil)
	return app, nil
}

// LoadExplicitCached is like LoadExplicitIn but uses a disk cache under
// cacheDir. The declared modules and rules on a are part of the cache key.
func LoadExplicitCached(ctx context.Context, dir string, a *Application, patterns []string, cacheDir string) error {
	if a == nil {
		return fmt.Errorf("modulith: application is nil")
	}
	if a.loaded {
		return fmt.Errorf("modulith: application already loaded")
	}
	dir, cacheDir = resolveCacheDirs(dir, cacheDir)
	// Key defs are the declaration-time rules (package membership is filled in
	// after loading, so it must not be part of the key).
	keyDefs := a.ruleDefs()
	if app, ok := tryLoadCache(ctx, cacheDir, dir, a.config, patterns, keyDefs); ok {
		a.restore(app)
		return nil
	}
	if err := LoadExplicitIn(ctx, dir, a, patterns...); err != nil {
		return err
	}
	saveCache(ctx, a, cacheDir, patterns, keyDefs)
	return nil
}

// resolveCacheDirs returns the effective working directory and cache directory.
func resolveCacheDirs(dir, cacheDir string) (string, string) {
	wd := dir
	if wd == "" {
		wd, _ = os.Getwd()
	}
	if cacheDir == "" {
		cacheDir = filepath.Join(wd, DefaultCacheDir)
	}
	return wd, cacheDir
}

// ruleDefs snapshots the declared module rules without package membership.
// Used as part of the cache key for explicit-mode applications.
func (a *Application) ruleDefs() []cachedModule {
	out := make([]cachedModule, 0, len(a.modules))
	for _, m := range a.modules {
		out = append(out, cachedModule{
			Name:                  m.Name,
			RootDir:               m.RootDir,
			Patterns:              append([]string(nil), m.Patterns...),
			PublicPackages:        append([]string(nil), m.PublicPackages...),
			AllowedDependencies:   append([]string(nil), m.AllowedDependencies...),
			ForbiddenDependencies: append([]string(nil), m.ForbiddenDependencies...),
			PublishedEvents:       append([]string(nil), m.PublishedEvents...),
			EventDrivenModules:    append([]string(nil), m.EventDrivenModules...),
		})
	}
	return out
}

// cachedModuleDefs snapshots the full module state including package
// membership, for storing in the cache.
func (a *Application) cachedModuleDefs() []cachedModule {
	out := a.ruleDefs()
	for i, m := range a.modules {
		out[i].PackageIDs = sortedKeys(m.pkgs)
	}
	return out
}

// tryLoadCache loads and validates the cache for the given key. It returns
// (app, true) on a cache hit.
func tryLoadCache(ctx context.Context, cacheDir, wd string, config Config, patterns []string, keyDefs []cachedModule) (*Application, bool) {
	data, err := os.ReadFile(filepath.Join(cacheDir, "model.json"))
	if err != nil {
		return nil, false
	}
	var cm cachedModel
	if err := json.Unmarshal(data, &cm); err != nil {
		return nil, false
	}
	if cm.Schema != cacheSchemaVersion || cm.WD != wd {
		return nil, false
	}
	fp, err := computeFingerprint(ctx, wd, config, patterns, keyDefs)
	if err != nil {
		return nil, false
	}
	if cm.Fingerprint != fp {
		return nil, false
	}
	app := &Application{
		config:   cm.Config,
		wd:       cm.WD,
		explicit: cm.Explicit,
		loaded:   true,
		byName:   map[string]*Module{},
		pkgs:     map[string]*Package{},
	}
	for _, p := range cm.Packages {
		app.pkgs[p.ID] = p
	}
	for _, md := range cm.Modules {
		m := &Module{
			Name:                  md.Name,
			RootDir:               md.RootDir,
			Patterns:              md.Patterns,
			PublicPackages:        md.PublicPackages,
			AllowedDependencies:   md.AllowedDependencies,
			ForbiddenDependencies: md.ForbiddenDependencies,
			PublishedEvents:       md.PublishedEvents,
			EventDrivenModules:    md.EventDrivenModules,
			pkgs:                  map[string]*Package{},
		}
		for _, id := range md.PackageIDs {
			if p, ok := app.pkgs[id]; ok {
				m.pkgs[id] = p
			}
		}
		app.byName[m.Name] = m
		app.modules = append(app.modules, m)
	}
	for _, id := range cm.Orphans {
		if p, ok := app.pkgs[id]; ok {
			app.orphanPkgs = append(app.orphanPkgs, p)
		}
	}
	return app, true
}

// restore replaces the receiver's state with a cached application. It is used
// to load a cached model into a pre-declared (explicit) Application.
func (a *Application) restore(from *Application) {
	a.config = from.config
	a.wd = from.wd
	a.explicit = from.explicit
	a.loaded = true
	a.byName = from.byName
	a.modules = from.modules
	a.pkgs = from.pkgs
	a.orphanPkgs = from.orphanPkgs
}

// saveCache writes the application state to the cache directory, ignoring
// errors (the cache is an optimisation, never a correctness requirement).
func saveCache(ctx context.Context, a *Application, cacheDir string, patterns []string, keyDefs []cachedModule) {
	fp, err := computeFingerprint(ctx, a.wd, a.config, patterns, keyDefs)
	if err != nil {
		return
	}
	cm := cachedModel{
		Schema:      cacheSchemaVersion,
		Fingerprint: fp,
		Config:      a.config,
		WD:          a.wd,
		Explicit:    a.explicit,
		Packages:    a.Packages(),
		Modules:     a.cachedModuleDefs(),
	}
	for _, p := range a.orphanPkgs {
		cm.Orphans = append(cm.Orphans, p.ID)
	}
	data, err := json.Marshal(cm)
	if err != nil {
		return
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(cacheDir, "model.json"), data, 0o644)
}

// computeFingerprint supports module-local source trees. If inputs outside
// that tree cannot be tracked, returning an error disables cache reads/writes.
func computeFingerprint(ctx context.Context, wd string, config Config, patterns []string, keyDefs []cachedModule) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "-json")
	cmd.Dir = wd
	data, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		return "", err
	}
	// GOGCCFLAGS contains a fresh temporary directory on every invocation;
	// its build-relevant inputs are already present in the other env values.
	delete(env, "GOGCCFLAGS")
	root, err := filepath.EvalSymlinks(wd)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	modPath, err := filepath.EvalSymlinks(env["GOMOD"])
	if err != nil {
		return "", err
	}
	if modPath != filepath.Join(root, "go.mod") || (env["GOWORK"] != "" && env["GOWORK"] != "off") ||
		strings.Contains(env["GOFLAGS"], "-overlay") || strings.Contains(env["GOFLAGS"], "-modfile") {
		return "", fmt.Errorf("cache does not track this build context")
	}
	if _, err := os.Stat(filepath.Join(wd, "vendor")); !os.IsNotExist(err) {
		return "", fmt.Errorf("cache does not track vendor trees")
	}
	if driver := os.Getenv("GOPACKAGESDRIVER"); driver != "off" {
		_, err := exec.LookPath("gopackagesdriver")
		if driver != "" || err == nil {
			return "", fmt.Errorf("cache does not track external package drivers")
		}
	}
	for _, pattern := range patterns {
		if pattern != "." && !strings.HasPrefix(pattern, "./") {
			return "", fmt.Errorf("cache requires module-local patterns")
		}
		if rel := filepath.Clean(pattern); rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("cache requires module-local patterns")
		}
	}
	modData, err := os.ReadFile(modPath)
	if err != nil {
		return "", err
	}
	mod, err := modfile.Parse(modPath, modData, nil)
	if err != nil {
		return "", err
	}
	for _, replacement := range mod.Replace {
		if replacement.New.Version == "" {
			return "", fmt.Errorf("cache does not track local replacement modules")
		}
	}
	h := sha256.New()

	write := func(s string) {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	for _, f := range []string{"go.mod", "go.sum"} {
		if data, err := os.ReadFile(filepath.Join(wd, f)); err == nil {
			write(f)
			write(string(data))
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	buildEnv, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	write(string(buildEnv))
	write(runtime.Version())
	write(config.String())
	for _, p := range patterns {
		write(p)
	}
	if keyDefs != nil {
		enc, _ := json.Marshal(keyDefs)
		write(string(enc))
	}

	// WalkDir visits entries in lexical order. Read content rather than file
	// metadata so restored mtimes and equal-sized edits still invalidate.
	err = filepath.WalkDir(wd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != wd && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("cache does not track symlinked sources")
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, rerr := filepath.Rel(wd, path)
		if rerr != nil {
			return rerr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		write(filepath.ToSlash(rel))
		write(string(data))
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

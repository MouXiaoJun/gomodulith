package modulith

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// ConfigFormat identifies a configuration file format.
type ConfigFormat string

const (
	// ConfigYAML is the YAML configuration format (.gomodulith.yaml).
	ConfigYAML ConfigFormat = "yaml"
	// ConfigTOML is the TOML configuration format (.gomodulith.toml).
	ConfigTOML ConfigFormat = "toml"
)

// FileConfig is the on-disk project configuration (.gomodulith.yaml or
// .gomodulith.toml). It is a strict superset of Config plus optional explicit
// module definitions.
type FileConfig struct {
	// ModuleRoot overrides the convention module root directory.
	ModuleRoot string `yaml:"module_root" toml:"module_root"`

	// APIElement overrides the public-API sub-package element.
	APIElement string `yaml:"api_element" toml:"api_element"`

	// ReportOrphans enables the orphan-package warning.
	ReportOrphans bool `yaml:"report_orphans" toml:"report_orphans"`

	// PublicAPIRequired makes missing public APIs a verification error.
	PublicAPIRequired bool `yaml:"public_api_required" toml:"public_api_required"`

	// Patterns are the default package patterns to load when none are given
	// on the command line.
	Patterns []string `yaml:"patterns" toml:"patterns"`

	// Modules optionally declares explicit modules and their rules. When
	// non-empty, the application runs in explicit (rule-driven) mode.
	Modules map[string]ModuleConfig `yaml:"modules" toml:"modules"`
}

// ModuleConfig is the on-disk description of a single module.
type ModuleConfig struct {
	// Patterns are the package patterns belonging to the module.
	Patterns []string `yaml:"patterns" toml:"patterns"`

	// Public are the module's public API packages.
	Public []string `yaml:"public" toml:"public"`

	// Allowed are the module names this module may depend on.
	Allowed []string `yaml:"allowed" toml:"allowed"`

	// Forbidden are the module names this module may not depend on.
	Forbidden []string `yaml:"forbidden" toml:"forbidden"`
}

// configFileNames are the recognised configuration file names, in discovery
// order.
var configFileNames = []string{
	".gomodulith.yaml",
	".gomodulith.yml",
	".gomodulith.toml",
}

// FindConfigFile returns the path of the first configuration file found in
// dir (or its parent directories), or "" when none exists.
func FindConfigFile(dir string) string {
	for {
		for _, name := range configFileNames {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// LoadConfigFile reads and parses a .gomodulith configuration file. The
// format is inferred from the file extension (.yaml/.yml or .toml).
func LoadConfigFile(path string) (*FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("modulith: read config %s: %w", path, err)
	}
	format := ConfigYAML
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		format = ConfigTOML
	}
	fc, err := ParseConfig(data, format)
	if err != nil {
		return nil, fmt.Errorf("modulith: parse config %s: %w", path, err)
	}
	return fc, nil
}

// ParseConfig parses configuration bytes in the given format.
func ParseConfig(data []byte, format ConfigFormat) (*FileConfig, error) {
	fc := &FileConfig{}
	switch format {
	case ConfigTOML:
		if err := toml.Unmarshal(data, fc); err != nil {
			return nil, err
		}
	case ConfigYAML:
		if err := yaml.Unmarshal(data, fc); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported config format %q", format)
	}
	if err := fc.validate(); err != nil {
		return nil, err
	}
	return fc, nil
}

func (fc *FileConfig) validate() error {
	for name, m := range fc.Modules {
		if name == "" {
			return fmt.Errorf("module with empty name")
		}
		for _, p := range m.Public {
			if p == "" {
				return fmt.Errorf("module %q has an empty public package", name)
			}
		}
	}
	return nil
}

// config returns the discovery/verification Config derived from the file.
func (fc *FileConfig) config() Config {
	cfg := NewConfig()
	if fc.ModuleRoot != "" {
		cfg.ModuleRoot = fc.ModuleRoot
	}
	if fc.APIElement != "" {
		cfg.APIElement = fc.APIElement
	}
	cfg.ReportOrphans = fc.ReportOrphans
	cfg.PublicAPIRequired = fc.PublicAPIRequired
	return cfg
}

// LoadPatterns returns the patterns to load: the explicit ones if given,
// otherwise the ones configured in the file, otherwise "./...".
func (fc *FileConfig) LoadPatterns(explicit []string) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if len(fc.Patterns) > 0 {
		return fc.Patterns
	}
	return []string{"./..."}
}

// Build constructs and loads an Application from the configuration. When the
// file declares modules, the application runs in explicit mode; otherwise
// convention discovery is used. dir is the working directory ("" for the
// process working directory).
func (fc *FileConfig) Build(ctx context.Context, dir string, patterns []string) (*Application, error) {
	patterns = fc.LoadPatterns(patterns)
	if len(fc.Modules) == 0 {
		app := NewWithConfig(fc.config())
		if err := app.loadIn(ctx, dir, patterns); err != nil {
			return nil, err
		}
		return app, nil
	}

	app := NewWithConfig(fc.config())
	// Deterministic module order for stable output.
	names := make([]string, 0, len(fc.Modules))
	for n := range fc.Modules {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		mc := fc.Modules[n]
		app.Module(n, mc.Patterns...)
		app.ModuleRules(n).AllowDependencies(mc.Allowed...)
		app.ModuleRules(n).ForbidDependencies(mc.Forbidden...)
		if len(mc.Public) > 0 {
			app.ModuleRules(n).Public(mc.Public...)
		}
	}
	if err := LoadExplicitIn(ctx, dir, app, patterns...); err != nil {
		return nil, err
	}
	return app, nil
}

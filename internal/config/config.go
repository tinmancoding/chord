// Package config handles parsing and validation of chord.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// TemplateRepo defines a repository within a template.
type TemplateRepo struct {
	URL           string `yaml:"url"`
	DefaultBranch string `yaml:"default_branch"` // Used when --branch is specified without per-repo override
	Name          string `yaml:"name"`           // Optional: directory name (defaults to repo name from URL)
}

// Template defines a frequently used repository group with defaults.
type Template struct {
	Namespace string         `yaml:"namespace"` // Optional: default namespace for this template
	Repos     []TemplateRepo `yaml:"repos"`
}

// Config is the top-level structure of chord.yaml.
type Config struct {
	BaseDirectory    string              `yaml:"base_directory"`
	DefaultNamespace string              `yaml:"default_namespace"` // Optional: defaults to "default"
	Templates        map[string]Template `yaml:"templates"`
	Aliases          map[string]string   `yaml:"aliases"` // Optional: short names for repository URLs
}

// DefaultPath returns the canonical config file location:
// ~/.config/chord/chord.yaml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "chord", "chord.yaml"), nil
}

// DefaultBaseDir returns the default workspace base directory: ~/chord.
func DefaultBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, "chord"), nil
}

// expandPath resolves a leading "~" to the user's home directory.
func expandPath(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}

// EffectiveBaseDir returns the base directory to use, with the following
// precedence (highest to lowest):
//  1. cliOverride — the value of the --base-dir flag (non-empty string)
//  2. c.BaseDirectory — the base_directory field in chord.yaml (non-empty)
//  3. DefaultBaseDir() — ~/chord
func (c *Config) EffectiveBaseDir(cliOverride string) (string, error) {
	raw := cliOverride
	if raw == "" {
		raw = c.BaseDirectory
	}
	if raw == "" {
		return DefaultBaseDir()
	}
	return expandPath(raw)
}

// Load reads and parses a chord.yaml file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// ParseChordName splits a chord name into namespace and name components.
// The last "/" separates the chord name from the namespace path.
// Examples:
//
//	"my-workspace" -> ("", "my-workspace")
//	"work/my-workspace" -> ("work", "my-workspace")
//	"work/team-a/my-workspace" -> ("work/team-a", "my-workspace")
func ParseChordName(chordName string) (namespace, name string) {
	idx := strings.LastIndex(chordName, "/")
	if idx == -1 {
		return "", chordName
	}
	return chordName[:idx], chordName[idx+1:]
}

// ResolveNamespace determines the effective namespace for a chord.
// Precedence (highest to lowest):
//  1. cliNamespace - the --namespace flag value
//  2. Namespace prefix in chordName (e.g., "work/team-a/my-workspace")
//  3. Template-specific namespace in config
//  4. c.DefaultNamespace from config
//  5. Built-in default: "default"
func (c *Config) ResolveNamespace(chordName, templateName, cliNamespace string) string {
	// 1. CLI flag wins
	if cliNamespace != "" {
		return cliNamespace
	}

	// 2. Prefix in chord name
	if ns, _ := ParseChordName(chordName); ns != "" {
		return ns
	}

	// 3. Template-specific namespace in config
	if templateName != "" {
		if template, ok := c.Templates[templateName]; ok && template.Namespace != "" {
			return template.Namespace
		}
	}

	// 4. Default namespace from config
	if c.DefaultNamespace != "" {
		return c.DefaultNamespace
	}

	// 5. Built-in default
	return "default"
}

// HasTemplate returns true if the given name exists as a template.
func (c *Config) HasTemplate(name string) bool {
	_, ok := c.Templates[name]
	return ok
}

// validate checks the config for structural integrity.
func (c *Config) validate() error {
	// Templates are optional - ad-hoc composition is always available
	for templateName, template := range c.Templates {
		if len(template.Repos) == 0 {
			return fmt.Errorf("template %q has no repos defined", templateName)
		}
		for i, repo := range template.Repos {
			if repo.URL == "" {
				return fmt.Errorf("template %q repo[%d] missing URL", templateName, i)
			}
		}
	}
	return nil
}

// GetTemplate returns a template by name or an error if it doesn't exist.
func (c *Config) GetTemplate(name string) (Template, error) {
	t, ok := c.Templates[name]
	if !ok {
		return Template{}, fmt.Errorf("unknown template %q", name)
	}
	return t, nil
}

// ResolveAlias resolves a repository alias to its full URL.
// If the input is not an alias, returns it unchanged.
func (c *Config) ResolveAlias(aliasOrURL string) string {
	if url, ok := c.Aliases[aliasOrURL]; ok {
		return url
	}
	return aliasOrURL
}

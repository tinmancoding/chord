// Package config handles parsing and validation of chord.yaml.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// RepositoryDef defines a single git repository.
type RepositoryDef struct {
	URL           string `yaml:"url"`
	DefaultBranch string `yaml:"default_branch"`
}

// ProjectDef defines a logical grouping of repositories.
type ProjectDef struct {
	Repos []string `yaml:"repos"`
}

// Config is the top-level structure of chord.yaml.
type Config struct {
	Repositories map[string]RepositoryDef `yaml:"repositories"`
	Projects     map[string]ProjectDef    `yaml:"projects"`
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

// validate checks the config for structural integrity.
func (c *Config) validate() error {
	if len(c.Repositories) == 0 {
		return fmt.Errorf("no repositories defined")
	}
	if len(c.Projects) == 0 {
		return fmt.Errorf("no projects defined")
	}
	for projectID, project := range c.Projects {
		if len(project.Repos) == 0 {
			return fmt.Errorf("project %q has no repos defined", projectID)
		}
		for _, repoID := range project.Repos {
			if _, ok := c.Repositories[repoID]; !ok {
				return fmt.Errorf("project %q references unknown repository %q", projectID, repoID)
			}
		}
	}
	return nil
}

// GetProject returns a project by ID or an error if it doesn't exist.
func (c *Config) GetProject(projectID string) (ProjectDef, error) {
	p, ok := c.Projects[projectID]
	if !ok {
		return ProjectDef{}, fmt.Errorf("unknown project %q", projectID)
	}
	return p, nil
}

// GetRepository returns a repository definition by ID.
func (c *Config) GetRepository(repoID string) (RepositoryDef, error) {
	r, ok := c.Repositories[repoID]
	if !ok {
		return RepositoryDef{}, fmt.Errorf("unknown repository %q", repoID)
	}
	return r, nil
}

// Package workspace manages chord workspace state on disk.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const stateFileName = ".chord-state.yaml"

// RepoState tracks the resolved state of a single repo within a workspace.
type RepoState struct {
	// Name is the directory name for this repo (e.g. "api-server").
	Name string `yaml:"name"`
	// URL is the git repository URL.
	URL string `yaml:"url"`
	// ExpectedBranch is the branch that was checked out at compose time.
	// This is what chord check compares against (not the raw commitish).
	ExpectedBranch string `yaml:"expected_branch"`
	// Path is the absolute path to this repo's full clone.
	Path string `yaml:"path"`
	// BaseClonePath is the absolute path to the backing base clone cache.
	BaseClonePath string `yaml:"base_clone_path"`
}

// DeferredRepoState tracks repositories that haven't been created yet.
type DeferredRepoState struct {
	// Name is the directory name for this repo (e.g. "api-server").
	Name string `yaml:"name"`
	// URL is the git repository URL.
	URL string `yaml:"url"`
	// ExpectedBranch is the branch we're waiting for.
	ExpectedBranch string `yaml:"expected_branch"`
	// Reason describes why this repo was deferred (e.g. "user-deferred").
	Reason string `yaml:"reason"`
	// LastChecked is when tune last checked for the remote branch.
	LastChecked time.Time `yaml:"last_checked"`
}

// State is the full workspace state, written to .chord-state.yaml.
type State struct {
	// ChordName is the unique name of this chord within its namespace.
	ChordName string `yaml:"chord_name"`
	// Namespace is the organizational namespace for this workspace (can contain slashes).
	Namespace string `yaml:"namespace"`
	// TemplateName is the template used (if any).
	TemplateName string `yaml:"template_name,omitempty"`
	// WorkspaceDir is the root directory of the workspace.
	WorkspaceDir string `yaml:"workspace_dir"`
	// Repos holds per-repo resolved state.
	Repos []RepoState `yaml:"repos"`
	// DeferredRepos holds repos that haven't been created yet.
	DeferredRepos []DeferredRepoState `yaml:"deferred_repos,omitempty"`
}

// Save writes the state to a .chord-state.yaml file in the workspace directory.
func (s *State) Save() error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal workspace state: %w", err)
	}
	path := filepath.Join(s.WorkspaceDir, stateFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write workspace state to %q: %w", path, err)
	}
	return nil
}

// LoadState reads the .chord-state.yaml from dir or any parent directory.
// It walks up the directory tree until it finds the state file or reaches the root.
func LoadState(dir string) (*State, error) {
	stateFile, err := findStateFile(dir)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, fmt.Errorf("could not read workspace state at %q: %w", stateFile, err)
	}
	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse workspace state: %w", err)
	}
	return &s, nil
}

// findStateFile searches for .chord-state.yaml starting from dir and walking up parent directories.
func findStateFile(dir string) (string, error) {
	// Get absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("could not get absolute path: %w", err)
	}

	current := absDir
	for {
		candidate := filepath.Join(current, stateFileName)
		if _, err := os.Stat(candidate); err == nil {
			// Found it!
			return candidate, nil
		}

		// Move to parent directory
		parent := filepath.Dir(current)

		// If we've reached the root, stop
		if parent == current {
			break
		}
		current = parent
	}

	return "", fmt.Errorf("could not find %s in %q or any parent directory — are you inside a chord workspace?", stateFileName, absDir)
}

// WorkspacePath returns the canonical path for a chord workspace.
// Flat structure: <baseDir>/<namespace>/<chordName>
// Namespaces can contain slashes for hierarchical organization.
// Examples:
//   - ~/chord/work/feat-123
//   - ~/chord/work/team-a/sprint-42
//   - ~/chord/personal/side-projects/blog-redesign
func WorkspacePath(baseDir, namespace, chordName string) string {
	return filepath.Join(baseDir, namespace, chordName)
}

// BaseCacheDir returns the root path where base clones are stored.
// Defaults to ~/.cache/chord/repos/
func BaseCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "chord", "repos"), nil
}

// CachePathForRepo returns the cache path for a repository based on its URL.
// Uses a sanitized version of the repo name extracted from the URL.
func CachePathForRepo(repoURL string) (string, error) {
	base, err := BaseCacheDir()
	if err != nil {
		return "", err
	}
	// Extract repo name from URL
	name := ExtractRepoName(repoURL)
	return filepath.Join(base, name), nil
}

// ExtractRepoName extracts a sanitized repository name from a URL.
// Examples:
//   - git@github.com:org/repo.git -> org-repo
//   - https://github.com/org/repo.git -> org-repo
//   - https://github.com/org/repo -> org-repo
func ExtractRepoName(url string) string {
	// Remove .git suffix
	url = strings.TrimSuffix(url, ".git")

	// Extract path portion
	var path string
	if strings.Contains(url, "://") {
		// HTTP(S) URL
		parts := strings.SplitN(url, "://", 2)
		if len(parts) == 2 {
			// Remove host, keep path
			pathParts := strings.SplitN(parts[1], "/", 2)
			if len(pathParts) == 2 {
				path = pathParts[1]
			}
		}
	} else if strings.Contains(url, ":") {
		// SSH URL (git@host:path)
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			path = parts[1]
		}
	} else {
		path = url
	}

	// Sanitize: replace / and other problematic chars with -
	path = strings.ReplaceAll(path, "/", "-")
	path = strings.ReplaceAll(path, " ", "-")

	return path
}

// RemoveDeferred removes a repo from the deferred list by name.
func (s *State) RemoveDeferred(name string) {
	filtered := make([]DeferredRepoState, 0, len(s.DeferredRepos))
	for _, d := range s.DeferredRepos {
		if d.Name != name {
			filtered = append(filtered, d)
		}
	}
	s.DeferredRepos = filtered
}

// UpdateLastChecked updates the last_checked timestamp for a deferred repo.
func (s *State) UpdateLastChecked(name string) {
	for i := range s.DeferredRepos {
		if s.DeferredRepos[i].Name == name {
			s.DeferredRepos[i].LastChecked = time.Now()
			return
		}
	}
}

// IsDeferred checks if a repo is in the deferred list.
func (s *State) IsDeferred(name string) bool {
	for _, d := range s.DeferredRepos {
		if d.Name == name {
			return true
		}
	}
	return false
}

// ScanWorkspaces scans the base directory for all workspace state files.
// Returns a slice of all found workspace states.
func ScanWorkspaces(baseDir string) ([]*State, error) {
	var states []*State

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip directories we can't access
			return nil
		}

		if info.Name() == stateFileName {
			state, err := LoadState(filepath.Dir(path))
			if err == nil {
				states = append(states, state)
			}
			// Don't descend into workspace directories
			return filepath.SkipDir
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("scan workspaces: %w", err)
	}

	return states, nil
}

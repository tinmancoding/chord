// Package workspace manages chord workspace state on disk.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const stateFileName = ".chord-state.yaml"

// RepoState tracks the resolved state of a single repo within a workspace.
type RepoState struct {
	// RepoID is the logical name from chord.yaml (e.g. "api-server").
	RepoID string `yaml:"repo_id"`
	// ExpectedBranch is the branch that was checked out at compose time.
	// This is what chord check compares against (not the raw target_branch).
	ExpectedBranch string `yaml:"expected_branch"`
	// WorktreePath is the absolute path to this repo's worktree.
	WorktreePath string `yaml:"worktree_path"`
	// BaseClonePath is the absolute path to the backing base clone.
	BaseClonePath string `yaml:"base_clone_path"`
}

// DeferredRepoState tracks repositories that haven't been created yet.
type DeferredRepoState struct {
	// RepoID is the logical name from chord.yaml (e.g. "api-server").
	RepoID string `yaml:"repo_id"`
	// Reason describes why this repo was deferred (e.g. "user-deferred").
	Reason string `yaml:"reason"`
	// LastChecked is when tune last checked for the remote branch.
	LastChecked time.Time `yaml:"last_checked"`
}

// State is the full workspace state, written to .chord-state.yaml.
type State struct {
	// ProjectID is the logical project name (e.g. "fullstack").
	ProjectID string `yaml:"project_id"`
	// TargetBranch is the user-supplied branch argument (e.g. "main", "feature-x").
	TargetBranch string `yaml:"target_branch"`
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

// LoadState reads the .chord-state.yaml from dir.
func LoadState(dir string) (*State, error) {
	path := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read workspace state at %q — are you inside a chord workspace?: %w", path, err)
	}
	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse workspace state: %w", err)
	}
	return &s, nil
}

// WorkspacePath returns the canonical path for a workspace:
// <baseDir>/<projectID>/<targetBranch>
func WorkspacePath(baseDir, projectID, targetBranch string) string {
	return filepath.Join(baseDir, projectID, targetBranch)
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

// BaseClonePath returns the path for the base clone of a specific repo.
func BaseClonePath(repoID string) (string, error) {
	base, err := BaseCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, repoID), nil
}

// RemoveDeferred removes a repo from the deferred list by repoID.
func (s *State) RemoveDeferred(repoID string) {
	filtered := make([]DeferredRepoState, 0, len(s.DeferredRepos))
	for _, d := range s.DeferredRepos {
		if d.RepoID != repoID {
			filtered = append(filtered, d)
		}
	}
	s.DeferredRepos = filtered
}

// UpdateLastChecked updates the last_checked timestamp for a deferred repo.
func (s *State) UpdateLastChecked(repoID string) {
	for i := range s.DeferredRepos {
		if s.DeferredRepos[i].RepoID == repoID {
			s.DeferredRepos[i].LastChecked = time.Now()
			return
		}
	}
}

// IsDeferred checks if a repo is in the deferred list.
func (s *State) IsDeferred(repoID string) bool {
	for _, d := range s.DeferredRepos {
		if d.RepoID == repoID {
			return true
		}
	}
	return false
}

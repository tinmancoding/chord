// Package git wraps raw git CLI operations needed by chord.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Repo represents a git repository on disk (the base clone).
type Repo struct {
	// Path is the absolute path to the repository root.
	Path string
}

// CacheMeta stores metadata about a cached repository.
type CacheMeta struct {
	RepoID    string    `yaml:"repo_id"`
	URL       string    `yaml:"url"`
	CreatedAt time.Time `yaml:"created_at"`
	LastUsed  time.Time `yaml:"last_used"`
}

// loadCacheMeta reads cache metadata from the .chord-cache-meta file.
func loadCacheMeta(metaPath string) (*CacheMeta, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read cache meta: %w", err)
	}

	var meta CacheMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse cache meta: %w", err)
	}

	return &meta, nil
}

// saveCacheMeta writes cache metadata to the .chord-cache-meta file.
func saveCacheMeta(metaPath string, meta *CacheMeta) error {
	data, err := yaml.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal cache meta: %w", err)
	}

	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write cache meta: %w", err)
	}

	return nil
}

// EnsureCache ensures a bare clone exists in the cache directory for the given repo.
// If the cache exists, it validates the URL matches. If not, it creates a new cache.
// Returns the Repo representing the cache location.
func EnsureCache(repoID, url, cacheDir string) (*Repo, error) {
	cachePath := filepath.Join(cacheDir, repoID)
	metaPath := filepath.Join(cachePath, ".chord-cache-meta")

	// If cache exists, validate URL
	if _, err := os.Stat(cachePath); err == nil {
		// Check if metadata file exists
		meta, err := loadCacheMeta(metaPath)
		if err != nil {
			// Metadata file doesn't exist (likely v1 cache) - check if it's a valid git repo
			if isValidGitRepo(cachePath) {
				// Valid git repo from v1, create metadata file
				meta = &CacheMeta{
					RepoID:    repoID,
					URL:       url,
					CreatedAt: time.Now(), // We don't know the real creation time
					LastUsed:  time.Now(),
				}
				if err := saveCacheMeta(metaPath, meta); err != nil {
					// Non-fatal: cache still works
					fmt.Fprintf(os.Stderr, "Warning: failed to create cache metadata: %v\n", err)
				}
				return &Repo{Path: cachePath}, nil
			}
			// Not a valid git repo, something is wrong
			return nil, fmt.Errorf("cache directory exists but is not a valid git repository: %s", cachePath)
		}

		// Metadata exists, validate URL
		if meta.URL != url {
			return nil, fmt.Errorf(
				"cache conflict for '%s':\n  Cached URL:     %s\n  Configured URL: %s\n\n"+
					"Solutions:\n"+
					"  1. Use a different repo_id in config\n"+
					"  2. Remove cache: rm -rf %s\n"+
					"  3. Update config to match cached URL",
				repoID, meta.URL, url, cachePath)
		}

		// Update last_used
		meta.LastUsed = time.Now()
		if err := saveCacheMeta(metaPath, meta); err != nil {
			// Non-fatal: just log and continue
			fmt.Fprintf(os.Stderr, "Warning: failed to update cache metadata: %v\n", err)
		}

		return &Repo{Path: cachePath}, nil
	}

	// Create new cache
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	// Bare clone
	repo, err := Clone(url, cachePath)
	if err != nil {
		return nil, err
	}

	// Save metadata
	meta := &CacheMeta{
		RepoID:    repoID,
		URL:       url,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}
	if err := saveCacheMeta(metaPath, meta); err != nil {
		// Non-fatal: cache is functional even without metadata
		fmt.Fprintf(os.Stderr, "Warning: failed to save cache metadata: %v\n", err)
	}

	return repo, nil
}

// isValidGitRepo checks if a directory is a valid git repository.
func isValidGitRepo(path string) bool {
	// Check if the directory has a HEAD file (bare repos have this in root)
	headPath := filepath.Join(path, "HEAD")
	if _, err := os.Stat(headPath); err == nil {
		return true
	}
	// Check if there's a .git directory (non-bare repos)
	gitPath := filepath.Join(path, ".git")
	if _, err := os.Stat(gitPath); err == nil {
		return true
	}
	return false
}

// New returns a Repo for the given path. Does not validate the path.
func New(path string) *Repo {
	return &Repo{Path: path}
}

// Clone performs a bare clone of the given URL into dest.
// Bare clones have no working directory, so any branch can be freely
// assigned to a worktree without "already checked out" conflicts.
// After cloning, we configure the remote fetch refspec so that
// `git fetch --all` populates refs/remotes/origin/* correctly.
func Clone(url, dest string) (*Repo, error) {
	if err := run("", "git", "clone", "--bare", url, dest); err != nil {
		return nil, fmt.Errorf("bare clone %q → %q: %w", url, dest, err)
	}
	repo := &Repo{Path: dest}
	// Bare clones don't set up the remote tracking refspec by default.
	if err := repo.run("git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return nil, fmt.Errorf("configure fetch refspec in %q: %w", dest, err)
	}
	return repo, nil
}

// CloneWithReference creates a full git clone using the bare clone as an object reference.
// This speeds up cloning by using local objects instead of fetching from remote.
// The dest directory will be a full working clone with origin pointing to the actual remote URL.
func (r *Repo) CloneWithReference(url, dest, branch string) error {
	// Clone with reference to the cache (without checking out yet)
	cmd := exec.Command("git", "clone",
		"--reference", r.Path,
		"--no-checkout",
		url,
		dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clone with reference %q → %q: %w", url, dest, err)
	}

	// Ensure origin points to actual remote (not cache)
	cmd = exec.Command("git", "-C", dest, "remote", "set-url", "origin", url)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("set origin URL in %q: %w", dest, err)
	}

	// Add cache as a remote to fetch local branches
	cmd = exec.Command("git", "-C", dest, "remote", "add", "cache", r.Path)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("add cache remote in %q: %w", dest, err)
	}

	// Fetch all branches from cache (including local-only branches)
	cmd = exec.Command("git", "-C", dest, "fetch", "cache")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fetch from cache in %q: %w", dest, err)
	}

	// Check if branch exists locally in dest
	cmd = exec.Command("git", "-C", dest, "branch", "--list", branch)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("check branch %q in %q: %w", branch, dest, err)
	}

	// If branch doesn't exist locally, check if it exists in cache and create tracking branch
	if strings.TrimSpace(string(out)) == "" {
		cmd = exec.Command("git", "-C", dest, "branch", "--list", "-r", "cache/"+branch)
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("check cache branch %q in %q: %w", branch, dest, err)
		}
		if strings.TrimSpace(string(out)) != "" {
			// Branch exists in cache, create local branch tracking it
			cmd = exec.Command("git", "-C", dest, "branch", branch, "cache/"+branch)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("create branch %q from cache in %q: %w", branch, dest, err)
			}
		}
	}

	// Checkout the branch
	cmd = exec.Command("git", "-C", dest, "checkout", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("checkout branch %q in %q: %w", branch, dest, err)
	}

	// Remove cache remote (we don't need it anymore)
	cmd = exec.Command("git", "-C", dest, "remote", "remove", "cache")
	if err := cmd.Run(); err != nil {
		// Non-fatal: just warn
		fmt.Fprintf(os.Stderr, "Warning: failed to remove cache remote: %v\n", err)
	}

	return nil
}

// Fetch runs `git fetch --all --prune` in the base clone.
func (r *Repo) Fetch() error {
	if err := r.run("git", "fetch", "--all", "--prune"); err != nil {
		return fmt.Errorf("fetch in %q: %w", r.Path, err)
	}
	return nil
}

// LocalBranchExists returns true if the branch exists locally.
func (r *Repo) LocalBranchExists(branch string) (bool, error) {
	out, err := r.output("git", "branch", "--list", branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// RemoteBranchExists returns true if origin/<branch> is known.
func (r *Repo) RemoteBranchExists(branch string) (bool, error) {
	out, err := r.output("git", "branch", "-r", "--list", "origin/"+branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// CreateBranchFrom creates a new local branch starting from the given commitish.
func (r *Repo) CreateBranchFrom(branch, commitish string) error {
	if err := r.run("git", "branch", branch, commitish); err != nil {
		return fmt.Errorf("create branch %q from %q: %w", branch, commitish, err)
	}
	return nil
}

// TrackRemoteBranch creates a local branch tracking origin/<branch>.
func (r *Repo) TrackRemoteBranch(branch string) error {
	if err := r.run("git", "branch", "--track", branch, "origin/"+branch); err != nil {
		return fmt.Errorf("track remote branch %q: %w", branch, err)
	}
	return nil
}

// AddWorktree adds a worktree at dest checked out at branch.
func (r *Repo) AddWorktree(dest, branch string) error {
	if err := r.run("git", "worktree", "add", dest, branch); err != nil {
		return fmt.Errorf("add worktree %q at %q: %w", dest, branch, err)
	}
	return nil
}

// RemoveWorktree removes the worktree at the given path.
// If force is true, uses --force to remove even with uncommitted changes.
func (r *Repo) RemoveWorktree(worktreePath string, force bool) error {
	args := []string{"worktree", "remove", worktreePath}
	if force {
		args = append(args, "--force")
	}
	if err := r.run("git", args...); err != nil {
		return fmt.Errorf("remove worktree %q: %w", worktreePath, err)
	}
	return nil
}

// PruneWorktrees runs `git worktree prune` to clean up stale metadata.
func (r *Repo) PruneWorktrees() error {
	if err := r.run("git", "worktree", "prune"); err != nil {
		return fmt.Errorf("worktree prune in %q: %w", r.Path, err)
	}
	return nil
}

// IsDirty returns true if the worktree at the given path has uncommitted changes.
func IsDirty(worktreePath string) (bool, error) {
	out, err := outputAt(worktreePath, "git", "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("status in %q: %w", worktreePath, err)
	}
	return strings.TrimSpace(out) != "", nil
}

// CurrentBranch returns the current branch name in the given worktree path.
func CurrentBranch(worktreePath string) (string, error) {
	out, err := outputAt(worktreePath, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("current branch in %q: %w", worktreePath, err)
	}
	return strings.TrimSpace(out), nil
}

// TrackingBranch returns the remote tracking branch for the current branch in the given worktree path.
// Returns an empty string if there is no tracking branch configured.
func TrackingBranch(worktreePath string) (string, error) {
	out, err := outputAt(worktreePath, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		// If there's no tracking branch, git rev-parse returns an error
		// In this case, we return empty string instead of an error
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// BranchSyncStatus represents the sync status between local and remote branches.
type BranchSyncStatus struct {
	HasTracking bool
	Ahead       int
	Behind      int
}

// InSync returns true if the local branch is in sync with the remote (not ahead or behind).
func (s BranchSyncStatus) InSync() bool {
	return s.HasTracking && s.Ahead == 0 && s.Behind == 0
}

// Diverged returns true if the local branch has diverged from the remote (both ahead and behind).
func (s BranchSyncStatus) Diverged() bool {
	return s.HasTracking && s.Ahead > 0 && s.Behind > 0
}

// BranchSyncStatus returns the ahead/behind status of the current branch relative to its tracking branch.
// If there is no tracking branch, returns a status with HasTracking=false.
func GetBranchSyncStatus(worktreePath string) (BranchSyncStatus, error) {
	// First check if there's a tracking branch
	trackingBranch, err := TrackingBranch(worktreePath)
	if err != nil || trackingBranch == "" {
		return BranchSyncStatus{HasTracking: false}, nil
	}

	// Use git rev-list to count ahead/behind commits
	// Format: "ahead<tab>behind"
	out, err := outputAt(worktreePath, "git", "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		// If upstream branch doesn't exist or other error, treat as no tracking
		return BranchSyncStatus{HasTracking: false}, nil
	}

	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) != 2 {
		return BranchSyncStatus{HasTracking: false}, nil
	}

	ahead := 0
	behind := 0
	fmt.Sscanf(parts[0], "%d", &ahead)
	fmt.Sscanf(parts[1], "%d", &behind)

	return BranchSyncStatus{
		HasTracking: true,
		Ahead:       ahead,
		Behind:      behind,
	}, nil
}

// RepoOperationStatus represents the state of an ongoing git operation.
type RepoOperationStatus struct {
	InProgress bool
	Operation  string
}

// GetRepoOperationStatus checks if there's an ongoing git operation in the worktree.
// Returns status indicating if a rebase, merge, cherry-pick, or revert is in progress.
func GetRepoOperationStatus(worktreePath string) RepoOperationStatus {
	gitDir := filepath.Join(worktreePath, ".git")

	// Check for various operation states
	checks := []struct {
		path      string
		operation string
	}{
		{filepath.Join(gitDir, "rebase-merge"), "rebase"},
		{filepath.Join(gitDir, "rebase-apply"), "rebase"},
		{filepath.Join(gitDir, "MERGE_HEAD"), "merge"},
		{filepath.Join(gitDir, "CHERRY_PICK_HEAD"), "cherry-pick"},
		{filepath.Join(gitDir, "REVERT_HEAD"), "revert"},
	}

	for _, check := range checks {
		if _, err := os.Stat(check.path); err == nil {
			return RepoOperationStatus{
				InProgress: true,
				Operation:  check.operation,
			}
		}
	}

	return RepoOperationStatus{InProgress: false}
}

// PushSetUpstream pushes the current branch and sets it as the upstream tracking branch.
func PushSetUpstream(worktreePath, branch string) error {
	if err := run(worktreePath, "git", "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("push set-upstream in %q: %w", worktreePath, err)
	}
	return nil
}

// FastForward performs a fast-forward merge with the upstream branch.
func FastForward(worktreePath string) error {
	if err := run(worktreePath, "git", "merge", "--ff-only", "@{u}"); err != nil {
		return fmt.Errorf("fast-forward in %q: %w", worktreePath, err)
	}
	return nil
}

// Rebase rebases the current branch onto its upstream tracking branch.
func Rebase(worktreePath string) error {
	if err := run(worktreePath, "git", "rebase", "@{u}"); err != nil {
		return fmt.Errorf("rebase in %q: %w", worktreePath, err)
	}
	return nil
}

// Stash stashes all uncommitted changes (both staged and unstaged).
func Stash(worktreePath string) error {
	if err := run(worktreePath, "git", "stash", "push", "-m", "chord tune auto-stash"); err != nil {
		return fmt.Errorf("stash in %q: %w", worktreePath, err)
	}
	return nil
}

// StashPop pops the most recent stash.
func StashPop(worktreePath string) error {
	if err := run(worktreePath, "git", "stash", "pop"); err != nil {
		return fmt.Errorf("stash pop in %q: %w", worktreePath, err)
	}
	return nil
}

// Push pushes the current branch to its upstream tracking branch.
func Push(worktreePath string) error {
	if err := run(worktreePath, "git", "push"); err != nil {
		return fmt.Errorf("push in %q: %w", worktreePath, err)
	}
	return nil
}

// DeleteRemoteBranch deletes the branch on the remote (origin).
func DeleteRemoteBranch(worktreePath, branch string) error {
	if err := run(worktreePath, "git", "push", "origin", "--delete", branch); err != nil {
		return fmt.Errorf("delete remote branch %q: %w", branch, err)
	}
	return nil
}

// PruneRemote removes stale remote-tracking references.
func PruneRemote(worktreePath string) error {
	if err := run(worktreePath, "git", "remote", "prune", "origin"); err != nil {
		return fmt.Errorf("prune remote in %q: %w", worktreePath, err)
	}
	return nil
}

// --- internal helpers ---

func (r *Repo) run(name string, args ...string) error {
	return run(r.Path, name, args...)
}

func (r *Repo) output(name string, args ...string) (string, error) {
	return outputAt(r.Path, name, args...)
}

func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func outputAt(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, buf.String())
	}
	return buf.String(), nil
}

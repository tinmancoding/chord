// Package git wraps raw git CLI operations needed by chord.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo represents a git repository on disk (the base clone).
type Repo struct {
	// Path is the absolute path to the repository root.
	Path string
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

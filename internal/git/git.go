// Package git wraps raw git CLI operations needed by chord.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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

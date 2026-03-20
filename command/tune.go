package command

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tinmancoding/chord/internal/git"
	"github.com/tinmancoding/chord/internal/prompt"
	"github.com/tinmancoding/chord/internal/render"
	"github.com/tinmancoding/chord/internal/workspace"
)

// NewTuneCmd builds the `chord tune` command.
func NewTuneCmd() *cobra.Command {
	var autoYes bool
	var push bool

	cmd := &cobra.Command{
		Use:   "tune",
		Short: "Synchronize all worktrees with their remote tracking branches",
		Long: `Tune synchronizes every repository in the current chord workspace with its
remote tracking branch. It fetches changes, rebases local commits if needed,
and optionally pushes changes to create or update remote branches.

For repositories without an upstream tracking branch, --push is required to
create and push the branch to origin.

For repositories with an upstream tracking branch, tune will:
- Fetch the latest changes from origin
- Fast-forward if only behind (no local commits)
- Rebase if there are both local and remote changes
- Stash uncommitted changes if needed during rebase (with --yes)

Repositories in the middle of a rebase, merge, or with stashed changes will
be skipped with a warning message.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTune(autoYes, push)
		},
	}

	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&push, "push", false, "Create upstream branches and push all changes at the end")

	return cmd
}

func runTune(autoYes, pushFlag bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	state, err := workspace.LoadState(cwd)
	if err != nil {
		return err
	}

	fmt.Printf("\n  %s  Tuning workspace: %s\n\n", render.Bold("♫"), render.Bold(state.TargetBranch))

	var reposToSync []string

	// Process each repository
	for _, rs := range state.Repos {
		render.Info("[%s] Processing...", rs.RepoID)

		// Check if repo is in middle of an operation
		status := git.GetRepoOperationStatus(rs.WorktreePath)
		if status.InProgress {
			render.Warn("[%s] Skipped: %s in progress. Please resolve and try again.", rs.RepoID, status.Operation)
			continue
		}

		// Get tracking branch and sync status
		trackingBranch, err := git.TrackingBranch(rs.WorktreePath)
		if err != nil {
			render.Error("[%s] Could not read tracking branch: %v", rs.RepoID, err)
			continue
		}

		// Case A: No upstream tracking branch
		if trackingBranch == "" {
			if !pushFlag {
				render.Warn("[%s] No upstream tracking branch. Use --push to create one.", rs.RepoID)
				continue
			}

			// Ask for confirmation
			if !autoYes {
				confirmed := prompt.Confirm(fmt.Sprintf("[%s] Create and push upstream branch to origin?", rs.RepoID))
				if !confirmed {
					render.Info("[%s] Skipped by user.", rs.RepoID)
					continue
				}
			}

			// Push to create upstream
			currentBranch, err := git.CurrentBranch(rs.WorktreePath)
			if err != nil {
				render.Error("[%s] Could not get current branch: %v", rs.RepoID, err)
				continue
			}

			if err := git.PushSetUpstream(rs.WorktreePath, currentBranch); err != nil {
				render.Error("[%s] Failed to push upstream: %v", rs.RepoID, err)
				continue
			}

			render.Success("[%s] Pushed and set upstream to origin/%s", rs.RepoID, currentBranch)
			continue
		}

		// Case B: Has upstream tracking branch
		syncStatus, err := git.GetBranchSyncStatus(rs.WorktreePath)
		if err != nil {
			render.Error("[%s] Could not get sync status: %v", rs.RepoID, err)
			continue
		}

		// Fetch latest changes
		render.Info("[%s] Fetching from origin...", rs.RepoID)
		repo := git.New(rs.BaseClonePath)
		if err := repo.Fetch(); err != nil {
			render.Error("[%s] Failed to fetch: %v", rs.RepoID, err)
			continue
		}

		// Re-check sync status after fetch
		syncStatus, err = git.GetBranchSyncStatus(rs.WorktreePath)
		if err != nil {
			render.Error("[%s] Could not get sync status after fetch: %v", rs.RepoID, err)
			continue
		}

		// If in sync, nothing to do
		if syncStatus.InSync() {
			render.Success("[%s] Already in sync with upstream.", rs.RepoID)
			if pushFlag && syncStatus.Ahead > 0 {
				reposToSync = append(reposToSync, rs.WorktreePath)
			}
			continue
		}

		// Only behind: fast-forward
		if syncStatus.Behind > 0 && syncStatus.Ahead == 0 {
			if !autoYes {
				confirmed := prompt.Confirm(fmt.Sprintf("[%s] Fast-forward %d commits from upstream?", rs.RepoID, syncStatus.Behind))
				if !confirmed {
					render.Info("[%s] Skipped by user.", rs.RepoID)
					continue
				}
			}

			if err := git.FastForward(rs.WorktreePath); err != nil {
				render.Error("[%s] Failed to fast-forward: %v", rs.RepoID, err)
				continue
			}

			render.Success("[%s] Fast-forwarded %d commits.", rs.RepoID, syncStatus.Behind)
			continue
		}

		// Ahead and/or behind: need to rebase
		dirty, _ := git.IsDirty(rs.WorktreePath)
		needsStash := dirty && (syncStatus.Ahead > 0 || syncStatus.Behind > 0)

		if needsStash && !autoYes {
			render.Warn("[%s] Uncommitted changes detected. Stash is required for rebase.", rs.RepoID)
			confirmed := prompt.Confirm(fmt.Sprintf("[%s] Stash changes, rebase, and unstash?", rs.RepoID))
			if !confirmed {
				render.Info("[%s] Skipped by user.", rs.RepoID)
				continue
			}
		}

		if syncStatus.Ahead > 0 && syncStatus.Behind > 0 {
			if !autoYes && !needsStash {
				confirmed := prompt.Confirm(fmt.Sprintf("[%s] Rebase %d local commits onto %d upstream commits?", rs.RepoID, syncStatus.Ahead, syncStatus.Behind))
				if !confirmed {
					render.Info("[%s] Skipped by user.", rs.RepoID)
					continue
				}
			}
		}

		// Perform stash if needed
		var stashed bool
		if needsStash {
			render.Info("[%s] Stashing uncommitted changes...", rs.RepoID)
			if err := git.Stash(rs.WorktreePath); err != nil {
				render.Error("[%s] Failed to stash: %v", rs.RepoID, err)
				continue
			}
			stashed = true
		}

		// Perform rebase
		render.Info("[%s] Rebasing...", rs.RepoID)
		if err := git.Rebase(rs.WorktreePath); err != nil {
			render.Error("[%s] Rebase failed: %v. Please resolve conflicts and run tune again.", rs.RepoID, err)
			// Don't try to unstash if rebase failed
			continue
		}

		render.Success("[%s] Rebased successfully.", rs.RepoID)

		// Pop stash if we stashed
		if stashed {
			render.Info("[%s] Unstashing changes...", rs.RepoID)
			if err := git.StashPop(rs.WorktreePath); err != nil {
				render.Warn("[%s] Failed to unstash: %v. Please resolve conflicts manually.", rs.RepoID, err)
				// Continue to next repo
				continue
			}
			render.Success("[%s] Unstashed changes successfully.", rs.RepoID)
		}

		// Mark for final push if needed
		if pushFlag {
			reposToSync = append(reposToSync, rs.WorktreePath)
		}
	}

	// Final push if --push flag was specified
	if pushFlag && len(reposToSync) > 0 {
		fmt.Println()
		render.Info("Final synchronization: pushing to remote...")
		for _, worktreePath := range reposToSync {
			// Find repo ID for this worktree
			var repoID string
			for _, rs := range state.Repos {
				if rs.WorktreePath == worktreePath {
					repoID = rs.RepoID
					break
				}
			}

			// Check if we're ahead
			syncStatus, err := git.GetBranchSyncStatus(worktreePath)
			if err != nil || !syncStatus.HasTracking || syncStatus.Ahead == 0 {
				continue
			}

			render.Info("[%s] Pushing %d commits to remote...", repoID, syncStatus.Ahead)
			if err := git.Push(worktreePath); err != nil {
				render.Error("[%s] Failed to push: %v", repoID, err)
				continue
			}
			render.Success("[%s] Pushed successfully.", repoID)
		}
	}

	fmt.Println()
	render.Success("Tune complete.")
	fmt.Println()

	return nil
}

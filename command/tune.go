package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tinmancoding/chord/internal/config"
	"github.com/tinmancoding/chord/internal/git"
	"github.com/tinmancoding/chord/internal/prompt"
	"github.com/tinmancoding/chord/internal/render"
	"github.com/tinmancoding/chord/internal/workspace"
)

// NewTuneCmd builds the `chord tune` command.
func NewTuneCmd(cfgPath *string, baseDirOverride *string) *cobra.Command {
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
			return runTune(*cfgPath, *baseDirOverride, autoYes, push)
		},
	}

	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&push, "push", false, "Create upstream branches and push all changes at the end")

	return cmd
}

func runTune(cfgPath, baseDirOverride string, autoYes, pushFlag bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	state, err := workspace.LoadState(cwd)
	if err != nil {
		return err
	}

	// Load config for deferred repos handling
	cfg, err := config.Load(cfgPath)
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

	// Check for deferred repositories
	if len(state.DeferredRepos) > 0 {
		fmt.Println()
		render.Info("Checking for deferred repositories...")

		var stateChanged bool

		for _, deferred := range state.DeferredRepos {
			// Get repo definition from config
			repoDef, err := cfg.GetRepository(deferred.RepoID)
			if err != nil {
				render.Warn("  [%s] Could not load repository definition: %v", deferred.RepoID, err)
				continue
			}

			// Ensure base clone exists
			clonePath, err := workspace.BaseClonePath(deferred.RepoID)
			if err != nil {
				render.Warn("  [%s] Could not determine base clone path: %v", deferred.RepoID, err)
				continue
			}

			repo, err := ensureBaseClone(deferred.RepoID, repoDef.URL, clonePath)
			if err != nil {
				render.Warn("  [%s] Could not ensure base clone: %v", deferred.RepoID, err)
				continue
			}

			// Fetch to get latest remote branches
			if err := repo.Fetch(); err != nil {
				render.Warn("  [%s] Could not fetch from remote: %v", deferred.RepoID, err)
				continue
			}

			// Check if remote branch exists
			exists, err := repo.RemoteBranchExists(state.TargetBranch)
			if err != nil {
				render.Warn("  [%s] Error checking remote branch: %v", deferred.RepoID, err)
				state.UpdateLastChecked(deferred.RepoID)
				stateChanged = true
				continue
			}

			if exists {
				// Remote branch found!
				render.Info("  [%s] Remote branch '%s' found", deferred.RepoID, state.TargetBranch)

				// Ask for confirmation unless --yes
				if !autoYes {
					confirmed := prompt.Confirm(fmt.Sprintf("    Create worktree for %s?", deferred.RepoID))
					if !confirmed {
						render.Info("  [%s] Skipped by user", deferred.RepoID)
						state.UpdateLastChecked(deferred.RepoID)
						stateChanged = true
						continue
					}
				}

				// Create worktree
				worktreePath := filepath.Join(state.WorkspaceDir, deferred.RepoID)

				// Resolve the branch (similar to compose logic)
				resolvedBranch := state.TargetBranch
				if state.TargetBranch == "main" {
					resolvedBranch = repoDef.DefaultBranch
				} else {
					// Check if local branch exists, otherwise create tracking branch
					localExists, _ := repo.LocalBranchExists(state.TargetBranch)
					if !localExists {
						render.Info("  [%s] Creating local tracking branch for origin/%s", deferred.RepoID, state.TargetBranch)
						if err := repo.TrackRemoteBranch(state.TargetBranch); err != nil {
							render.Error("  [%s] Failed to create tracking branch: %v", deferred.RepoID, err)
							continue
						}
					}
				}

				// Add the worktree
				if err := repo.AddWorktree(worktreePath, resolvedBranch); err != nil {
					render.Error("  [%s] Failed to create worktree: %v", deferred.RepoID, err)
					continue
				}

				// Add to active repos in state
				state.Repos = append(state.Repos, workspace.RepoState{
					RepoID:         deferred.RepoID,
					ExpectedBranch: resolvedBranch,
					WorktreePath:   worktreePath,
					BaseClonePath:  clonePath,
				})

				// Remove from deferred list
				state.RemoveDeferred(deferred.RepoID)
				stateChanged = true

				render.Success("  [%s] Worktree created at %s", deferred.RepoID, worktreePath)
			} else {
				render.Info("  [%s] Remote branch not yet available", deferred.RepoID)
				state.UpdateLastChecked(deferred.RepoID)
				stateChanged = true
			}
		}

		// Save state if any changes were made
		if stateChanged {
			if err := state.Save(); err != nil {
				render.Warn("Failed to save updated state: %v", err)
			}
		}
	}

	fmt.Println()
	render.Success("Tune complete.")
	fmt.Println()

	return nil
}

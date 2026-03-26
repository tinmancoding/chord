package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
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

	fmt.Printf("\n  %s  Tuning workspace: %s\n\n", render.Bold("♫"), render.Bold(state.ChordName))

	var reposToSync []string

	// Process each repository
	for _, rs := range state.Repos {
		render.Info("[%s] Processing...", rs.Name)

		// Check if repo is in middle of an operation
		status := git.GetRepoOperationStatus(rs.Path)
		if status.InProgress {
			render.Warn("[%s] Skipped: %s in progress. Please resolve and try again.", rs.Name, status.Operation)
			continue
		}

		// Get tracking branch and sync status
		trackingBranch, err := git.TrackingBranch(rs.Path)
		if err != nil {
			render.Error("[%s] Could not read tracking branch: %v", rs.Name, err)
			continue
		}

		// Case A: No upstream tracking branch
		if trackingBranch == "" {
			if !pushFlag {
				render.Warn("[%s] No upstream tracking branch. Use --push to create one.", rs.Name)
				continue
			}

			// Ask for confirmation
			if !autoYes {
				confirmed := prompt.Confirm(fmt.Sprintf("[%s] Create and push upstream branch to origin?", rs.Name))
				if !confirmed {
					render.Info("[%s] Skipped by user.", rs.Name)
					continue
				}
			}

			// Push to create upstream
			currentBranch, err := git.CurrentBranch(rs.Path)
			if err != nil {
				render.Error("[%s] Could not get current branch: %v", rs.Name, err)
				continue
			}

			if err := git.PushSetUpstream(rs.Path, currentBranch); err != nil {
				render.Error("[%s] Failed to push upstream: %v", rs.Name, err)
				continue
			}

			render.Success("[%s] Pushed and set upstream to origin/%s", rs.Name, currentBranch)
			continue
		}

		// Case B: Has upstream tracking branch
		syncStatus, err := git.GetBranchSyncStatus(rs.Path)
		if err != nil {
			render.Error("[%s] Could not get sync status: %v", rs.Name, err)
			continue
		}

		// Fetch latest changes from workspace directory
		render.Info("[%s] Fetching from origin...", rs.Name)
		repo := git.New(rs.Path)
		if err := repo.Fetch(); err != nil {
			render.Error("[%s] Failed to fetch: %v", rs.Name, err)
			continue
		}

		// Re-check sync status after fetch
		syncStatus, err = git.GetBranchSyncStatus(rs.Path)
		if err != nil {
			render.Error("[%s] Could not get sync status after fetch: %v", rs.Name, err)
			continue
		}

		// If in sync, nothing to do
		if syncStatus.InSync() {
			render.Success("[%s] Already in sync with upstream.", rs.Name)
			if pushFlag && syncStatus.Ahead > 0 {
				reposToSync = append(reposToSync, rs.Path)
			}
			continue
		}

		// Only behind: fast-forward
		if syncStatus.Behind > 0 && syncStatus.Ahead == 0 {
			if !autoYes {
				confirmed := prompt.Confirm(fmt.Sprintf("[%s] Fast-forward %d commits from upstream?", rs.Name, syncStatus.Behind))
				if !confirmed {
					render.Info("[%s] Skipped by user.", rs.Name)
					continue
				}
			}

			if err := git.FastForward(rs.Path); err != nil {
				render.Error("[%s] Failed to fast-forward: %v", rs.Name, err)
				continue
			}

			render.Success("[%s] Fast-forwarded %d commits.", rs.Name, syncStatus.Behind)
			continue
		}

		// Ahead and/or behind: need to rebase
		dirty, _ := git.IsDirty(rs.Path)
		needsStash := dirty && (syncStatus.Ahead > 0 || syncStatus.Behind > 0)

		if needsStash && !autoYes {
			render.Warn("[%s] Uncommitted changes detected. Stash is required for rebase.", rs.Name)
			confirmed := prompt.Confirm(fmt.Sprintf("[%s] Stash changes, rebase, and unstash?", rs.Name))
			if !confirmed {
				render.Info("[%s] Skipped by user.", rs.Name)
				continue
			}
		}

		if syncStatus.Ahead > 0 && syncStatus.Behind > 0 {
			if !autoYes && !needsStash {
				confirmed := prompt.Confirm(fmt.Sprintf("[%s] Rebase %d local commits onto %d upstream commits?", rs.Name, syncStatus.Ahead, syncStatus.Behind))
				if !confirmed {
					render.Info("[%s] Skipped by user.", rs.Name)
					continue
				}
			}
		}

		// Perform stash if needed
		var stashed bool
		if needsStash {
			render.Info("[%s] Stashing uncommitted changes...", rs.Name)
			if err := git.Stash(rs.Path); err != nil {
				render.Error("[%s] Failed to stash: %v", rs.Name, err)
				continue
			}
			stashed = true
		}

		// Perform rebase
		render.Info("[%s] Rebasing...", rs.Name)
		if err := git.Rebase(rs.Path); err != nil {
			render.Error("[%s] Rebase failed: %v. Please resolve conflicts and run tune again.", rs.Name, err)
			// Don't try to unstash if rebase failed
			continue
		}

		render.Success("[%s] Rebased successfully.", rs.Name)

		// Pop stash if we stashed
		if stashed {
			render.Info("[%s] Unstashing changes...", rs.Name)
			if err := git.StashPop(rs.Path); err != nil {
				render.Warn("[%s] Failed to unstash: %v. Please resolve conflicts manually.", rs.Name, err)
				// Continue to next repo
				continue
			}
			render.Success("[%s] Unstashed changes successfully.", rs.Name)
		}

		// Mark for final push if needed
		if pushFlag {
			reposToSync = append(reposToSync, rs.Path)
		}
	}

	// Final push if --push flag was specified
	if pushFlag && len(reposToSync) > 0 {
		fmt.Println()
		render.Info("Final synchronization: pushing to remote...")
		for _, repoPath := range reposToSync {
			// Find repo ID for this repo path
			var repoID string
			for _, rs := range state.Repos {
				if rs.Path == repoPath {
					repoID = rs.Name
					break
				}
			}

			// Check if we're ahead
			syncStatus, err := git.GetBranchSyncStatus(repoPath)
			if err != nil || !syncStatus.HasTracking || syncStatus.Ahead == 0 {
				continue
			}

			render.Info("[%s] Pushing %d commits to remote...", repoID, syncStatus.Ahead)
			if err := git.Push(repoPath); err != nil {
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
			// Ensure cache exists
			cacheDir, err := workspace.BaseCacheDir()
			if err != nil {
				render.Warn("  [%s] Could not determine cache directory: %v", deferred.Name, err)
				continue
			}

			repo, err := git.EnsureCache(deferred.Name, deferred.URL, cacheDir)
			if err != nil {
				render.Warn("  [%s] Could not ensure cache: %v", deferred.Name, err)
				continue
			}

			// Fetch to get latest remote branches
			if err := repo.Fetch(); err != nil {
				render.Warn("  [%s] Could not fetch from remote: %v", deferred.Name, err)
				continue
			}

			// Check if remote branch exists
			exists, err := repo.RemoteBranchExists(deferred.ExpectedBranch)
			if err != nil {
				render.Warn("  [%s] Error checking remote branch: %v", deferred.Name, err)
				state.UpdateLastChecked(deferred.Name)
				stateChanged = true
				continue
			}

			if exists {
				// Remote branch found!
				render.Info("  [%s] Remote branch '%s' found", deferred.Name, deferred.ExpectedBranch)

				// Ask for confirmation unless --yes
				if !autoYes {
					confirmed := prompt.Confirm(fmt.Sprintf("Create clone for %s?", deferred.Name))
					if !confirmed {
						render.Info("  [%s] Skipped by user", deferred.Name)
						state.UpdateLastChecked(deferred.Name)
						stateChanged = true
						continue
					}
				}

				// Create full clone with reference to cache
				repoPath := filepath.Join(state.WorkspaceDir, deferred.Name)

				// Check if local branch exists in cache, otherwise create tracking branch
				localExists, _ := repo.LocalBranchExists(deferred.ExpectedBranch)
				if !localExists {
					render.Info("  [%s] Creating local tracking branch for origin/%s in cache", deferred.Name, deferred.ExpectedBranch)
					if err := repo.TrackRemoteBranch(deferred.ExpectedBranch); err != nil {
						render.Error("  [%s] Failed to create tracking branch in cache: %v", deferred.Name, err)
						continue
					}
				}

				cachePath, _ := workspace.CachePathForRepo(deferred.URL)
				if err := repo.CloneWithReference(deferred.URL, repoPath, deferred.ExpectedBranch); err != nil {
					render.Error("  [%s] Failed to create clone: %v", deferred.Name, err)
					continue
				}

				// Add to active repos in state
				state.Repos = append(state.Repos, workspace.RepoState{
					Name:           deferred.Name,
					URL:            deferred.URL,
					ExpectedBranch: deferred.ExpectedBranch,
					Path:           repoPath,
					BaseClonePath:  cachePath,
				})

				render.Success("  [%s] Clone created at %s", deferred.Name, repoPath)

				// Remove from deferred list
				state.RemoveDeferred(deferred.Name)
				stateChanged = true
			} else {
				render.Info("  [%s] Remote branch not yet available", deferred.Name)
				state.UpdateLastChecked(deferred.Name)
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

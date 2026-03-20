package command

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tinmancoding/chord/internal/config"
	"github.com/tinmancoding/chord/internal/git"
	"github.com/tinmancoding/chord/internal/prompt"
	"github.com/tinmancoding/chord/internal/render"
	"github.com/tinmancoding/chord/internal/workspace"
)

// NewMuteCmd builds the `chord mute` command.
func NewMuteCmd(cfgPath *string, baseDirOverride *string) *cobra.Command {
	var force bool
	var remote bool
	var autoYes bool

	cmd := &cobra.Command{
		Use:   "mute <project_id> <target_branch>",
		Short: "Remove a workspace and clean up worktree metadata",
		Long: `Mute removes the workspace directory for the given project and branch,
and prunes the Git worktree metadata from every base clone.

By default, mute refuses to delete a workspace that contains any
repository with uncommitted changes. Use --force to override.

When --remote is specified, also deletes the remote branches on origin
and their local tracking branches. This requires confirmation unless --yes is used.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMute(*cfgPath, *baseDirOverride, args[0], args[1], force, remote, autoYes)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Remove even if worktrees have uncommitted changes")
	cmd.Flags().BoolVarP(&remote, "remote", "r", false, "Also delete remote branches on origin")
	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func runMute(cfgPath, baseDirOverride, projectID, targetBranch string, force, remote, autoYes bool) error {
	// --- Load config to resolve effective base directory ---
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	baseDir, err := cfg.EffectiveBaseDir(baseDirOverride)
	if err != nil {
		return fmt.Errorf("resolve base directory: %w", err)
	}

	workspaceDir := workspace.WorkspacePath(baseDir, projectID, targetBranch)

	state, err := workspace.LoadState(workspaceDir)
	if err != nil {
		return err
	}

	// REQ-MUTE-01: check for dirty worktrees before doing anything destructive.
	hasDirtyRepos := false
	if !force {
		for _, rs := range state.Repos {
			dirty, err := git.IsDirty(rs.WorktreePath)
			if err != nil {
				return fmt.Errorf("checking dirty state for %s: %w", rs.RepoID, err)
			}
			if dirty {
				return fmt.Errorf(
					"repo %q has uncommitted changes — use --force to override (REQ-MUTE-01)",
					rs.RepoID,
				)
			}
		}
	} else {
		// Check if there are actually dirty repos when force is used (for warning)
		for _, rs := range state.Repos {
			dirty, err := git.IsDirty(rs.WorktreePath)
			if err != nil {
				// Ignore errors here, we're just checking for the warning
				continue
			}
			if dirty {
				hasDirtyRepos = true
				break
			}
		}
	}

	// Show confirmation if there's risk of data loss
	needsConfirmation := (force || remote) && !autoYes

	if needsConfirmation {
		msg := fmt.Sprintf("\nThis will:\n")
		msg += fmt.Sprintf("  - Delete workspace: %s\n", workspaceDir)
		msg += fmt.Sprintf("  - Delete %d worktree(s)\n", len(state.Repos))

		if force && hasDirtyRepos {
			msg += "  ⚠️  WARNING: Uncommitted changes will be LOST!\n"
		}

		if remote {
			msg += fmt.Sprintf("  - Delete %d remote branch(es) on origin\n", len(state.Repos))
			msg += "  ⚠️  WARNING: Remote branches will be deleted from origin!\n"
		}

		msg += "\nContinue?"

		if !prompt.Confirm(msg) {
			return fmt.Errorf("cancelled by user")
		}
		fmt.Println()
	}

	// Remove remote branches first (if requested)
	if remote {
		for _, rs := range state.Repos {
			if err := deleteRemoteBranch(rs.WorktreePath, targetBranch); err != nil {
				// Log warning but continue - the branch might already be deleted
				render.Warn("[%s] Could not delete remote branch: %v", rs.RepoID, err)
			} else {
				render.Success("[%s] Remote branch deleted.", rs.RepoID)
			}
		}
		fmt.Println()
	}

	// Remove each worktree from its base clone.
	for _, rs := range state.Repos {
		render.Info("[%s] Removing worktree…", rs.RepoID)
		baseRepo := git.New(rs.BaseClonePath)
		if err := baseRepo.RemoveWorktree(rs.WorktreePath, force); err != nil {
			return err
		}

		// REQ-MUTE-02: prune worktree metadata from the base clone.
		if err := baseRepo.PruneWorktrees(); err != nil {
			return err
		}
		render.Success("[%s] Worktree removed and pruned.", rs.RepoID)
	}

	// Remove the workspace directory itself.
	if err := os.RemoveAll(workspaceDir); err != nil {
		return fmt.Errorf("remove workspace dir %q: %w", workspaceDir, err)
	}

	fmt.Println()
	render.Success("Workspace %q for project %q has been muted.", targetBranch, projectID)
	return nil
}

// deleteRemoteBranch deletes the remote branch on origin and the local remote-tracking branch.
func deleteRemoteBranch(worktreePath, branch string) error {
	// First, try to delete the remote branch on origin
	// git push origin --delete <branch>
	if err := git.DeleteRemoteBranch(worktreePath, branch); err != nil {
		return fmt.Errorf("delete remote branch: %w", err)
	}

	// The remote-tracking branch should be automatically removed by the push,
	// but we can also explicitly prune to be sure
	if err := git.PruneRemote(worktreePath); err != nil {
		// This is not critical, just log it
		return fmt.Errorf("prune remote refs: %w", err)
	}

	return nil
}

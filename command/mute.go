package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tinmancoding/chord/internal/git"
	"github.com/tinmancoding/chord/internal/render"
	"github.com/tinmancoding/chord/internal/workspace"
	"github.com/spf13/cobra"
)

// NewMuteCmd builds the `chord mute` command.
func NewMuteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "mute <target_branch>",
		Short: "Remove a workspace and clean up worktree metadata",
		Long: `Mute removes the workspace directory for the given branch and prunes
the Git worktree metadata from every base clone.

By default, mute refuses to delete a workspace that contains any
repository with uncommitted changes. Use --force to override.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMute(args[0], force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Remove even if worktrees have uncommitted changes")
	return cmd
}

func runMute(targetBranch string, force bool) error {
	workspaceDir, err := filepath.Abs(targetBranch)
	if err != nil {
		return err
	}

	state, err := workspace.LoadState(workspaceDir)
	if err != nil {
		return err
	}

	// REQ-MUTE-01: check for dirty worktrees before doing anything destructive.
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
	render.Success("Workspace %q has been muted.", targetBranch)
	return nil
}

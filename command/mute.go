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
	var namespaceFlag string

	cmd := &cobra.Command{
		Use:   "mute <chord-name>",
		Short: "Remove a chord workspace and clean up metadata",
		Long: `Mute removes the workspace directory for the given chord.

By default, mute refuses to delete a workspace that contains any
repository with uncommitted changes. Use --force to override.

When --remote is specified, also deletes the remote branches on origin
and their local tracking branches. This requires confirmation unless --yes is used.

The chord name can include namespace prefixes (e.g., work/team-a/my-workspace).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMute(*cfgPath, *baseDirOverride, args[0], force, remote, autoYes, namespaceFlag)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Remove even if repos have uncommitted changes")
	cmd.Flags().BoolVarP(&remote, "remote", "r", false, "Also delete remote branches on origin")
	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().StringVarP(&namespaceFlag, "namespace", "n", "", "Namespace for workspace (overrides prefix)")
	return cmd
}

func runMute(cfgPath, baseDirOverride, chordName string, force, remote, autoYes bool, namespaceFlag string) error {
	// --- Load config to resolve effective base directory ---
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	baseDir, err := cfg.EffectiveBaseDir(baseDirOverride)
	if err != nil {
		return fmt.Errorf("resolve base directory: %w", err)
	}

	// Parse chord name and resolve namespace
	namespace := cfg.ResolveNamespace(chordName, "", namespaceFlag)
	_, name := config.ParseChordName(chordName)

	// Build workspace path
	workspaceDir := workspace.WorkspacePath(baseDir, namespace, name)

	state, err := workspace.LoadState(workspaceDir)
	if err != nil {
		return err
	}

	// REQ-MUTE-01: check for dirty worktrees before doing anything destructive.
	hasDirtyRepos := false
	if !force {
		for _, rs := range state.Repos {
			dirty, err := git.IsDirty(rs.Path)
			if err != nil {
				render.Warn("Could not check dirty state for %s: %v", rs.Name, err)
				continue
			}
			if dirty {
				render.Warn("Repository %s has uncommitted changes.", rs.Name)
				hasDirtyRepos = true
			}
		}

		if hasDirtyRepos {
			return fmt.Errorf("workspace contains uncommitted changes; use --force to remove anyway")
		}
	}

	// REQ-MUTE-02: Delete remote branches if requested
	if remote {
		if !autoYes {
			render.Warn("This will delete remote branches for all repositories in this workspace!")
			confirmed := prompt.Confirm("Are you sure you want to delete remote branches?")
			if !confirmed {
				return fmt.Errorf("aborted by user")
			}
		}

		for _, rs := range state.Repos {
			branch := rs.ExpectedBranch

			// Try to delete remote branch
			render.Info("[%s] Deleting remote branch origin/%s...", rs.Name, branch)
			if err := git.DeleteRemoteBranch(rs.Path, branch); err != nil {
				render.Warn("[%s] Failed to delete remote branch: %v", rs.Name, err)
				// Continue with other repos even if one fails
			} else {
				render.Success("[%s] Deleted origin/%s", rs.Name, branch)
			}
		}
	}

	// Delete the workspace directory
	render.Info("Removing workspace directory: %s", workspaceDir)
	if err := os.RemoveAll(workspaceDir); err != nil {
		return fmt.Errorf("could not remove workspace directory: %w", err)
	}

	render.Success("Workspace %s removed successfully.", name)
	if state.Namespace != "" {
		fmt.Printf("  Namespace: %s\n", state.Namespace)
	}
	fmt.Printf("  Repos removed: %d\n", len(state.Repos))

	return nil
}

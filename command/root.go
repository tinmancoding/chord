package command

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the root cobra command for chord.
func NewRootCmd() *cobra.Command {
	var cfgPath string

	root := &cobra.Command{
		Use:   "chord",
		Short: "Multi-repository Git worktree orchestrator",
		Long: `Chord manages complex development environments where a single feature
spans multiple Git repositories. It uses Git worktrees to create
isolated workspaces — "Chords" — where every repository is tuned
to the correct branch.`,
	}

	root.PersistentFlags().StringVarP(
		&cfgPath, "config", "c", "chord.yaml",
		"Path to chord.yaml config file",
	)

	root.AddCommand(NewComposeCmd(&cfgPath))
	root.AddCommand(NewTuneCmd())
	root.AddCommand(NewMuteCmd())

	return root
}

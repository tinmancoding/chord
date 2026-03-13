package command

import (
	"github.com/tinmancoding/chord/internal/config"
	"github.com/spf13/cobra"
)

// NewRootCmd builds the root cobra command for chord.
func NewRootCmd() *cobra.Command {
	var cfgPath string

	// Resolve the default config path (~/.config/chord/chord.yaml).
	// Fall back to "chord.yaml" in the current directory if the home
	// directory cannot be determined (e.g. in some CI/container environments).
	defaultCfgPath, err := config.DefaultPath()
	if err != nil {
		defaultCfgPath = "chord.yaml"
	}

	root := &cobra.Command{
		Use:   "chord",
		Short: "Multi-repository Git worktree orchestrator",
		Long: `Chord manages complex development environments where a single feature
spans multiple Git repositories. It uses Git worktrees to create
isolated workspaces — "Chords" — where every repository is tuned
to the correct branch.`,
	}

	root.PersistentFlags().StringVarP(
		&cfgPath, "config", "c", defaultCfgPath,
		"Path to chord.yaml config file",
	)

	root.AddCommand(NewComposeCmd(&cfgPath))
	root.AddCommand(NewTuneCmd())
	root.AddCommand(NewMuteCmd())

	return root
}

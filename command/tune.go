package command

import (
	"os"

	"github.com/tinmancoding/chord/internal/git"
	"github.com/tinmancoding/chord/internal/render"
	"github.com/tinmancoding/chord/internal/workspace"
	"github.com/spf13/cobra"
)

// NewTuneCmd builds the `chord tune` command.
func NewTuneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tune",
		Short: "Check the harmony of the current workspace",
		Long: `Tune inspects every repository in the current chord workspace and
reports whether each is on its expected branch and whether any have
uncommitted changes.

Dissonance is flagged when a repo's current branch differs from the
branch that was resolved at compose time — the "Sanity Rule" ensures
that repos whose default_branch was substituted for "main" are still
reported as In Tune.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTune()
		},
	}
}

func runTune() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	state, err := workspace.LoadState(cwd)
	if err != nil {
		return err
	}

	var statuses []render.RepoStatus

	for _, rs := range state.Repos {
		currentBranch, err := git.CurrentBranch(rs.WorktreePath)
		if err != nil {
			render.Error("Could not read branch for %s: %v", rs.RepoID, err)
			currentBranch = "unknown"
		}

		dirty, err := git.IsDirty(rs.WorktreePath)
		if err != nil {
			render.Warn("Could not check dirty state for %s: %v", rs.RepoID, err)
		}

		// REQ-TUNE-02 / Sanity Rule: compare against the resolved expected branch,
		// not the raw target_branch string. This is why we store ExpectedBranch in state.
		inTune := currentBranch == rs.ExpectedBranch

		statuses = append(statuses, render.RepoStatus{
			RepoID:         rs.RepoID,
			CurrentBranch:  currentBranch,
			ExpectedBranch: rs.ExpectedBranch,
			InTune:         inTune,
			Dirty:          dirty,
		})
	}

	// REQ-TUNE-01: output status table.
	render.TuneTable(statuses, state.TargetBranch)
	return nil
}

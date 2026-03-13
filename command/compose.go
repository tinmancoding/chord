package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tinmancoding/chord/internal/config"
	"github.com/tinmancoding/chord/internal/git"
	"github.com/tinmancoding/chord/internal/render"
	"github.com/tinmancoding/chord/internal/workspace"
	"github.com/spf13/cobra"
)

// NewComposeCmd builds the `chord compose` command.
func NewComposeCmd(cfgPath *string) *cobra.Command {
	var startAt string

	cmd := &cobra.Command{
		Use:   "compose <project_id> <target_branch>",
		Short: "Create a new workspace for a project at the given branch",
		Long: `Compose creates a workspace directory and initialises Git worktrees
for every repository in the project, tuning each one to the correct branch.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectID := args[0]
			targetBranch := args[1]
			return runCompose(*cfgPath, projectID, targetBranch, startAt)
		},
	}

	cmd.Flags().StringVar(&startAt, "start-at", "", "Commitish to start new branches from")
	return cmd
}

func runCompose(cfgPath, projectID, targetBranch, startAt string) error {
	// --- Load config ---
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	project, err := cfg.GetProject(projectID)
	if err != nil {
		return err
	}

	// --- Determine workspace directory name ---
	// REQ-COM-02: abort if target directory exists and is non-empty.
	workspaceDir, err := filepath.Abs(targetBranch)
	if err != nil {
		return err
	}
	if info, err := os.Stat(workspaceDir); err == nil && info.IsDir() {
		entries, _ := os.ReadDir(workspaceDir)
		if len(entries) > 0 {
			return fmt.Errorf("workspace directory %q already exists and is not empty (REQ-COM-02)", workspaceDir)
		}
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return fmt.Errorf("create workspace dir %q: %w", workspaceDir, err)
	}

	state := &workspace.State{
		ProjectID:    projectID,
		TargetBranch: targetBranch,
		WorkspaceDir: workspaceDir,
	}

	// --- Process each repo ---
	for _, repoID := range project.Repos {
		repoDef, err := cfg.GetRepository(repoID)
		if err != nil {
			return err
		}

		// Resolve base clone path.
		clonePath, err := workspace.BaseClonePath(repoID)
		if err != nil {
			return err
		}

		// Ensure the base clone exists; clone if not.
		repo, err := ensureBaseClone(repoID, repoDef.URL, clonePath)
		if err != nil {
			return err
		}

		// REQ-COM-01: Fetch before branch resolution.
		render.Info("[%s] Fetching…", repoID)
		if err := repo.Fetch(); err != nil {
			return err
		}

		// Resolve the branch to check out (the "Tuning Logic").
		// REQ-COM-03: honour per-repo default_branch.
		resolvedBranch, err := resolveBranch(repo, repoID, targetBranch, repoDef.DefaultBranch, startAt)
		if err != nil {
			return err
		}
		render.Info("[%s] Resolved branch → %s", repoID, resolvedBranch)

		// Add the worktree.
		worktreePath := filepath.Join(workspaceDir, repoID)
		if err := repo.AddWorktree(worktreePath, resolvedBranch); err != nil {
			return err
		}

		state.Repos = append(state.Repos, workspace.RepoState{
			RepoID:         repoID,
			ExpectedBranch: resolvedBranch,
			WorktreePath:   worktreePath,
			BaseClonePath:  clonePath,
		})

		render.Success("[%s] Worktree ready at %s", repoID, worktreePath)
	}

	// Persist workspace state for tune/mute.
	if err := state.Save(); err != nil {
		return err
	}

	fmt.Println()
	render.Success("Workspace %q composed at %s", targetBranch, workspaceDir)
	return nil
}

// ensureBaseClone returns a Repo for the base clone, cloning it if it doesn't exist yet.
func ensureBaseClone(repoID, url, clonePath string) (*git.Repo, error) {
	if _, err := os.Stat(clonePath); os.IsNotExist(err) {
		render.Info("[%s] Base clone not found — cloning from %s…", repoID, url)
		if err := os.MkdirAll(filepath.Dir(clonePath), 0o755); err != nil {
			return nil, err
		}
		return git.Clone(url, clonePath)
	}
	return git.New(clonePath), nil
}

// resolveBranch implements the "Tuning Logic" from the spec.
//
// Special case ("main"): if targetBranch == "main", use the repo's
// default_branch so chord tune can later validate with the Sanity Rule.
func resolveBranch(repo *git.Repo, repoID, targetBranch, defaultBranch, startAt string) (string, error) {
	// Special case: "main" always maps to the repo's configured default branch.
	if targetBranch == "main" {
		return defaultBranch, nil
	}

	// Check local branch.
	localExists, err := repo.LocalBranchExists(targetBranch)
	if err != nil {
		return "", err
	}
	if localExists {
		return targetBranch, nil
	}

	// Check remote branch.
	remoteExists, err := repo.RemoteBranchExists(targetBranch)
	if err != nil {
		return "", err
	}
	if remoteExists {
		render.Info("[%s] Creating local tracking branch for origin/%s…", repoID, targetBranch)
		if err := repo.TrackRemoteBranch(targetBranch); err != nil {
			return "", err
		}
		return targetBranch, nil
	}

	// No branch found — create from --start-at or default_branch.
	from := defaultBranch
	if startAt != "" {
		from = startAt
	}
	render.Info("[%s] Branch %q not found — creating from %q…", repoID, targetBranch, from)
	if err := repo.CreateBranchFrom(targetBranch, from); err != nil {
		return "", err
	}
	return targetBranch, nil
}

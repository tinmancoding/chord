package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tinmancoding/chord/internal/config"
	"github.com/tinmancoding/chord/internal/git"
	"github.com/tinmancoding/chord/internal/render"
	"github.com/tinmancoding/chord/internal/workspace"
)

// RepoSpec represents a repository specification from CLI or template.
type RepoSpec struct {
	URL           string // Full git URL
	Commitish     string // Branch, tag, or commit SHA
	Name          string // Directory name for the repo
	DefaultBranch string // Default branch from template (if applicable)
}

// NewComposeCmd builds the `chord compose` command.
func NewComposeCmd(cfgPath *string, baseDirOverride *string) *cobra.Command {
	var templateName string
	var branch string
	var from string
	var repos []string
	var onlyRepos string
	var namespaceFlag string

	cmd := &cobra.Command{
		Use:   "compose <chord-name>",
		Short: "Create a new workspace with ad-hoc repository composition",
		Long: `Compose creates a workspace with flexible repository composition.

You can use a template from your config, specify repositories ad-hoc, or mix both.

The chord name can include namespace prefixes (e.g., work/team-a/my-workspace).
Chord names must be unique within their namespace.

Repository specification format: <url@commitish> or <alias@commitish>
- git@github.com:org/repo.git@feature/branch
- https://github.com/org/repo.git@v1.2.3
- alias@main (using aliases from config)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chordName := args[0]
			return runCompose(*cfgPath, *baseDirOverride, chordName, templateName, branch, from, repos, onlyRepos, namespaceFlag)
		},
	}

	cmd.Flags().StringVarP(&templateName, "template", "t", "", "Use a template from config")
	cmd.Flags().StringVar(&branch, "branch", "", "Branch to use for all template repos")
	cmd.Flags().StringVar(&from, "from", "", "When creating new branches, start from this commitish (tag, commit, or branch)")
	cmd.Flags().StringArrayVarP(&repos, "repo", "r", []string{}, "Add repository: <url@commitish> or <alias@commitish>")
	cmd.Flags().StringVar(&onlyRepos, "only", "", "Comma-separated list of repo names to create (defers others)")
	cmd.Flags().StringVarP(&namespaceFlag, "namespace", "n", "", "Namespace for workspace (overrides prefix)")

	return cmd
}

func runCompose(cfgPath, baseDirOverride, chordName, templateName, branch, from string, repos []string, onlyRepos, namespaceFlag string) error {
	// --- Load config ---
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// --- Resolve namespace ---
	namespace := cfg.ResolveNamespace(chordName, templateName, namespaceFlag)
	_, name := config.ParseChordName(chordName)

	// --- Resolve workspace directory ---
	baseDir, err := cfg.EffectiveBaseDir(baseDirOverride)
	if err != nil {
		return fmt.Errorf("resolve base directory: %w", err)
	}

	workspaceDir := workspace.WorkspacePath(baseDir, namespace, name)

	// --- Check if workspace already exists ---
	if info, err := os.Stat(workspaceDir); err == nil && info.IsDir() {
		entries, _ := os.ReadDir(workspaceDir)
		if len(entries) > 0 {
			return fmt.Errorf("workspace directory %q already exists and is not empty", workspaceDir)
		}
	}

	// --- Build list of repositories to compose ---
	repoSpecs, err := buildRepoSpecs(cfg, templateName, branch, repos)
	if err != nil {
		return err
	}

	if len(repoSpecs) == 0 {
		return fmt.Errorf("no repositories specified (use --template or --repo)")
	}

	// --- Parse and validate --only flag ---
	var selectedRepos map[string]bool
	if onlyRepos != "" {
		selectedRepos, err = parseAndValidateRepoList(onlyRepos, repoSpecs)
		if err != nil {
			return err
		}
	}

	// --- Create workspace directory ---
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return fmt.Errorf("create workspace dir %q: %w", workspaceDir, err)
	}

	// --- Initialize workspace state ---
	state := &workspace.State{
		ChordName:    name,
		Namespace:    namespace,
		TemplateName: templateName,
		WorkspaceDir: workspaceDir,
	}

	// --- Get cache directory ---
	cacheDir, err := workspace.BaseCacheDir()
	if err != nil {
		return err
	}

	// --- Process each repository ---
	var deferredRepos []string
	for _, spec := range repoSpecs {
		// Check if this repo should be deferred
		if selectedRepos != nil && !selectedRepos[spec.Name] {
			deferredRepos = append(deferredRepos, spec.Name)
			state.DeferredRepos = append(state.DeferredRepos, workspace.DeferredRepoState{
				Name:           spec.Name,
				URL:            spec.URL,
				ExpectedBranch: spec.Commitish,
				Reason:         "user-deferred",
				LastChecked:    time.Now(),
			})
			continue
		}

		// Ensure cache
		render.Info("[%s] Ensuring cache…", spec.Name)
		cachePath, err := workspace.CachePathForRepo(spec.URL)
		if err != nil {
			return err
		}
		cache, err := git.EnsureCache(spec.Name, spec.URL, cacheDir)
		if err != nil {
			return err
		}

		// Fetch
		render.Info("[%s] Fetching…", spec.Name)
		if err := cache.Fetch(); err != nil {
			return err
		}

		// Resolve branch
		resolvedBranch, err := resolveBranch(cache, spec.Name, spec.Commitish, spec.DefaultBranch, from)
		if err != nil {
			return err
		}
		render.Info("[%s] Resolved branch → %s", spec.Name, resolvedBranch)

		// Clone with reference
		repoPath := filepath.Join(workspaceDir, spec.Name)
		render.Info("[%s] Cloning to %s…", spec.Name, repoPath)
		if err := cache.CloneWithReference(spec.URL, repoPath, resolvedBranch); err != nil {
			return err
		}

		state.Repos = append(state.Repos, workspace.RepoState{
			Name:           spec.Name,
			URL:            spec.URL,
			ExpectedBranch: resolvedBranch,
			Path:           repoPath,
			BaseClonePath:  cachePath,
		})

		render.Success("[%s] Clone ready at %s", spec.Name, repoPath)
	}

	// --- Persist workspace state ---
	if err := state.Save(); err != nil {
		return err
	}

	fmt.Println()
	render.Success("Chord workspace composed at %s", workspaceDir)
	fmt.Printf("  Namespace: %s\n", namespace)
	fmt.Printf("  Chord: %s\n", name)
	fmt.Printf("  Repos: %d\n", len(state.Repos))

	// Show summary of deferred repos if any
	if len(deferredRepos) > 0 {
		fmt.Println()
		render.Info("Deferred repositories (use 'chord tune' to create later):")
		for _, name := range deferredRepos {
			fmt.Printf("  • %s\n", name)
		}
	}

	return nil
}

// buildRepoSpecs builds the list of repositories to compose from template and/or ad-hoc repos.
func buildRepoSpecs(cfg *config.Config, templateName, branch string, repoFlags []string) ([]RepoSpec, error) {
	var specs []RepoSpec
	repoNames := make(map[string]bool) // Track names to prevent duplicates

	// Add repos from template
	if templateName != "" {
		template, err := cfg.GetTemplate(templateName)
		if err != nil {
			return nil, err
		}

		for _, tr := range template.Repos {
			// Determine the commitish: --branch flag overrides template default
			commitish := tr.DefaultBranch
			if branch != "" {
				commitish = branch
			}
			if commitish == "" {
				commitish = "main" // Fallback
			}

			// Determine the repo name
			name := tr.Name
			if name == "" {
				name = workspace.ExtractRepoName(tr.URL)
			}

			if repoNames[name] {
				return nil, fmt.Errorf("duplicate repository name %q", name)
			}
			repoNames[name] = true

			specs = append(specs, RepoSpec{
				URL:           tr.URL,
				Commitish:     commitish,
				Name:          name,
				DefaultBranch: tr.DefaultBranch,
			})
		}
	}

	// Add ad-hoc repos from --repo flags
	for _, repoFlag := range repoFlags {
		spec, err := parseRepoFlag(cfg, repoFlag)
		if err != nil {
			return nil, fmt.Errorf("invalid --repo flag %q: %w", repoFlag, err)
		}

		if repoNames[spec.Name] {
			return nil, fmt.Errorf("duplicate repository name %q", spec.Name)
		}
		repoNames[spec.Name] = true

		specs = append(specs, spec)
	}

	return specs, nil
}

// parseRepoFlag parses a --repo flag value.
// Format: <url@commitish> or <alias@commitish>
// Examples:
//   - git@github.com:org/repo.git@feature/branch
//   - https://github.com/org/repo.git@v1.2.3
//   - alias@main
func parseRepoFlag(cfg *config.Config, flag string) (RepoSpec, error) {
	// Find the last @ separator
	idx := strings.LastIndex(flag, "@")
	if idx == -1 {
		return RepoSpec{}, fmt.Errorf("missing commitish (format: <url@commitish>)")
	}

	urlOrAlias := flag[:idx]
	commitish := flag[idx+1:]

	if urlOrAlias == "" {
		return RepoSpec{}, fmt.Errorf("missing repository URL or alias")
	}
	if commitish == "" {
		return RepoSpec{}, fmt.Errorf("missing commitish")
	}

	// Detect if this looks like an SSH URL without commitish
	// SSH URLs: git@host:org/repo.git or git@host:/path/repo.git
	if strings.HasPrefix(urlOrAlias, "git") && len(urlOrAlias) <= 4 && strings.Contains(commitish, ":") {
		return RepoSpec{}, fmt.Errorf("missing @commitish separator\n"+
			"Format: <url@commitish>\n"+
			"Example: %s@%s@main", urlOrAlias, commitish)
	}

	// Resolve alias
	url := cfg.ResolveAlias(urlOrAlias)

	// Extract repo name
	name := workspace.ExtractRepoName(url)

	return RepoSpec{
		URL:       url,
		Commitish: commitish,
		Name:      name,
	}, nil
}

// resolveBranch implements the "Tuning Logic" from the spec.
//
// Special case ("main"): if commitish == "main", use the repo's
// default_branch if available.
func resolveBranch(repo *git.Repo, name, commitish, defaultBranch, from string) (string, error) {
	// Special case: "main" can map to the repo's configured default branch if available.
	if commitish == "main" && defaultBranch != "" {
		return defaultBranch, nil
	}

	// Check local branch.
	localExists, err := repo.LocalBranchExists(commitish)
	if err != nil {
		return "", err
	}
	if localExists {
		return commitish, nil
	}

	// Check remote branch.
	remoteExists, err := repo.RemoteBranchExists(commitish)
	if err != nil {
		return "", err
	}
	if remoteExists {
		render.Info("[%s] Creating local tracking branch for origin/%s…", name, commitish)
		if err := repo.TrackRemoteBranch(commitish); err != nil {
			return "", err
		}
		return commitish, nil
	}

	// No branch found — create from --from, default_branch, or "main".
	startFrom := "main"
	if from != "" {
		startFrom = from
	} else if defaultBranch != "" {
		startFrom = defaultBranch
	}
	render.Info("[%s] Branch %q not found — creating from %q…", name, commitish, startFrom)
	if err := repo.CreateBranchFrom(commitish, startFrom); err != nil {
		return "", err
	}
	return commitish, nil
}

// parseAndValidateRepoList parses a comma-separated list of repo names and validates them.
func parseAndValidateRepoList(onlyRepos string, repoSpecs []RepoSpec) (map[string]bool, error) {
	parts := strings.Split(onlyRepos, ",")
	selected := make(map[string]bool)

	// Build a set of valid repo names from the specs
	validRepos := make(map[string]bool)
	for _, spec := range repoSpecs {
		validRepos[spec.Name] = true
	}

	// Validate each selected repo
	for _, name := range parts {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !validRepos[name] {
			return nil, fmt.Errorf("invalid repo name %q in --only flag", name)
		}
		selected[name] = true
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("--only flag specified but no valid repo names provided")
	}

	return selected, nil
}

package command

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tinmancoding/chord/internal/config"
	"github.com/tinmancoding/chord/internal/workspace"
)

// NewListCmd builds the `chord list` command.
func NewListCmd(cfgPath *string, baseDirOverride *string) *cobra.Command {
	var namespaceFilter string

	cmd := &cobra.Command{
		Use:     "list [filter]",
		Aliases: []string{"ls"},
		Short:   "List all chord workspaces",
		Long: `List displays all chord workspaces in the base directory.

You can filter by namespace or name pattern.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return runList(*cfgPath, *baseDirOverride, filter, namespaceFilter)
		},
	}

	cmd.Flags().StringVarP(&namespaceFilter, "namespace", "n", "", "Filter by namespace")
	return cmd
}

func runList(cfgPath, baseDirOverride, filter, namespaceFilter string) error {
	// Load config to resolve base directory
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	baseDir, err := cfg.EffectiveBaseDir(baseDirOverride)
	if err != nil {
		return fmt.Errorf("resolve base directory: %w", err)
	}

	// Scan for all workspaces
	states, err := workspace.ScanWorkspaces(baseDir)
	if err != nil {
		return fmt.Errorf("scan workspaces: %w", err)
	}

	if len(states) == 0 {
		fmt.Println("No workspaces found.")
		return nil
	}

	// Apply filters
	filtered := filterStates(states, filter, namespaceFilter)

	if len(filtered) == 0 {
		fmt.Println("No workspaces match the filter criteria.")
		return nil
	}

	// Print header
	fmt.Printf("%-20s  %-30s  %-15s  %-8s  %s\n", "Namespace", "Chord", "Template", "Repos", "Path")
	fmt.Printf("%-20s  %-30s  %-15s  %-8s  %s\n",
		strings.Repeat("-", 20),
		strings.Repeat("-", 30),
		strings.Repeat("-", 15),
		strings.Repeat("-", 8),
		strings.Repeat("-", 40))

	for _, state := range filtered {
		namespace := state.Namespace
		if namespace == "" {
			namespace = "-"
		}

		name := state.ChordName
		if name == "" {
			name = "-"
		}

		template := state.TemplateName
		if template == "" {
			template = "-"
		}

		repoCount := fmt.Sprintf("%d", len(state.Repos))
		if len(state.DeferredRepos) > 0 {
			repoCount = fmt.Sprintf("%d/%d",
				len(state.Repos),
				len(state.Repos)+len(state.DeferredRepos))
		}

		// Make path relative to base directory for cleaner display
		relPath, err := filepath.Rel(baseDir, state.WorkspaceDir)
		if err != nil {
			relPath = state.WorkspaceDir
		}

		fmt.Printf("%-20s  %-30s  %-15s  %-8s  %s\n",
			namespace,
			name,
			template,
			repoCount,
			relPath)
	}

	fmt.Printf("\nTotal workspaces: %d\n", len(filtered))
	return nil
}

// filterStates applies user-specified filters to the list of workspaces.
func filterStates(states []*workspace.State, filter, namespaceFilter string) []*workspace.State {
	var result []*workspace.State

	for _, state := range states {
		// Apply namespace filter
		if namespaceFilter != "" && !strings.Contains(state.Namespace, namespaceFilter) {
			continue
		}

		// Apply name filter (case-insensitive substring match)
		if filter != "" {
			nameMatch := strings.Contains(strings.ToLower(state.ChordName), strings.ToLower(filter))
			if !nameMatch {
				continue
			}
		}

		result = append(result, state)
	}

	return result
}

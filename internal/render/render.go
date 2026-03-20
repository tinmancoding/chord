// Package render handles all terminal output for chord.
package render

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

var (
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	bold   = color.New(color.Bold).SprintFunc()
)

const (
	symbolOK         = "✔"
	symbolWarn       = "⚠"
	symbolDirty      = "✎"
	symbolError      = "✖"
	symbolInTune     = "♪"
	symbolDissonance = "♭"
)

// RepoStatus holds the display data for a single repo row in the check table.
type RepoStatus struct {
	RepoID         string
	CurrentBranch  string
	ExpectedBranch string
	TrackingBranch string
	Ahead          int
	Behind         int
	HasTracking    bool
	InTune         bool
	Dirty          bool
}

// CheckTable prints the harmony status table for `chord check`.
func CheckTable(statuses []RepoStatus, targetBranch string) {
	fmt.Printf("\n  %s  Chord workspace: %s\n\n", bold("♫"), bold(targetBranch))

	table := tablewriter.NewTable(os.Stdout)
	table.Header([]string{"Repo", "Expected Branch", "Current Branch", "Tracking Branch", "Sync Status", "Harmony", "Dirty"})

	allInTune := true
	for _, s := range statuses {
		var harmony string
		if s.InTune {
			harmony = green(symbolInTune + " In Tune")
		} else {
			harmony = yellow(symbolDissonance + " Dissonance")
			allInTune = false
		}

		dirtyStr := ""
		if s.Dirty {
			dirtyStr = yellow(symbolDirty + " Yes")
		}

		trackingBranchStr := s.TrackingBranch
		if trackingBranchStr == "" {
			trackingBranchStr = yellow("(none)")
		}

		// Build sync status string
		syncStatusStr := ""
		if !s.HasTracking {
			syncStatusStr = yellow("(no upstream)")
		} else if s.Ahead == 0 && s.Behind == 0 {
			syncStatusStr = green("✔ In Sync")
		} else if s.Ahead > 0 && s.Behind > 0 {
			syncStatusStr = yellow(fmt.Sprintf("⇅ Diverged (↑%d ↓%d)", s.Ahead, s.Behind))
		} else if s.Ahead > 0 {
			syncStatusStr = yellow(fmt.Sprintf("↑ Ahead %d", s.Ahead))
		} else if s.Behind > 0 {
			syncStatusStr = yellow(fmt.Sprintf("↓ Behind %d", s.Behind))
		}

		table.Append([]string{
			s.RepoID,
			s.ExpectedBranch,
			s.CurrentBranch,
			trackingBranchStr,
			syncStatusStr,
			harmony,
			dirtyStr,
		})
	}

	if err := table.Render(); err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
	}

	fmt.Println()
	if allInTune {
		Success("All repositories are in tune.")
	} else {
		Warn("One or more repositories are out of tune (Dissonance detected).")
	}
	fmt.Println()
}

// TuneTable is deprecated, use CheckTable instead.
// Kept for backward compatibility.
func TuneTable(statuses []RepoStatus, targetBranch string) {
	CheckTable(statuses, targetBranch)
}

// Success prints a green success message.
func Success(msg string, args ...any) {
	fmt.Printf("  %s %s\n", green(symbolOK), fmt.Sprintf(msg, args...))
}

// Info prints a plain informational message.
func Info(msg string, args ...any) {
	fmt.Printf("  → %s\n", fmt.Sprintf(msg, args...))
}

// Warn prints a yellow warning message.
func Warn(msg string, args ...any) {
	fmt.Printf("  %s %s\n", yellow(symbolWarn), fmt.Sprintf(msg, args...))
}

// Error prints a red error message.
func Error(msg string, args ...any) {
	errColor := color.New(color.FgRed).SprintFunc()
	fmt.Fprintf(os.Stderr, "  %s %s\n", errColor(symbolError), fmt.Sprintf(msg, args...))
}

// Bold returns the input string in bold format.
func Bold(s string) string {
	return bold(s)
}

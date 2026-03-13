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

// RepoStatus holds the display data for a single repo row in the tune table.
type RepoStatus struct {
	RepoID         string
	CurrentBranch  string
	ExpectedBranch string
	InTune         bool
	Dirty          bool
}

// TuneTable prints the harmony status table for `chord tune`.
func TuneTable(statuses []RepoStatus, targetBranch string) {
	fmt.Printf("\n  %s  Chord workspace: %s\n\n", bold("♫"), bold(targetBranch))

	table := tablewriter.NewTable(os.Stdout)
	table.Header([]string{"Repo", "Expected Branch", "Current Branch", "Harmony", "Dirty"})

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

		table.Append([]string{
			s.RepoID,
			s.ExpectedBranch,
			s.CurrentBranch,
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

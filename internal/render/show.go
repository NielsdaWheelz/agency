// Package render provides output formatting for agency commands.
// This file implements show-specific rendering.
package render

import (
	"fmt"
	"io"
	"path/filepath"
)

// ShowPathsData holds the paths for --path output.
type ShowPathsData struct {
	RepoRoot       string // may be empty if unknown
	WorktreeRoot   string
	RunDir         string
	LogsDir        string
	EventsPath     string
	TranscriptPath string
	ReportPath     string
}

// RunnerStatusDisplay holds runner status data for human show output.
type RunnerStatusDisplay struct {
	// Status is the runner-reported status (working, needs_input, blocked, ready_for_review).
	Status string

	// UpdatedAt is the time since last update (e.g., "5m ago").
	UpdatedAt string

	// Summary is the runner-reported summary.
	Summary string

	// Questions are the questions the runner is waiting for (needs_input status).
	Questions []string

	// Blockers are the blockers preventing progress (blocked status).
	Blockers []string

	// HowToTest is the testing instructions (ready_for_review status).
	HowToTest string

	// Risks are potential risks identified by the runner.
	Risks []string
}

// ShowHumanData holds the data for human show output.
type ShowHumanData struct {
	// Core
	RunID     string
	Name      string
	Runner    string
	CreatedAt string // RFC3339
	RepoID    string
	RepoKey   string // may be empty
	OriginURL string // may be empty

	// Git/workspace
	ParentBranch    string
	Branch          string
	WorktreePath    string
	WorktreePresent bool
	TmuxSessionName string
	TmuxActive      bool

	// PR (may be zero values)
	PRNumber         int
	PRURL            string
	LastPushAt       string // RFC3339
	LastReportSyncAt string // RFC3339
	LastReportHash   string // sha256 hex

	// Report
	ReportPath   string
	ReportExists bool
	ReportBytes  int

	// Logs
	SetupLogPath   string
	VerifyLogPath  string
	ArchiveLogPath string

	// Derived
	DerivedStatus string
	Archived      bool

	// Runner status (nil if no runner_status.json or invalid)
	RunnerStatus *RunnerStatusDisplay

	// Warnings
	RepoNotFoundWarning    bool
	WorktreeMissingWarning bool
	TmuxUnavailableWarning bool
}

// WriteShowPaths writes --path output in the locked format.
// Exits early on error; returns nil on success.
func WriteShowPaths(w io.Writer, data ShowPathsData) error {
	lines := []struct {
		key   string
		value string
	}{
		{"repo_root", data.RepoRoot},
		{"worktree_root", data.WorktreeRoot},
		{"run_dir", data.RunDir},
		{"logs_dir", data.LogsDir},
		{"events_path", data.EventsPath},
		{"transcript_path", data.TranscriptPath},
		{"report_path", data.ReportPath},
	}

	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s: %s\n", line.key, line.value); err != nil {
			return err
		}
	}
	return nil
}

// WriteShowHuman writes human-readable show output in the spec-defined format.
// Per PR-4 spec, output is plain key/value lines in exact order.
func WriteShowHuman(w io.Writer, data ShowHumanData) error {
	// Format name for display
	displayName := data.Name
	if displayName == "" {
		displayName = NameUntitled
	}

	// Format tmux session display
	tmuxDisplay := data.TmuxSessionName
	if tmuxDisplay == "" || !data.TmuxActive {
		if data.TmuxSessionName == "" {
			tmuxDisplay = "none"
		}
	}

	// Format PR display per spec: pr: <url|none> (#<number|->)
	prURLDisplay := data.PRURL
	if prURLDisplay == "" {
		prURLDisplay = "none"
	}
	prNumberDisplay := "-"
	if data.PRNumber != 0 {
		prNumberDisplay = fmt.Sprintf("%d", data.PRNumber)
	}

	// Format timestamps (none if empty)
	lastPushDisplay := data.LastPushAt
	if lastPushDisplay == "" {
		lastPushDisplay = "none"
	}
	lastReportSyncDisplay := data.LastReportSyncAt
	if lastReportSyncDisplay == "" {
		lastReportSyncDisplay = "none"
	}
	reportHashDisplay := data.LastReportHash
	if reportHashDisplay == "" {
		reportHashDisplay = "none"
	}

	// Format status with archived suffix if applicable
	statusDisplay := formatStatus(data.DerivedStatus, data.Archived)

	// Output in spec-defined order with blank line between worktree and tmux
	_, _ = fmt.Fprintf(w, "run: %s\n", data.RunID)
	_, _ = fmt.Fprintf(w, "name: %s\n", displayName)
	_, _ = fmt.Fprintf(w, "repo: %s\n", data.RepoID)
	_, _ = fmt.Fprintf(w, "runner: %s\n", data.Runner)
	_, _ = fmt.Fprintf(w, "parent: %s\n", data.ParentBranch)
	_, _ = fmt.Fprintf(w, "branch: %s\n", data.Branch)
	_, _ = fmt.Fprintf(w, "worktree: %s\n", data.WorktreePath)

	// Blank line between worktree and tmux (per spec)
	_, _ = fmt.Fprintln(w)

	_, _ = fmt.Fprintf(w, "tmux: %s\n", tmuxDisplay)
	_, _ = fmt.Fprintf(w, "pr: %s (#%s)\n", prURLDisplay, prNumberDisplay)
	_, _ = fmt.Fprintf(w, "last_push_at: %s\n", lastPushDisplay)
	_, _ = fmt.Fprintf(w, "last_report_sync_at: %s\n", lastReportSyncDisplay)
	_, _ = fmt.Fprintf(w, "report_hash: %s\n", reportHashDisplay)
	_, _ = fmt.Fprintf(w, "status: %s\n", statusDisplay)

	// Runner status section (if available)
	if data.RunnerStatus != nil {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "runner_status:")
		_, _ = fmt.Fprintf(w, "  status: %s\n", data.RunnerStatus.Status)
		_, _ = fmt.Fprintf(w, "  updated: %s\n", data.RunnerStatus.UpdatedAt)
		_, _ = fmt.Fprintf(w, "  summary: %s\n", data.RunnerStatus.Summary)

		// Show questions if present (needs_input status)
		if len(data.RunnerStatus.Questions) > 0 {
			_, _ = fmt.Fprintln(w, "  questions:")
			for _, q := range data.RunnerStatus.Questions {
				_, _ = fmt.Fprintf(w, "    - %s\n", q)
			}
		}

		// Show blockers if present (blocked status)
		if len(data.RunnerStatus.Blockers) > 0 {
			_, _ = fmt.Fprintln(w, "  blockers:")
			for _, b := range data.RunnerStatus.Blockers {
				_, _ = fmt.Fprintf(w, "    - %s\n", b)
			}
		}

		// Show how_to_test if present (ready_for_review status)
		if data.RunnerStatus.HowToTest != "" {
			_, _ = fmt.Fprintf(w, "  how_to_test: %s\n", data.RunnerStatus.HowToTest)
		}

		// Show risks if present
		if len(data.RunnerStatus.Risks) > 0 {
			_, _ = fmt.Fprintln(w, "  risks:")
			for _, r := range data.RunnerStatus.Risks {
				_, _ = fmt.Fprintf(w, "    - %s\n", r)
			}
		}
	}

	return nil
}

// ResolveScriptLogPaths resolves the log paths for setup/verify/archive scripts.
// Uses the canonical s1 log path format: <run_dir>/logs/<script>.log
// Returns absolute paths even if files don't exist (for display purposes).
func ResolveScriptLogPaths(runDir string) (setup, verify, archive string) {
	logsDir := filepath.Join(runDir, "logs")
	setup = filepath.Join(logsDir, "setup.log")
	verify = filepath.Join(logsDir, "verify.log")
	archive = filepath.Join(logsDir, "archive.log")
	return
}

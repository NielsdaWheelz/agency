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

// ShowHumanData holds the data for human show output.
type ShowHumanData struct {
	// Core
	RunID     string
	Title     string
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
	PRNumber   int
	PRURL      string
	LastPushAt string // RFC3339

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

	// Warnings
	RepoNotFoundWarning     bool
	WorktreeMissingWarning  bool
	TmuxUnavailableWarning  bool
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

// WriteShowHuman writes human-readable show output.
func WriteShowHuman(w io.Writer, data ShowHumanData) error {
	// Helper for yes/no booleans
	yesNo := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}

	// Helper for optional string
	optStr := func(s string) string {
		if s == "" {
			return ""
		}
		return s
	}

	// Format title for display
	displayTitle := data.Title
	if displayTitle == "" {
		displayTitle = TitleUntitled
	}

	// === HEADER / CORE ===
	fmt.Fprintln(w, "=== run ===")
	fmt.Fprintf(w, "run_id: %s\n", data.RunID)
	fmt.Fprintf(w, "title: %s\n", displayTitle)
	fmt.Fprintf(w, "runner: %s\n", data.Runner)
	fmt.Fprintf(w, "created_at: %s\n", data.CreatedAt)
	fmt.Fprintf(w, "repo_id: %s\n", data.RepoID)
	if data.RepoKey != "" {
		fmt.Fprintf(w, "repo_key: %s\n", data.RepoKey)
	}
	if data.OriginURL != "" {
		fmt.Fprintf(w, "origin_url: %s\n", data.OriginURL)
	}

	// === GIT/WORKSPACE ===
	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== workspace ===")
	fmt.Fprintf(w, "parent_branch: %s\n", data.ParentBranch)
	fmt.Fprintf(w, "branch: %s\n", data.Branch)
	fmt.Fprintf(w, "worktree_path: %s\n", data.WorktreePath)
	fmt.Fprintf(w, "worktree_present: %s\n", yesNo(data.WorktreePresent))
	fmt.Fprintf(w, "tmux_session_name: %s\n", data.TmuxSessionName)
	fmt.Fprintf(w, "tmux_active: %s\n", yesNo(data.TmuxActive))

	// === PR (if present) ===
	if data.PRNumber != 0 || data.PRURL != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "=== pr ===")
		if data.PRNumber != 0 {
			fmt.Fprintf(w, "pr_number: %d\n", data.PRNumber)
		}
		if data.PRURL != "" {
			fmt.Fprintf(w, "pr_url: %s\n", data.PRURL)
		}
		if data.LastPushAt != "" {
			fmt.Fprintf(w, "last_push_at: %s\n", optStr(data.LastPushAt))
		}
	}

	// === REPORT ===
	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== report ===")
	fmt.Fprintf(w, "report_path: %s\n", data.ReportPath)
	fmt.Fprintf(w, "report_exists: %s\n", yesNo(data.ReportExists))
	fmt.Fprintf(w, "report_bytes: %d\n", data.ReportBytes)

	// === LOGS ===
	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== logs ===")
	fmt.Fprintf(w, "setup_log: %s\n", data.SetupLogPath)
	fmt.Fprintf(w, "verify_log: %s\n", data.VerifyLogPath)
	fmt.Fprintf(w, "archive_log: %s\n", data.ArchiveLogPath)

	// === DERIVED ===
	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== status ===")
	statusDisplay := formatStatus(data.DerivedStatus, data.Archived)
	fmt.Fprintf(w, "derived_status: %s\n", statusDisplay)
	fmt.Fprintf(w, "archived: %s\n", yesNo(data.Archived))

	// === WARNINGS ===
	if data.RepoNotFoundWarning || data.WorktreeMissingWarning || data.TmuxUnavailableWarning {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "=== warnings ===")
		if data.RepoNotFoundWarning {
			fmt.Fprintln(w, "warning: repo not found on disk")
		}
		if data.WorktreeMissingWarning {
			fmt.Fprintln(w, "warning: worktree archived/missing")
		}
		if data.TmuxUnavailableWarning {
			fmt.Fprintln(w, "warning: tmux unavailable; tmux_active=false")
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

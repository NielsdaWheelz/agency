// Package render provides output formatting for agency commands.
package render

import (
	"encoding/json"
	"io"
	"time"

	"github.com/NielsdaWheelz/agency/internal/store"
)

// RunSummary represents a run in ls output (both human and JSON).
// This is the public contract for ls --json output.
type RunSummary struct {
	// RunID is the run identifier from directory name (canonical).
	RunID string `json:"run_id"`

	// RepoID is the repo identifier from directory name (canonical).
	RepoID string `json:"repo_id"`

	// RepoKey is the repo key from repo.json (nullable if missing/corrupt).
	RepoKey *string `json:"repo_key"`

	// OriginURL is the origin URL from repo.json (nullable if missing/corrupt).
	OriginURL *string `json:"origin_url"`

	// Title is the run title ("<broken>" for broken runs).
	Title string `json:"title"`

	// Runner is the runner name (null for broken runs).
	Runner *string `json:"runner"`

	// CreatedAt is the creation timestamp in RFC3339Nano (null for broken runs).
	CreatedAt *time.Time `json:"created_at"`

	// LastPushAt is the last push timestamp (null if not pushed).
	LastPushAt *time.Time `json:"last_push_at"`

	// TmuxActive indicates whether the tmux session exists.
	TmuxActive bool `json:"tmux_active"`

	// WorktreePresent indicates whether the worktree exists on disk.
	WorktreePresent bool `json:"worktree_present"`

	// Archived is true if the worktree is not present.
	Archived bool `json:"archived"`

	// PRNumber is the GitHub PR number (null if no PR).
	PRNumber *int `json:"pr_number"`

	// PRURL is the GitHub PR URL (null if no PR).
	PRURL *string `json:"pr_url"`

	// DerivedStatus is the human-readable status string.
	DerivedStatus string `json:"derived_status"`

	// Broken indicates whether meta.json is unreadable/invalid.
	Broken bool `json:"broken"`
}

// LSJSONEnvelope is the stable JSON output format for ls --json.
type LSJSONEnvelope struct {
	SchemaVersion string       `json:"schema_version"`
	Data          []RunSummary `json:"data"`
}

// WriteLSJSON writes the ls output as JSON to the given writer.
func WriteLSJSON(w io.Writer, summaries []RunSummary) error {
	env := LSJSONEnvelope{
		SchemaVersion: "1.0",
		Data:          summaries,
	}
	// Use empty slice if nil for valid JSON array output
	if env.Data == nil {
		env.Data = []RunSummary{}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// ============================================================================
// Show command JSON types
// ============================================================================

// RunDetail represents the full run detail for show --json output.
// This is the public contract for show --json output (v1 stable).
type RunDetail struct {
	// Meta is the raw parsed meta.json object; null if broken.
	Meta *store.RunMeta `json:"meta"`

	// RepoID is the repo identifier from directory name (canonical).
	RepoID string `json:"repo_id"`

	// RepoKey is the repo key from repo.json (nullable if missing/corrupt).
	RepoKey *string `json:"repo_key"`

	// OriginURL is the origin URL from repo.json (nullable if missing/corrupt).
	OriginURL *string `json:"origin_url"`

	// Archived is true if the worktree is not present.
	Archived bool `json:"archived"`

	// Derived contains computed status and presence fields.
	Derived DerivedJSON `json:"derived"`

	// Paths contains resolved filesystem paths.
	Paths PathsJSON `json:"paths"`

	// Broken indicates whether meta.json is unreadable/invalid.
	Broken bool `json:"broken"`
}

// DerivedJSON contains derived status and presence fields for show --json.
type DerivedJSON struct {
	// DerivedStatus is the human-readable status string.
	DerivedStatus string `json:"derived_status"`

	// TmuxActive is true iff the tmux session exists.
	TmuxActive bool `json:"tmux_active"`

	// WorktreePresent is true iff the worktree path exists on disk.
	WorktreePresent bool `json:"worktree_present"`

	// Report contains report file info.
	Report ReportJSON `json:"report"`

	// Logs contains log file paths.
	Logs LogsJSON `json:"logs"`
}

// ReportJSON contains report file info for show --json.
type ReportJSON struct {
	// Exists is true iff the report file exists.
	Exists bool `json:"exists"`

	// Bytes is the file size in bytes (0 if missing).
	Bytes int `json:"bytes"`

	// Path is the absolute path to the report file.
	Path string `json:"path"`
}

// LogsJSON contains script log paths for show --json.
type LogsJSON struct {
	// SetupLogPath is the path to setup.log.
	SetupLogPath string `json:"setup_log_path"`

	// VerifyLogPath is the path to verify.log.
	VerifyLogPath string `json:"verify_log_path"`

	// ArchiveLogPath is the path to archive.log.
	ArchiveLogPath string `json:"archive_log_path"`
}

// PathsJSON contains resolved filesystem paths for show --json.
type PathsJSON struct {
	// RepoRoot is the resolved repo root path (nullable if unknown).
	RepoRoot *string `json:"repo_root"`

	// WorktreeRoot is the worktree path from meta.
	WorktreeRoot string `json:"worktree_root"`

	// RunDir is the run directory path.
	RunDir string `json:"run_dir"`

	// EventsPath is the path to events.jsonl.
	EventsPath string `json:"events_path"`

	// TranscriptPath is the path to transcript.txt.
	TranscriptPath string `json:"transcript_path"`
}

// ShowJSONEnvelope is the stable JSON output format for show --json.
type ShowJSONEnvelope struct {
	SchemaVersion string     `json:"schema_version"`
	Data          *RunDetail `json:"data"` // nullable on error
}

// WriteShowJSON writes the show output as JSON to the given writer.
func WriteShowJSON(w io.Writer, detail *RunDetail) error {
	env := ShowJSONEnvelope{
		SchemaVersion: "1.0",
		Data:          detail,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

package store

import (
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
)

// VerifyRecordSchemaVersion is the current schema version for verify_record.json.
const VerifyRecordSchemaVersion = "1.0"

// VerifyRecord is the canonical evidence record for a verify run.
// Written to a store-owned verify_record.json path for the verified execution surface.
type VerifyRecord struct {
	// SchemaVersion is the schema version for this record.
	SchemaVersion string `json:"schema_version"`

	// RepoID is the repository identifier (16 hex chars).
	RepoID string `json:"repo_id"`

	// RunID is the unique run identifier.
	RunID string `json:"run_id"`

	// ScriptPath is the exact script string executed (from agency.json), not a realpath.
	ScriptPath string `json:"script_path"`

	// StartedAt is the RFC3339Nano UTC timestamp when verify started.
	StartedAt string `json:"started_at,omitempty"`

	// FinishedAt is the RFC3339Nano UTC timestamp when verify finished.
	FinishedAt string `json:"finished_at,omitempty"`

	// DurationMS is the duration of the verify script in milliseconds.
	DurationMS int64 `json:"duration_ms"`

	// TimeoutMS is the configured timeout in milliseconds.
	TimeoutMS int64 `json:"timeout_ms"`

	// TimedOut is true if the verify script exceeded the timeout.
	// TimedOut and Cancelled are mutually exclusive.
	TimedOut bool `json:"timed_out"`

	// Cancelled is true if the user interrupted the verify script (SIGINT).
	// TimedOut and Cancelled are mutually exclusive.
	Cancelled bool `json:"cancelled"`

	// ExitCode is the exit code of the verify script.
	// null if process failed to start or was terminated by signal.
	ExitCode *int `json:"exit_code"`

	// Signal is the signal name (e.g., "SIGKILL") if process was terminated by signal.
	// null otherwise.
	Signal *string `json:"signal"`

	// Error is a human-readable string for internal failures only:
	// exec failed, log open failed, json write failed.
	// Not used for script failures (those use ExitCode).
	Error *string `json:"error"`

	// OK is the final verification result after applying precedence rules.
	OK bool `json:"ok"`

	// VerifyJSONPath is the path to <worktree>/.agency/out/verify.json if it existed.
	// null if absent.
	VerifyJSONPath *string `json:"verify_json_path"`

	// LogPath is the absolute path to the verify log file.
	LogPath string `json:"log_path"`

	// Summary is the human-readable summary.
	// Prefers verify.json.summary if present, else generic message.
	Summary string `json:"summary"`
}

// WriteIntegrationWorktreeVerifyRecord writes verify_record.json atomically for
// an integration worktree verify run.
func (s *Store) WriteIntegrationWorktreeVerifyRecord(repoID, worktreeID string, record VerifyRecord) error {
	recordPath := s.integrationWorktreeVerifyRecordPath(repoID, worktreeID)
	if err := agencyfs.WriteJSONAtomic(s.fsys, recordPath, record, 0o600); err != nil {
		return errors.WrapWithDetails(
			errors.EPersistFailed,
			"failed to write integration worktree verify_record.json atomically",
			err,
			map[string]string{"record_path": recordPath},
		)
	}
	return nil
}

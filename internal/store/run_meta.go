package store

import (
	"encoding/json"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// RunMeta represents the metadata for a run, persisted to meta.json.
// This is the public contract per the constitution.
type RunMeta struct {
	// SchemaVersion is the schema version string (e.g., "1.0").
	SchemaVersion string `json:"schema_version"`

	// RunID is the unique identifier for this run.
	RunID string `json:"run_id"`

	// RepoID is the repository identifier (16 hex chars).
	RepoID string `json:"repo_id"`

	// Title is the run title (may be empty; not slugified).
	Title string `json:"title"`

	// Runner is the runner name (e.g., "claude" or "codex").
	Runner string `json:"runner"`

	// RunnerCmd is the verbatim shell command string for the runner.
	RunnerCmd string `json:"runner_cmd"`

	// ParentBranch is the local branch this run branched from.
	ParentBranch string `json:"parent_branch"`

	// Branch is the full branch name (e.g., "agency/my-feature-a3f2").
	Branch string `json:"branch"`

	// WorktreePath is the absolute path to the worktree directory.
	WorktreePath string `json:"worktree_path"`

	// CreatedAt is the creation timestamp in RFC3339 UTC format.
	CreatedAt string `json:"created_at"`

	// TmuxSessionName is the tmux session name (set only on successful tmux creation).
	// Omit when writing initial meta (PR-06); set in PR-08.
	TmuxSessionName string `json:"tmux_session_name,omitempty"`

	// Flags contains optional boolean flags for run state.
	Flags *RunMetaFlags `json:"flags,omitempty"`

	// Setup contains optional setup script execution details.
	Setup *RunMetaSetup `json:"setup,omitempty"`

	// PRNumber is the GitHub PR number (set by push, not in PR-06).
	PRNumber int `json:"pr_number,omitempty"`

	// PRURL is the GitHub PR URL (set by push, not in PR-06).
	PRURL string `json:"pr_url,omitempty"`

	// LastPushAt is the timestamp of the last push (set by push, not in PR-06).
	LastPushAt string `json:"last_push_at,omitempty"`

	// LastVerifyAt is the timestamp of the last verify (set by merge, not in PR-06).
	LastVerifyAt string `json:"last_verify_at,omitempty"`

	// Archive contains archive-related fields (set by merge/clean, not in PR-06).
	Archive *RunMetaArchive `json:"archive,omitempty"`
}

// RunMetaFlags contains optional boolean flags for run state.
type RunMetaFlags struct {
	// SetupFailed is true if the setup script failed.
	SetupFailed bool `json:"setup_failed,omitempty"`

	// TmuxFailed is true if tmux session creation failed.
	TmuxFailed bool `json:"tmux_failed,omitempty"`

	// NeedsAttention is true if the run requires user attention.
	NeedsAttention bool `json:"needs_attention,omitempty"`

	// Abandoned is true if the run was abandoned by the user.
	Abandoned bool `json:"abandoned,omitempty"`
}

// RunMetaSetup contains setup script execution details.
type RunMetaSetup struct {
	// ExitCode is the exit code of the setup script.
	ExitCode int `json:"exit_code,omitempty"`

	// DurationMs is the duration of the setup script in milliseconds.
	DurationMs int64 `json:"duration_ms,omitempty"`

	// TimedOut is true if the setup script timed out.
	TimedOut bool `json:"timed_out,omitempty"`
}

// RunMetaArchive contains archive-related fields.
type RunMetaArchive struct {
	// ArchivedAt is the timestamp when the run was archived.
	ArchivedAt string `json:"archived_at,omitempty"`

	// MergedAt is the timestamp when the PR was merged.
	MergedAt string `json:"merged_at,omitempty"`
}

// EnsureRunDir creates the run directory with exclusive semantics.
// Returns the run dir path on success.
// Fails with E_RUN_DIR_EXISTS if the directory already exists.
// Fails with E_RUN_DIR_CREATE_FAILED for other mkdir errors.
//
// This function also creates the logs/ subdirectory.
func (s *Store) EnsureRunDir(repoID, runID string) (string, error) {
	runDir := s.RunDir(repoID, runID)

	// Ensure parent directories exist (repos/<repo_id>/runs/)
	runsDir := s.RunsDir(repoID)
	if err := s.FS.MkdirAll(runsDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.ERunDirCreateFailed,
			"failed to create runs directory",
			err,
			map[string]string{"runs_dir": runsDir},
		)
	}

	// Create run directory with exclusive semantics using os.Mkdir
	// This fails if the directory already exists
	if err := os.Mkdir(runDir, 0o700); err != nil {
		if os.IsExist(err) {
			return "", errors.NewWithDetails(
				errors.ERunDirExists,
				"run directory already exists (run_id collision or stale state)",
				map[string]string{"run_dir": runDir},
			)
		}
		return "", errors.WrapWithDetails(
			errors.ERunDirCreateFailed,
			"failed to create run directory",
			err,
			map[string]string{"run_dir": runDir},
		)
	}

	// Create logs subdirectory
	logsDir := s.RunLogsDir(repoID, runID)
	if err := s.FS.MkdirAll(logsDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.ERunDirCreateFailed,
			"failed to create logs directory",
			err,
			map[string]string{"logs_dir": logsDir},
		)
	}

	return runDir, nil
}

// WriteInitialMeta writes the initial meta.json for a run atomically.
// The meta parameter should contain all required fields.
// Returns E_META_WRITE_FAILED on any write error.
func (s *Store) WriteInitialMeta(repoID, runID string, meta *RunMeta) error {
	metaPath := s.RunMetaPath(repoID, runID)

	if err := fs.WriteJSONAtomic(metaPath, meta, 0o644); err != nil {
		return errors.WrapWithDetails(
			errors.EMetaWriteFailed,
			"failed to write meta.json atomically",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	return nil
}

// UpdateMeta reads, updates, and writes meta.json atomically.
// The updateFn receives the current meta and should modify it in place.
// Returns E_META_WRITE_FAILED on read or write errors.
func (s *Store) UpdateMeta(repoID, runID string, updateFn func(*RunMeta)) error {
	metaPath := s.RunMetaPath(repoID, runID)

	// Read current meta
	meta, err := s.ReadMeta(repoID, runID)
	if err != nil {
		return err
	}

	// Apply update
	updateFn(meta)

	// Write back atomically
	if err := fs.WriteJSONAtomic(metaPath, meta, 0o644); err != nil {
		return errors.WrapWithDetails(
			errors.EMetaWriteFailed,
			"failed to write meta.json atomically",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	return nil
}

// ReadMeta reads and parses meta.json for a run.
// Returns E_RUN_NOT_FOUND if the meta file doesn't exist.
// Returns E_STORE_CORRUPT if the file can't be parsed.
func (s *Store) ReadMeta(repoID, runID string) (*RunMeta, error) {
	metaPath := s.RunMetaPath(repoID, runID)

	data, err := s.FS.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.NewWithDetails(
				errors.ERunNotFound,
				"run not found (meta.json does not exist)",
				map[string]string{"meta_path": metaPath},
			)
		}
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to read meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	var meta RunMeta
	if err := jsonUnmarshal(data, &meta); err != nil {
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to parse meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	return &meta, nil
}

// NewRunMeta creates a new RunMeta with required fields set.
// createdAt should be the current time in UTC.
func NewRunMeta(runID, repoID, title, runner, runnerCmd, parentBranch, branch, worktreePath string, createdAt time.Time) *RunMeta {
	return &RunMeta{
		SchemaVersion: "1.0",
		RunID:         runID,
		RepoID:        repoID,
		Title:         title,
		Runner:        runner,
		RunnerCmd:     runnerCmd,
		ParentBranch:  parentBranch,
		Branch:        branch,
		WorktreePath:  worktreePath,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
	}
}

// jsonUnmarshal wraps json.Unmarshal (can be stubbed for testing).
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

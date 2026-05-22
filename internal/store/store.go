// Package store provides persistence for repo_index.json and repo.json files.
// Files are written atomically via temp file + rename.
package store

import (
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// validateSchemaVersion checks that version equals the canonical SchemaVersion.
// It returns a typed EStoreCorrupt error keyed to a human label and the on-disk path.
func validateSchemaVersion(version, label, path string) error {
	if version == "" {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			label+" missing schema_version",
			map[string]string{"path": path},
		)
	}
	if version != SchemaVersion {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			label+" has unsupported schema_version",
			map[string]string{
				"path":            path,
				"schema_version":  version,
				"expected_schema": SchemaVersion,
			},
		)
	}
	return nil
}

// Store handles persistence of repo index and repo records.
type Store struct {
	fsys    fs.FS
	now     func() time.Time
	DataDir string // resolved AGENCY_DATA_DIR
}

// NewStore creates a new Store with the given dependencies.
func NewStore(filesystem fs.FS, dataDir string, now func() time.Time) *Store {
	if filesystem == nil {
		filesystem = fs.NewRealFS()
	}
	if now == nil {
		now = time.Now
	}
	return &Store{
		fsys:    filesystem,
		DataDir: dataDir,
		now:     now,
	}
}

func (s *Store) nowTime() time.Time {
	return s.now()
}

// RepoIndexPath returns the path to repo_index.json.
func (s *Store) RepoIndexPath() string {
	return filepath.Join(s.DataDir, "repo_index.json")
}

// RepoDir returns the directory for a repo's data.
func (s *Store) RepoDir(repoID string) string {
	return filepath.Join(s.DataDir, "repos", repoID)
}

// RepoRecordPath returns the path to a repo's repo.json.
func (s *Store) RepoRecordPath(repoID string) string {
	return filepath.Join(s.RepoDir(repoID), "repo.json")
}

// RepoEventsPath returns the path to a repo's events.jsonl.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/events.jsonl
func (s *Store) RepoEventsPath(repoID string) string {
	return filepath.Join(s.RepoDir(repoID), "events.jsonl")
}

// tasksDir returns the tasks directory for a repo.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/tasks/
func (s *Store) tasksDir(repoID string) string {
	return filepath.Join(s.RepoDir(repoID), "tasks")
}

// taskDir returns the directory for a specific task.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/tasks/<task_id>/
func (s *Store) taskDir(repoID, taskID string) string {
	return filepath.Join(s.tasksDir(repoID), taskID)
}

// taskMetaPath returns the path to a task's meta.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/tasks/<task_id>/meta.json
func (s *Store) taskMetaPath(repoID, taskID string) string {
	return filepath.Join(s.taskDir(repoID, taskID), "meta.json")
}

// TaskEventsPath returns the path to a task's events.jsonl.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/tasks/<task_id>/events.jsonl
func (s *Store) TaskEventsPath(repoID, taskID string) string {
	return filepath.Join(s.taskDir(repoID, taskID), "events.jsonl")
}

// integrationWorktreesDir returns the integration worktrees directory for a repo.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/
func (s *Store) integrationWorktreesDir(repoID string) string {
	return filepath.Join(s.RepoDir(repoID), "integration_worktrees")
}

// IntegrationWorktreeDir returns the directory for a specific integration worktree.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/
func (s *Store) IntegrationWorktreeDir(repoID, worktreeID string) string {
	return filepath.Join(s.integrationWorktreesDir(repoID), worktreeID)
}

// IntegrationWorktreeMetaPath returns the path to an integration worktree's meta.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/meta.json
func (s *Store) IntegrationWorktreeMetaPath(repoID, worktreeID string) string {
	return filepath.Join(s.IntegrationWorktreeDir(repoID, worktreeID), "meta.json")
}

// IntegrationWorktreeEventsPath returns the path to an integration worktree's events.jsonl.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/events.jsonl
func (s *Store) IntegrationWorktreeEventsPath(repoID, worktreeID string) string {
	return filepath.Join(s.IntegrationWorktreeDir(repoID, worktreeID), "events.jsonl")
}

// IntegrationWorktreeMergePath returns the path to an integration worktree's merge.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/merge.json
func (s *Store) IntegrationWorktreeMergePath(repoID, worktreeID string) string {
	return filepath.Join(s.IntegrationWorktreeDir(repoID, worktreeID), "merge.json")
}

// integrationWorktreeVerifyRecordPath returns the path to an integration worktree's verify_record.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/verify_record.json
func (s *Store) integrationWorktreeVerifyRecordPath(repoID, worktreeID string) string {
	return filepath.Join(s.IntegrationWorktreeDir(repoID, worktreeID), "verify_record.json")
}

// IntegrationWorktreeLogsDir returns the logs directory for an integration worktree.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/logs/
func (s *Store) IntegrationWorktreeLogsDir(repoID, worktreeID string) string {
	return filepath.Join(s.IntegrationWorktreeDir(repoID, worktreeID), "logs")
}

// InvocationsDir returns the invocations directory for a repo.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/
func (s *Store) InvocationsDir(repoID string) string {
	return filepath.Join(s.RepoDir(repoID), "invocations")
}

// InvocationDir returns the directory for a specific invocation.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/
func (s *Store) InvocationDir(repoID, invocationID string) string {
	return filepath.Join(s.InvocationsDir(repoID), invocationID)
}

// InvocationMetaPath returns the path to an invocation's meta.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/meta.json
func (s *Store) InvocationMetaPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationDir(repoID, invocationID), "meta.json")
}

// InvocationEventsPath returns the path to an invocation's events.jsonl.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/events.jsonl
func (s *Store) InvocationEventsPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationDir(repoID, invocationID), "events.jsonl")
}

// InvocationPromptPath returns the path to an invocation's prompt.txt.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/prompt.txt
func (s *Store) InvocationPromptPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationDir(repoID, invocationID), "prompt.txt")
}

// InvocationCheckpointsPath returns the path to an invocation's checkpoints.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/checkpoints.json
func (s *Store) InvocationCheckpointsPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationDir(repoID, invocationID), "checkpoints.json")
}

// InvocationLogsDir returns the logs directory for an invocation.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/logs/
func (s *Store) InvocationLogsDir(repoID, invocationID string) string {
	return filepath.Join(s.InvocationDir(repoID, invocationID), "logs")
}

// InvocationRawLogPath returns the path to an invocation's raw.jsonl log file.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/logs/raw.jsonl
func (s *Store) InvocationRawLogPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationLogsDir(repoID, invocationID), "raw.jsonl")
}

// InvocationStderrLogPath returns the path to an invocation's stderr.log file.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/logs/stderr.log
func (s *Store) InvocationStderrLogPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationLogsDir(repoID, invocationID), "stderr.log")
}

// InvocationStreamLogPath returns the path to an invocation's stream.jsonl file.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/logs/stream.jsonl
func (s *Store) InvocationStreamLogPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationLogsDir(repoID, invocationID), "stream.jsonl")
}

// InvocationHooksLogPath returns the path to an invocation's hooks.jsonl file.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/logs/hooks.jsonl
func (s *Store) InvocationHooksLogPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationLogsDir(repoID, invocationID), "hooks.jsonl")
}

// InvocationTerminalLogPath returns the path to an invocation's terminal.log file.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/logs/terminal.log
func (s *Store) InvocationTerminalLogPath(repoID, invocationID string) string {
	return filepath.Join(s.InvocationLogsDir(repoID, invocationID), "terminal.log")
}

// ----- Daemon state paths -----

// DaemonPidPath returns the path to the daemon's pid file.
// Format: ${AGENCY_DATA_DIR}/agencyd.pid
func (s *Store) DaemonPidPath() string {
	return filepath.Join(s.DataDir, "agencyd.pid")
}

// DaemonSocketPath returns the path to the daemon's Unix socket.
// Format: ${AGENCY_DATA_DIR}/agencyd.sock
func (s *Store) DaemonSocketPath() string {
	return filepath.Join(s.DataDir, "agencyd.sock")
}

// DaemonLogPath returns the path to the daemon's log file.
// Format: ${AGENCY_DATA_DIR}/agencyd.log
func (s *Store) DaemonLogPath() string {
	return filepath.Join(s.DataDir, "agencyd.log")
}

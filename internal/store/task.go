// Package store provides persistence for agency data.
// This file implements durable task metadata.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// TaskState represents the lifecycle state of a task.
type TaskState string

const (
	// TaskStateStarting indicates the task is being created.
	TaskStateStarting TaskState = "starting"

	// TaskStateRunning indicates the task has a live or inspectable primary invocation.
	TaskStateRunning TaskState = "running"

	// TaskStateFailed indicates task orchestration failed before reaching running state.
	TaskStateFailed TaskState = "failed"

	// TaskStateArchived indicates the task is hidden from default operational views.
	TaskStateArchived TaskState = "archived"
)

// TaskMeta is the durable task record. A task owns the high-level delegation
// workflow; worktrees and invocations remain the execution surfaces.
type TaskMeta struct {
	SchemaVersion string    `json:"schema_version"`
	TaskID        string    `json:"task_id"`
	Name          string    `json:"name"`
	State         TaskState `json:"state"`

	RepoID   string `json:"repo_id"`
	RepoRoot string `json:"repo_root"`

	BaseBranch string `json:"base_branch"`

	WorktreeID   string `json:"worktree_id,omitempty"`
	WorktreeName string `json:"worktree_name,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Branch       string `json:"branch,omitempty"`

	PrimaryInvocationID string     `json:"primary_invocation_id,omitempty"`
	Mode                RunnerMode `json:"mode"`
	Runner              string     `json:"runner"`

	ClientRequestID    string                     `json:"client_request_id,omitempty"`
	RequestFingerprint string                     `json:"request_fingerprint,omitempty"`
	RetryRequests      map[string]TaskRetryRecord `json:"retry_requests,omitempty"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	FailedPhase string `json:"failed_phase,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
	Error       string `json:"error,omitempty"`
}

// TaskRetryRecord is the durable idempotency record for one retry request.
type TaskRetryRecord struct {
	RequestFingerprint string `json:"request_fingerprint"`
	InvocationID       string `json:"invocation_id,omitempty"`
	State              string `json:"state"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	ErrorCode          string `json:"error_code,omitempty"`
	Error              string `json:"error,omitempty"`
}

// TaskRecord represents a discovered task with parsed metadata.
type TaskRecord struct {
	TaskID  string
	RepoID  string
	Name    string
	Broken  bool
	Meta    *TaskMeta
	TaskDir string
}

// NewTaskMeta creates a task meta record in starting state.
func NewTaskMeta(taskID, name, repoID, repoRoot, baseBranch string, mode RunnerMode, runner, clientRequestID, requestFingerprint string, createdAt time.Time) *TaskMeta {
	now := createdAt.UTC().Format(time.RFC3339)
	return &TaskMeta{
		SchemaVersion:      SchemaVersion,
		TaskID:             taskID,
		Name:               name,
		State:              TaskStateStarting,
		RepoID:             repoID,
		RepoRoot:           repoRoot,
		BaseBranch:         baseBranch,
		Mode:               mode,
		Runner:             runner,
		ClientRequestID:    clientRequestID,
		RequestFingerprint: requestFingerprint,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// EnsureTaskDir creates the task record directory with exclusive semantics.
func (s *Store) EnsureTaskDir(repoID, taskID string) (string, error) {
	taskDir := s.TaskDir(repoID, taskID)
	parentDir := s.TasksDir(repoID)
	if err := s.FS.MkdirAll(parentDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.ETaskCreateFailed,
			"failed to create tasks directory",
			err,
			map[string]string{"dir": parentDir},
		)
	}
	if err := os.Mkdir(taskDir, 0o700); err != nil {
		if os.IsExist(err) {
			return "", errors.NewWithDetails(
				errors.ETaskDirExists,
				"task directory already exists (task_id collision or stale state)",
				map[string]string{"task_dir": taskDir},
			)
		}
		return "", errors.WrapWithDetails(
			errors.ETaskCreateFailed,
			"failed to create task directory",
			err,
			map[string]string{"task_dir": taskDir},
		)
	}
	return taskDir, nil
}

// WriteTaskMeta writes a task meta.json atomically.
func (s *Store) WriteTaskMeta(repoID, taskID string, meta *TaskMeta) error {
	metaPath := s.TaskMetaPath(repoID, taskID)
	if err := fs.WriteJSONAtomic(metaPath, meta, 0o600); err != nil {
		return errors.WrapWithDetails(
			errors.EMetaWriteFailed,
			"failed to write task meta.json atomically",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}
	return nil
}

// UpdateTaskMeta reads, updates, and writes task meta.json atomically.
func (s *Store) UpdateTaskMeta(repoID, taskID string, updateFn func(*TaskMeta)) error {
	meta, err := s.ReadTaskMeta(repoID, taskID)
	if err != nil {
		return err
	}
	updateFn(meta)
	if meta.UpdatedAt == "" {
		now := time.Now
		if s.Now != nil {
			now = s.Now
		}
		meta.UpdatedAt = now().UTC().Format(time.RFC3339)
	}
	return s.WriteTaskMeta(repoID, taskID, meta)
}

// ReadTaskMeta reads and parses a task meta.json.
func (s *Store) ReadTaskMeta(repoID, taskID string) (*TaskMeta, error) {
	metaPath := s.TaskMetaPath(repoID, taskID)
	data, err := s.FS.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.NewWithDetails(
				errors.ETaskNotFound,
				"task not found (meta.json does not exist)",
				map[string]string{"meta_path": metaPath},
			)
		}
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to read task meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	var meta TaskMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to parse task meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}
	if meta.SchemaVersion == "" || meta.TaskID == "" || meta.Name == "" || meta.RepoID == "" || meta.State == "" || meta.CreatedAt == "" || meta.UpdatedAt == "" {
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"task meta.json missing required fields",
			map[string]string{"meta_path": metaPath},
		)
	}
	if meta.SchemaVersion != SchemaVersion {
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"task meta.json has unsupported schema_version",
			map[string]string{
				"meta_path":       metaPath,
				"schema_version":  meta.SchemaVersion,
				"expected_schema": SchemaVersion,
			},
		)
	}
	if meta.TaskID != taskID || meta.RepoID != repoID {
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"task meta.json id does not match record path",
			map[string]string{
				"meta_path":    metaPath,
				"path_task_id": taskID,
				"meta_task_id": meta.TaskID,
				"path_repo_id": repoID,
				"meta_repo_id": meta.RepoID,
			},
		)
	}
	if !validTaskState(meta.State) {
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"task meta.json has unsupported state",
			map[string]string{
				"meta_path": metaPath,
				"state":     string(meta.State),
			},
		)
	}
	return &meta, nil
}

// RemoveTaskDir removes a task record directory. It is intended for failed
// creation cleanup before external side effects have committed.
func (s *Store) RemoveTaskDir(repoID, taskID string) error {
	return fs.SafeRemoveAll(s.TaskDir(repoID, taskID), s.DataDir)
}

// ScanTasksForRepo discovers tasks for a single repo_id.
func ScanTasksForRepo(dataDir, repoID string) ([]TaskRecord, error) {
	tasksDir := filepath.Join(dataDir, "repos", repoID, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	records := make([]TaskRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskID := entry.Name()
		taskDir := filepath.Join(tasksDir, taskID)
		metaPath := filepath.Join(taskDir, "meta.json")
		record := TaskRecord{
			TaskID:  taskID,
			RepoID:  repoID,
			TaskDir: taskDir,
		}
		data, err := os.ReadFile(metaPath)
		if err != nil {
			record.Broken = true
			records = append(records, record)
			continue
		}
		var meta TaskMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			record.Broken = true
			records = append(records, record)
			continue
		}
		if meta.SchemaVersion == "" || meta.TaskID == "" || meta.RepoID == "" || meta.Name == "" || meta.State == "" || meta.CreatedAt == "" || !validTaskState(meta.State) {
			record.Broken = true
			records = append(records, record)
			continue
		}
		record.Meta = &meta
		record.Name = meta.Name
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Broken != records[j].Broken {
			return !records[i].Broken
		}
		if records[i].Broken && records[j].Broken {
			return records[i].TaskID > records[j].TaskID
		}
		ti, erri := time.Parse(time.RFC3339, records[i].Meta.CreatedAt)
		tj, errj := time.Parse(time.RFC3339, records[j].Meta.CreatedAt)
		if erri != nil || errj != nil {
			return records[i].TaskID > records[j].TaskID
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return records[i].TaskID > records[j].TaskID
	})

	return records, nil
}

func validTaskState(state TaskState) bool {
	switch state {
	case TaskStateStarting, TaskStateRunning, TaskStateFailed, TaskStateArchived:
		return true
	default:
		return false
	}
}

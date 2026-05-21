package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func TestTaskPaths(t *testing.T) {
	st := NewStore(fs.NewRealFS(), "/tmp/agency-data", time.Now)
	assert.Equal(t, filepath.Join("/tmp/agency-data", "repos", "repo-1", "tasks"), st.TasksDir("repo-1"))
	assert.Equal(t, filepath.Join("/tmp/agency-data", "repos", "repo-1", "tasks", "task-1", "meta.json"), st.TaskMetaPath("repo-1", "task-1"))
	assert.Equal(t, filepath.Join("/tmp/agency-data", "repos", "repo-1", "tasks", "task-1", "events.jsonl"), st.TaskEventsPath("repo-1", "task-1"))
}

func TestTaskMetaReadWriteAndPermissions(t *testing.T) {
	dataDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	_, err := st.EnsureTaskDir("repo-1", "task-1")
	require.NoError(t, err)

	now := st.Now().UTC().Format(time.RFC3339)
	meta := &TaskMeta{
		SchemaVersion:      SchemaVersion,
		TaskID:             "task-1",
		Name:               "feature",
		State:              TaskStateStarting,
		RepoID:             "repo-1",
		RepoRoot:           "/repo",
		BaseBranch:         "main",
		CheckoutRoot:       "/checkouts/repo-1",
		ExecutionProfile:   "work",
		Mode:               RunnerModeHeadless,
		Runner:             "claude-code",
		ClientRequestID:    "req-1",
		RequestFingerprint: "fp-1",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, st.WriteTaskMeta("repo-1", "task-1", meta))

	info, err := os.Stat(st.TaskDir("repo-1", "task-1"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	info, err = os.Stat(st.TaskMetaPath("repo-1", "task-1"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	got, err := st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskStateStarting, got.State)
	assert.Equal(t, "req-1", got.ClientRequestID)
}

func TestReadTaskMetaRejectsPathMismatch(t *testing.T) {
	dataDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), dataDir, time.Now)
	_, err := st.EnsureTaskDir("repo-1", "task-1")
	require.NoError(t, err)
	now := time.Now().UTC().Format(time.RFC3339)
	meta := &TaskMeta{
		SchemaVersion:    SchemaVersion,
		TaskID:           "other-task",
		Name:             "feature",
		State:            TaskStateStarting,
		RepoID:           "repo-1",
		RepoRoot:         "/repo",
		BaseBranch:       "main",
		CheckoutRoot:     "/checkouts/repo-1",
		ExecutionProfile: "work",
		Mode:             RunnerModeHeadless,
		Runner:           "claude-code",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(st.TaskMetaPath("repo-1", "task-1"), data, 0o600))

	_, err = st.ReadTaskMeta("repo-1", "task-1")
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

func TestReadAndScanTaskMetaRejectUnknownFields(t *testing.T) {
	dataDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), dataDir, time.Now)
	_, err := st.EnsureTaskDir("repo-1", "task-1")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(st.TaskMetaPath("repo-1", "task-1"), []byte(`{
		"schema_version":"2.0",
		"task_id":"task-1",
		"name":"feature",
		"state":"starting",
		"repo_id":"repo-1",
		"repo_root":"/repo",
		"base_branch":"main",
		"checkout_root":"/checkouts/repo-1",
		"execution_profile":"work",
		"mode":"headless",
		"runner":"claude-code",
		"created_at":"2026-01-01T00:00:00Z",
		"updated_at":"2026-01-01T00:00:00Z",
		"unexpected":true
	}`), 0o600))

	_, err = st.ReadTaskMeta("repo-1", "task-1")
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))

	records, err := ScanTasksForRepo(dataDir, "repo-1")
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.True(t, records[0].Broken)
	assert.Nil(t, records[0].Meta)
}

func TestScanTasksForRepoSortsNewestFirstAndMarksBroken(t *testing.T) {
	dataDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), dataDir, time.Now)
	for _, entry := range []struct {
		id string
		at time.Time
	}{
		{"task-old", time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)},
		{"task-new", time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC)},
	} {
		_, err := st.EnsureTaskDir("repo-1", entry.id)
		require.NoError(t, err)
		now := entry.at.UTC().Format(time.RFC3339)
		meta := &TaskMeta{
			SchemaVersion:    SchemaVersion,
			TaskID:           entry.id,
			Name:             entry.id,
			State:            TaskStateStarting,
			RepoID:           "repo-1",
			RepoRoot:         "/repo",
			BaseBranch:       "main",
			CheckoutRoot:     "/checkouts/repo-1",
			ExecutionProfile: "work",
			Mode:             RunnerModeHeadless,
			Runner:           "claude-code",
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		require.NoError(t, st.WriteTaskMeta("repo-1", entry.id, meta))
	}
	brokenDir := filepath.Join(st.TasksDir("repo-1"), "broken")
	require.NoError(t, os.MkdirAll(brokenDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(brokenDir, "meta.json"), []byte("{"), 0o600))

	records, err := ScanTasksForRepo(dataDir, "repo-1")
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, "task-new", records[0].TaskID)
	assert.Equal(t, "task-old", records[1].TaskID)
	assert.Equal(t, "broken", records[2].TaskID)
	assert.True(t, records[2].Broken)
}

func TestScanTasksForRepoRejectsStrictMetaViolations(t *testing.T) {
	dataDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), dataDir, time.Now)

	cases := []struct {
		id     string
		mutate func(*TaskMeta)
	}{
		{
			id: "unsupported-schema-version",
			mutate: func(meta *TaskMeta) {
				meta.SchemaVersion = "1.0"
			},
		},
		{
			id: "missing-execution",
			mutate: func(meta *TaskMeta) {
				meta.CheckoutRoot = ""
			},
		},
		{
			id: "relative-repo-root",
			mutate: func(meta *TaskMeta) {
				meta.RepoRoot = "repo"
			},
		},
		{
			id: "relative-worktree-path",
			mutate: func(meta *TaskMeta) {
				meta.WorktreePath = "worktree"
			},
		},
		{
			id: "task-id-mismatch",
			mutate: func(meta *TaskMeta) {
				meta.TaskID = "other-task"
			},
		},
		{
			id: "repo-id-mismatch",
			mutate: func(meta *TaskMeta) {
				meta.RepoID = "other-repo"
			},
		},
		{
			id: "missing-retry-request-fingerprint",
			mutate: func(meta *TaskMeta) {
				meta.RetryRequests = map[string]TaskRetryRecord{
					"retry-1": {
						State:     TaskRetryStateStarting,
						CreatedAt: meta.CreatedAt,
						UpdatedAt: meta.UpdatedAt,
					},
				}
			},
		},
		{
			id: "missing-retry-created-at",
			mutate: func(meta *TaskMeta) {
				meta.RetryRequests = map[string]TaskRetryRecord{
					"retry-1": {
						RequestFingerprint: "retry-fp",
						State:              TaskRetryStateStarting,
						UpdatedAt:          meta.UpdatedAt,
					},
				}
			},
		},
		{
			id: "missing-retry-updated-at",
			mutate: func(meta *TaskMeta) {
				meta.RetryRequests = map[string]TaskRetryRecord{
					"retry-1": {
						RequestFingerprint: "retry-fp",
						State:              TaskRetryStateStarting,
						CreatedAt:          meta.CreatedAt,
					},
				}
			},
		},
		{
			id: "invalid-retry-state",
			mutate: func(meta *TaskMeta) {
				meta.RetryRequests = map[string]TaskRetryRecord{
					"retry-1": {
						RequestFingerprint: "retry-fp",
						State:              "paused",
						CreatedAt:          meta.CreatedAt,
						UpdatedAt:          meta.UpdatedAt,
					},
				}
			},
		},
	}

	for _, tc := range cases {
		_, err := st.EnsureTaskDir("repo-1", tc.id)
		require.NoError(t, err)
		now := time.Now().UTC().Format(time.RFC3339)
		meta := &TaskMeta{
			SchemaVersion:    SchemaVersion,
			TaskID:           tc.id,
			Name:             tc.id,
			State:            TaskStateStarting,
			RepoID:           "repo-1",
			RepoRoot:         "/repo",
			BaseBranch:       "main",
			CheckoutRoot:     "/checkouts/repo-1",
			ExecutionProfile: "work",
			Mode:             RunnerModeHeadless,
			Runner:           "claude-code",
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		tc.mutate(meta)
		data, err := json.MarshalIndent(meta, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(st.TaskMetaPath("repo-1", tc.id), data, 0o600))
	}

	records, err := ScanTasksForRepo(dataDir, "repo-1")
	require.NoError(t, err)
	require.Len(t, records, len(cases))

	byID := make(map[string]TaskRecord, len(records))
	for _, record := range records {
		byID[record.TaskID] = record
	}
	for _, tc := range cases {
		record, ok := byID[tc.id]
		require.True(t, ok, "missing record %s", tc.id)
		assert.True(t, record.Broken, tc.id)
		assert.Nil(t, record.Meta, tc.id)
	}
}

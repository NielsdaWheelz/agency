package store

import (
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

	meta := NewTaskMeta("task-1", "feature", "repo-1", "/repo", "main", RunnerModeHeadless, "claude-code", "req-1", "fp-1", st.Now())
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
	meta := NewTaskMeta("other-task", "feature", "repo-1", "/repo", "main", RunnerModeHeadless, "claude-code", "", "", time.Now())
	require.NoError(t, st.WriteTaskMeta("repo-1", "task-1", meta))

	_, err = st.ReadTaskMeta("repo-1", "task-1")
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
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
		meta := NewTaskMeta(entry.id, entry.id, "repo-1", "/repo", "main", RunnerModeHeadless, "claude-code", "", "", entry.at)
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

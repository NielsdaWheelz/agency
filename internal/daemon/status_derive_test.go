package daemon_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

var fixedNow = time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)

func TestInvocationMetaToDTO_StateProjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		meta        *store.InvocationMeta
		runnerMeta  *runnerstatus.RunnerStatus
		runnerErr   error
		wantState   string
		wantReason  string
		wantSortKey int
		wantFlags   []string
	}{
		{
			name:        "starting",
			meta:        &store.InvocationMeta{Status: store.InvocationStatusStarting},
			wantState:   "starting",
			wantSortKey: daemon.SortKeyStarting,
		},
		{
			name:        "running",
			meta:        &store.InvocationMeta{Status: store.InvocationStatusRunning},
			wantState:   "running",
			wantSortKey: daemon.SortKeyRunning,
		},
		{
			name: "running_waiting",
			meta: &store.InvocationMeta{Status: store.InvocationStatusRunning},
			runnerMeta: &runnerstatus.RunnerStatus{
				SchemaVersion: runnerstatus.SchemaVersion,
				State:         runnerstatus.StateWaiting,
				Reason:        runnerstatus.ReasonTurnComplete,
				Summary:       "waiting",
			},
			wantState:   "waiting",
			wantReason:  runnerstatus.ReasonTurnComplete,
			wantSortKey: daemon.SortKeyWaiting,
		},
		{
			name:        "stopping",
			meta:        &store.InvocationMeta{Status: store.InvocationStatusStopping},
			wantState:   "stopping",
			wantSortKey: daemon.SortKeyStopping,
		},
		{
			name: "failed",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFailed,
				FailureReason: "runner_exit_nonzero",
			},
			wantState:   "failed",
			wantReason:  "runner_exit_nonzero",
			wantSortKey: daemon.SortKeyFailed,
		},
		{
			name: "finished_succeeded",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFinished,
				LandingStatus: store.LandingStatusPending,
			},
			runnerMeta: &runnerstatus.RunnerStatus{
				SchemaVersion: runnerstatus.SchemaVersion,
				State:         runnerstatus.StateSucceeded,
				Summary:       "done",
				HowToTest:     "go test ./...",
			},
			wantState:   "succeeded",
			wantSortKey: daemon.SortKeySucceeded,
			wantFlags:   []string{"landable"},
		},
		{
			name:        "finished_missing_runner_status_fails",
			meta:        &store.InvocationMeta{Status: store.InvocationStatusFinished},
			wantState:   "failed",
			wantReason:  "runner_status_missing",
			wantSortKey: daemon.SortKeyFailed,
			wantFlags:   []string{"landable"},
		},
		{
			name: "finished_invalid_runner_state_fails",
			meta: &store.InvocationMeta{Status: store.InvocationStatusFinished},
			runnerMeta: &runnerstatus.RunnerStatus{
				SchemaVersion: runnerstatus.SchemaVersion,
				State:         runnerstatus.StateRunning,
				Summary:       "still running",
			},
			wantState:   "failed",
			wantReason:  "invalid_runner_state",
			wantSortKey: daemon.SortKeyFailed,
			wantFlags:   []string{"landable"},
		},
		{
			name: "landed_sorts_late",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFinished,
				LandingStatus: store.LandingStatusLanded,
			},
			runnerMeta: &runnerstatus.RunnerStatus{
				SchemaVersion: runnerstatus.SchemaVersion,
				State:         runnerstatus.StateSucceeded,
				Summary:       "done",
				HowToTest:     "go test ./...",
			},
			wantState:   "succeeded",
			wantSortKey: daemon.SortKeyLanded,
		},
		{
			name: "discarded_sorts_last",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFinished,
				LandingStatus: store.LandingStatusDiscarded,
			},
			runnerMeta: &runnerstatus.RunnerStatus{
				SchemaVersion: runnerstatus.SchemaVersion,
				State:         runnerstatus.StateSucceeded,
				Summary:       "done",
				HowToTest:     "go test ./...",
			},
			wantState:   "succeeded",
			wantSortKey: daemon.SortKeyDiscarded,
		},
		{
			name: "attention_flag_raises_priority",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusRunning,
				Flags:  store.InvocationFlags{NeedsAttention: true, Orphaned: true},
			},
			wantState:   "running",
			wantSortKey: daemon.SortKeyNeedsAttention,
			wantFlags:   []string{"needs_attention", "orphaned"},
		},
		{
			name: "stalled_flag",
			meta: &store.InvocationMeta{
				Status:       store.InvocationStatusRunning,
				LastOutputAt: fixedNow.Add(-10 * time.Minute).Format(time.RFC3339),
			},
			wantState:   "running",
			wantSortKey: daemon.SortKeyRunning,
			wantFlags:   []string{"stalled"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dto := daemon.InvocationMetaToDTO(tt.meta, "repo-1", "/tmp/logs", tt.runnerMeta, tt.runnerErr, fixedNow)
			assert.Equal(t, tt.wantState, dto.State)
			assert.Equal(t, tt.wantReason, dto.Reason)
			assert.Equal(t, tt.wantSortKey, dto.SortKey)
			assert.ElementsMatch(t, tt.wantFlags, dto.AttentionFlags)
		})
	}
}

func TestInvocationMetaToDTO_FieldMapping(t *testing.T) {
	t.Parallel()

	exitCode := 0
	meta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          "inv-123",
		InvocationName:        "my-invocation",
		IntegrationWorktreeID: "wt-456",
		SandboxPath:           "/tmp/sandbox/inv-123",
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             "2026-02-05T11:50:00Z",
		FinishedAt:            "2026-02-05T11:55:00Z",
		LastOutputAt:          "2026-02-05T11:54:00Z",
		Status:                store.InvocationStatusFinished,
		ExitReason:            "exited",
		ExitCode:              &exitCode,
		LandingStatus:         store.LandingStatusPending,
	}
	runnerMeta := &runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		State:         runnerstatus.StateSucceeded,
		Summary:       "done",
		HowToTest:     "go test ./...",
	}

	dto := daemon.InvocationMetaToDTO(meta, "repo-abc", "/tmp/logs/inv-123", runnerMeta, nil, fixedNow)

	assert.Equal(t, "inv-123", dto.InvocationID)
	assert.Equal(t, "my-invocation", dto.InvocationName)
	assert.Equal(t, "wt-456", dto.WorktreeID)
	assert.Equal(t, "repo-abc", dto.RepoID)
	assert.Equal(t, "claude-code", dto.Runner)
	assert.Equal(t, "headless", dto.Mode)
	assert.Equal(t, "2026-02-05T11:50:00Z", dto.StartedAt)
	assert.Equal(t, "2026-02-05T11:55:00Z", dto.FinishedAt)
	assert.Equal(t, "2026-02-05T11:54:00Z", dto.LastOutputAt)
	assert.Equal(t, "succeeded", dto.State)
	assert.Equal(t, "exited", dto.ExitReason)
	require.NotNil(t, dto.ExitCode)
	assert.Equal(t, 0, *dto.ExitCode)
	assert.Equal(t, "pending", dto.LandingStatus)
	assert.Equal(t, "/tmp/sandbox/inv-123", dto.SandboxPath)
	assert.Equal(t, "/tmp/logs/inv-123", dto.LogsDir)
}

func TestWorktreeMetaToDTO(t *testing.T) {
	t.Parallel()

	meta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "wt-789",
		Name:          "my-feature",
		RepoID:        "repo-xyz",
		Branch:        "agency/my-feature",
		BaseBranch:    "main",
		TreePath:      "/tmp/worktrees/my-feature",
		CreatedAt:     "2026-02-05T10:00:00Z",
		LastUsedAt:    "2026-02-05T11:00:00Z",
		State:         store.WorktreeStatePresent,
	}
	mergeMeta := store.NewIntegrationWorktreeMergeMeta(
		"repo-xyz",
		"wt-789",
		"merge-attempt-1",
		"req-merge-1",
		"squash",
		true,
		"",
		fixedNow,
	)
	mergeMeta.Branch = "agency/my-feature"
	mergeMeta.PRNumber = 77
	mergeMeta.PRURL = "https://github.com/example/repo/pull/77"
	mergeMeta.MergeLogPath = "/tmp/logs/wt-789/merge.log"
	mergeMeta.VerifyLogPath = "/tmp/logs/wt-789/verify.log"
	mergeMeta.ArchiveLogPath = "/tmp/logs/wt-789/archive.log"

	dto := daemon.WorktreeMetaToDTO(meta, mergeMeta)

	assert.Equal(t, "wt-789", dto.WorktreeID)
	assert.Equal(t, "my-feature", dto.Name)
	assert.Equal(t, "repo-xyz", dto.RepoID)
	assert.Equal(t, "agency/my-feature", dto.Branch)
	assert.Equal(t, "main", dto.BaseBranch)
	assert.Equal(t, "/tmp/worktrees/my-feature", dto.TreePath)
	assert.Equal(t, "present", dto.State)
	assert.Equal(t, "2026-02-05T10:00:00Z", dto.CreatedAt)
	assert.Equal(t, "2026-02-05T11:00:00Z", dto.LastUsedAt)
	require.NotNil(t, dto.Merge)
	assert.Equal(t, "merge-attempt-1", dto.Merge.AttemptID)
	assert.Equal(t, "req-merge-1", dto.Merge.RequestID)
	assert.Equal(t, "running", dto.Merge.State)
	assert.Equal(t, "preflight", dto.Merge.Stage)
	assert.Equal(t, "preparing merge", dto.Merge.StatusSummary)
	assert.Equal(t, "squash", dto.Merge.Strategy)
	assert.True(t, dto.Merge.DeleteBranch)
	assert.Equal(t, "agency/my-feature", dto.Merge.Branch)
	assert.Equal(t, 77, dto.Merge.PRNumber)
	assert.Equal(t, "https://github.com/example/repo/pull/77", dto.Merge.PRURL)
	assert.Equal(t, "/tmp/logs/wt-789/merge.log", dto.Merge.MergeLogPath)
	assert.Equal(t, "/tmp/logs/wt-789/verify.log", dto.Merge.VerifyLogPath)
	assert.Equal(t, "/tmp/logs/wt-789/archive.log", dto.Merge.ArchiveLogPath)
	assert.Equal(t, fixedNow.Format(time.RFC3339), dto.Merge.StartedAt)
	assert.Equal(t, fixedNow.Format(time.RFC3339), dto.Merge.UpdatedAt)
}

func TestWorktreeMetaToDTO_WithoutMergeMeta(t *testing.T) {
	t.Parallel()

	meta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "wt-789",
		Name:          "my-feature",
		RepoID:        "repo-xyz",
		Branch:        "agency/my-feature",
		BaseBranch:    "main",
		TreePath:      "/tmp/worktrees/my-feature",
		CreatedAt:     "2026-02-05T10:00:00Z",
		LastUsedAt:    "2026-02-05T11:00:00Z",
		State:         store.WorktreeStatePresent,
	}

	dto := daemon.WorktreeMetaToDTO(meta, nil)

	assert.Nil(t, dto.Merge)
}

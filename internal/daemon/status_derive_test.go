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

func ptr[T any](v T) *T { return &v }

func TestDeriveDisplayStatus_Precedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		meta              *store.InvocationMeta
		wantDisplayStatus string
		wantSortKey       int
	}{
		{
			name: "failed",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusFailed,
			},
			wantDisplayStatus: "failed",
			wantSortKey:       daemon.SortKeyFailed,
		},
		{
			name: "failed_overrides_needs_attention",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusFailed,
				Flags:  store.InvocationFlags{NeedsAttention: true},
			},
			wantDisplayStatus: "failed",
			wantSortKey:       daemon.SortKeyFailed,
		},
		{
			name: "landed",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFinished,
				LandingStatus: store.LandingStatusLanded,
			},
			wantDisplayStatus: "landed",
			wantSortKey:       daemon.SortKeyLanded,
		},
		{
			name: "landed_overrides_needs_attention",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFinished,
				LandingStatus: store.LandingStatusLanded,
				Flags:         store.InvocationFlags{NeedsAttention: true},
			},
			wantDisplayStatus: "landed",
			wantSortKey:       daemon.SortKeyLanded,
		},
		{
			name: "discarded",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFinished,
				LandingStatus: store.LandingStatusDiscarded,
			},
			wantDisplayStatus: "discarded",
			wantSortKey:       daemon.SortKeyDiscarded,
		},
		{
			name: "needs_attention",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusRunning,
				Flags:  store.InvocationFlags{NeedsAttention: true},
			},
			wantDisplayStatus: "needs attention",
			wantSortKey:       daemon.SortKeyNeedsAttention,
		},
		{
			name: "needs_input",
			meta: &store.InvocationMeta{
				Status:         store.InvocationStatusRunning,
				SemanticStatus: ptr(runnerstatus.StatusNeedsInput),
			},
			wantDisplayStatus: "needs input",
			wantSortKey:       daemon.SortKeyNeedsInput,
		},
		{
			name: "blocked",
			meta: &store.InvocationMeta{
				Status:         store.InvocationStatusRunning,
				SemanticStatus: ptr(runnerstatus.StatusBlocked),
			},
			wantDisplayStatus: "blocked",
			wantSortKey:       daemon.SortKeyBlocked,
		},
		{
			name: "ready_for_review",
			meta: &store.InvocationMeta{
				Status:         store.InvocationStatusFinished,
				SemanticStatus: ptr(runnerstatus.StatusReadyForReview),
			},
			wantDisplayStatus: "ready for review",
			wantSortKey:       daemon.SortKeyReadyForReview,
		},
		{
			name: "working",
			meta: &store.InvocationMeta{
				Status:         store.InvocationStatusRunning,
				SemanticStatus: ptr(runnerstatus.StatusWorking),
			},
			wantDisplayStatus: "working",
			wantSortKey:       daemon.SortKeyWorking,
		},
		{
			name: "running_no_semantic",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusRunning,
			},
			wantDisplayStatus: "running",
			wantSortKey:       daemon.SortKeyRunning,
		},
		{
			name: "finished",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusFinished,
			},
			wantDisplayStatus: "finished",
			wantSortKey:       daemon.SortKeyFinished,
		},
		{
			name: "starting",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusStarting,
			},
			wantDisplayStatus: "starting",
			wantSortKey:       daemon.SortKeyStarting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := daemon.DeriveDisplayStatus(tt.meta, fixedNow)
			assert.Equal(t, tt.wantDisplayStatus, got.DisplayStatus)
			assert.Equal(t, tt.wantSortKey, got.SortKey)
		})
	}
}

func TestDeriveDisplayStatus_AttentionFlags(t *testing.T) {
	t.Parallel()

	tenMinAgo := fixedNow.Add(-10 * time.Minute).Format(time.RFC3339)
	twoMinAgo := fixedNow.Add(-2 * time.Minute).Format(time.RFC3339)

	tests := []struct {
		name      string
		meta      *store.InvocationMeta
		wantFlags []string
	}{
		{
			name: "needs_attention_flag",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusRunning,
				Flags:  store.InvocationFlags{NeedsAttention: true},
			},
			wantFlags: []string{"needs_attention"},
		},
		{
			name: "orphaned_flag",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusRunning,
				Flags:  store.InvocationFlags{Orphaned: true},
			},
			wantFlags: []string{"orphaned"},
		},
		{
			name: "stalled_flag",
			meta: &store.InvocationMeta{
				Status:       store.InvocationStatusRunning,
				LastOutputAt: tenMinAgo,
			},
			wantFlags: []string{"stalled"},
		},
		{
			name: "not_stalled_recent",
			meta: &store.InvocationMeta{
				Status:       store.InvocationStatusRunning,
				LastOutputAt: twoMinAgo,
			},
			wantFlags: nil,
		},
		{
			name: "not_stalled_no_output",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusRunning,
			},
			wantFlags: nil,
		},
		{
			name: "not_stalled_not_running",
			meta: &store.InvocationMeta{
				Status:       store.InvocationStatusFinished,
				LastOutputAt: tenMinAgo,
			},
			wantFlags: []string{"landable"},
		},
		{
			name: "landable_flag",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusFinished,
			},
			wantFlags: []string{"landable"},
		},
		{
			name: "landable_not_landed",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFinished,
				LandingStatus: store.LandingStatusLanded,
			},
			wantFlags: nil,
		},
		{
			name: "landable_not_discarded",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFinished,
				LandingStatus: store.LandingStatusDiscarded,
			},
			wantFlags: nil,
		},
		{
			name: "combined_flags",
			meta: &store.InvocationMeta{
				Status:       store.InvocationStatusRunning,
				Flags:        store.InvocationFlags{NeedsAttention: true, Orphaned: true},
				LastOutputAt: tenMinAgo,
			},
			wantFlags: []string{"needs_attention", "orphaned", "stalled"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := daemon.DeriveDisplayStatus(tt.meta, fixedNow)
			assert.ElementsMatch(t, tt.wantFlags, got.AttentionFlags)
		})
	}
}

func TestInvocationMetaToDTO(t *testing.T) {
	t.Parallel()

	semanticStatus := runnerstatus.StatusWorking
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
		SemanticStatus:        &semanticStatus,
		LandingStatus:         store.LandingStatusPending,
	}

	repoID := "repo-abc"
	logsDir := "/tmp/logs/inv-123"

	dto := daemon.InvocationMetaToDTO(meta, repoID, logsDir, fixedNow)

	// Direct field mappings
	assert.Equal(t, "inv-123", dto.InvocationID)
	assert.Equal(t, "my-invocation", dto.InvocationName)
	assert.Equal(t, "wt-456", dto.WorktreeID)
	assert.Equal(t, "repo-abc", dto.RepoID)
	assert.Equal(t, "claude-code", dto.Runner)
	assert.Equal(t, "headless", dto.Mode)
	assert.Equal(t, "2026-02-05T11:50:00Z", dto.StartedAt)
	assert.Equal(t, "2026-02-05T11:55:00Z", dto.FinishedAt)
	assert.Equal(t, "2026-02-05T11:54:00Z", dto.LastOutputAt)
	assert.Equal(t, "finished", dto.Status)
	assert.Equal(t, "exited", dto.ExitReason)
	require.NotNil(t, dto.ExitCode)
	assert.Equal(t, 0, *dto.ExitCode)
	assert.Equal(t, "working", dto.SemanticStatus)
	assert.Equal(t, "pending", dto.LandingStatus)
	assert.Equal(t, "/tmp/sandbox/inv-123", dto.SandboxPath)
	assert.Equal(t, "/tmp/logs/inv-123", dto.LogsDir)

	// Derived fields — status=finished, semantic=working, landing=pending → "finished"
	assert.Equal(t, "finished", dto.DisplayStatus)
	assert.Equal(t, daemon.SortKeyFinished, dto.SortKey)
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

	dto := daemon.WorktreeMetaToDTO(meta)

	assert.Equal(t, "wt-789", dto.WorktreeID)
	assert.Equal(t, "my-feature", dto.Name)
	assert.Equal(t, "repo-xyz", dto.RepoID)
	assert.Equal(t, "agency/my-feature", dto.Branch)
	assert.Equal(t, "main", dto.BaseBranch)
	assert.Equal(t, "/tmp/worktrees/my-feature", dto.TreePath)
	assert.Equal(t, "present", dto.State)
	assert.Equal(t, "2026-02-05T10:00:00Z", dto.CreatedAt)
	assert.Equal(t, "2026-02-05T11:00:00Z", dto.LastUsedAt)
}

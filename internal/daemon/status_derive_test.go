package daemon

import (
	"slices"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

var fixedNow = time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)

func TestInvocationStatusProjection(t *testing.T) {
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
			wantSortKey: sortKeyStarting,
		},
		{
			name:        "running",
			meta:        &store.InvocationMeta{Status: store.InvocationStatusRunning},
			wantState:   "running",
			wantSortKey: sortKeyRunning,
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
			wantSortKey: sortKeyWaiting,
		},
		{
			name:        "stopping",
			meta:        &store.InvocationMeta{Status: store.InvocationStatusStopping},
			wantState:   "stopping",
			wantSortKey: sortKeyStopping,
		},
		{
			name: "failed",
			meta: &store.InvocationMeta{
				Status:        store.InvocationStatusFailed,
				FailureReason: "runner_exit_nonzero",
			},
			wantState:   "failed",
			wantReason:  "runner_exit_nonzero",
			wantSortKey: sortKeyFailed,
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
			wantSortKey: sortKeySucceeded,
			wantFlags:   []string{"landable"},
		},
		{
			name:        "finished_missing_runner_status_fails",
			meta:        &store.InvocationMeta{Status: store.InvocationStatusFinished},
			wantState:   "failed",
			wantReason:  "runner_status_missing",
			wantSortKey: sortKeyFailed,
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
			wantSortKey: sortKeyFailed,
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
			wantSortKey: sortKeyLanded,
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
			wantSortKey: sortKeyDiscarded,
		},
		{
			name: "attention_flag_raises_priority",
			meta: &store.InvocationMeta{
				Status: store.InvocationStatusRunning,
				Flags:  store.InvocationFlags{NeedsAttention: true, Orphaned: true},
			},
			wantState:   "running",
			wantSortKey: sortKeyNeedsAttention,
			wantFlags:   []string{"needs_attention", "orphaned"},
		},
		{
			name: "stalled_flag",
			meta: &store.InvocationMeta{
				Status:       store.InvocationStatusRunning,
				LastOutputAt: fixedNow.Add(-10 * time.Minute).Format(time.RFC3339),
			},
			wantState:   "running",
			wantSortKey: sortKeyRunning,
			wantFlags:   []string{"stalled"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dto := invocationMetaToDTO(tt.meta, "repo-1", "/tmp/logs", tt.runnerMeta, tt.runnerErr, fixedNow)
			if dto.State != tt.wantState {
				t.Fatalf("state = %q, want %q", dto.State, tt.wantState)
			}
			if dto.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", dto.Reason, tt.wantReason)
			}
			if dto.SortKey != tt.wantSortKey {
				t.Fatalf("sort key = %d, want %d", dto.SortKey, tt.wantSortKey)
			}
			gotFlags := slices.Clone(dto.AttentionFlags)
			wantFlags := slices.Clone(tt.wantFlags)
			slices.Sort(gotFlags)
			slices.Sort(wantFlags)
			if !slices.Equal(gotFlags, wantFlags) {
				t.Fatalf("attention flags = %v, want %v", dto.AttentionFlags, tt.wantFlags)
			}
		})
	}
}

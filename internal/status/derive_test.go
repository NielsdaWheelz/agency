package status

import (
	"testing"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/watchdog"
)

// Test helper: create a minimal valid RunMeta with optional modifications.
func mkMeta(fn func(*store.RunMeta)) *store.RunMeta {
	meta := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         "20260110-a3f2",
		RepoID:        "abcd1234ef567890",
		Name:          "test run",
		Runner:        "claude",
		RunnerCmd:     "claude",
		ParentBranch:  "main",
		Branch:        "agency/test-run-a3f2",
		WorktreePath:  "/tmp/worktree",
		CreatedAt:     "2026-01-10T12:00:00Z",
	}
	if fn != nil {
		fn(meta)
	}
	return meta
}

// mkRunnerStatus creates a runner status for testing.
func mkRunnerStatus(status runnerstatus.Status) *runnerstatus.RunnerStatus {
	return &runnerstatus.RunnerStatus{
		SchemaVersion: "1.0",
		Status:        status,
		UpdatedAt:     "2026-01-10T12:00:00Z",
		Summary:       "Test summary",
		Questions:     []string{},
		Blockers:      []string{},
		HowToTest:     "Run tests",
		Risks:         []string{},
	}
}

func TestDerive(t *testing.T) {
	tests := []struct {
		name              string
		meta              *store.RunMeta
		snapshot          Snapshot
		wantDerivedStatus string
		wantArchived      bool
	}{
		// ============================================================
		// 1. nil meta => broken
		// ============================================================
		{
			name:              "nil meta, worktree present",
			meta:              nil,
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusBroken,
			wantArchived:      false,
		},
		{
			name:              "nil meta, worktree absent (archived)",
			meta:              nil,
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: false},
			wantDerivedStatus: StatusBroken,
			wantArchived:      true,
		},
		{
			name:              "nil meta, tmux active (still broken)",
			meta:              nil,
			snapshot:          Snapshot{TmuxActive: true, WorktreePresent: true},
			wantDerivedStatus: StatusBroken,
			wantArchived:      false,
		},

		// ============================================================
		// 2. merged wins (even if other flags are set)
		// ============================================================
		{
			name: "merged wins over setup_failed",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: "2026-01-10T14:00:00Z"}
				m.Flags = &store.RunMetaFlags{SetupFailed: true}
			}),
			snapshot:          Snapshot{TmuxActive: true, WorktreePresent: true},
			wantDerivedStatus: StatusMerged,
			wantArchived:      false,
		},
		{
			name: "merged wins over needs_attention",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: "2026-01-10T14:00:00Z"}
				m.Flags = &store.RunMetaFlags{NeedsAttention: true}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusMerged,
			wantArchived:      false,
		},
		{
			name: "merged with worktree absent (archived)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: "2026-01-10T14:00:00Z"}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: false},
			wantDerivedStatus: StatusMerged,
			wantArchived:      true,
		},

		// ============================================================
		// 3. abandoned wins (except merged)
		// ============================================================
		{
			name: "abandoned wins over setup_failed",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{Abandoned: true, SetupFailed: true}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusAbandoned,
			wantArchived:      false,
		},
		{
			name: "abandoned wins over needs_attention",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{Abandoned: true, NeedsAttention: true}
			}),
			snapshot:          Snapshot{TmuxActive: true, WorktreePresent: true},
			wantDerivedStatus: StatusAbandoned,
			wantArchived:      false,
		},
		{
			name: "abandoned with worktree absent",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{Abandoned: true}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: false},
			wantDerivedStatus: StatusAbandoned,
			wantArchived:      true,
		},

		// ============================================================
		// 4. setup_failed beats needs_attention
		// ============================================================
		{
			name: "setup_failed beats needs_attention",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{SetupFailed: true, NeedsAttention: true}
			}),
			snapshot:          Snapshot{TmuxActive: true, WorktreePresent: true},
			wantDerivedStatus: StatusFailed,
			wantArchived:      false,
		},
		{
			name: "setup_failed alone",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{SetupFailed: true}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusFailed,
			wantArchived:      false,
		},

		// ============================================================
		// 5. needs_attention beats runner status
		// ============================================================
		{
			name: "needs_attention beats runner status",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{NeedsAttention: true}
			}),
			snapshot: Snapshot{
				TmuxActive:      true,
				WorktreePresent: true,
				RunnerStatus:    mkRunnerStatus(runnerstatus.StatusReadyForReview),
			},
			wantDerivedStatus: StatusNeedsAttention,
			wantArchived:      false,
		},
		{
			name: "needs_attention alone",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{NeedsAttention: true}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusNeedsAttention,
			wantArchived:      false,
		},

		// ============================================================
		// 6. Runner-reported statuses
		// ============================================================
		{
			name: "runner status: ready_for_review",
			meta: mkMeta(nil),
			snapshot: Snapshot{
				TmuxActive:      false,
				WorktreePresent: true,
				RunnerStatus:    mkRunnerStatus(runnerstatus.StatusReadyForReview),
			},
			wantDerivedStatus: StatusReadyForReview,
			wantArchived:      false,
		},
		{
			name: "runner status: needs_input",
			meta: mkMeta(nil),
			snapshot: Snapshot{
				TmuxActive:      true,
				WorktreePresent: true,
				RunnerStatus:    mkRunnerStatus(runnerstatus.StatusNeedsInput),
			},
			wantDerivedStatus: StatusNeedsInput,
			wantArchived:      false,
		},
		{
			name: "runner status: blocked",
			meta: mkMeta(nil),
			snapshot: Snapshot{
				TmuxActive:      true,
				WorktreePresent: true,
				RunnerStatus:    mkRunnerStatus(runnerstatus.StatusBlocked),
			},
			wantDerivedStatus: StatusBlocked,
			wantArchived:      false,
		},
		{
			name: "runner status: working",
			meta: mkMeta(nil),
			snapshot: Snapshot{
				TmuxActive:      true,
				WorktreePresent: true,
				RunnerStatus:    mkRunnerStatus(runnerstatus.StatusWorking),
			},
			wantDerivedStatus: StatusWorking,
			wantArchived:      false,
		},

		// ============================================================
		// 7. Stalled detection
		// ============================================================
		{
			name: "stalled: tmux active, stall detected",
			meta: mkMeta(nil),
			snapshot: Snapshot{
				TmuxActive:      true,
				WorktreePresent: true,
				StallResult:     &watchdog.StallResult{IsStalled: true, StalledDuration: 30 * 60e9}, // 30 min
			},
			wantDerivedStatus: StatusStalled,
			wantArchived:      false,
		},
		{
			name: "not stalled: tmux active, stall not detected",
			meta: mkMeta(nil),
			snapshot: Snapshot{
				TmuxActive:      true,
				WorktreePresent: true,
				StallResult:     &watchdog.StallResult{IsStalled: false},
			},
			wantDerivedStatus: StatusActive,
			wantArchived:      false,
		},
		{
			name: "not stalled: no tmux, even if stall flag set",
			meta: mkMeta(nil),
			snapshot: Snapshot{
				TmuxActive:      false,
				WorktreePresent: true,
				StallResult:     &watchdog.StallResult{IsStalled: true},
			},
			wantDerivedStatus: StatusIdle,
			wantArchived:      false,
		},

		// ============================================================
		// 8. Activity fallbacks (no runner status)
		// ============================================================
		{
			name:              "active: tmux active, no runner status",
			meta:              mkMeta(nil),
			snapshot:          Snapshot{TmuxActive: true, WorktreePresent: true},
			wantDerivedStatus: StatusActive,
			wantArchived:      false,
		},
		{
			name:              "idle: tmux inactive, no runner status",
			meta:              mkMeta(nil),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusIdle,
			wantArchived:      false,
		},

		// ============================================================
		// 9. Archived boolean (worktree_present=false => Archived true)
		// ============================================================
		{
			name:              "archived: worktree_present=false",
			meta:              mkMeta(nil),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: false},
			wantDerivedStatus: StatusIdle,
			wantArchived:      true,
		},
		{
			name:              "not archived: worktree_present=true",
			meta:              mkMeta(nil),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusIdle,
			wantArchived:      false,
		},
		{
			name: "archived applies to all statuses (merged + archived)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: "2026-01-10T14:00:00Z"}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: false},
			wantDerivedStatus: StatusMerged,
			wantArchived:      true,
		},
		{
			name:              "archived applies to all statuses (active + archived)",
			meta:              mkMeta(nil),
			snapshot:          Snapshot{TmuxActive: true, WorktreePresent: false},
			wantDerivedStatus: StatusActive,
			wantArchived:      true,
		},

		// ============================================================
		// Edge cases: nil sub-structs in meta
		// ============================================================
		{
			name:              "nil flags struct (not setup_failed)",
			meta:              mkMeta(nil), // Flags is nil by default in mkMeta
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusIdle,
			wantArchived:      false,
		},
		{
			name: "nil archive struct (not merged)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = nil // explicitly nil
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusIdle,
			wantArchived:      false,
		},
		{
			name: "empty merged_at string (not merged)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: ""}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusIdle,
			wantArchived:      false,
		},
		{
			name: "archived_at set but not merged_at (not merged status)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{ArchivedAt: "2026-01-10T14:00:00Z", MergedAt: ""}
			}),
			snapshot:          Snapshot{TmuxActive: false, WorktreePresent: true},
			wantDerivedStatus: StatusIdle,
			wantArchived:      false,
		},

		// ============================================================
		// Runner status takes precedence over stall detection
		// ============================================================
		{
			name: "runner status beats stall detection",
			meta: mkMeta(nil),
			snapshot: Snapshot{
				TmuxActive:      true,
				WorktreePresent: true,
				RunnerStatus:    mkRunnerStatus(runnerstatus.StatusWorking),
				StallResult:     &watchdog.StallResult{IsStalled: true},
			},
			wantDerivedStatus: StatusWorking,
			wantArchived:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Derive(tt.meta, tt.snapshot)

			if got.DerivedStatus != tt.wantDerivedStatus {
				t.Errorf("DerivedStatus = %q, want %q", got.DerivedStatus, tt.wantDerivedStatus)
			}
			if got.Archived != tt.wantArchived {
				t.Errorf("Archived = %v, want %v", got.Archived, tt.wantArchived)
			}
		})
	}
}

// TestDeriveNilMetaDoesNotPanic ensures Derive handles nil meta gracefully.
func TestDeriveNilMetaDoesNotPanic(t *testing.T) {
	// This test exists to explicitly verify the "must not panic" requirement
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Derive panicked on nil meta: %v", r)
		}
	}()

	_ = Derive(nil, Snapshot{TmuxActive: true, WorktreePresent: true})
}

// TestStatusStringConstants verifies status strings match expected values.
func TestStatusStringConstants(t *testing.T) {
	// These are user-visible contracts and must remain stable
	expected := map[string]string{
		"StatusBroken":         "broken",
		"StatusMerged":         "merged",
		"StatusAbandoned":      "abandoned",
		"StatusFailed":         "failed",
		"StatusNeedsAttention": "needs attention",
		"StatusReadyForReview": "ready for review",
		"StatusNeedsInput":     "needs input",
		"StatusBlocked":        "blocked",
		"StatusWorking":        "working",
		"StatusStalled":        "stalled",
		"StatusActive":         "active",
		"StatusIdle":           "idle",
	}

	actual := map[string]string{
		"StatusBroken":         StatusBroken,
		"StatusMerged":         StatusMerged,
		"StatusAbandoned":      StatusAbandoned,
		"StatusFailed":         StatusFailed,
		"StatusNeedsAttention": StatusNeedsAttention,
		"StatusReadyForReview": StatusReadyForReview,
		"StatusNeedsInput":     StatusNeedsInput,
		"StatusBlocked":        StatusBlocked,
		"StatusWorking":        StatusWorking,
		"StatusStalled":        StatusStalled,
		"StatusActive":         StatusActive,
		"StatusIdle":           StatusIdle,
	}

	for name, want := range expected {
		got := actual[name]
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

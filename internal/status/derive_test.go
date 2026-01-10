package status

import (
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/store"
)

// Test helper: create a minimal valid RunMeta with optional modifications.
func mkMeta(fn func(*store.RunMeta)) *store.RunMeta {
	meta := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         "20260110-a3f2",
		RepoID:        "abcd1234ef567890",
		Title:         "test run",
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

// Test helper: create a pointer to a time.Time.
func ptrTime(t time.Time) *time.Time {
	return &t
}

// Test helper: create a pointer to an int.
func ptrInt(n int) *int {
	return &n
}

// Test helper: create a pointer to a bool.
func ptrBool(b bool) *bool {
	return &b
}

func TestDerive(t *testing.T) {
	tests := []struct {
		name               string
		meta               *store.RunMeta
		snapshot           Snapshot
		wantDerivedStatus  string
		wantArchived       bool
		wantReportNonempty bool
	}{
		// ============================================================
		// 1. nil meta => broken
		// ============================================================
		{
			name:               "nil meta, worktree present",
			meta:               nil,
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusBroken,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name:               "nil meta, worktree absent (archived)",
			meta:               nil,
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: false, ReportBytes: 100},
			wantDerivedStatus:  StatusBroken,
			wantArchived:       true,
			wantReportNonempty: true,
		},
		{
			name:               "nil meta, tmux active (still broken)",
			meta:               nil,
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 64},
			wantDerivedStatus:  StatusBroken,
			wantArchived:       false,
			wantReportNonempty: true,
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
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusMerged,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "merged wins over needs_attention",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: "2026-01-10T14:00:00Z"}
				m.Flags = &store.RunMetaFlags{NeedsAttention: true}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 64},
			wantDerivedStatus:  StatusMerged,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "merged wins over ready_for_review conditions",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: "2026-01-10T14:00:00Z"}
				m.PRNumber = 123
				m.LastPushAt = "2026-01-10T13:00:00Z"
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusMerged,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "merged with worktree absent (archived)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: "2026-01-10T14:00:00Z"}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: false, ReportBytes: 0},
			wantDerivedStatus:  StatusMerged,
			wantArchived:       true,
			wantReportNonempty: false,
		},

		// ============================================================
		// 3. abandoned wins (except merged)
		// ============================================================
		{
			name: "abandoned wins over setup_failed",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{Abandoned: true, SetupFailed: true}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusAbandoned,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "abandoned wins over needs_attention",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{Abandoned: true, NeedsAttention: true}
			}),
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusAbandoned,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "abandoned with worktree absent",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{Abandoned: true}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: false, ReportBytes: 64},
			wantDerivedStatus:  StatusAbandoned,
			wantArchived:       true,
			wantReportNonempty: true,
		},

		// ============================================================
		// 4. setup_failed beats needs_attention
		// ============================================================
		{
			name: "setup_failed beats needs_attention",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{SetupFailed: true, NeedsAttention: true}
			}),
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusFailed,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "setup_failed alone",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{SetupFailed: true}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusFailed,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "setup_failed beats ready_for_review conditions",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{SetupFailed: true}
				m.PRNumber = 123
				m.LastPushAt = "2026-01-10T13:00:00Z"
			}),
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusFailed,
			wantArchived:       false,
			wantReportNonempty: true,
		},

		// ============================================================
		// 5. needs_attention beats ready_for_review
		// ============================================================
		{
			name: "needs_attention beats ready_for_review",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{NeedsAttention: true}
				m.PRNumber = 123
				m.LastPushAt = "2026-01-10T13:00:00Z"
			}),
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusNeedsAttention,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "needs_attention alone",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Flags = &store.RunMetaFlags{NeedsAttention: true}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusNeedsAttention,
			wantArchived:       false,
			wantReportNonempty: false,
		},

		// ============================================================
		// 6. ready_for_review predicate (positive and negative cases)
		// ============================================================
		{
			name: "ready_for_review: all predicates met (exact threshold)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.PRNumber = 123
				m.LastPushAt = "2026-01-10T13:00:00Z"
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 64},
			wantDerivedStatus:  StatusReadyForReview,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "ready_for_review: all predicates met (above threshold)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.PRNumber = 456
				m.LastPushAt = "2026-01-10T13:00:00Z"
			}),
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 1000},
			wantDerivedStatus:  StatusReadyForReview,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "NOT ready_for_review: missing pr_number",
			meta: mkMeta(func(m *store.RunMeta) {
				m.PRNumber = 0 // not set
				m.LastPushAt = "2026-01-10T13:00:00Z"
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "NOT ready_for_review: missing last_push_at",
			meta: mkMeta(func(m *store.RunMeta) {
				m.PRNumber = 123
				m.LastPushAt = "" // not set
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusIdlePR,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name: "NOT ready_for_review: report too small (63 bytes)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.PRNumber = 123
				m.LastPushAt = "2026-01-10T13:00:00Z"
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 63},
			wantDerivedStatus:  StatusIdlePR,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "NOT ready_for_review: report missing (0 bytes)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.PRNumber = 123
				m.LastPushAt = "2026-01-10T13:00:00Z"
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdlePR,
			wantArchived:       false,
			wantReportNonempty: false,
		},

		// ============================================================
		// 7. activity fallbacks
		// ============================================================
		{
			name: "active (pr): tmux_active=true, pr_number set",
			meta: mkMeta(func(m *store.RunMeta) {
				m.PRNumber = 123
			}),
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusActivePR,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "active: tmux_active=true, no pr_number",
			meta: mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusActive,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "idle (pr): tmux_active=false, pr_number set",
			meta: mkMeta(func(m *store.RunMeta) {
				m.PRNumber = 123
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdlePR,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "idle: tmux_active=false, no pr_number",
			meta: mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},

		// ============================================================
		// 8. archived boolean (worktree_present=false => Archived true)
		// ============================================================
		{
			name:               "archived: worktree_present=false",
			meta:               mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: false, ReportBytes: 0},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       true,
			wantReportNonempty: false,
		},
		{
			name:               "not archived: worktree_present=true",
			meta:               mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "archived applies to all statuses (merged + archived)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: "2026-01-10T14:00:00Z"}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: false, ReportBytes: 0},
			wantDerivedStatus:  StatusMerged,
			wantArchived:       true,
			wantReportNonempty: false,
		},
		{
			name: "archived applies to all statuses (active + archived)",
			meta: mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: true, WorktreePresent: false, ReportBytes: 0},
			wantDerivedStatus:  StatusActive,
			wantArchived:       true,
			wantReportNonempty: false,
		},

		// ============================================================
		// 9. report_nonempty boolean (threshold tests)
		// ============================================================
		{
			name:               "report_nonempty: 0 bytes => false",
			meta:               mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name:               "report_nonempty: 63 bytes => false",
			meta:               mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 63},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name:               "report_nonempty: 64 bytes => true (threshold)",
			meta:               mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 64},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name:               "report_nonempty: 100 bytes => true",
			meta:               mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 100},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: true,
		},
		{
			name:               "report_nonempty: negative bytes clamped to 0 => false",
			meta:               mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: -1},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name:               "report_nonempty: large negative clamped => false",
			meta:               mkMeta(nil),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: -1000},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},

		// ============================================================
		// Edge cases: nil sub-structs in meta
		// ============================================================
		{
			name:               "nil flags struct (not setup_failed)",
			meta:               mkMeta(nil), // Flags is nil by default in mkMeta
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "nil archive struct (not merged)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = nil // explicitly nil
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "empty merged_at string (not merged)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{MergedAt: ""}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
		},
		{
			name: "archived_at set but not merged_at (not merged status)",
			meta: mkMeta(func(m *store.RunMeta) {
				m.Archive = &store.RunMetaArchive{ArchivedAt: "2026-01-10T14:00:00Z", MergedAt: ""}
			}),
			snapshot:           Snapshot{TmuxActive: false, WorktreePresent: true, ReportBytes: 0},
			wantDerivedStatus:  StatusIdle,
			wantArchived:       false,
			wantReportNonempty: false,
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
			if got.ReportNonempty != tt.wantReportNonempty {
				t.Errorf("ReportNonempty = %v, want %v", got.ReportNonempty, tt.wantReportNonempty)
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

	_ = Derive(nil, Snapshot{TmuxActive: true, WorktreePresent: true, ReportBytes: 100})
}

// TestReportNonemptyThresholdConstant verifies the constant value.
func TestReportNonemptyThresholdConstant(t *testing.T) {
	if ReportNonemptyThresholdBytes != 64 {
		t.Errorf("ReportNonemptyThresholdBytes = %d, want 64", ReportNonemptyThresholdBytes)
	}
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
		"StatusActivePR":       "active (pr)",
		"StatusActive":         "active",
		"StatusIdlePR":         "idle (pr)",
		"StatusIdle":           "idle",
	}

	actual := map[string]string{
		"StatusBroken":         StatusBroken,
		"StatusMerged":         StatusMerged,
		"StatusAbandoned":      StatusAbandoned,
		"StatusFailed":         StatusFailed,
		"StatusNeedsAttention": StatusNeedsAttention,
		"StatusReadyForReview": StatusReadyForReview,
		"StatusActivePR":       StatusActivePR,
		"StatusActive":         StatusActive,
		"StatusIdlePR":         StatusIdlePR,
		"StatusIdle":           StatusIdle,
	}

	for name, want := range expected {
		got := actual[name]
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

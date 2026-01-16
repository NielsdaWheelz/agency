package verifyservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// TestMetaAttentionUpdateRules tests the needs_attention flag update rules.
func TestMetaAttentionUpdateRules(t *testing.T) {
	tests := []struct {
		name                  string
		initialNeedsAttention bool
		initialReason         string
		verifyOK              bool
		wantNeedsAttention    bool
		wantReason            string
	}{
		{
			name:                  "verify ok clears attention when reason is verify_failed",
			initialNeedsAttention: true,
			initialReason:         NeedsAttentionReasonVerifyFailed,
			verifyOK:              true,
			wantNeedsAttention:    false,
			wantReason:            "",
		},
		{
			name:                  "verify ok does NOT clear attention when reason is different",
			initialNeedsAttention: true,
			initialReason:         "stop_requested",
			verifyOK:              true,
			wantNeedsAttention:    true,
			wantReason:            "stop_requested",
		},
		{
			name:                  "verify ok does NOT clear attention when reason is setup_failed",
			initialNeedsAttention: true,
			initialReason:         "setup_failed",
			verifyOK:              true,
			wantNeedsAttention:    true,
			wantReason:            "setup_failed",
		},
		{
			name:                  "verify ok does nothing when no attention",
			initialNeedsAttention: false,
			initialReason:         "",
			verifyOK:              true,
			wantNeedsAttention:    false,
			wantReason:            "",
		},
		{
			name:                  "verify fail sets attention with reason verify_failed",
			initialNeedsAttention: false,
			initialReason:         "",
			verifyOK:              false,
			wantNeedsAttention:    true,
			wantReason:            NeedsAttentionReasonVerifyFailed,
		},
		{
			name:                  "verify fail overwrites different reason with verify_failed",
			initialNeedsAttention: true,
			initialReason:         "stop_requested",
			verifyOK:              false,
			wantNeedsAttention:    true,
			wantReason:            NeedsAttentionReasonVerifyFailed,
		},
		{
			name:                  "verify fail keeps attention when already verify_failed",
			initialNeedsAttention: true,
			initialReason:         NeedsAttentionReasonVerifyFailed,
			verifyOK:              false,
			wantNeedsAttention:    true,
			wantReason:            NeedsAttentionReasonVerifyFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create initial meta with flags
			meta := &store.RunMeta{
				SchemaVersion: "1.0",
				RunID:         "test-run",
				RepoID:        "test-repo",
				CreatedAt:     time.Now().Format(time.RFC3339),
			}

			if tt.initialNeedsAttention || tt.initialReason != "" {
				meta.Flags = &store.RunMetaFlags{
					NeedsAttention:       tt.initialNeedsAttention,
					NeedsAttentionReason: tt.initialReason,
				}
			}

			// Simulate the update logic from VerifyRun
			record := &store.VerifyRecord{OK: tt.verifyOK, StartedAt: "2025-01-01T00:00:00Z"}
			applyVerifyResultToMeta(meta, record)

			// Check results
			gotNeedsAttention := false
			gotReason := ""
			if meta.Flags != nil {
				gotNeedsAttention = meta.Flags.NeedsAttention
				gotReason = meta.Flags.NeedsAttentionReason
			}

			if gotNeedsAttention != tt.wantNeedsAttention {
				t.Errorf("NeedsAttention = %v, want %v", gotNeedsAttention, tt.wantNeedsAttention)
			}
			if gotReason != tt.wantReason {
				t.Errorf("NeedsAttentionReason = %q, want %q", gotReason, tt.wantReason)
			}
		})
	}
}

// applyVerifyResultToMeta simulates the meta update logic from VerifyRun.
// This is extracted to make the update logic testable without full integration.
func applyVerifyResultToMeta(meta *store.RunMeta, record *store.VerifyRecord) {
	if record.StartedAt != "" {
		meta.LastVerifyAt = record.FinishedAt
	}

	if record.OK {
		// Clear attention only if reason was verify_failed
		if meta.Flags != nil && meta.Flags.NeedsAttention && meta.Flags.NeedsAttentionReason == NeedsAttentionReasonVerifyFailed {
			meta.Flags.NeedsAttention = false
			meta.Flags.NeedsAttentionReason = ""
		}
	} else {
		// Set attention with reason verify_failed
		if meta.Flags == nil {
			meta.Flags = &store.RunMetaFlags{}
		}
		meta.Flags.NeedsAttention = true
		meta.Flags.NeedsAttentionReason = NeedsAttentionReasonVerifyFailed
	}
}

// TestWorkspacePredicate tests the workspace existence check.
func TestWorkspacePredicate(t *testing.T) {
	t.Run("worktree missing on disk returns E_WORKSPACE_ARCHIVED", func(t *testing.T) {
		dataDir := t.TempDir()
		repoID := "test-repo-id"
		runID := "20250110-abcd"

		// Create run directory and meta.json
		runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
		if err := os.MkdirAll(runDir, 0755); err != nil {
			t.Fatalf("failed to create run dir: %v", err)
		}

		// Write meta pointing to non-existent worktree
		meta := store.RunMeta{
			SchemaVersion: "1.0",
			RunID:         runID,
			RepoID:        repoID,
			Name:         "test",
			Runner:        "claude",
			ParentBranch:  "main",
			Branch:        "agency/test-abcd",
			WorktreePath:  "/nonexistent/worktree/path",
			CreatedAt:     time.Now().Format(time.RFC3339),
		}
		metaData, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(runDir, "meta.json"), metaData, 0644); err != nil {
			t.Fatalf("failed to write meta.json: %v", err)
		}

		// Create service and try to verify
		svc := NewService(dataDir, fs.NewRealFS())
		_, err := svc.VerifyRun(context.Background(), runID, 30*time.Minute)

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		code := errors.GetCode(err)
		if code != errors.EWorkspaceArchived {
			t.Errorf("error code = %s, want %s", code, errors.EWorkspaceArchived)
		}
	})

	t.Run("archived run returns E_WORKSPACE_ARCHIVED", func(t *testing.T) {
		dataDir := t.TempDir()
		repoID := "test-repo-id"
		runID := "20250110-efgh"

		// Create run directory and meta.json
		runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
		if err := os.MkdirAll(runDir, 0755); err != nil {
			t.Fatalf("failed to create run dir: %v", err)
		}

		// Create worktree directory
		worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
		if err := os.MkdirAll(worktreePath, 0755); err != nil {
			t.Fatalf("failed to create worktree: %v", err)
		}

		// Write meta with archive.archived_at set
		meta := store.RunMeta{
			SchemaVersion: "1.0",
			RunID:         runID,
			RepoID:        repoID,
			Name:         "test",
			Runner:        "claude",
			ParentBranch:  "main",
			Branch:        "agency/test-efgh",
			WorktreePath:  worktreePath,
			CreatedAt:     time.Now().Format(time.RFC3339),
			Archive: &store.RunMetaArchive{
				ArchivedAt: time.Now().Format(time.RFC3339),
			},
		}
		metaData, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(runDir, "meta.json"), metaData, 0644); err != nil {
			t.Fatalf("failed to write meta.json: %v", err)
		}

		// Create service and try to verify
		svc := NewService(dataDir, fs.NewRealFS())
		_, err := svc.VerifyRun(context.Background(), runID, 30*time.Minute)

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		code := errors.GetCode(err)
		if code != errors.EWorkspaceArchived {
			t.Errorf("error code = %s, want %s", code, errors.EWorkspaceArchived)
		}
	})
}

// TestVerifyRecordErrorAugmentation tests the error augmentation logic.
func TestVerifyRecordErrorAugmentation(t *testing.T) {
	t.Run("augments empty error", func(t *testing.T) {
		tmpDir := t.TempDir()
		recordPath := filepath.Join(tmpDir, "verify_record.json")

		// Write initial record with no error
		record := store.VerifyRecord{
			SchemaVersion: "1.0",
			RepoID:        "test",
			RunID:         "test",
			OK:            true,
		}
		data, _ := json.Marshal(record)
		if err := os.WriteFile(recordPath, data, 0644); err != nil {
			t.Fatalf("failed to write record: %v", err)
		}

		// Augment
		augmentRecordError(recordPath, []string{"event1 failed", "event2 failed"})

		// Read back
		data, err := os.ReadFile(recordPath)
		if err != nil {
			t.Fatalf("failed to read record: %v", err)
		}

		var result store.VerifyRecord
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("failed to parse record: %v", err)
		}

		if result.Error == nil {
			t.Fatal("expected error to be set")
		}

		want := "events append failed: event1 failed; event2 failed"
		if *result.Error != want {
			t.Errorf("error = %q, want %q", *result.Error, want)
		}
	})

	t.Run("preserves existing error", func(t *testing.T) {
		tmpDir := t.TempDir()
		recordPath := filepath.Join(tmpDir, "verify_record.json")

		// Write initial record with existing error
		existingErr := "some internal error"
		record := store.VerifyRecord{
			SchemaVersion: "1.0",
			RepoID:        "test",
			RunID:         "test",
			OK:            false,
			Error:         &existingErr,
		}
		data, _ := json.Marshal(record)
		if err := os.WriteFile(recordPath, data, 0644); err != nil {
			t.Fatalf("failed to write record: %v", err)
		}

		// Augment
		augmentRecordError(recordPath, []string{"event failed"})

		// Read back
		data, err := os.ReadFile(recordPath)
		if err != nil {
			t.Fatalf("failed to read record: %v", err)
		}

		var result store.VerifyRecord
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("failed to parse record: %v", err)
		}

		if result.Error == nil {
			t.Fatal("expected error to be set")
		}

		want := "some internal error; events append failed: event failed"
		if *result.Error != want {
			t.Errorf("error = %q, want %q", *result.Error, want)
		}
	})
}

// TestRunNotFound tests that non-existent runs return E_RUN_NOT_FOUND.
func TestRunNotFound(t *testing.T) {
	dataDir := t.TempDir()

	svc := NewService(dataDir, fs.NewRealFS())
	_, err := svc.VerifyRun(context.Background(), "nonexistent-run", 30*time.Minute)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	code := errors.GetCode(err)
	if code != errors.ERunNotFound {
		t.Errorf("error code = %s, want %s", code, errors.ERunNotFound)
	}
}

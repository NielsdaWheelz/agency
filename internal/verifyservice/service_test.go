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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMetaAttentionUpdateRules tests the needs_attention flag update rules.
func TestMetaAttentionUpdateRules(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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

			assert.Equal(t, tt.wantNeedsAttention, gotNeedsAttention)
			assert.Equal(t, tt.wantReason, gotReason)
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
	t.Parallel()
	t.Run("worktree missing on disk returns E_WORKSPACE_ARCHIVED", func(t *testing.T) {
		t.Parallel()
		dataDir := t.TempDir()
		repoID := "test-repo-id"
		runID := "20250110-abcd"

		// Create run directory and meta.json
		runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
		require.NoError(t, os.MkdirAll(runDir, 0755), "failed to create run dir")

		// Write meta pointing to non-existent worktree
		meta := store.RunMeta{
			SchemaVersion: "1.0",
			RunID:         runID,
			RepoID:        repoID,
			Name:          "test",
			Runner:        "claude-code",
			ParentBranch:  "main",
			Branch:        "agency/test-abcd",
			WorktreePath:  "/nonexistent/worktree/path",
			CreatedAt:     time.Now().Format(time.RFC3339),
		}
		metaData, _ := json.Marshal(meta)
		require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), metaData, 0644), "failed to write meta.json")

		// Create service and try to verify
		svc := NewService(dataDir, fs.NewRealFS())
		_, err := svc.VerifyRun(context.Background(), runID, 30*time.Minute)

		require.Error(t, err)

		code := errors.GetCode(err)
		assert.Equal(t, errors.EWorkspaceArchived, code)
	})

	t.Run("archived run returns E_WORKSPACE_ARCHIVED", func(t *testing.T) {
		t.Parallel()
		dataDir := t.TempDir()
		repoID := "test-repo-id"
		runID := "20250110-efgh"

		// Create run directory and meta.json
		runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
		require.NoError(t, os.MkdirAll(runDir, 0755), "failed to create run dir")

		// Create worktree directory
		worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
		require.NoError(t, os.MkdirAll(worktreePath, 0755), "failed to create worktree")

		// Write meta with archive.archived_at set
		meta := store.RunMeta{
			SchemaVersion: "1.0",
			RunID:         runID,
			RepoID:        repoID,
			Name:          "test",
			Runner:        "claude-code",
			ParentBranch:  "main",
			Branch:        "agency/test-efgh",
			WorktreePath:  worktreePath,
			CreatedAt:     time.Now().Format(time.RFC3339),
			Archive: &store.RunMetaArchive{
				ArchivedAt: time.Now().Format(time.RFC3339),
			},
		}
		metaData, _ := json.Marshal(meta)
		require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), metaData, 0644), "failed to write meta.json")

		// Create service and try to verify
		svc := NewService(dataDir, fs.NewRealFS())
		_, err := svc.VerifyRun(context.Background(), runID, 30*time.Minute)

		require.Error(t, err)

		code := errors.GetCode(err)
		assert.Equal(t, errors.EWorkspaceArchived, code)
	})
}

// TestVerifyRecordErrorAugmentation tests the error augmentation logic.
func TestVerifyRecordErrorAugmentation(t *testing.T) {
	t.Parallel()
	t.Run("augments empty error", func(t *testing.T) {
		t.Parallel()
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
		require.NoError(t, os.WriteFile(recordPath, data, 0644), "failed to write record")

		// Augment
		augmentRecordError(recordPath, []string{"event1 failed", "event2 failed"})

		// Read back
		data, err := os.ReadFile(recordPath)
		require.NoError(t, err, "failed to read record")

		var result store.VerifyRecord
		require.NoError(t, json.Unmarshal(data, &result), "failed to parse record")

		require.NotNil(t, result.Error, "expected error to be set")

		want := "events append failed: event1 failed; event2 failed"
		assert.Equal(t, want, *result.Error)
	})

	t.Run("preserves existing error", func(t *testing.T) {
		t.Parallel()
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
		require.NoError(t, os.WriteFile(recordPath, data, 0644), "failed to write record")

		// Augment
		augmentRecordError(recordPath, []string{"event failed"})

		// Read back
		data, err := os.ReadFile(recordPath)
		require.NoError(t, err, "failed to read record")

		var result store.VerifyRecord
		require.NoError(t, json.Unmarshal(data, &result), "failed to parse record")

		require.NotNil(t, result.Error, "expected error to be set")

		want := "some internal error; events append failed: event failed"
		assert.Equal(t, want, *result.Error)
	})
}

// TestRunNotFound tests that non-existent runs return E_RUN_NOT_FOUND.
func TestRunNotFound(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	svc := NewService(dataDir, fs.NewRealFS())
	_, err := svc.VerifyRun(context.Background(), "nonexistent-run", 30*time.Minute)

	require.Error(t, err)

	code := errors.GetCode(err)
	assert.Equal(t, errors.ERunNotFound, code)
}

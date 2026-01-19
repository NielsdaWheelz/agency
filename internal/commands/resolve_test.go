package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// setupResolveTestEnv creates a temporary test environment with runs for resolution tests.
func setupResolveTestEnv(t *testing.T, runs []testRun) string {
	t.Helper()

	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")

	for _, run := range runs {
		runDir := filepath.Join(dataDir, "repos", run.repoID, "runs", run.runID)
		if err := os.MkdirAll(runDir, 0755); err != nil {
			t.Fatal(err)
		}

		meta := &store.RunMeta{
			SchemaVersion: "1.0",
			RunID:         run.runID,
			RepoID:        run.repoID,
			Name:          run.name,
			Runner:        "claude",
			RunnerCmd:     "claude",
			ParentBranch:  "main",
			Branch:        "agency/test-" + run.runID[:4],
			WorktreePath:  filepath.Join(dataDir, "repos", run.repoID, "worktrees", run.runID),
			CreatedAt:     "2026-01-10T12:00:00Z",
		}

		if run.archived {
			meta.Archive = &store.RunMetaArchive{
				ArchivedAt: "2026-01-11T12:00:00Z",
			}
		}

		metaBytes, _ := json.MarshalIndent(meta, "", "  ")
		metaPath := filepath.Join(runDir, "meta.json")
		if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
			t.Fatal(err)
		}
	}

	return dataDir
}

type testRun struct {
	repoID   string
	runID    string
	name     string
	archived bool
}

func TestResolveRunByNameOrID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		records     []store.RunRecord
		wantRunID   string
		wantErrType string // "not_found", "ambiguous", or ""
	}{
		{
			name:  "resolve by exact name",
			input: "my-feature",
			records: []store.RunRecord{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "my-feature", Meta: &store.RunMeta{Name: "my-feature"}},
				{RepoID: "r1", RunID: "20260110-bbbb", Name: "other", Meta: &store.RunMeta{Name: "other"}},
			},
			wantRunID: "20260110-aaaa",
		},
		{
			name:  "resolve by exact run_id",
			input: "20260110-bbbb",
			records: []store.RunRecord{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "my-feature", Meta: &store.RunMeta{Name: "my-feature"}},
				{RepoID: "r1", RunID: "20260110-bbbb", Name: "other", Meta: &store.RunMeta{Name: "other"}},
			},
			wantRunID: "20260110-bbbb",
		},
		{
			name:  "resolve by run_id prefix",
			input: "20260110-aa",
			records: []store.RunRecord{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "my-feature", Meta: &store.RunMeta{Name: "my-feature"}},
				{RepoID: "r1", RunID: "20260110-bbbb", Name: "other", Meta: &store.RunMeta{Name: "other"}},
			},
			wantRunID: "20260110-aaaa",
		},
		{
			name:  "name takes priority over run_id prefix",
			input: "my-feature",
			records: []store.RunRecord{
				{RepoID: "r1", RunID: "my-feature-a3f2", Name: "other", Meta: &store.RunMeta{Name: "other"}},
				{RepoID: "r1", RunID: "20260110-bbbb", Name: "my-feature", Meta: &store.RunMeta{Name: "my-feature"}},
			},
			wantRunID: "20260110-bbbb",
		},
		{
			name:  "archived runs excluded from name matching",
			input: "test-name",
			records: []store.RunRecord{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "test-name", Meta: &store.RunMeta{Name: "test-name", Archive: &store.RunMetaArchive{ArchivedAt: "2026-01-11T00:00:00Z"}}},
				{RepoID: "r1", RunID: "20260110-bbbb", Name: "test-name", Meta: &store.RunMeta{Name: "test-name"}},
			},
			wantRunID: "20260110-bbbb",
		},
		{
			name:  "not found",
			input: "nonexistent",
			records: []store.RunRecord{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "my-feature", Meta: &store.RunMeta{Name: "my-feature"}},
			},
			wantErrType: "not_found",
		},
		{
			name:  "ambiguous name",
			input: "dup-name",
			records: []store.RunRecord{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "dup-name", Meta: &store.RunMeta{Name: "dup-name"}},
				{RepoID: "r2", RunID: "20260110-bbbb", Name: "dup-name", Meta: &store.RunMeta{Name: "dup-name"}},
			},
			wantErrType: "ambiguous",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, record, err := resolveRunByNameOrID(tt.input, tt.records)

			if tt.wantErrType != "" {
				if err == nil {
					t.Fatalf("expected error type %s, got nil", tt.wantErrType)
				}
				// Check for raw ids errors (before conversion by handleResolveErr)
				switch tt.wantErrType {
				case "not_found":
					if _, ok := err.(*ids.ErrNotFound); !ok {
						t.Errorf("expected *ids.ErrNotFound, got %T", err)
					}
				case "ambiguous":
					if _, ok := err.(*ids.ErrAmbiguous); !ok {
						t.Errorf("expected *ids.ErrAmbiguous, got %T", err)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ref.RunID != tt.wantRunID {
				t.Errorf("RunID = %s, want %s", ref.RunID, tt.wantRunID)
			}

			if record == nil {
				t.Fatal("record is nil")
			}
			if record.RunID != tt.wantRunID {
				t.Errorf("record.RunID = %s, want %s", record.RunID, tt.wantRunID)
			}
		})
	}
}

func TestResolveRunInRepo(t *testing.T) {
	runs := []testRun{
		{repoID: "repo1", runID: "20260110-aaaa", name: "feature-x"},
		{repoID: "repo1", runID: "20260110-bbbb", name: "feature-y"},
		{repoID: "repo2", runID: "20260110-cccc", name: "feature-x"}, // same name, different repo
	}
	dataDir := setupResolveTestEnv(t, runs)

	tests := []struct {
		name      string
		input     string
		repoID    string
		wantRunID string
		wantErr   errors.Code
	}{
		{
			name:      "resolve by name in repo",
			input:     "feature-x",
			repoID:    "repo1",
			wantRunID: "20260110-aaaa",
		},
		{
			name:      "resolve by name in different repo",
			input:     "feature-x",
			repoID:    "repo2",
			wantRunID: "20260110-cccc",
		},
		{
			name:      "resolve by run_id",
			input:     "20260110-bbbb",
			repoID:    "repo1",
			wantRunID: "20260110-bbbb",
		},
		{
			name:    "not found in repo",
			input:   "feature-z",
			repoID:  "repo1",
			wantErr: errors.ERunNotFound,
		},
		{
			name:    "run exists in different repo",
			input:   "feature-y",
			repoID:  "repo2", // feature-y only exists in repo1
			wantErr: errors.ERunNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, record, err := resolveRunInRepo(tt.input, tt.repoID, dataDir)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %s, got nil", tt.wantErr)
				}
				code := errors.GetCode(err)
				if code != tt.wantErr {
					t.Errorf("error code = %s, want %s", code, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ref.RunID != tt.wantRunID {
				t.Errorf("RunID = %s, want %s", ref.RunID, tt.wantRunID)
			}

			if record == nil {
				t.Fatal("record is nil")
			}
		})
	}
}

func TestResolveRunGlobal(t *testing.T) {
	runs := []testRun{
		{repoID: "repo1", runID: "20260110-aaaa", name: "feature-x"},
		{repoID: "repo1", runID: "20260110-bbbb", name: "feature-y"},
		{repoID: "repo2", runID: "20260110-cccc", name: "feature-z"},
		{repoID: "repo2", runID: "20260110-dddd", name: "archived-run", archived: true},
	}
	dataDir := setupResolveTestEnv(t, runs)

	tests := []struct {
		name      string
		input     string
		wantRunID string
		wantErr   errors.Code
	}{
		{
			name:      "resolve by unique name globally",
			input:     "feature-x",
			wantRunID: "20260110-aaaa",
		},
		{
			name:      "resolve by run_id globally",
			input:     "20260110-cccc",
			wantRunID: "20260110-cccc",
		},
		{
			name:      "resolve by run_id prefix globally",
			input:     "20260110-bb",
			wantRunID: "20260110-bbbb",
		},
		{
			name:    "not found globally",
			input:   "nonexistent",
			wantErr: errors.ERunNotFound,
		},
		{
			name:    "archived run not matched by name, falls back to run_id (not found)",
			input:   "archived-run",
			wantErr: errors.ERunNotFound, // name excluded because archived, no run_id match
		},
		{
			name:      "archived run can still be resolved by run_id",
			input:     "20260110-dddd",
			wantRunID: "20260110-dddd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, record, err := resolveRunGlobal(tt.input, dataDir)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %s, got nil", tt.wantErr)
				}
				code := errors.GetCode(err)
				if code != tt.wantErr {
					t.Errorf("error code = %s, want %s", code, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ref.RunID != tt.wantRunID {
				t.Errorf("RunID = %s, want %s", ref.RunID, tt.wantRunID)
			}

			if record == nil {
				t.Fatal("record is nil")
			}
		})
	}
}

func TestHandleResolveErr(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		input    string
		wantCode errors.Code
	}{
		{
			name:     "not found error",
			err:      &ids.ErrNotFound{Input: "test"},
			input:    "test",
			wantCode: errors.ERunNotFound,
		},
		{
			name: "ambiguous error",
			err: &ids.ErrAmbiguous{
				Input: "test",
				Candidates: []ids.RunRef{
					{RunID: "run1", Name: "name1"},
					{RunID: "run2", Name: "name2"},
				},
			},
			input:    "test",
			wantCode: errors.ERunIDAmbiguous,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handleResolveErr(tt.err, tt.input)
			code := errors.GetCode(err)
			if code != tt.wantCode {
				t.Errorf("error code = %s, want %s", code, tt.wantCode)
			}
		})
	}
}

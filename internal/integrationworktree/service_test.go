package integrationworktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// TestCreateAndRemove tests the full lifecycle of creating and removing a worktree.
// This is an integration test that requires git to be installed.
func TestCreateAndRemove(t *testing.T) {
	// Hermetic git environment: blocks system/global config, provides test identity.
	testutil.HermeticGitEnv(t)

	// Skip if git not available
	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := cr.Run(ctx, "git", []string{"--version"}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		t.Skip("git not available")
	}

	// Create temp directories
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	dataDir := filepath.Join(tmpDir, "data")

	// Initialize git repo with explicit initial branch name
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	result, err = cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git init failed: %v, exit %d", err, result.ExitCode)
	}

	// Create initial commit
	readme := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test\n"), 0o644); err != nil {
		t.Fatalf("failed to write readme: %v", err)
	}

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git add failed: %v, exit %d", err, result.ExitCode)
	}
	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "Initial commit"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git commit failed: %v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	// Create service
	fsys := fs.NewRealFS()
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })
	svc := NewService(st, cr, fsys, func() time.Time { return now })

	repoID := "abc123def456"

	var createdWorktreeID string

	// Test Create
	t.Run("Create", func(t *testing.T) {
		result, err := svc.Create(ctx, CreateOpts{
			Name:         "test-feature",
			RepoRoot:     repoDir,
			RepoID:       repoID,
			ParentBranch: "main",
		})

		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		// Verify result
		if result.WorktreeID == "" {
			t.Error("WorktreeID is empty")
		}
		if result.Branch == "" {
			t.Error("Branch is empty")
		}
		if result.TreePath == "" {
			t.Error("TreePath is empty")
		}

		// Verify tree directory exists
		if _, err := os.Stat(result.TreePath); os.IsNotExist(err) {
			t.Error("tree directory not created")
		}

		// Verify INTEGRATION_MARKER exists
		markerPath := filepath.Join(result.TreePath, ".agency", IntegrationMarkerFileName)
		if _, err := os.Stat(markerPath); os.IsNotExist(err) {
			t.Error("INTEGRATION_MARKER not created")
		}

		// Verify meta.json exists
		metaPath := st.IntegrationWorktreeMetaPath(repoID, result.WorktreeID)
		if _, err := os.Stat(metaPath); os.IsNotExist(err) {
			t.Error("meta.json not created")
		}

		// Verify HasIntegrationMarker
		if !HasIntegrationMarker(result.TreePath) {
			t.Error("HasIntegrationMarker returned false")
		}

		// Store the worktree ID for later tests
		createdWorktreeID = result.WorktreeID
	})

	// Test Resolve
	t.Run("Resolve", func(t *testing.T) {
		if createdWorktreeID == "" {
			t.Skip("Create test failed, skipping")
		}
		record, err := svc.Resolve(repoID, "test-feature", false)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if record.Name != "test-feature" {
			t.Errorf("Name = %v, want test-feature", record.Name)
		}
	})

	// Test name uniqueness
	t.Run("NameUniqueness", func(t *testing.T) {
		if createdWorktreeID == "" {
			t.Skip("Create test failed, skipping")
		}
		_, err := svc.Create(ctx, CreateOpts{
			Name:         "test-feature",
			RepoRoot:     repoDir,
			RepoID:       repoID,
			ParentBranch: "main",
		})
		if err == nil {
			t.Error("expected error for duplicate name")
		}
	})

	// Test Remove
	t.Run("Remove", func(t *testing.T) {
		if createdWorktreeID == "" {
			t.Skip("Create test failed, skipping")
		}
		record, err := svc.Resolve(repoID, "test-feature", false)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		treePath := record.Meta.TreePath

		err = svc.Remove(ctx, repoID, record.WorktreeID, RemoveOpts{
			RepoRoot: repoDir,
			Force:    true,
		})
		if err != nil {
			t.Fatalf("Remove() error = %v", err)
		}

		// Verify tree directory is gone
		if _, err := os.Stat(treePath); !os.IsNotExist(err) {
			t.Error("tree directory still exists after remove")
		}

		// Verify meta.json still exists with archived state
		meta, err := st.ReadIntegrationWorktreeMeta(repoID, record.WorktreeID)
		if err != nil {
			t.Fatalf("ReadIntegrationWorktreeMeta() error = %v", err)
		}
		if meta.State != store.WorktreeStateArchived {
			t.Errorf("State = %v, want archived", meta.State)
		}
	})
}

func TestHasIntegrationMarker(t *testing.T) {
	tmpDir := t.TempDir()

	// Test without marker
	if HasIntegrationMarker(tmpDir) {
		t.Error("should return false for dir without marker")
	}

	// Create marker
	agencyDir := filepath.Join(tmpDir, ".agency")
	if err := os.Mkdir(agencyDir, 0o755); err != nil {
		t.Fatalf("failed to create .agency dir: %v", err)
	}
	markerPath := filepath.Join(agencyDir, IntegrationMarkerFileName)
	if err := os.WriteFile(markerPath, []byte("marker"), 0o644); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	// Test with marker
	if !HasIntegrationMarker(tmpDir) {
		t.Error("should return true for dir with marker")
	}
}

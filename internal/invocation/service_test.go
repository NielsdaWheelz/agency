// Package invocation provides invocation operations for Slice 8.
// This file implements tests for invariant enforcement (Slice 8 PR-02).
package invocation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// testNow returns a fixed time for deterministic tests.
func testNow() time.Time {
	return time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
}

// setupTestRepo creates a test git repository with a main branch.
// Returns the repo path, the branch name, and a cleanup function.
func setupTestRepo(t *testing.T) (string, string, func()) {
	t.Helper()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "agency-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	ctx := context.Background()
	cr := exec.NewRealRunner()

	// Initialize git repo with main as initial branch
	result, err := cr.Run(ctx, "git", []string{"-C", tmpDir, "init", "-b", "main"}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		cleanup()
		t.Fatalf("failed to init git repo: %v, stderr: %s", err, result.Stderr)
	}

	// Configure git for commits
	_, _ = cr.Run(ctx, "git", []string{"-C", tmpDir, "config", "user.email", "test@test.com"}, exec.RunOpts{})
	_, _ = cr.Run(ctx, "git", []string{"-C", tmpDir, "config", "user.name", "Test"}, exec.RunOpts{})

	// Create initial commit
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		cleanup()
		t.Fatalf("failed to create test file: %v", err)
	}

	_, _ = cr.Run(ctx, "git", []string{"-C", tmpDir, "add", "."}, exec.RunOpts{})
	result, err = cr.Run(ctx, "git", []string{"-C", tmpDir, "commit", "-m", "initial"}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		cleanup()
		t.Fatalf("failed to commit: %v, stderr: %s", err, result.Stderr)
	}

	return tmpDir, "main", cleanup
}

// setupIntegrationWorktree creates an integration worktree for testing.
func setupIntegrationWorktree(t *testing.T, dataDir, repoRoot, repoID string) *store.IntegrationWorktreeMeta {
	t.Helper()

	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	svc := integrationworktree.NewService(st, cr, fsys, testNow)

	result, err := svc.Create(ctx, integrationworktree.CreateOpts{
		Name:         "test-feature",
		RepoRoot:     repoRoot,
		RepoID:       repoID,
		ParentBranch: "main",
	})
	if err != nil {
		t.Fatalf("failed to create integration worktree: %v", err)
	}

	meta, err := st.ReadIntegrationWorktreeMeta(repoID, result.WorktreeID)
	if err != nil {
		t.Fatalf("failed to read integration worktree meta: %v", err)
	}

	return meta
}

// TestSandboxNeverResolvesToIntegrationTree verifies that sandbox path
// cannot resolve to the integration tree path.
// This is a CRITICAL invariant test per PR-02.
func TestSandboxNeverResolvesToIntegrationTree(t *testing.T) {
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	if err := fsys.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Create invocation service
	invSvc := NewService(st, cr, fsys, testNow)

	// Test: validate sandbox path against integration path
	// These should all fail if paths would overlap

	t.Run("sandbox_equals_integration", func(t *testing.T) {
		err := invSvc.validateSandboxPath(wtMeta.TreePath, wtMeta.TreePath)
		if err == nil {
			t.Error("expected error when sandbox path equals integration path")
		}
		if errors.GetCode(err) != errors.ESandboxPathUnsafe {
			t.Errorf("expected E_SANDBOX_PATH_UNSAFE, got %s", errors.GetCode(err))
		}
	})

	t.Run("sandbox_parent_of_integration", func(t *testing.T) {
		integrationParent := filepath.Dir(wtMeta.TreePath)
		err := invSvc.validateSandboxPath(integrationParent, wtMeta.TreePath)
		if err == nil {
			t.Error("expected error when sandbox is parent of integration")
		}
		if errors.GetCode(err) != errors.ESandboxPathUnsafe {
			t.Errorf("expected E_SANDBOX_PATH_UNSAFE, got %s", errors.GetCode(err))
		}
	})

	t.Run("sandbox_child_of_integration", func(t *testing.T) {
		integrationChild := filepath.Join(wtMeta.TreePath, "subdir")
		err := invSvc.validateSandboxPath(integrationChild, wtMeta.TreePath)
		if err == nil {
			t.Error("expected error when sandbox is child of integration")
		}
		if errors.GetCode(err) != errors.ESandboxPathUnsafe {
			t.Errorf("expected E_SANDBOX_PATH_UNSAFE, got %s", errors.GetCode(err))
		}
	})

	t.Run("valid_sandbox_path", func(t *testing.T) {
		validPath := filepath.Join(dataDir, "sandboxes", "test-inv", "tree")
		err := invSvc.validateSandboxPath(validPath, wtMeta.TreePath)
		if err != nil {
			t.Errorf("expected no error for valid sandbox path, got %v", err)
		}
	})
}

// TestIntegrationMarkerEnforcement verifies that agent start fails
// if targeting a non-integration worktree.
// This is a CRITICAL invariant test per PR-02.
func TestIntegrationMarkerEnforcement(t *testing.T) {
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	if err := fsys.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Remove the INTEGRATION_MARKER to simulate non-integration directory
	markerPath := filepath.Join(wtMeta.TreePath, ".agency", integrationworktree.IntegrationMarkerFileName)
	if err := os.Remove(markerPath); err != nil {
		t.Fatalf("failed to remove integration marker: %v", err)
	}

	// Try to create invocation - should fail
	invSvc := NewService(st, cr, fsys, testNow)

	_, err = invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude",
		Mode:                    store.RunnerModeHeaded,
	})

	if err == nil {
		t.Fatal("expected error when INTEGRATION_MARKER is missing")
	}

	if errors.GetCode(err) != errors.EIntegrationMarkerMissing {
		t.Errorf("expected E_INTEGRATION_MARKER_MISSING, got %s", errors.GetCode(err))
	}
}

// TestSandboxMarkerWritten verifies that SANDBOX_MARKER is written
// after successful sandbox creation.
// This is a CRITICAL invariant test per PR-02.
func TestSandboxMarkerWritten(t *testing.T) {
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	if err := fsys.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Create invocation
	invSvc := NewService(st, cr, fsys, testNow)

	result, err := invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude",
		Mode:                    store.RunnerModeHeaded,
	})
	if err != nil {
		t.Fatalf("failed to create invocation: %v", err)
	}

	// Verify SANDBOX_MARKER exists in sandbox tree
	sandboxMarkerPath := filepath.Join(result.SandboxPath, ".agency", SandboxMarkerFileName)
	if _, err := os.Stat(sandboxMarkerPath); os.IsNotExist(err) {
		t.Errorf("SANDBOX_MARKER not found at %s", sandboxMarkerPath)
	}

	// Verify integration tree does NOT contain SANDBOX_MARKER
	integrationSandboxMarker := filepath.Join(wtMeta.TreePath, ".agency", SandboxMarkerFileName)
	if _, err := os.Stat(integrationSandboxMarker); err == nil {
		t.Errorf("SANDBOX_MARKER should NOT exist in integration tree at %s", integrationSandboxMarker)
	}

	// Verify sandbox does NOT contain INTEGRATION_MARKER
	sandboxIntegrationMarker := filepath.Join(result.SandboxPath, ".agency", integrationworktree.IntegrationMarkerFileName)
	if _, err := os.Stat(sandboxIntegrationMarker); err == nil {
		t.Errorf("INTEGRATION_MARKER should NOT exist in sandbox tree at %s", sandboxIntegrationMarker)
	}
}

// TestCleanupOnPartialFailure verifies that partial state is cleaned up
// when sandbox creation fails after worktree add.
// This is a CRITICAL invariant test per PR-02.
func TestCleanupOnPartialFailure(t *testing.T) {
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()

	// Create repo directory
	realFS := fs.NewRealFS()
	st := store.NewStore(realFS, dataDir, testNow)
	repoDir := st.RepoDir(repoID)
	if err := realFS.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Create a failing filesystem that errors on SANDBOX_MARKER write
	failingFS := &failOnMarkerWriteFS{
		FS:         realFS,
		failOnPath: SandboxMarkerFileName,
	}

	invSvc := NewService(st, cr, failingFS, testNow)

	// Attempt to create invocation - should fail
	_, err = invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude",
		Mode:                    store.RunnerModeHeaded,
	})
	if err == nil {
		t.Fatal("expected error when marker write fails")
	}

	// Verify cleanup happened:
	// 1. Check git worktree list - should not contain sandbox worktree
	result, gitErr := cr.Run(ctx, "git", []string{"-C", repoRoot, "worktree", "list"}, exec.RunOpts{})
	if gitErr != nil {
		t.Fatalf("failed to list worktrees: %v", gitErr)
	}
	if strings.Contains(result.Stdout, "sandboxes") {
		t.Error("sandbox worktree should have been cleaned up from git worktree list")
	}

	// 2. Check for sandbox branches - should not exist
	branchResult, _ := cr.Run(ctx, "git", []string{"-C", repoRoot, "branch", "--list", "agency/sandbox-*"}, exec.RunOpts{})
	if strings.TrimSpace(branchResult.Stdout) != "" {
		t.Errorf("sandbox branch should have been cleaned up, found: %s", branchResult.Stdout)
	}

	// 3. Check sandbox directory does not exist
	sandboxesDir := st.SandboxesDir(repoID)
	entries, _ := os.ReadDir(sandboxesDir)
	if len(entries) > 0 {
		t.Errorf("sandbox directory should have been cleaned up, found %d entries", len(entries))
	}

	// 4. Check invocation directory does not exist
	invocationsDir := st.InvocationsDir(repoID)
	entries, _ = os.ReadDir(invocationsDir)
	if len(entries) > 0 {
		t.Errorf("invocation directory should have been cleaned up, found %d entries", len(entries))
	}
}

// TestMultipleSandboxesPerWorktree verifies that multiple sandboxes
// can be created for the same integration worktree.
func TestMultipleSandboxesPerWorktree(t *testing.T) {
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	if err := fsys.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Create invocation service with incrementing clock
	callCount := 0
	incrementingNow := func() time.Time {
		callCount++
		return time.Date(2026, 1, 31, 12, 0, callCount, 0, time.UTC)
	}
	invSvc := NewService(st, cr, fsys, incrementingNow)

	// Create multiple invocations
	var invocations []*CreateResult
	for i := 0; i < 3; i++ {
		result, err := invSvc.Create(ctx, CreateOpts{
			IntegrationWorktreeID:   wtMeta.WorktreeID,
			IntegrationWorktreeMeta: wtMeta,
			RepoRoot:                repoRoot,
			RepoID:                  repoID,
			Runner:                  "claude",
			Mode:                    store.RunnerModeHeaded,
			InvocationName:          "test-agent-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatalf("failed to create invocation %d: %v", i, err)
		}
		invocations = append(invocations, result)
	}

	// Verify all invocations have unique IDs
	seenIDs := make(map[string]bool)
	for _, inv := range invocations {
		if seenIDs[inv.InvocationID] {
			t.Errorf("duplicate invocation ID: %s", inv.InvocationID)
		}
		seenIDs[inv.InvocationID] = true
	}

	// Verify all sandboxes have unique paths
	seenPaths := make(map[string]bool)
	for _, inv := range invocations {
		if seenPaths[inv.SandboxPath] {
			t.Errorf("duplicate sandbox path: %s", inv.SandboxPath)
		}
		seenPaths[inv.SandboxPath] = true

		// Verify sandbox exists
		if _, err := os.Stat(inv.SandboxPath); os.IsNotExist(err) {
			t.Errorf("sandbox path does not exist: %s", inv.SandboxPath)
		}
	}

	// Verify integration tree remains unchanged (marker still present)
	if !integrationworktree.HasIntegrationMarker(wtMeta.TreePath) {
		t.Error("integration worktree should still have INTEGRATION_MARKER")
	}
}

// TestIntegrationTreeUntouched verifies that the integration tree
// is never modified by sandbox creation.
func TestIntegrationTreeUntouched(t *testing.T) {
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	if err := fsys.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Record state of integration tree before
	integrationBefore := dirSnapshot(t, wtMeta.TreePath)

	// Create invocation
	invSvc := NewService(st, cr, fsys, testNow)
	_, err = invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude",
		Mode:                    store.RunnerModeHeaded,
	})
	if err != nil {
		t.Fatalf("failed to create invocation: %v", err)
	}

	// Verify integration tree is unchanged
	integrationAfter := dirSnapshot(t, wtMeta.TreePath)

	if integrationBefore != integrationAfter {
		t.Errorf("integration tree was modified during sandbox creation\nbefore: %s\nafter: %s",
			integrationBefore, integrationAfter)
	}
}

// dirSnapshot creates a simple snapshot of directory contents for comparison.
func dirSnapshot(t *testing.T, path string) string {
	t.Helper()
	var result strings.Builder
	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(path, p)
		if rel == "." {
			return nil
		}
		result.WriteString(rel)
		result.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk directory: %v", err)
	}
	return result.String()
}

// failOnMarkerWriteFS is a filesystem that fails when writing the marker file.
type failOnMarkerWriteFS struct {
	fs.FS
	failOnPath string
}

func (f *failOnMarkerWriteFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	if strings.HasSuffix(path, f.failOnPath) {
		return os.ErrPermission
	}
	return f.FS.WriteFile(path, data, perm)
}

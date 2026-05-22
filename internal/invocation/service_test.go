// Package invocation provides invocation operations.
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err, "failed to create temp dir")

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	ctx := context.Background()
	cr := exec.NewRealRunner()

	// Initialize git repo with main as initial branch
	result, err := cr.Run(ctx, "git", []string{"-C", tmpDir, "init", "-b", "main"}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		cleanup()
		require.Fail(t, "failed to init git repo", "err=%v, stderr=%s", err, result.Stderr)
	}

	// Configure git for commits
	_, _ = cr.Run(ctx, "git", []string{"-C", tmpDir, "config", "user.email", "test@test.com"}, exec.RunOpts{})
	_, _ = cr.Run(ctx, "git", []string{"-C", tmpDir, "config", "user.name", "Test"}, exec.RunOpts{})

	// Create initial commit
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o644), "failed to create test file")

	_, _ = cr.Run(ctx, "git", []string{"-C", tmpDir, "add", "."}, exec.RunOpts{})
	result, err = cr.Run(ctx, "git", []string{"-C", tmpDir, "commit", "-m", "initial"}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		cleanup()
		require.Fail(t, "failed to commit", "err=%v, stderr=%s", err, result.Stderr)
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
		Name:             "test-feature",
		RepoRoot:         repoRoot,
		RepoID:           repoID,
		BaseBranch:       "main",
		CheckoutRoot:     filepath.Join(dataDir, "checkouts", repoID),
		ExecutionProfile: "work",
	})
	require.NoError(t, err, "failed to create integration worktree")

	meta, err := st.ReadIntegrationWorktreeMeta(repoID, result.WorktreeID)
	require.NoError(t, err, "failed to read integration worktree meta")

	return meta
}

// TestSandboxNeverResolvesToIntegrationTree verifies that sandbox path
// cannot resolve to the integration tree path.
// This is a critical invariant test.
func TestSandboxNeverResolvesToIntegrationTree(t *testing.T) {
	t.Parallel()
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	require.NoError(t, err, "failed to create data dir")
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	require.NoError(t, fsys.MkdirAll(repoDir, 0o700), "failed to create repo dir")

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Create invocation service
	invSvc := NewService(st, cr, fsys, testNow)

	// Test: validate sandbox path against integration path
	// These should all fail if paths would overlap

	t.Run("sandbox_equals_integration", func(t *testing.T) {
		err := invSvc.validateSandboxPath(wtMeta.TreePath, wtMeta.TreePath)
		require.Error(t, err, "expected error when sandbox path equals integration path")
		assert.Equal(t, errors.ESandboxPathUnsafe, errors.GetCode(err))
	})

	t.Run("sandbox_parent_of_integration", func(t *testing.T) {
		integrationParent := filepath.Dir(wtMeta.TreePath)
		err := invSvc.validateSandboxPath(integrationParent, wtMeta.TreePath)
		require.Error(t, err, "expected error when sandbox is parent of integration")
		assert.Equal(t, errors.ESandboxPathUnsafe, errors.GetCode(err))
	})

	t.Run("sandbox_child_of_integration", func(t *testing.T) {
		integrationChild := filepath.Join(wtMeta.TreePath, "subdir")
		err := invSvc.validateSandboxPath(integrationChild, wtMeta.TreePath)
		require.Error(t, err, "expected error when sandbox is child of integration")
		assert.Equal(t, errors.ESandboxPathUnsafe, errors.GetCode(err))
	})

	t.Run("sandbox_equals_integration_through_symlink", func(t *testing.T) {
		integrationAlias := filepath.Join(dataDir, "integration-alias")
		if err := os.Symlink(wtMeta.TreePath, integrationAlias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		err := invSvc.validateSandboxPath(integrationAlias, wtMeta.TreePath)
		require.Error(t, err, "expected error when sandbox aliases integration path")
		assert.Equal(t, errors.ESandboxPathUnsafe, errors.GetCode(err))
	})

	t.Run("missing_sandbox_child_of_integration_through_symlink", func(t *testing.T) {
		integrationAlias := filepath.Join(dataDir, "integration-child-alias")
		if err := os.Symlink(wtMeta.TreePath, integrationAlias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		sandboxPath := filepath.Join(integrationAlias, "missing", "sandbox")
		err := invSvc.validateSandboxPath(sandboxPath, wtMeta.TreePath)
		require.Error(t, err, "expected error when missing sandbox path resolves inside integration")
		assert.Equal(t, errors.ESandboxPathUnsafe, errors.GetCode(err))
	})

	t.Run("sandbox_parent_of_integration_through_symlink", func(t *testing.T) {
		parentAlias := filepath.Join(dataDir, "integration-parent-alias")
		if err := os.Symlink(filepath.Dir(wtMeta.TreePath), parentAlias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		err := invSvc.validateSandboxPath(parentAlias, wtMeta.TreePath)
		require.Error(t, err, "expected error when sandbox aliases parent of integration")
		assert.Equal(t, errors.ESandboxPathUnsafe, errors.GetCode(err))
	})

	t.Run("valid_sandbox_path", func(t *testing.T) {
		validPath := filepath.Join(dataDir, "sandboxes", "test-inv", "tree")
		err := invSvc.validateSandboxPath(validPath, wtMeta.TreePath)
		assert.NoError(t, err, "expected no error for valid sandbox path")
	})

	t.Run("valid_missing_sandbox_path_through_symlinked_parent", func(t *testing.T) {
		realSandboxRoot := filepath.Join(dataDir, "real-sandbox-root")
		require.NoError(t, os.Mkdir(realSandboxRoot, 0o700), "failed to create real sandbox root")
		sandboxAlias := filepath.Join(dataDir, "sandbox-root-alias")
		if err := os.Symlink(realSandboxRoot, sandboxAlias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		validPath := filepath.Join(sandboxAlias, "missing", "tree")
		err := invSvc.validateSandboxPath(validPath, wtMeta.TreePath)
		assert.NoError(t, err, "expected no error for valid missing sandbox path through symlink")
	})
}

// TestIntegrationMarkerEnforcement verifies that agent start fails
// if targeting a non-integration worktree.
// This is a critical invariant test.
func TestIntegrationMarkerEnforcement(t *testing.T) {
	t.Parallel()
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	require.NoError(t, err, "failed to create data dir")
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	require.NoError(t, fsys.MkdirAll(repoDir, 0o700), "failed to create repo dir")

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Remove the INTEGRATION_MARKER to simulate non-integration directory
	markerPath := filepath.Join(wtMeta.TreePath, ".agency", integrationworktree.IntegrationMarkerFileName)
	require.NoError(t, os.Remove(markerPath), "failed to remove integration marker")

	// Try to create invocation - should fail
	invSvc := NewService(st, cr, fsys, testNow)

	_, err = invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude-code",
		Mode:                    store.RunnerModeHeaded,
		CheckoutRoot:            wtMeta.CheckoutRoot,
		ExecutionProfile:        wtMeta.ExecutionProfile,
	})

	require.Error(t, err, "expected error when INTEGRATION_MARKER is missing")
	assert.Equal(t, errors.EIntegrationMarkerMissing, errors.GetCode(err))
}

// TestSandboxMarkerWritten verifies that SANDBOX_MARKER is written
// after successful sandbox creation.
// This is a critical invariant test.
func TestSandboxMarkerWritten(t *testing.T) {
	t.Parallel()
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	require.NoError(t, err, "failed to create data dir")
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	require.NoError(t, fsys.MkdirAll(repoDir, 0o700), "failed to create repo dir")

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Create invocation
	invSvc := NewService(st, cr, fsys, testNow)

	result, err := invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude-code",
		Mode:                    store.RunnerModeHeaded,
		CheckoutRoot:            wtMeta.CheckoutRoot,
		ExecutionProfile:        wtMeta.ExecutionProfile,
	})
	require.NoError(t, err, "failed to create invocation")

	// Verify SANDBOX_MARKER exists in sandbox tree
	sandboxMarkerPath := filepath.Join(result.SandboxPath, ".agency", SandboxMarkerFileName)
	assert.FileExists(t, sandboxMarkerPath, "SANDBOX_MARKER not found at %s", sandboxMarkerPath)

	// Verify integration tree does NOT contain SANDBOX_MARKER
	integrationSandboxMarker := filepath.Join(wtMeta.TreePath, ".agency", SandboxMarkerFileName)
	assert.NoFileExists(t, integrationSandboxMarker, "SANDBOX_MARKER should NOT exist in integration tree at %s", integrationSandboxMarker)

	// Verify sandbox does NOT contain INTEGRATION_MARKER
	sandboxIntegrationMarker := filepath.Join(result.SandboxPath, ".agency", integrationworktree.IntegrationMarkerFileName)
	assert.NoFileExists(t, sandboxIntegrationMarker, "INTEGRATION_MARKER should NOT exist in sandbox tree at %s", sandboxIntegrationMarker)
}

// TestCleanupOnPartialFailure verifies that partial state is cleaned up
// when sandbox creation fails after worktree add.
// This is a critical invariant test.
func TestCleanupOnPartialFailure(t *testing.T) {
	t.Parallel()
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	require.NoError(t, err, "failed to create data dir")
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()

	// Create repo directory
	realFS := fs.NewRealFS()
	st := store.NewStore(realFS, dataDir, testNow)
	repoDir := st.RepoDir(repoID)
	require.NoError(t, realFS.MkdirAll(repoDir, 0o700), "failed to create repo dir")

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
		Runner:                  "claude-code",
		Mode:                    store.RunnerModeHeaded,
		CheckoutRoot:            wtMeta.CheckoutRoot,
		ExecutionProfile:        wtMeta.ExecutionProfile,
	})
	require.Error(t, err, "expected error when marker write fails")

	// Verify cleanup happened:
	// 1. Check git worktree list - should not contain sandbox worktree
	gitResult, gitErr := cr.Run(ctx, "git", []string{"-C", repoRoot, "worktree", "list"}, exec.RunOpts{})
	require.NoError(t, gitErr, "failed to list worktrees")
	assert.NotContains(t, gitResult.Stdout, "sandboxes", "sandbox worktree should have been cleaned up from git worktree list")

	// 2. Check for sandbox branches - should not exist
	branchResult, _ := cr.Run(ctx, "git", []string{"-C", repoRoot, "branch", "--list", "agency/sandbox-*"}, exec.RunOpts{})
	assert.Empty(t, strings.TrimSpace(branchResult.Stdout), "sandbox branch should have been cleaned up")

	// 3. Check sandbox directory does not exist
	sandboxesDir := filepath.Join(wtMeta.CheckoutRoot, "sandboxes")
	entries, _ := os.ReadDir(sandboxesDir)
	assert.Empty(t, entries, "sandbox directory should have been cleaned up")

	// 4. Check invocation directory does not exist
	invocationsDir := st.InvocationsDir(repoID)
	entries, _ = os.ReadDir(invocationsDir)
	assert.Empty(t, entries, "invocation directory should have been cleaned up")
}

// TestMultipleSandboxesPerWorktree verifies that multiple sandboxes
// can be created for the same integration worktree.
func TestMultipleSandboxesPerWorktree(t *testing.T) {
	t.Parallel()
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	require.NoError(t, err, "failed to create data dir")
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	require.NoError(t, fsys.MkdirAll(repoDir, 0o700), "failed to create repo dir")

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
			Runner:                  "claude-code",
			Mode:                    store.RunnerModeHeaded,
			CheckoutRoot:            wtMeta.CheckoutRoot,
			ExecutionProfile:        wtMeta.ExecutionProfile,
			InvocationName:          "test-agent-" + string(rune('a'+i)),
		})
		require.NoError(t, err, "failed to create invocation %d", i)
		invocations = append(invocations, result)
	}

	// Verify all invocations have unique IDs
	seenIDs := make(map[string]bool)
	for _, inv := range invocations {
		assert.False(t, seenIDs[inv.InvocationID], "duplicate invocation ID: %s", inv.InvocationID)
		seenIDs[inv.InvocationID] = true
	}

	// Verify all sandboxes have unique paths
	seenPaths := make(map[string]bool)
	for _, inv := range invocations {
		assert.False(t, seenPaths[inv.SandboxPath], "duplicate sandbox path: %s", inv.SandboxPath)
		seenPaths[inv.SandboxPath] = true

		// Verify sandbox exists
		assert.DirExists(t, inv.SandboxPath, "sandbox path does not exist: %s", inv.SandboxPath)
	}

	// Verify integration tree remains unchanged (marker still present)
	assert.True(t, integrationworktree.HasIntegrationMarker(wtMeta.TreePath), "integration worktree should still have INTEGRATION_MARKER")
}

// TestIntegrationTreeUntouched verifies that the integration tree
// is never modified by sandbox creation.
func TestIntegrationTreeUntouched(t *testing.T) {
	t.Parallel()
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create data directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	require.NoError(t, err, "failed to create data dir")
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-id-12345678"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	require.NoError(t, fsys.MkdirAll(repoDir, 0o700), "failed to create repo dir")

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
		Runner:                  "claude-code",
		Mode:                    store.RunnerModeHeaded,
		CheckoutRoot:            wtMeta.CheckoutRoot,
		ExecutionProfile:        wtMeta.ExecutionProfile,
	})
	require.NoError(t, err, "failed to create invocation")

	// Verify integration tree is unchanged
	integrationAfter := dirSnapshot(t, wtMeta.TreePath)

	assert.Equal(t, integrationBefore, integrationAfter, "integration tree was modified during sandbox creation")
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
	require.NoError(t, err, "failed to walk directory")
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

// --- Error code coverage tests ---

// TestScanInvocations_CorruptMeta verifies that ScanInvocationsForRepo marks
// an invocation as Broken (E_INVOCATION_BROKEN precondition) when meta.json
// contains invalid JSON.
func TestScanInvocations_CorruptMeta(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), dataDir, nil)
	repoID := "test-repo-corrupt"

	// Create the invocations directory with a fake invocation
	invocationID := "20260131120000-abcd"
	invocationDir := filepath.Join(dataDir, "repos", repoID, "invocations", invocationID)
	require.NoError(t, os.MkdirAll(invocationDir, 0o755))

	// Write corrupt meta.json (invalid JSON)
	metaPath := filepath.Join(invocationDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, []byte("{invalid json!!!"), 0o644))

	records, err := st.ScanInvocationsForRepo(repoID)
	require.NoError(t, err, "ScanInvocationsForRepo should not error on corrupt meta")
	require.Len(t, records, 1, "expected exactly one record")

	assert.True(t, records[0].Broken, "record should be marked as Broken")
	assert.Nil(t, records[0].Meta, "Meta should be nil for broken invocations")
	assert.Equal(t, invocationID, records[0].InvocationID, "InvocationID should still be populated")
}

// TestScanInvocations_MissingMeta verifies that ScanInvocationsForRepo marks
// an invocation as Broken when meta.json is completely absent.
func TestScanInvocations_MissingMeta(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), dataDir, nil)
	repoID := "test-repo-missing"

	// Create the invocation directory without meta.json
	invocationID := "20260131120000-0001"
	invocationDir := filepath.Join(dataDir, "repos", repoID, "invocations", invocationID)
	require.NoError(t, os.MkdirAll(invocationDir, 0o755))

	records, err := st.ScanInvocationsForRepo(repoID)
	require.NoError(t, err)
	require.Len(t, records, 1)

	assert.True(t, records[0].Broken, "record should be marked as Broken when meta.json is missing")
	assert.Nil(t, records[0].Meta)
	assert.Equal(t, invocationID, records[0].InvocationID)
}

// TestEnsureInvocationDir_DirAlreadyExists verifies that EnsureInvocationDir
// returns E_INVOCATION_DIR_EXISTS when the invocation directory already exists.
func TestEnsureInvocationDir_DirAlreadyExists(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	repoID := "test-repo-dup-dir"
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	invocationID := "20260131120000-aaaa"

	// First call should succeed
	dir1, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err, "first EnsureInvocationDir call should succeed")
	require.DirExists(t, dir1)

	// Second call with same ID should fail with E_INVOCATION_DIR_EXISTS
	_, err = st.EnsureInvocationDir(repoID, invocationID)
	require.Error(t, err, "second EnsureInvocationDir call should fail")
	assert.Equal(t, errors.EInvocationDirExists, errors.GetCode(err),
		"expected E_INVOCATION_DIR_EXISTS error code")
}

// TestEnsureInvocationDir_MkdirFails verifies that EnsureInvocationDir
// returns E_INVOCATION_CREATE_FAILED when the parent directory cannot be created.
func TestEnsureInvocationDir_MkdirFails(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	repoID := "test-repo-mkdir-fail"

	// Create a failing filesystem that errors on MkdirAll
	failFS := &failOnMkdirAllFS{
		FS: fs.NewRealFS(),
	}
	st := store.NewStore(failFS, dataDir, testNow)

	_, err := st.EnsureInvocationDir(repoID, "20260131120000-bbbb")
	require.Error(t, err, "EnsureInvocationDir should fail when MkdirAll fails")
	assert.Equal(t, errors.EInvocationCreateFailed, errors.GetCode(err),
		"expected E_INVOCATION_CREATE_FAILED error code")
}

// failOnMkdirAllFS is a filesystem that always fails on MkdirAll.
type failOnMkdirAllFS struct {
	fs.FS
}

func (f *failOnMkdirAllFS) MkdirAll(path string, perm os.FileMode) error {
	return os.ErrPermission
}

// TestCreate_DuplicateName verifies that Create returns E_INVOCATION_NAME_EXISTS
// when creating a second invocation with the same name while the first is still active.
func TestCreate_DuplicateName(t *testing.T) {
	t.Parallel()
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	dataDir, err := os.MkdirTemp("", "agency-data-*")
	require.NoError(t, err, "failed to create data dir")
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-dup-name"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	require.NoError(t, fsys.MkdirAll(repoDir, 0o700))

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	// Use an incrementing clock so each invocation gets a unique ID
	callCount := 0
	incrementingNow := func() time.Time {
		callCount++
		return time.Date(2026, 1, 31, 12, 0, callCount, 0, time.UTC)
	}
	invSvc := NewService(st, cr, fsys, incrementingNow)

	// First invocation with name "my-agent" should succeed
	_, err = invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude-code",
		Mode:                    store.RunnerModeHeaded,
		CheckoutRoot:            wtMeta.CheckoutRoot,
		ExecutionProfile:        wtMeta.ExecutionProfile,
		InvocationName:          "my-agent",
	})
	require.NoError(t, err, "first invocation creation should succeed")

	// Second invocation with same name should fail
	_, err = invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude-code",
		Mode:                    store.RunnerModeHeaded,
		CheckoutRoot:            wtMeta.CheckoutRoot,
		ExecutionProfile:        wtMeta.ExecutionProfile,
		InvocationName:          "my-agent",
	})
	require.Error(t, err, "second invocation with same name should fail")
	assert.Equal(t, errors.EInvocationNameExists, errors.GetCode(err),
		"expected E_INVOCATION_NAME_EXISTS error code")

	// Verify the error message contains useful context
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "error should be an AgencyError")
	assert.Contains(t, ae.Msg, "my-agent", "error message should contain the duplicate name")
	assert.NotEmpty(t, ae.Details["existing_invocation"], "details should include the existing invocation ID")
}

// TestCreate_DuplicateNameAllowedAfterTerminal verifies that a name can be reused
// after the original invocation reaches a terminal state (finished/failed).
func TestCreate_DuplicateNameAllowedAfterTerminal(t *testing.T) {
	t.Parallel()
	repoRoot, _, cleanup := setupTestRepo(t)
	defer cleanup()

	dataDir, err := os.MkdirTemp("", "agency-data-*")
	require.NoError(t, err, "failed to create data dir")
	defer func() { _ = os.RemoveAll(dataDir) }()

	repoID := "test-repo-dup-name-terminal"
	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, testNow)

	// Create repo directory
	repoDir := st.RepoDir(repoID)
	require.NoError(t, fsys.MkdirAll(repoDir, 0o700))

	// Create integration worktree
	wtMeta := setupIntegrationWorktree(t, dataDir, repoRoot, repoID)

	callCount := 0
	incrementingNow := func() time.Time {
		callCount++
		return time.Date(2026, 1, 31, 12, 0, callCount, 0, time.UTC)
	}
	invSvc := NewService(st, cr, fsys, incrementingNow)

	// Create first invocation with name "reusable"
	result, err := invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude-code",
		Mode:                    store.RunnerModeHeaded,
		CheckoutRoot:            wtMeta.CheckoutRoot,
		ExecutionProfile:        wtMeta.ExecutionProfile,
		InvocationName:          "reusable",
	})
	require.NoError(t, err, "first invocation creation should succeed")

	// Mark the first invocation as finished (terminal state)
	err = st.UpdateInvocationMeta(repoID, result.InvocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFinished
	})
	require.NoError(t, err, "failed to update invocation meta to finished")

	// Creating another invocation with the same name should now succeed
	_, err = invSvc.Create(ctx, CreateOpts{
		IntegrationWorktreeID:   wtMeta.WorktreeID,
		IntegrationWorktreeMeta: wtMeta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  "claude-code",
		Mode:                    store.RunnerModeHeaded,
		CheckoutRoot:            wtMeta.CheckoutRoot,
		ExecutionProfile:        wtMeta.ExecutionProfile,
		InvocationName:          "reusable",
	})
	require.NoError(t, err, "name reuse after terminal state should succeed")
}

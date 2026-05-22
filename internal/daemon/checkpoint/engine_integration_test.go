package checkpoint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test infrastructure for integration tests
// ---------------------------------------------------------------------------

// setupRealGitRepo creates a temporary git repo with one initial commit.
func setupRealGitRepo(t *testing.T) string {
	t.Helper()
	testutil.HermeticGitEnv(t)

	repoDir := t.TempDir()
	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err, "git init exec error")
	require.Equal(t, 0, result.ExitCode, "git init failed: stderr: %s", result.Stderr)

	testFile := filepath.Join(repoDir, "README.md")
	require.NoError(t, os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644), "failed to write test file")

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err, "git add exec error")
	require.Equal(t, 0, result.ExitCode, "git add failed")

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "Initial commit"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err, "git commit exec error")
	require.Equal(t, 0, result.ExitCode, "git commit failed: stderr: %s", result.Stderr)

	return repoDir
}

func newRealEngine(t *testing.T, repoDir string) (*Engine, string) {
	t.Helper()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	cfg := DefaultConfig()
	cfg.IncludeUntracked = true

	e := newEngineForTest(
		"test-inv-integration",
		"test-repo",
		repoDir,
		repoDir,
		checkpointsDir,
		eventsPath,
		cfg,
		exec.NewRealRunner(),
		fs.NewRealFS(),
		time.Now,
	)
	return e, checkpointsDir
}

func TestEngine_createCheckpointInternal_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)

	// Modify a tracked file
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Modified\n"), 0o644))

	err := e.createCheckpointInternal(context.Background())
	require.NoError(t, err, "createCheckpointInternal()")

	// Verify checkpoints.json has 1 checkpoint
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 1)
	cp := cpFile.Checkpoints[0]

	// Verify snapshot ref exists
	cr := exec.NewRealRunner()
	result, err := cr.Run(context.Background(), "git", []string{"-C", repoDir, "show-ref", cp.SnapshotRef}, exec.RunOpts{})
	require.NoError(t, err, "show-ref exec error")
	assert.Equal(t, 0, result.ExitCode, "snapshot ref %s not found: stderr: %s", cp.SnapshotRef, result.Stderr)

	// Verify snapshot commit is valid
	result, err = cr.Run(context.Background(), "git", []string{"-C", repoDir, "cat-file", "-t", cp.SnapshotCommit}, exec.RunOpts{})
	require.NoError(t, err, "cat-file exec error")
	assert.Equal(t, 0, result.ExitCode, "cat-file failed for %s", cp.SnapshotCommit)
	assert.Equal(t, "commit", strings.TrimSpace(result.Stdout))

	// Verify diffstat is non-empty
	assert.NotEmpty(t, cp.Diffstat, "expected non-empty diffstat")

	// Verify events.jsonl has checkpoint_created event
	events := readEvents(t, e.eventsPath)
	require.Len(t, events, 1)
	assert.Equal(t, eventKindCheckpointCreated, events[0].Kind)
}

func TestApplier_Apply_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, checkpointsDir := newRealEngine(t, repoDir)
	cr := exec.NewRealRunner()
	ctx := context.Background()

	// State 1: modify file
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# State 1\n"), 0o644))
	require.NoError(t, e.createCheckpointInternal(ctx), "checkpoint 1 failed")

	// State 2: modify file again
	// Need to advance the clock past rate limit
	e.mu.Lock()
	e.lastCheckpoint = time.Time{} // reset rate limit
	e.mu.Unlock()

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# State 2\n"), 0o644))
	require.NoError(t, e.createCheckpointInternal(ctx), "checkpoint 2 failed")

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 2)

	// Apply checkpoint 1
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	applier := newApplierForTest("test-inv-integration", repoDir, checkpointsDir, eventsPath, cr, fs.NewRealFS(), time.Now)

	_, err = applier.ApplyWithOptions(ctx, 1, ApplyOptions{})
	require.NoError(t, err, "Apply(1)")

	// Verify file matches state 1
	data, err := os.ReadFile(filepath.Join(repoDir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "# State 1\n", string(data), "after apply(1)")

	// Apply checkpoint 2
	_, err = applier.ApplyWithOptions(ctx, 2, ApplyOptions{})
	require.NoError(t, err, "Apply(2)")

	// Verify file matches state 2
	data, err = os.ReadFile(filepath.Join(repoDir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "# State 2\n", string(data), "after apply(2)")
}

func TestApplier_Apply_CleansUntrackedFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, checkpointsDir := newRealEngine(t, repoDir)
	ctx := context.Background()

	// Modify something so checkpoint isn't empty
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Changed\n"), 0o644))
	require.NoError(t, e.createCheckpointInternal(ctx), "checkpoint failed")

	// Add untracked file after checkpoint
	untrackedPath := filepath.Join(repoDir, "untracked-new-file.txt")
	require.NoError(t, os.WriteFile(untrackedPath, []byte("untracked content\n"), 0o644))
	// Verify it exists
	require.FileExists(t, untrackedPath, "untracked file should exist before apply")

	// Apply checkpoint
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	applier := newApplierForTest("test-inv-integration", repoDir, checkpointsDir, eventsPath, exec.NewRealRunner(), fs.NewRealFS(), time.Now)

	_, err := applier.ApplyWithOptions(ctx, 1, ApplyOptions{})
	require.NoError(t, err, "Apply()")

	// Verify untracked file was removed
	assert.NoFileExists(t, untrackedPath, "untracked file should have been removed by apply")
}

func TestApplier_Apply_RemovesTrackedFilesAddedAfterCheckpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, checkpointsDir := newRealEngine(t, repoDir)
	ctx := context.Background()
	cr := exec.NewRealRunner()

	// Create checkpoint from a modified tracked file.
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Snapshot State\n"), 0o644))
	require.NoError(t, e.createCheckpointInternal(ctx), "checkpoint failed")

	// Commit a new tracked file after the checkpoint.
	postCheckpointTracked := filepath.Join(repoDir, "post-checkpoint.txt")
	require.NoError(t, os.WriteFile(postCheckpointTracked, []byte("tracked after checkpoint\n"), 0o644))
	addRes, addErr := cr.Run(ctx, "git", []string{"-C", repoDir, "add", "README.md", "post-checkpoint.txt"}, exec.RunOpts{})
	require.NoError(t, addErr)
	require.Equal(t, 0, addRes.ExitCode, "git add failed: %s", addRes.Stderr)
	commitRes, commitErr := cr.Run(ctx, "git", []string{"-C", repoDir, "commit", "-m", "post checkpoint tracked file"}, exec.RunOpts{})
	require.NoError(t, commitErr)
	require.Equal(t, 0, commitRes.ExitCode, "git commit failed: %s", commitRes.Stderr)

	// Apply checkpoint 1.
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	applier := newApplierForTest("test-inv-integration", repoDir, checkpointsDir, eventsPath, cr, fs.NewRealFS(), time.Now)
	_, err := applier.ApplyWithOptions(ctx, 1, ApplyOptions{})
	require.NoError(t, err, "Apply(1)")

	// Exact restore must remove files that were tracked only after the checkpoint.
	assert.NoFileExists(t, postCheckpointTracked, "tracked file introduced after checkpoint should be removed")
}

func TestEngine_isDirty_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)
	ctx := context.Background()
	cr := exec.NewRealRunner()

	// Clean state
	dirty, err := e.isDirty(ctx)
	require.NoError(t, err)
	assert.False(t, dirty, "expected clean workspace")

	// Modify tracked file
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("modified\n"), 0o644))
	dirty, err = e.isDirty(ctx)
	require.NoError(t, err)
	assert.True(t, dirty, "expected dirty workspace after modify")

	// Stage the change
	_, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	dirty, err = e.isDirty(ctx)
	require.NoError(t, err)
	assert.True(t, dirty, "expected dirty workspace after stage")

	// Commit
	_, err = cr.Run(ctx, "git", []string{"commit", "-m", "change"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	dirty, err = e.isDirty(ctx)
	require.NoError(t, err)
	assert.False(t, dirty, "expected clean workspace after commit")

	// Add untracked file (engine config has IncludeUntracked=true)
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "newfile.txt"), []byte("new\n"), 0o644))
	dirty, err = e.isDirty(ctx)
	require.NoError(t, err)
	assert.True(t, dirty, "expected dirty workspace with untracked file")
}

func TestEngine_DenylistIntegration_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)

	// Create .env file (denylisted)
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, ".env"), []byte("SECRET=value\n"), 0o644))

	// Also modify a tracked file so there's something to snapshot
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Modified\n"), 0o644))

	err := e.createCheckpointInternal(context.Background())
	require.NoError(t, err, "createCheckpointInternal()")

	// Verify checkpoint was created with IncludesUntracked=false (degraded)
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 1)
	assert.False(t, cpFile.Checkpoints[0].IncludesUntracked, "expected IncludesUntracked=false due to .env denylist")

	// Verify denylist event was emitted
	events := readEvents(t, e.eventsPath)
	foundDenylist := false
	for _, ev := range events {
		if ev.Kind == eventKindCheckpointDenylistTriggered {
			foundDenylist = true
		}
	}
	assert.True(t, foundDenylist, "expected denylist_triggered event")
}

func TestEngine_MultipleCheckpoints_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		// Reset rate limit
		e.mu.Lock()
		e.lastCheckpoint = time.Time{}
		e.mu.Unlock()

		// Modify file to create unique state
		content := strings.Repeat("x", i*10) + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte(content), 0o644))
		require.NoError(t, e.createCheckpointInternal(ctx), "checkpoint %d failed", i)
	}

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 3)

	// Verify monotonic IDs
	for i, cp := range cpFile.Checkpoints {
		assert.Equal(t, i+1, cp.ID, "checkpoint[%d].ID", i)
	}

	// Verify distinct commits
	commits := make(map[string]bool)
	for _, cp := range cpFile.Checkpoints {
		assert.False(t, commits[cp.SnapshotCommit], "duplicate commit SHA: %s", cp.SnapshotCommit)
		commits[cp.SnapshotCommit] = true
	}

	// Verify all refs exist
	cr := exec.NewRealRunner()
	for _, cp := range cpFile.Checkpoints {
		result, err := cr.Run(ctx, "git", []string{"-C", repoDir, "show-ref", cp.SnapshotRef}, exec.RunOpts{})
		require.NoError(t, err, "show-ref exec error for %s", cp.SnapshotRef)
		assert.Equal(t, 0, result.ExitCode, "ref %s not found", cp.SnapshotRef)
	}
}

func TestEngine_SkipsDuplicate_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)
	ctx := context.Background()

	// Modify a file and create checkpoint
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Changed\n"), 0o644))
	require.NoError(t, e.createCheckpointInternal(ctx), "first checkpoint failed")

	// Reset rate limit
	e.mu.Lock()
	e.lastCheckpoint = time.Time{}
	e.mu.Unlock()

	// Create checkpoint again without any new changes → should be skipped
	require.NoError(t, e.createCheckpointInternal(ctx), "second checkpoint call failed")

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 1, "expected 1 checkpoint (duplicate skipped)")
	assert.NotEmpty(t, cpFile.Checkpoints[0].TreeSHA, "expected TreeSHA to be populated")
}

func TestEngine_FinalCheckpoint_SkipsDuplicateOnShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)
	ctx := context.Background()

	// Modify file and create first checkpoint
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# State A\n"), 0o644))
	require.NoError(t, e.createCheckpointInternal(ctx), "first checkpoint failed")

	// Call doFinalCheckpoint with no new changes → should skip (duplicate tree)
	e.doFinalCheckpoint(ctx)

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 1, "expected 1 checkpoint after no-change final")

	// Now modify file again, call doFinalCheckpoint → should create new checkpoint
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# State B\n"), 0o644))
	e.doFinalCheckpoint(ctx)

	cpFile, err = e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 2, "expected 2 checkpoints after modified final")
}

func TestEngine_CreateSemanticCheckpoint_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)

	// Modify a tracked file
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Semantic Edit\n"), 0o644))

	trigger := &TriggerEvent{
		Kind:     TriggerToolEnd,
		ToolName: "Edit",
		Seq:      7,
	}
	err := e.CreateSemanticCheckpoint(context.Background(), trigger)
	require.NoError(t, err)

	// Verify checkpoint has semantic metadata
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 1)

	cp := cpFile.Checkpoints[0]
	assert.Equal(t, TriggerToolEnd, cp.Trigger)
	assert.Equal(t, "Edit", cp.ToolName)
	assert.Equal(t, uint64(7), cp.StreamSeq)
	assert.NotEmpty(t, cp.Description)
	assert.NotEmpty(t, cp.Diffstat)

	// Verify schema version is 1.1
	assert.Equal(t, "1.1", cpFile.SchemaVersion)

	// Verify the snapshot ref exists in git
	cr := exec.NewRealRunner()
	result, err := cr.Run(context.Background(), "git", []string{"-C", repoDir, "show-ref", cp.SnapshotRef}, exec.RunOpts{})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode, "snapshot ref should exist")
}

func TestEngine_SemanticCheckpoints_NoRateLimit_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)
	ctx := context.Background()

	// Create 3 rapid semantic checkpoints (no rate limit between them)
	for i := 1; i <= 3; i++ {
		content := strings.Repeat("z", i*10) + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte(content), 0o644))

		trigger := &TriggerEvent{
			Kind:     TriggerToolEnd,
			ToolName: "Edit",
			Seq:      uint64(i),
		}
		require.NoError(t, e.CreateSemanticCheckpoint(ctx, trigger), "semantic checkpoint %d", i)
	}

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 3, "all 3 semantic checkpoints should exist (no rate limiting)")

	// All should have semantic metadata
	for i, cp := range cpFile.Checkpoints {
		assert.Equal(t, TriggerToolEnd, cp.Trigger, "checkpoint[%d].Trigger", i)
		assert.Equal(t, "Edit", cp.ToolName, "checkpoint[%d].ToolName", i)
		assert.Equal(t, uint64(i+1), cp.StreamSeq, "checkpoint[%d].StreamSeq", i)
	}
}

func TestEngine_CreateSemanticCheckpoint_PersistsAuthoritativeChangedPaths_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, checkpointsDir := newRealEngine(t, repoDir)
	ctx := context.Background()

	// Checkpoint 1: modify tracked file and add a new file.
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Changed once\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "docs", "note.txt"), []byte("note v1\n"), 0o644))
	require.NoError(t, e.CreateSemanticCheckpoint(ctx, &TriggerEvent{
		Kind:     TriggerToolEnd,
		ToolName: "Edit",
		Seq:      1,
	}))

	// Checkpoint 2: modify only README.md. docs/note.txt should not appear if
	// changed paths are computed against previous checkpoint snapshot.
	e.mu.Lock()
	e.lastCheckpoint = time.Time{}
	e.mu.Unlock()
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Changed twice\n"), 0o644))
	require.NoError(t, e.CreateSemanticCheckpoint(ctx, &TriggerEvent{
		Kind:     TriggerToolEnd,
		ToolName: "Edit",
		Seq:      2,
	}))

	checkpointsPath := filepath.Join(checkpointsDir, "checkpoints.json")
	raw, err := os.ReadFile(checkpointsPath)
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &payload))

	rawCheckpoints, ok := payload["checkpoints"].([]interface{})
	require.True(t, ok, "checkpoints must decode as array")
	require.Len(t, rawCheckpoints, 2)

	first, ok := rawCheckpoints[0].(map[string]interface{})
	require.True(t, ok)
	second, ok := rawCheckpoints[1].(map[string]interface{})
	require.True(t, ok)

	firstPaths, ok := first["changed_paths"].([]interface{})
	require.True(t, ok, "checkpoint 1 must persist changed_paths")
	secondPaths, ok := second["changed_paths"].([]interface{})
	require.True(t, ok, "checkpoint 2 must persist changed_paths")

	var firstPathStrings []string
	for _, p := range firstPaths {
		ps, ok := p.(string)
		require.True(t, ok)
		firstPathStrings = append(firstPathStrings, ps)
	}
	var secondPathStrings []string
	for _, p := range secondPaths {
		ps, ok := p.(string)
		require.True(t, ok)
		secondPathStrings = append(secondPathStrings, ps)
	}

	assert.Contains(t, firstPathStrings, "README.md")
	assert.Contains(t, firstPathStrings, "docs/note.txt")

	assert.Contains(t, secondPathStrings, "README.md")
	assert.NotContains(t, secondPathStrings, "docs/note.txt", "checkpoint 2 paths should reflect delta from checkpoint 1, not cumulative workspace state")
}

func TestEngine_pruneCheckpoints_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)

	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	// Use small max for testing
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true

	e := newEngineForTest(
		"test-inv-prune",
		"test-repo",
		repoDir, repoDir,
		checkpointsDir, eventsPath,
		cfg,
		exec.NewRealRunner(),
		fs.NewRealFS(),
		time.Now,
	)

	ctx := context.Background()
	cr := exec.NewRealRunner()

	// Create 5 checkpoints
	for i := 1; i <= 5; i++ {
		e.mu.Lock()
		e.lastCheckpoint = time.Time{}
		e.mu.Unlock()

		content := strings.Repeat("y", i*5) + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte(content), 0o644))
		require.NoError(t, e.createCheckpointInternal(ctx), "checkpoint %d failed", i)
	}

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 5)

	// Record the first 2 refs
	ref1 := cpFile.Checkpoints[0].SnapshotRef
	ref2 := cpFile.Checkpoints[1].SnapshotRef

	// Prune: keep only 3
	excess := len(cpFile.Checkpoints) - 3
	if excess > 0 {
		for i := 0; i < excess; i++ {
			cp := cpFile.Checkpoints[i]
			_, _ = cr.Run(ctx, "git", []string{"-C", repoDir, "update-ref", "-d", cp.SnapshotRef}, exec.RunOpts{})
		}
		cpFile.Checkpoints = cpFile.Checkpoints[excess:]
	}

	// Verify pruned refs are gone
	result, _ := cr.Run(ctx, "git", []string{"-C", repoDir, "show-ref", ref1}, exec.RunOpts{})
	assert.NotEqual(t, 0, result.ExitCode, "ref %s should have been deleted", ref1)
	result, _ = cr.Run(ctx, "git", []string{"-C", repoDir, "show-ref", ref2}, exec.RunOpts{})
	assert.NotEqual(t, 0, result.ExitCode, "ref %s should have been deleted", ref2)

	// Verify remaining refs still exist
	for _, cp := range cpFile.Checkpoints {
		result, err := cr.Run(ctx, "git", []string{"-C", repoDir, "show-ref", cp.SnapshotRef}, exec.RunOpts{})
		require.NoError(t, err, "show-ref exec error for %s", cp.SnapshotRef)
		assert.Equal(t, 0, result.ExitCode, "ref %s should still exist", cp.SnapshotRef)
	}
}

// TestEngine_setupInitialWatches_SkipsGitIgnoredDirs verifies that the fsnotify
// watch setup skips gitignored directories (node_modules, .venv, build) while
// watching legitimate source directories.
func TestEngine_setupInitialWatches_SkipsGitIgnoredDirs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// NOTE: setupRealGitRepo uses HermeticGitEnv(t) which calls t.Setenv().
	// This is incompatible with t.Parallel(). Do NOT add t.Parallel() here.

	repoDir := setupRealGitRepo(t)
	cr := exec.NewRealRunner()
	ctx := context.Background()

	// Create .gitignore listing dirs to ignore
	gitignoreContent := "node_modules/\n.venv/\nbuild/\n"
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte(gitignoreContent), 0o644))

	// Commit the .gitignore
	result, err := cr.Run(ctx, "git", []string{"-C", repoDir, "add", ".gitignore"}, exec.RunOpts{})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git add .gitignore failed: %s", result.Stderr)
	result, err = cr.Run(ctx, "git", []string{"-C", repoDir, "commit", "-m", "add gitignore"}, exec.RunOpts{})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git commit failed: %s", result.Stderr)

	// Create the gitignored directories with files inside
	for _, dir := range []string{"node_modules/express", ".venv/lib/python3", "build/dist"} {
		fullDir := filepath.Join(repoDir, dir)
		require.NoError(t, os.MkdirAll(fullDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(fullDir, "file.txt"), []byte("content"), 0o644))
	}

	// Create legitimate source directories
	for _, dir := range []string{"src/components", "lib/utils"} {
		fullDir := filepath.Join(repoDir, dir)
		require.NoError(t, os.MkdirAll(fullDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(fullDir, "file.go"), []byte("package main"), 0o644))
	}

	// Compute gitignored dirs using git's full exclude-standard behavior.
	gitIgnoredDirs, err := DiscoverGitIgnoredDirs(ctx, cr, repoDir, nil)
	require.NoError(t, err)
	require.NotEmpty(t, gitIgnoredDirs, "expected gitignored dirs to be computed")

	// Verify git-based parsing got the right dirs
	assert.True(t, gitIgnoredDirs[filepath.Join(repoDir, "node_modules")], "node_modules should be gitignored")
	assert.True(t, gitIgnoredDirs[filepath.Join(repoDir, ".venv")], ".venv should be gitignored")
	assert.True(t, gitIgnoredDirs[filepath.Join(repoDir, "build")], "build should be gitignored")

	// Create engine with the gitignored dirs
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	cfg := DefaultConfig()
	e := NewEngineWithWriter(
		"test-inv-ignored",
		"test-repo",
		repoDir,
		repoDir,
		checkpointsDir,
		eventsPath,
		cfg,
		cr,
		fs.NewRealFS(),
		time.Now,
		nil, // eventWriter
	)
	e.SetGitIgnoredDirs(gitIgnoredDirs)

	// Initialize the fsnotify watcher (normally done in Run())
	watcher, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	t.Cleanup(func() { _ = watcher.Close() })
	e.watcher = watcher

	// Run setupInitialWatches
	require.NoError(t, e.setupInitialWatches())

	// Assert: gitignored dirs should NOT be in watchedDirs
	e.mu.Lock()
	watched := make(map[string]bool)
	for k, v := range e.watchedDirs {
		watched[k] = v
	}
	e.mu.Unlock()

	assert.False(t, watched[filepath.Join(repoDir, "node_modules")], "node_modules should not be watched")
	assert.False(t, watched[filepath.Join(repoDir, "node_modules", "express")], "node_modules/express should not be watched")
	assert.False(t, watched[filepath.Join(repoDir, ".venv")], ".venv should not be watched")
	assert.False(t, watched[filepath.Join(repoDir, ".venv", "lib")], ".venv/lib should not be watched")
	assert.False(t, watched[filepath.Join(repoDir, ".venv", "lib", "python3")], ".venv/lib/python3 should not be watched")
	assert.False(t, watched[filepath.Join(repoDir, "build")], "build should not be watched")
	assert.False(t, watched[filepath.Join(repoDir, "build", "dist")], "build/dist should not be watched")

	// Assert: legitimate dirs SHOULD be in watchedDirs
	assert.True(t, watched[repoDir], "repo root should be watched")
	assert.True(t, watched[filepath.Join(repoDir, "src")], "src should be watched")
	assert.True(t, watched[filepath.Join(repoDir, "src", "components")], "src/components should be watched")
	assert.True(t, watched[filepath.Join(repoDir, "lib")], "lib should be watched")
	assert.True(t, watched[filepath.Join(repoDir, "lib", "utils")], "lib/utils should be watched")
}

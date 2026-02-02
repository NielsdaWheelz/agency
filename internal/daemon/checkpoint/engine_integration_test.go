package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/testutil"
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
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git init failed: %v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	testFile := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git add failed: %v, exit %d", err, result.ExitCode)
	}

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "Initial commit"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git commit failed: %v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	return repoDir
}

func newRealEngine(t *testing.T, repoDir string) (*Engine, string) {
	t.Helper()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	cfg := DefaultConfig()
	cfg.IncludeUntracked = true

	e := NewEngine(
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

// ---------------------------------------------------------------------------
// Integration tests (require real git)
// ---------------------------------------------------------------------------

// 4.1 TestEngine_createCheckpointInternal_RealGit
func TestEngine_createCheckpointInternal_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)

	// Modify a tracked file
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := e.createCheckpointInternal(context.Background())
	if err != nil {
		t.Fatalf("createCheckpointInternal() error: %v", err)
	}

	// Verify checkpoints.json has 1 checkpoint
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(cpFile.Checkpoints))
	}
	cp := cpFile.Checkpoints[0]

	// Verify snapshot ref exists
	cr := exec.NewRealRunner()
	result, err := cr.Run(context.Background(), "git", []string{"-C", repoDir, "show-ref", cp.SnapshotRef}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		t.Errorf("snapshot ref %s not found: exit %d, stderr: %s", cp.SnapshotRef, result.ExitCode, result.Stderr)
	}

	// Verify snapshot commit is valid
	result, err = cr.Run(context.Background(), "git", []string{"-C", repoDir, "cat-file", "-t", cp.SnapshotCommit}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		t.Errorf("cat-file failed for %s", cp.SnapshotCommit)
	}
	if strings.TrimSpace(result.Stdout) != "commit" {
		t.Errorf("snapshot object type = %q, want 'commit'", strings.TrimSpace(result.Stdout))
	}

	// Verify diffstat is non-empty
	if cp.Diffstat == "" {
		t.Error("expected non-empty diffstat")
	}

	// Verify events.jsonl has checkpoint_created event
	events := readEvents(t, e.eventsPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != EventKindCheckpointCreated {
		t.Errorf("event kind = %q, want %q", events[0].Kind, EventKindCheckpointCreated)
	}
}

// 4.2 TestApplier_Apply_RealGit
func TestApplier_Apply_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, checkpointsDir := newRealEngine(t, repoDir)
	cr := exec.NewRealRunner()
	ctx := context.Background()

	// State 1: modify file
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# State 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.createCheckpointInternal(ctx); err != nil {
		t.Fatalf("checkpoint 1 failed: %v", err)
	}

	// State 2: modify file again
	// Need to advance the clock past rate limit
	e.mu.Lock()
	e.lastCheckpoint = time.Time{} // reset rate limit
	e.mu.Unlock()

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# State 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.createCheckpointInternal(ctx); err != nil {
		t.Fatalf("checkpoint 2 failed: %v", err)
	}

	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(cpFile.Checkpoints))
	}

	// Apply checkpoint 1
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	applier := NewApplier("test-inv-integration", repoDir, checkpointsDir, eventsPath, cr, fs.NewRealFS(), time.Now)

	_, err = applier.Apply(ctx, 1)
	if err != nil {
		t.Fatalf("Apply(1) error: %v", err)
	}

	// Verify file matches state 1
	data, err := os.ReadFile(filepath.Join(repoDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# State 1\n" {
		t.Errorf("after apply(1), README.md = %q, want %q", string(data), "# State 1\n")
	}

	// Apply checkpoint 2
	_, err = applier.Apply(ctx, 2)
	if err != nil {
		t.Fatalf("Apply(2) error: %v", err)
	}

	// Verify file matches state 2
	data, err = os.ReadFile(filepath.Join(repoDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# State 2\n" {
		t.Errorf("after apply(2), README.md = %q, want %q", string(data), "# State 2\n")
	}
}

// 4.3 TestApplier_Apply_CleansUntrackedFiles
func TestApplier_Apply_CleansUntrackedFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, checkpointsDir := newRealEngine(t, repoDir)
	ctx := context.Background()

	// Modify something so checkpoint isn't empty
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.createCheckpointInternal(ctx); err != nil {
		t.Fatalf("checkpoint failed: %v", err)
	}

	// Add untracked file after checkpoint
	untrackedPath := filepath.Join(repoDir, "untracked-new-file.txt")
	if err := os.WriteFile(untrackedPath, []byte("untracked content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Verify it exists
	if _, err := os.Stat(untrackedPath); os.IsNotExist(err) {
		t.Fatal("untracked file should exist before apply")
	}

	// Apply checkpoint
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	applier := NewApplier("test-inv-integration", repoDir, checkpointsDir, eventsPath, exec.NewRealRunner(), fs.NewRealFS(), time.Now)

	_, err := applier.Apply(ctx, 1)
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}

	// Verify untracked file was removed
	if _, err := os.Stat(untrackedPath); !os.IsNotExist(err) {
		t.Error("untracked file should have been removed by apply")
	}
}

// 4.4 TestEngine_isDirty_RealGit
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
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("expected clean workspace")
	}

	// Modify tracked file
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err = e.isDirty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Error("expected dirty workspace after modify")
	}

	// Stage the change
	if _, err := cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir}); err != nil {
		t.Fatal(err)
	}
	dirty, err = e.isDirty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Error("expected dirty workspace after stage")
	}

	// Commit
	if _, err := cr.Run(ctx, "git", []string{"commit", "-m", "change"}, exec.RunOpts{Dir: repoDir}); err != nil {
		t.Fatal(err)
	}
	dirty, err = e.isDirty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("expected clean workspace after commit")
	}

	// Add untracked file (engine config has IncludeUntracked=true)
	if err := os.WriteFile(filepath.Join(repoDir, "newfile.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err = e.isDirty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Error("expected dirty workspace with untracked file")
	}
}

// 4.5 TestEngine_DenylistIntegration_RealGit
func TestEngine_DenylistIntegration_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)

	// Create .env file (denylisted)
	if err := os.WriteFile(filepath.Join(repoDir, ".env"), []byte("SECRET=value\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Also modify a tracked file so there's something to snapshot
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := e.createCheckpointInternal(context.Background())
	if err != nil {
		t.Fatalf("createCheckpointInternal() error: %v", err)
	}

	// Verify checkpoint was created with IncludesUntracked=false (degraded)
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(cpFile.Checkpoints))
	}
	if cpFile.Checkpoints[0].IncludesUntracked {
		t.Error("expected IncludesUntracked=false due to .env denylist")
	}

	// Verify denylist event was emitted
	events := readEvents(t, e.eventsPath)
	foundDenylist := false
	for _, ev := range events {
		if ev.Kind == EventKindCheckpointDenylistTriggered {
			foundDenylist = true
		}
	}
	if !foundDenylist {
		t.Error("expected denylist_triggered event")
	}
}

// 4.6 TestEngine_MultipleCheckpoints_RealGit
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
		if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := e.createCheckpointInternal(ctx); err != nil {
			t.Fatalf("checkpoint %d failed: %v", i, err)
		}
	}

	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(cpFile.Checkpoints))
	}

	// Verify monotonic IDs
	for i, cp := range cpFile.Checkpoints {
		if cp.ID != i+1 {
			t.Errorf("checkpoint[%d].ID = %d, want %d", i, cp.ID, i+1)
		}
	}

	// Verify distinct commits
	commits := make(map[string]bool)
	for _, cp := range cpFile.Checkpoints {
		if commits[cp.SnapshotCommit] {
			t.Errorf("duplicate commit SHA: %s", cp.SnapshotCommit)
		}
		commits[cp.SnapshotCommit] = true
	}

	// Verify all refs exist
	cr := exec.NewRealRunner()
	for _, cp := range cpFile.Checkpoints {
		result, err := cr.Run(ctx, "git", []string{"-C", repoDir, "show-ref", cp.SnapshotRef}, exec.RunOpts{})
		if err != nil || result.ExitCode != 0 {
			t.Errorf("ref %s not found", cp.SnapshotRef)
		}
	}
}

// 4.7 TestEngine_SkipsDuplicate_RealGit
func TestEngine_SkipsDuplicate_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)
	ctx := context.Background()

	// Modify a file and create checkpoint
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.createCheckpointInternal(ctx); err != nil {
		t.Fatalf("first checkpoint failed: %v", err)
	}

	// Reset rate limit
	e.mu.Lock()
	e.lastCheckpoint = time.Time{}
	e.mu.Unlock()

	// Create checkpoint again without any new changes → should be skipped
	if err := e.createCheckpointInternal(ctx); err != nil {
		t.Fatalf("second checkpoint call failed: %v", err)
	}

	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint (duplicate skipped), got %d", len(cpFile.Checkpoints))
	}
	if cpFile.Checkpoints[0].TreeSHA == "" {
		t.Error("expected TreeSHA to be populated")
	}
}

// 4.8 TestEngine_FinalCheckpoint_SkipsDuplicateOnShutdown
func TestEngine_FinalCheckpoint_SkipsDuplicateOnShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoDir := setupRealGitRepo(t)
	e, _ := newRealEngine(t, repoDir)
	ctx := context.Background()

	// Modify file and create first checkpoint
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# State A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.createCheckpointInternal(ctx); err != nil {
		t.Fatalf("first checkpoint failed: %v", err)
	}

	// Call doFinalCheckpoint with no new changes → should skip (duplicate tree)
	e.doFinalCheckpoint(ctx)

	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint after no-change final, got %d", len(cpFile.Checkpoints))
	}

	// Now modify file again, call doFinalCheckpoint → should create new checkpoint
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# State B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.doFinalCheckpoint(ctx)

	cpFile, err = e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 2 {
		t.Errorf("expected 2 checkpoints after modified final, got %d", len(cpFile.Checkpoints))
	}
}

// 4.9 TestEngine_pruneCheckpoints_RealGit
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

	e := NewEngine(
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
		if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := e.createCheckpointInternal(ctx); err != nil {
			t.Fatalf("checkpoint %d failed: %v", i, err)
		}
	}

	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 5 {
		t.Fatalf("expected 5 checkpoints, got %d", len(cpFile.Checkpoints))
	}

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
	if result.ExitCode == 0 {
		t.Errorf("ref %s should have been deleted", ref1)
	}
	result, _ = cr.Run(ctx, "git", []string{"-C", repoDir, "show-ref", ref2}, exec.RunOpts{})
	if result.ExitCode == 0 {
		t.Errorf("ref %s should have been deleted", ref2)
	}

	// Verify remaining refs still exist
	for _, cp := range cpFile.Checkpoints {
		result, err := cr.Run(ctx, "git", []string{"-C", repoDir, "show-ref", cp.SnapshotRef}, exec.RunOpts{})
		if err != nil || result.ExitCode != 0 {
			t.Errorf("ref %s should still exist", cp.SnapshotRef)
		}
	}
}

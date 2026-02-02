package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

type checkpointTestEnv struct {
	DataDir      string
	RepoPath     string
	RepoID       string
	InvocationID string
	SandboxDir   string
	Runner       exec.CommandRunner
	FS           fs.FS
}

// setupCheckpointTestEnv creates a minimal environment for checkpoint command tests.
// Sets up a git repo, writes invocation meta (headless, finished), and optionally writes checkpoints.
func setupCheckpointTestEnv(t *testing.T, mode store.RunnerMode, status store.InvocationStatus, checkpoints []checkpoint.Checkpoint) *checkpointTestEnv {
	t.Helper()
	testutil.HermeticGitEnv(t)

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	// Create git repo
	repoDir := t.TempDir()
	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git init failed: %v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git add failed: %v", err)
	}
	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "init"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git commit failed: %v, stderr: %s", err, result.Stderr)
	}

	// Set up data dir via env
	dataDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	// Derive repo ID using the same logic the commands will use.
	// The commands call git rev-parse --show-toplevel which resolves symlinks
	// (e.g., /var -> /private/var on macOS), so we must do the same.
	st := store.NewStore(fsys, dataDir, time.Now)
	gitRoot, err := cr.Run(ctx, "git", []string{"rev-parse", "--show-toplevel"}, exec.RunOpts{Dir: repoDir})
	if err != nil || gitRoot.ExitCode != 0 {
		t.Fatalf("git rev-parse --show-toplevel failed: %v", err)
	}
	resolvedRepoDir := strings.TrimSpace(gitRoot.Stdout)
	repoIdentity := identity.DeriveRepoIdentity(resolvedRepoDir, "") // no origin URL
	repoID := repoIdentity.RepoID

	// Create invocation and meta
	invocationID := "20260115120000-abcd"

	invDir := filepath.Join(dataDir, "repos", repoID, "invocations", invocationID)
	if err := os.MkdirAll(invDir, 0o700); err != nil {
		t.Fatal(err)
	}

	meta := store.NewInvocationMeta(
		invocationID,
		"",
		"wt-001",
		repoDir,
		"agency/sandbox-"+invocationID,
		"basecommit",
		"claude",
		mode,
		time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	)
	meta.Status = status
	if status == store.InvocationStatusFinished || status == store.InvocationStatusFailed {
		meta.FinishedAt = "2026-01-15T12:30:00Z"
	}

	if err := st.WriteInvocationMeta(repoID, invocationID, meta); err != nil {
		t.Fatal(err)
	}

	// Create sandbox dir and write checkpoints.json if checkpoints provided
	sandboxDir := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID)
	if err := os.MkdirAll(sandboxDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if checkpoints != nil {
		cpFile := &checkpoint.CheckpointsFile{
			SchemaVersion: checkpoint.SchemaVersion,
			Checkpoints:   checkpoints,
		}
		cpData, _ := json.MarshalIndent(cpFile, "", "  ")
		if err := os.WriteFile(filepath.Join(sandboxDir, "checkpoints.json"), cpData, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return &checkpointTestEnv{
		DataDir:      dataDir,
		RepoPath:     repoDir,
		RepoID:       repoID,
		InvocationID: invocationID,
		SandboxDir:   sandboxDir,
		Runner:       cr,
		FS:           fsys,
	}
}

// ---------------------------------------------------------------------------
// CheckpointLS tests
// ---------------------------------------------------------------------------

// 3.1 TestCheckpointLS_TableOutput
func TestCheckpointLS_TableOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	checkpoints := []checkpoint.Checkpoint{
		{
			ID:                1,
			SnapshotRef:       "refs/agency/snapshots/inv/1",
			SnapshotCommit:    "aaa111bbb222",
			SandboxHeadSHA:    "deadbeef12345678",
			CreatedAt:         "2026-01-15T12:00:00Z",
			IncludesUntracked: true,
			Diffstat:          "+10 -5 in 3 files",
		},
		{
			ID:                2,
			SnapshotRef:       "refs/agency/snapshots/inv/2",
			SnapshotCommit:    "ccc333ddd444",
			SandboxHeadSHA:    "cafebabe12345678",
			CreatedAt:         "2026-01-15T12:05:00Z",
			IncludesUntracked: false,
			Diffstat:          "+2 -1 in 1 files",
		},
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, checkpoints)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef: env.InvocationID,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("CheckpointLS() error: %v", err)
	}

	out := stdout.String()

	// Verify header row
	if !strings.Contains(out, "ID") {
		t.Error("expected header row with ID")
	}
	if !strings.Contains(out, "Created") {
		t.Error("expected header row with Created")
	}

	// Verify data rows
	if !strings.Contains(out, "+10 -5 in 3 files") {
		t.Error("expected diffstat for checkpoint 1")
	}
	if !strings.Contains(out, "+2 -1 in 1 files") {
		t.Error("expected diffstat for checkpoint 2")
	}

	// Verify truncated head SHAs
	if !strings.Contains(out, "deadbeef") {
		t.Error("expected truncated head SHA deadbeef")
	}
}

// 3.2 TestCheckpointLS_JSONOutput
func TestCheckpointLS_JSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	checkpoints := []checkpoint.Checkpoint{
		{ID: 1, SnapshotCommit: "aaa", CreatedAt: "2026-01-15T12:00:00Z"},
		{ID: 2, SnapshotCommit: "bbb", CreatedAt: "2026-01-15T12:05:00Z"},
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, checkpoints)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef: env.InvocationID,
		JSON:          true,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("CheckpointLS() error: %v", err)
	}

	// Verify valid JSON
	var cpFile checkpoint.CheckpointsFile
	if err := json.NewDecoder(&stdout).Decode(&cpFile); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(cpFile.Checkpoints) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(cpFile.Checkpoints))
	}
}

// 3.3 TestCheckpointLS_NoCheckpoints
func TestCheckpointLS_NoCheckpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, []checkpoint.Checkpoint{})

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef: env.InvocationID,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("CheckpointLS() error: %v", err)
	}

	if !strings.Contains(stdout.String(), "No checkpoints found") {
		t.Errorf("expected 'No checkpoints found', got: %s", stdout.String())
	}
}

// 3.4 TestCheckpointLS_InvocationNotFound
func TestCheckpointLS_InvocationNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, nil)

	var stdout, stderr bytes.Buffer
	// Use a non-existent invocation ref
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef: "nonexistent-invocation",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	code := errors.GetCode(err)
	if code != errors.EInvocationNotFound {
		t.Errorf("error code = %q, want %q", code, errors.EInvocationNotFound)
	}
}

// 3.5 TestCheckpointLS_WrongMode
func TestCheckpointLS_WrongMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeaded, store.InvocationStatusFinished, nil)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef: env.InvocationID,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	code := errors.GetCode(err)
	if code != errors.EInvocationInvalidMode {
		t.Errorf("error code = %q, want %q", code, errors.EInvocationInvalidMode)
	}
}

// ---------------------------------------------------------------------------
// CheckpointApply tests
// ---------------------------------------------------------------------------

// 3.6 TestCheckpointApply_InvalidID
func TestCheckpointApply_InvalidID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "abc", id: "abc"},
		{name: "zero", id: "0"},
		{name: "negative", id: "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			var stdout, stderr bytes.Buffer

			err := CheckpointApply(context.Background(), cr, fsys, "/tmp", CheckpointApplyOpts{
				InvocationRef: "some-inv",
				CheckpointID:  tt.id,
			}, &stdout, &stderr)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			code := errors.GetCode(err)
			if code != errors.EUsage {
				t.Errorf("error code = %q, want %q", code, errors.EUsage)
			}
		})
	}
}

// 3.7 TestCheckpointApply_InvocationNotFound
func TestCheckpointApply_InvocationNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, nil)

	var stdout, stderr bytes.Buffer
	err := CheckpointApply(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointApplyOpts{
		InvocationRef: "nonexistent-invocation",
		CheckpointID:  "1",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	code := errors.GetCode(err)
	if code != errors.EInvocationNotFound {
		t.Errorf("error code = %q, want %q", code, errors.EInvocationNotFound)
	}
}

// 3.8 TestCheckpointApply_WrongMode
func TestCheckpointApply_WrongMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeaded, store.InvocationStatusFinished, nil)

	var stdout, stderr bytes.Buffer
	err := CheckpointApply(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointApplyOpts{
		InvocationRef: env.InvocationID,
		CheckpointID:  "1",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	code := errors.GetCode(err)
	if code != errors.EInvocationInvalidMode {
		t.Errorf("error code = %q, want %q", code, errors.EInvocationInvalidMode)
	}
}

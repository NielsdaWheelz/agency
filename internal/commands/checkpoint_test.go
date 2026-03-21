package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	SocketPath   string // test daemon socket path (set by startTestDaemonForCheckpoint)
}

// startTestDaemonForCheckpoint starts a daemon server backed by the test env's data dir.
// Returns the Unix socket path. The daemon is shut down when the test finishes.
func startTestDaemonForCheckpoint(t *testing.T, env *checkpointTestEnv) string {
	t.Helper()

	st := store.NewStore(env.FS, env.DataDir, time.Now)
	configDir := filepath.Join(env.DataDir, "config")
	srv := daemon.NewServer(st, env.Runner, env.FS, configDir)

	// Use a short temp dir for the socket to stay under macOS ~104-byte limit.
	sockDir, err := os.MkdirTemp("", "dsock")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })

	socketPath := filepath.Join(sockDir, "d.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	go func() { _ = srv.Serve(listener) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	// Wait for daemon to be healthy.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.Dial("unix", socketPath)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("test daemon did not become ready")
		case <-time.After(50 * time.Millisecond):
		}
	}

	env.SocketPath = socketPath
	return socketPath
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
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git init failed: exit %d, stderr: %s", result.ExitCode, result.Stderr)
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\n"), 0o644))
	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git add failed")
	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "init"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git commit failed: stderr: %s", result.Stderr)

	dataDir := t.TempDir()

	// Derive repo ID using the same logic the commands will use.
	// The commands call git rev-parse --show-toplevel which resolves symlinks
	// (e.g., /var -> /private/var on macOS), so we must do the same.
	st := store.NewStore(fsys, dataDir, time.Now)
	gitRoot, err := cr.Run(ctx, "git", []string{"rev-parse", "--show-toplevel"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, gitRoot.ExitCode, "git rev-parse --show-toplevel failed")
	resolvedRepoDir := strings.TrimSpace(gitRoot.Stdout)
	repoIdentity := identity.DeriveRepoIdentity(resolvedRepoDir, "") // no origin URL
	repoID := repoIdentity.RepoID

	// Create invocation and meta
	invocationID := "20260115120000-abcd"

	invDir := filepath.Join(dataDir, "repos", repoID, "invocations", invocationID)
	require.NoError(t, os.MkdirAll(invDir, 0o700))

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

	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta))

	// Create sandbox dir and write checkpoints.json if checkpoints provided
	sandboxDir := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID)
	require.NoError(t, os.MkdirAll(sandboxDir, 0o700))

	if checkpoints != nil {
		cpFile := &checkpoint.CheckpointsFile{
			SchemaVersion: checkpoint.SchemaVersion,
			Checkpoints:   checkpoints,
		}
		cpData, _ := json.MarshalIndent(cpFile, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(sandboxDir, "checkpoints.json"), cpData, 0o644))
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

	socketPath := startTestDaemonForCheckpoint(t, env)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:        env.InvocationID,
		DataDirOverride:      env.DataDir,
		DaemonSocketOverride: socketPath,
	}, &stdout, &stderr)
	require.NoError(t, err, "CheckpointLS() error")

	out := stdout.String()

	// Human output should use shared activity language, not a bespoke table.
	assert.Contains(t, out, "[checkpoint]", "expected shared activity label in checkpoint ls output")
	assert.Contains(t, out, "cp:1")
	assert.Contains(t, out, "cp:2")
	assert.NotContains(t, out, "ID", "checkpoint ls output should no longer render a custom table header")

	// Verify data rows
	assert.Contains(t, out, "+10 -5 in 3 files", "expected diffstat for checkpoint 1")
	assert.Contains(t, out, "+2 -1 in 1 files", "expected diffstat for checkpoint 2")

	// Verify truncated snapshot commit SHAs (PR-12: uses SnapshotCommit, not SandboxHeadSHA)
	assert.Contains(t, out, "aaa111bb", "expected truncated snapshot commit aaa111bb")
}

func TestCheckpointLS_TableOutput_UsesSharedChangedPathPreview(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	checkpoints := []checkpoint.Checkpoint{
		{
			ID:                   1,
			SnapshotRef:          "refs/agency/snapshots/inv/1",
			SnapshotCommit:       "aaa111bbb222",
			SandboxHeadSHA:       "deadbeef12345678",
			CreatedAt:            "2026-01-15T12:00:00Z",
			IncludesUntracked:    true,
			Diffstat:             "+10 -5 in 3 files",
			ChangedPaths:         []string{"pkg/math/add.go", "pkg/math/subtract.go"},
			ChangedPathCount:     3,
			ChangedPathTruncated: true,
		},
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, checkpoints)
	socketPath := startTestDaemonForCheckpoint(t, env)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:        env.InvocationID,
		DataDirOverride:      env.DataDir,
		DaemonSocketOverride: socketPath,
	}, &stdout, &stderr)
	require.NoError(t, err, "CheckpointLS() error")

	out := stdout.String()
	assert.Contains(t, out, "paths:pkg/math/add.go, pkg/math/subtract.go, ... (+1 more)", "checkpoint ls changed-path preview must use shared renderer style")
	assert.NotContains(t, out, "paths:pkg/math/add.go, pkg/math/subtract.go (+1 more)", "legacy bespoke preview format must not be used")
}

func TestCheckpointLS_RepoFlagResolvesOutsideGitContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	checkpoints := []checkpoint.Checkpoint{
		{
			ID:             1,
			SnapshotCommit: "aaa111bbb222",
			CreatedAt:      "2026-01-15T12:00:00Z",
			Diffstat:       "+1 -0 in 1 files",
		},
	}
	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, checkpoints)
	socketPath := startTestDaemonForCheckpoint(t, env)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), testutil.NewFakeCommandRunner(), env.FS, t.TempDir(), CheckpointLSOpts{
		InvocationRef:        env.InvocationID,
		RepoFlag:             env.RepoID,
		DataDirOverride:      env.DataDir,
		DaemonSocketOverride: socketPath,
	}, &stdout, &stderr)
	require.NoError(t, err, "checkpoint ls with --repo should not depend on local git context")
	assert.Contains(t, stdout.String(), "cp:1")
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

	socketPath := startTestDaemonForCheckpoint(t, env)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:        env.InvocationID,
		JSON:                 true,
		DataDirOverride:      env.DataDir,
		DaemonSocketOverride: socketPath,
	}, &stdout, &stderr)
	require.NoError(t, err, "CheckpointLS() error")

	// Verify valid JSON — daemon returns ListCheckpointsData, not CheckpointsFile
	var result []map[string]interface{}
	require.NoError(t, json.NewDecoder(&stdout).Decode(&result), "output is not valid JSON")
	assert.Len(t, result, 2, "expected 2 checkpoints")
}

// 3.3 TestCheckpointLS_NoCheckpoints
func TestCheckpointLS_NoCheckpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, []checkpoint.Checkpoint{})
	socketPath := startTestDaemonForCheckpoint(t, env)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:        env.InvocationID,
		DataDirOverride:      env.DataDir,
		DaemonSocketOverride: socketPath,
	}, &stdout, &stderr)
	require.NoError(t, err, "CheckpointLS() error")

	assert.Contains(t, stdout.String(), "No checkpoints found")
}

// 3.4 TestCheckpointLS_InvocationNotFound
func TestCheckpointLS_InvocationNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, nil)

	socketPath := startTestDaemonForCheckpoint(t, env)

	var stdout, stderr bytes.Buffer
	// Use a non-existent invocation ref
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:        "nonexistent-invocation",
		DataDirOverride:      env.DataDir,
		DaemonSocketOverride: socketPath,
	}, &stdout, &stderr)
	require.Error(t, err, "expected error, got nil")

	assert.Equal(t, errors.EInvocationNotFound, errors.GetCode(err))
}

// 3.5 TestCheckpointLS_WrongMode
// PR-12: The daemon read API does not enforce mode restrictions on
// checkpoint listing (only on apply). So headed invocations return
// an empty list rather than an error.
func TestCheckpointLS_WrongMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeaded, store.InvocationStatusFinished, nil)
	socketPath := startTestDaemonForCheckpoint(t, env)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:        env.InvocationID,
		DataDirOverride:      env.DataDir,
		DaemonSocketOverride: socketPath,
	}, &stdout, &stderr)
	require.NoError(t, err, "CheckpointLS() error")

	assert.Contains(t, stdout.String(), "No checkpoints found")
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
			require.Error(t, err, "expected error, got nil")

			assert.Equal(t, errors.EUsage, errors.GetCode(err))
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
		InvocationRef:   "nonexistent-invocation",
		CheckpointID:    "1",
		DataDirOverride: env.DataDir,
	}, &stdout, &stderr)
	require.Error(t, err, "expected error, got nil")

	assert.Equal(t, errors.EInvocationNotFound, errors.GetCode(err))
}

// 3.8 TestCheckpointApply_WrongMode
func TestCheckpointApply_WrongMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeaded, store.InvocationStatusFinished, nil)

	var stdout, stderr bytes.Buffer
	err := CheckpointApply(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointApplyOpts{
		InvocationRef:   env.InvocationID,
		CheckpointID:    "1",
		DataDirOverride: env.DataDir,
	}, &stdout, &stderr)
	require.Error(t, err, "expected error, got nil")

	assert.Equal(t, errors.EInvocationInvalidMode, errors.GetCode(err))
}

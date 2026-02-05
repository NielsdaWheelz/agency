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

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:   env.InvocationID,
		DataDirOverride: env.DataDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "CheckpointLS() error")

	out := stdout.String()

	// Verify header row
	assert.Contains(t, out, "ID", "expected header row with ID")
	assert.Contains(t, out, "Created", "expected header row with Created")

	// Verify data rows
	assert.Contains(t, out, "+10 -5 in 3 files", "expected diffstat for checkpoint 1")
	assert.Contains(t, out, "+2 -1 in 1 files", "expected diffstat for checkpoint 2")

	// Verify truncated head SHAs
	assert.Contains(t, out, "deadbeef", "expected truncated head SHA deadbeef")
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
		InvocationRef:   env.InvocationID,
		JSON:            true,
		DataDirOverride: env.DataDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "CheckpointLS() error")

	// Verify valid JSON
	var cpFile checkpoint.CheckpointsFile
	require.NoError(t, json.NewDecoder(&stdout).Decode(&cpFile), "output is not valid JSON")
	assert.Equal(t, 2, len(cpFile.Checkpoints), "expected 2 checkpoints")
}

// 3.3 TestCheckpointLS_NoCheckpoints
func TestCheckpointLS_NoCheckpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeadless, store.InvocationStatusFinished, []checkpoint.Checkpoint{})

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:   env.InvocationID,
		DataDirOverride: env.DataDir,
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

	var stdout, stderr bytes.Buffer
	// Use a non-existent invocation ref
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:   "nonexistent-invocation",
		DataDirOverride: env.DataDir,
	}, &stdout, &stderr)
	require.Error(t, err, "expected error, got nil")

	assert.Equal(t, errors.EInvocationNotFound, errors.GetCode(err))
}

// 3.5 TestCheckpointLS_WrongMode
func TestCheckpointLS_WrongMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupCheckpointTestEnv(t, store.RunnerModeHeaded, store.InvocationStatusFinished, nil)

	var stdout, stderr bytes.Buffer
	err := CheckpointLS(context.Background(), env.Runner, env.FS, env.RepoPath, CheckpointLSOpts{
		InvocationRef:   env.InvocationID,
		DataDirOverride: env.DataDir,
	}, &stdout, &stderr)
	require.Error(t, err, "expected error, got nil")

	assert.Equal(t, errors.EInvocationInvalidMode, errors.GetCode(err))
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

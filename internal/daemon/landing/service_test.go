package landing_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/daemon/landing"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// setupTestService creates a landing.Service backed by a real store in a temp dir.
// It also writes the minimum invocation + worktree meta needed for Land() precondition checks.
func setupTestService(t *testing.T, status store.InvocationStatus, landingStatus store.LandingStatus) *landing.Service {
	t.Helper()

	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	st := store.NewStore(realFS, dataDir, time.Now)

	repoID := "test-repo"
	invocationID := "test-inv"
	worktreeID := "test-wt"

	// Create integration worktree meta (so Land() can read it at line 95).
	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)
	wtMeta := store.NewIntegrationWorktreeMeta(worktreeID, "test", repoID, "agency/integration-test", "main", "/nonexistent/tree", time.Now())
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, worktreeID, wtMeta))

	// Create invocation meta with the desired status.
	_, err = st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	meta := store.NewInvocationMeta(invocationID, "", worktreeID, "/nonexistent/sandbox", "agency/sandbox-test", "abc123", "claude", store.RunnerModeHeadless, time.Now())
	meta.Status = status
	meta.LandingStatus = landingStatus
	if status == store.InvocationStatusFinished || status == store.InvocationStatusFailed {
		meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta))

	return landing.NewService(st, exec.NewRealRunner(), realFS, time.Now)
}

// testHarness holds all the pieces needed for landing error‐code tests.
// The sandbox and integration paths are real directories so os.Stat succeeds.
type testHarness struct {
	svc             *landing.Service
	store           *store.Store
	runner          *testutil.FakeCommandRunner
	sandboxPath     string
	integrationPath string
}

type failingEventAppender struct {
	err error
}

func (f failingEventAppender) Append(string, string, string, map[string]any, invocationevents.AppendOptions) (invocationevents.AppendResult, error) {
	return invocationevents.AppendResult{}, f.err
}

// setupHarness creates a landing.Service backed by a real store and a fake
// command runner. The invocation meta is written with status=finished and
// landing_status=pending so that precondition checks pass. Real directories
// are created for both sandbox and integration tree paths.
func setupHarness(t *testing.T) *testHarness {
	t.Helper()

	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	st := store.NewStore(realFS, dataDir, time.Now)

	repoID := "test-repo"
	invocationID := "test-inv"
	worktreeID := "test-wt"

	// Create real directories so os.Stat succeeds in Land().
	sandboxPath := filepath.Join(t.TempDir(), "sandbox-tree")
	integrationPath := filepath.Join(t.TempDir(), "integration-tree")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o755))
	require.NoError(t, os.MkdirAll(integrationPath, 0o755))

	// Integration worktree meta.
	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)
	wtMeta := store.NewIntegrationWorktreeMeta(
		worktreeID, "test", repoID, "agency/integration-test", "main",
		integrationPath, time.Now(),
	)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, worktreeID, wtMeta))

	// Invocation meta — finished + pending so all preconditions pass.
	_, err = st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	meta := store.NewInvocationMeta(
		invocationID, "", worktreeID,
		sandboxPath, "agency/sandbox-test", "abc123",
		"claude", store.RunnerModeHeadless, time.Now(),
	)
	meta.Status = store.InvocationStatusFinished
	meta.LandingStatus = store.LandingStatusPending
	meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta))

	runner := testutil.NewFakeCommandRunner()
	svc := landing.NewService(st, runner, realFS, time.Now)

	return &testHarness{
		svc:             svc,
		store:           st,
		runner:          runner,
		sandboxPath:     sandboxPath,
		integrationPath: integrationPath,
	}
}

func TestLand_Preconditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        store.InvocationStatus
		landingStatus store.LandingStatus
		wantCode      errors.Code
	}{
		{
			name:          "still running",
			status:        store.InvocationStatusRunning,
			landingStatus: "",
			wantCode:      errors.EInvocationStillRunning,
		},
		{
			name:          "still starting",
			status:        store.InvocationStatusStarting,
			landingStatus: "",
			wantCode:      errors.EInvocationStillRunning,
		},
		{
			name:          "still stopping",
			status:        store.InvocationStatusStopping,
			landingStatus: "",
			wantCode:      errors.EInvocationStillRunning,
		},
		{
			name:          "already landed",
			status:        store.InvocationStatusFinished,
			landingStatus: store.LandingStatusLanded,
			wantCode:      errors.ELandAlreadyLanded,
		},
		{
			name:          "already discarded",
			status:        store.InvocationStatusFinished,
			landingStatus: store.LandingStatusDiscarded,
			wantCode:      errors.ELandAlreadyDiscarded,
		},
		{
			name:          "finished passes preconditions",
			status:        store.InvocationStatusFinished,
			landingStatus: store.LandingStatusPending,
			wantCode:      errors.ESandboxMissing, // passes preconditions, fails at path check
		},
		{
			name:          "failed passes preconditions",
			status:        store.InvocationStatusFailed,
			landingStatus: "",
			wantCode:      errors.ESandboxMissing, // passes preconditions, fails at path check
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := setupTestService(t, tt.status, tt.landingStatus)
			_, err := svc.Land(context.Background(), landing.LandOpts{
				RepoID:       "test-repo",
				InvocationID: "test-inv",
				RepoRoot:     "/nonexistent",
			})

			require.Error(t, err)
			assert.Equal(t, tt.wantCode, errors.GetCode(err))
		})
	}
}

// ---------------------------------------------------------------------------
// Error‐code coverage tests: EIntegrationTreeMissing
// ---------------------------------------------------------------------------

func TestLand_IntegrationTreeMissing(t *testing.T) {
	t.Parallel()

	// Set up a harness with real paths, then delete the integration dir.
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	st := store.NewStore(realFS, dataDir, time.Now)

	repoID := "test-repo"
	invocationID := "test-inv"
	worktreeID := "test-wt"

	sandboxPath := filepath.Join(t.TempDir(), "sandbox-tree")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o755))

	// Point integration tree at a path that does NOT exist.
	missingIntegrationPath := filepath.Join(t.TempDir(), "gone")

	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)
	wtMeta := store.NewIntegrationWorktreeMeta(
		worktreeID, "test", repoID, "agency/integration-test", "main",
		missingIntegrationPath, time.Now(),
	)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, worktreeID, wtMeta))

	_, err = st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	meta := store.NewInvocationMeta(
		invocationID, "", worktreeID,
		sandboxPath, "agency/sandbox-test", "abc123",
		"claude", store.RunnerModeHeadless, time.Now(),
	)
	meta.Status = store.InvocationStatusFinished
	meta.LandingStatus = store.LandingStatusPending
	meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta))

	runner := testutil.NewFakeCommandRunner()
	svc := landing.NewService(st, runner, realFS, time.Now)

	_, err = svc.Land(context.Background(), landing.LandOpts{
		RepoID:       repoID,
		InvocationID: invocationID,
		RepoRoot:     t.TempDir(),
	})

	require.Error(t, err)
	assert.Equal(t, errors.EIntegrationTreeMissing, errors.GetCode(err))
	assert.Contains(t, err.Error(), "integration worktree tree no longer exists")
}

// ---------------------------------------------------------------------------
// Error‐code coverage tests: ELandNothingToLand
// ---------------------------------------------------------------------------

func TestLand_NothingToLand(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD in integration tree
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// rev-list --count: 0 commits between base and sandbox branch
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "0\n",
	}

	// git status --porcelain: nothing dirty (clean sandbox)
	h.runner.Responses[fmt.Sprintf("git -C %s status --porcelain -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{
		Stdout: "",
	}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandNothingToLand, errors.GetCode(err))
	assert.Contains(t, err.Error(), "nothing to land")
}

// ---------------------------------------------------------------------------
// Error‐code coverage tests: ELandApplyRequired
// ---------------------------------------------------------------------------

func TestLand_ApplyRequired(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// 0 commits
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "0\n",
	}

	// Dirty sandbox (uncommitted changes present)
	h.runner.Responses[fmt.Sprintf("git -C %s status --porcelain -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{
		Stdout: " M file.txt\n",
	}

	// Apply=false triggers the ELandApplyRequired code path
	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
		Apply:        false,
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandApplyRequired, errors.GetCode(err))
	assert.Contains(t, err.Error(), "no commits to cherry-pick")

	// Verify hint is present in the details.
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["hint"], "--apply")
}

// ---------------------------------------------------------------------------
// Error‐code coverage tests: ELandConflict
// ---------------------------------------------------------------------------

func TestLand_CherryPickConflict(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// 2 commits to cherry-pick
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "2\n",
	}

	// Cherry-pick fails with non-zero exit (conflict)
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --no-edit abc123..agency/sandbox-test", h.integrationPath)] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "error: could not apply abc456",
	}

	// Conflict file list
	h.runner.Responses[fmt.Sprintf("git -C %s diff --name-only --diff-filter=U", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "src/main.go\nREADME.md\n",
	}

	// cherry-pick --abort (cleanup)
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --abort", h.integrationPath)] = testutil.FakeResponse{}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandConflict, errors.GetCode(err))
	assert.Contains(t, err.Error(), "merge conflicts")

	// Verify details contain conflict file info.
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "2", ae.Details["conflict_count"])
	assert.Contains(t, ae.Details["conflict_files"], "src/main.go")
	assert.Contains(t, ae.Details["conflict_files"], "README.md")
}

// ---------------------------------------------------------------------------
// Error‐code coverage tests: ELandFailed (multiple paths)
// ---------------------------------------------------------------------------

func TestLand_Failed_CherryPickExecError(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD succeeds
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// 1 commit
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "1\n",
	}

	// Cherry-pick returns an execution error (not exit code, but actual error)
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --no-edit abc123..agency/sandbox-test", h.integrationPath)] = testutil.FakeResponse{
		Err: fmt.Errorf("git binary not found"),
	}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "failed to execute cherry-pick")
}

func TestLand_Failed_CherryPickNonZeroNoConflicts(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// 1 commit
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "1\n",
	}

	// Cherry-pick exits non-zero
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --no-edit abc123..agency/sandbox-test", h.integrationPath)] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "fatal: bad revision",
	}

	// No conflict files — diff returns empty
	h.runner.Responses[fmt.Sprintf("git -C %s diff --name-only --diff-filter=U", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "",
	}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "cherry-pick failed")
	assert.Contains(t, err.Error(), "bad revision")
}

func TestLand_Failed_HeadCaptureFails(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD returns an execution error
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Err: fmt.Errorf("not a git repository"),
	}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "failed to capture integration HEAD")
}

func TestLand_Failed_CommitCountFails(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD succeeds
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// rev-list --count returns exec error
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Err: fmt.Errorf("git binary crashed"),
	}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "failed to determine commit count")
}

func TestLand_Failed_SandboxStatusCheckFails(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// 0 commits
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "0\n",
	}

	// git status --porcelain returns exec error
	h.runner.Responses[fmt.Sprintf("git -C %s status --porcelain -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{
		Err: fmt.Errorf("git index corrupted"),
	}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "failed to check sandbox status")
}

func TestLand_Failed_RequireBaseDiverged(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// Integration HEAD is different from base_commit ("abc123").
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "def456\n",
	}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
		RequireBase:  true,
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "integration branch has diverged from base_commit")

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "abc123", ae.Details["base_commit"])
	assert.Equal(t, "def456", ae.Details["integration_head"])
}

// ---------------------------------------------------------------------------
// Error‐code coverage tests: ELandNothingToLand (apply mode — empty diff)
// ---------------------------------------------------------------------------

func TestLand_NothingToLand_EmptyDiffApplyMode(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// 0 commits
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "0\n",
	}

	// Dirty sandbox
	h.runner.Responses[fmt.Sprintf("git -C %s status --porcelain -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{
		Stdout: " M file.txt\n",
	}

	// git add -A (stage all in sandbox)
	h.runner.Responses[fmt.Sprintf("git -C %s add -A -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{}

	// diff --cached produces empty output (only .agency changes, excluded)
	h.runner.Responses[fmt.Sprintf("git -C %s diff --cached abc123 -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{
		Stdout: "   \n",
	}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
		Apply:        true,
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandNothingToLand, errors.GetCode(err))
	assert.Contains(t, err.Error(), "diff is empty")
}

// ---------------------------------------------------------------------------
// Note on ELandDenylistViolation
// ---------------------------------------------------------------------------
//
// ELandDenylistViolation is defined in errors.go but is NOT returned by any
// production code path as of this commit. The landing service does not
// currently perform denylist checks during land operations (the checkpoint
// engine has its own denylist logic). This error code is likely reserved for
// future use. No test is added because there is no production code to exercise.

func TestLand_Failed_ApplyPatchFails(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	// rev-parse HEAD
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}

	// 0 commits
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "0\n",
	}

	// Dirty sandbox
	h.runner.Responses[fmt.Sprintf("git -C %s status --porcelain -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{
		Stdout: " M file.txt\n",
	}

	// git add -A
	h.runner.Responses[fmt.Sprintf("git -C %s add -A -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{}

	// diff --cached returns non-empty patch
	h.runner.Responses[fmt.Sprintf("git -C %s diff --cached abc123 -- :(exclude).agency", h.sandboxPath)] = testutil.FakeResponse{
		Stdout: "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n",
	}

	// git apply --index fails with non-zero exit
	// The patch path is dynamic (contains temp dir), so we need a prefix-based matcher.
	// Since FakeCommandRunner matches on exact key, we rely on the fact that the
	// patch is written to a known store path. Let's compute it.
	sandboxDir := h.store.SandboxDir("test-repo", "test-inv")
	patchPath := filepath.Join(sandboxDir, "tmp", "land.patch")

	// Ensure the sandbox store dir exists so the patch can be written.
	require.NoError(t, os.MkdirAll(filepath.Join(sandboxDir, "tmp"), 0o755))

	h.runner.Responses[fmt.Sprintf("git -C %s apply --index %s", h.integrationPath, patchPath)] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "error: patch does not apply",
	}

	// reset --hard (cleanup on apply failure)
	h.runner.Responses[fmt.Sprintf("git -C %s reset --hard", h.integrationPath)] = testutil.FakeResponse{}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
		Apply:        true,
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "patch apply failed")
}

// ---------------------------------------------------------------------------
// Verify that ELandConflict details include the expected structured info
// ---------------------------------------------------------------------------

func TestLand_CherryPickConflict_DetailsStructure(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "3\n",
	}
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --no-edit abc123..agency/sandbox-test", h.integrationPath)] = testutil.FakeResponse{
		ExitCode: 1,
	}
	h.runner.Responses[fmt.Sprintf("git -C %s diff --name-only --diff-filter=U", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "a.go\nb.go\nc.go\n",
	}
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --abort", h.integrationPath)] = testutil.FakeResponse{}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandConflict, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "3", ae.Details["conflict_count"])
	assert.Contains(t, ae.Details["hint"], "resolve conflicts")

	// Verify conflict_files is valid JSON containing all three files.
	var files []string
	require.NoError(t, json.Unmarshal([]byte(ae.Details["conflict_files"]), &files))
	assert.Equal(t, []string{"a.go", "b.go", "c.go"}, files)
}

// ---------------------------------------------------------------------------
// Verify cherry-pick abort is called on conflict
// ---------------------------------------------------------------------------

func TestLand_CherryPickConflict_AbortsOnConflict(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)

	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "1\n",
	}
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --no-edit abc123..agency/sandbox-test", h.integrationPath)] = testutil.FakeResponse{
		ExitCode: 1,
	}
	h.runner.Responses[fmt.Sprintf("git -C %s diff --name-only --diff-filter=U", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "conflict.txt\n",
	}
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --abort", h.integrationPath)] = testutil.FakeResponse{}

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, errors.ELandConflict, errors.GetCode(err))

	// Verify cherry-pick --abort was called.
	abortKey := fmt.Sprintf("git -C %s cherry-pick --abort", h.integrationPath)
	found := false
	for _, call := range h.runner.Calls {
		if strings.Contains(call, "cherry-pick --abort") {
			found = true
			assert.Equal(t, abortKey, call)
		}
	}
	assert.True(t, found, "expected cherry-pick --abort to be called")
}

func readInvocationEventsAsMaps(t *testing.T, eventsPath string) []map[string]any {
	t.Helper()

	f, err := os.Open(eventsPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var events []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		events = append(events, line)
	}
	require.NoError(t, scanner.Err())
	return events
}

func TestLand_EventsUseMonotonicInvocationSequence(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)
	h.runner.Responses[fmt.Sprintf("git -C %s rev-parse HEAD", h.integrationPath)] = testutil.FakeResponse{
		Stdout: "abc123\n",
	}
	h.runner.Responses["git -C /nonexistent rev-list --count abc123..agency/sandbox-test"] = testutil.FakeResponse{
		Stdout: "1\n",
	}
	h.runner.Responses[fmt.Sprintf("git -C %s cherry-pick --no-edit abc123..agency/sandbox-test", h.integrationPath)] = testutil.FakeResponse{}

	result, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	eventsPath := h.store.InvocationEventsPath("test-repo", "test-inv")
	events := readInvocationEventsAsMaps(t, eventsPath)
	require.Len(t, events, 2)

	kind1, ok := events[0]["kind"].(string)
	require.True(t, ok)
	kind2, ok := events[1]["kind"].(string)
	require.True(t, ok)
	assert.Equal(t, "agency.land_started", kind1)
	assert.Equal(t, "agency.land_succeeded", kind2)
	assert.Equal(t, float64(1), events[0]["seq"])
	assert.Equal(t, float64(2), events[1]["seq"])
}

func TestLand_EventAppendFailureStopsOperation(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)
	h.svc = landing.NewServiceWithWriter(
		h.store,
		h.runner,
		fs.NewRealFS(),
		time.Now,
		failingEventAppender{err: fmt.Errorf("append failed")},
	)

	_, err := h.svc.Land(context.Background(), landing.LandOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})
	require.Error(t, err)
	assert.Equal(t, errors.ELandFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "append invocation event")
	assert.Len(t, h.runner.Calls, 0, "land should fail before any git command when event append fails")
}

func TestDiscard_EventsUseMonotonicInvocationSequence(t *testing.T) {
	t.Parallel()

	h := setupHarness(t)
	err := h.svc.Discard(context.Background(), landing.DiscardOpts{
		RepoID:       "test-repo",
		InvocationID: "test-inv",
		RepoRoot:     "/nonexistent",
	})
	require.NoError(t, err)

	eventsPath := h.store.InvocationEventsPath("test-repo", "test-inv")
	events := readInvocationEventsAsMaps(t, eventsPath)
	require.Len(t, events, 2)

	kind1, ok := events[0]["kind"].(string)
	require.True(t, ok)
	kind2, ok := events[1]["kind"].(string)
	require.True(t, ok)
	assert.Equal(t, "agency.discard_started", kind1)
	assert.Equal(t, "agency.discard_succeeded", kind2)
	assert.Equal(t, float64(1), events[0]["seq"])
	assert.Equal(t, float64(2), events[1]["seq"])
}

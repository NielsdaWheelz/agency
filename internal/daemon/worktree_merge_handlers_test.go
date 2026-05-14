package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestHandleWorktreeMerge_MissingRepoIDRemainsInvalidRequest(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/wt-1/pr/merge",
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp WorktreePRMergeResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvalidArgument), resp.ErrorCode)
	assert.Equal(t, "repo_id query parameter is required", resp.Message)
}

func TestHandleWorktreeMerge_RequiresConfirmation(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupWorktreeMergeReadyState(t, env, "")
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", "", "")

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/wt-1/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":false}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp WorktreePRMergeResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EConfirmationRequired), resp.ErrorCode)
}

func TestHandleWorktreeMerge_SuccessWritesWorktreeScopedLogs(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	resp := requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)
	assert.Equal(t, filepath.Join(env.Store.IntegrationWorktreeLogsDir(env.RepoID, worktreeID), "merge.log"), resp.Merge.MergeLogPath)
	assert.Equal(t, filepath.Join(env.Store.IntegrationWorktreeLogsDir(env.RepoID, worktreeID), "verify.log"), resp.Merge.VerifyLogPath)
	assert.Equal(t, filepath.Join(env.Store.IntegrationWorktreeLogsDir(env.RepoID, worktreeID), "archive.log"), resp.Merge.ArchiveLogPath)

	mergeMeta := requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusSucceeded
	})
	assert.Equal(t, store.WorktreeMergeStageCompleted, mergeMeta.Stage)
	assert.Equal(t, resp.Merge.AttemptID, mergeMeta.AttemptID)
	assert.Equal(t, resp.Merge.MergeLogPath, mergeMeta.MergeLogPath)
	assert.Equal(t, resp.Merge.VerifyLogPath, mergeMeta.VerifyLogPath)
	assert.Equal(t, resp.Merge.ArchiveLogPath, mergeMeta.ArchiveLogPath)

	mergeInfo, err := os.Stat(resp.Merge.MergeLogPath)
	require.NoError(t, err)
	verifyInfo, err := os.Stat(resp.Merge.VerifyLogPath)
	require.NoError(t, err)
	archiveInfo, err := os.Stat(resp.Merge.ArchiveLogPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), mergeInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o600), verifyInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o600), archiveInfo.Mode().Perm())
	assert.NotEmpty(t, workspaceRoot)
	meta, err := env.Store.ReadIntegrationWorktreeMeta(env.RepoID, worktreeID)
	require.NoError(t, err)
	assert.Equal(t, store.WorktreeStateArchived, meta.State)
	require.Contains(t, fakeRunner.Calls, filepath.Join(canonicalRepoRoot, "scripts", "archive.sh"))
	require.Contains(t, fakeRunner.Calls, "git -C "+canonicalRepoRoot+" worktree remove --force "+workspaceRoot)
}

func TestHandleWorktreeMerge_UsesLocalAgencyConfigWhenCanonicalRepoHasNone(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	require.NoError(t, os.Remove(filepath.Join(repoRoot, "agency.json")))
	localConfigRoot := filepath.Dir(config.LocalAgencyConfigPath(env.Server.ConfigDir, env.RepoID))
	writeWorktreeMergeScriptsAndConfig(t, localConfigRoot, "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeWorktreeMergeScriptsAndConfig(t, workspaceRoot, "#!/usr/bin/env bash\nset -euo pipefail\necho worktree verify should not run >&2\nexit 17\n")

	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)
	localArchiveScript := filepath.Join(localConfigRoot, "scripts", "archive.sh")
	fakeRunner.Responses[localArchiveScript] = testutil.FakeResponse{Stdout: "archived\n", ExitCode: 0}

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)
	requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusSucceeded
	})
	require.Contains(t, fakeRunner.Calls, localArchiveScript)
	assert.NotContains(t, fakeRunner.Calls, filepath.Join(workspaceRoot, "scripts", "archive.sh"))
	require.Contains(t, fakeRunner.Calls, "git -C "+canonicalRepoRoot+" worktree remove --force "+workspaceRoot)
}

func TestHandleWorktreeMerge_UsesCanonicalRepoConfigInsteadOfWorktreeConfig(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "scripts", "verify.sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nmkdir -p \"$AGENCY_OUTPUT_DIR\"\nprintf 'canonical\\n' > \"$AGENCY_OUTPUT_DIR/verify-source.txt\"\nexit 0\n"), 0o755))
	writeWorktreeMergeScriptsAndConfig(t, workspaceRoot, "#!/usr/bin/env bash\nset -euo pipefail\necho worktree verify should not run >&2\nexit 17\n")

	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)
	requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusSucceeded
	})

	verifySourcePath := filepath.Join(workspaceRoot, ".agency", "out", "verify-source.txt")
	verifySourceBytes, err := os.ReadFile(verifySourcePath)
	require.NoError(t, err)
	assert.Equal(t, "canonical\n", string(verifySourceBytes))
	require.Contains(t, fakeRunner.Calls, filepath.Join(canonicalRepoRoot, "scripts", "archive.sh"))
	assert.NotContains(t, fakeRunner.Calls, filepath.Join(workspaceRoot, "scripts", "archive.sh"))
}

func TestHandleWorktreeMerge_UsesRepoRootLastSeenWhenPreferredRootEmpty(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	writeWorktreeMergeRepoRecord(t, env, "", repoRoot)
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "scripts", "verify.sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nmkdir -p \"$AGENCY_OUTPUT_DIR\"\nprintf 'repo_root_last_seen\\n' > \"$AGENCY_OUTPUT_DIR/verify-source.txt\"\nexit 0\n"), 0o755))
	writeWorktreeMergeScriptsAndConfig(t, workspaceRoot, "#!/usr/bin/env bash\nset -euo pipefail\necho worktree verify should not run >&2\nexit 17\n")

	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)
	requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusSucceeded
	})

	verifySourcePath := filepath.Join(workspaceRoot, ".agency", "out", "verify-source.txt")
	verifySourceBytes, err := os.ReadFile(verifySourcePath)
	require.NoError(t, err)
	assert.Equal(t, "repo_root_last_seen\n", string(verifySourceBytes))
	require.Contains(t, fakeRunner.Calls, filepath.Join(canonicalRepoRoot, "scripts", "archive.sh"))
	assert.NotContains(t, fakeRunner.Calls, filepath.Join(workspaceRoot, "scripts", "archive.sh"))
}

func TestHandleWorktreeMerge_InvalidExplicitAgencyConfigFailsWithoutFallback(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

	explicitConfigPath := filepath.Join(t.TempDir(), "selected agency.json")
	require.NoError(t, os.WriteFile(explicitConfigPath, []byte(`{"version":1}`), 0o644))

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(fmt.Sprintf(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true,"agency_config_path":%q}`, explicitConfigPath)),
	)
	requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)

	mergeMeta := requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusFailed
	})
	assert.Equal(t, string(errors.EInvalidAgencyJSON), mergeMeta.ErrorCode)
	assert.Contains(t, mergeMeta.Hint, "--agency-config")
	assert.NotContains(t, fakeRunner.Calls, "gh pr merge 77 -R test/agent-repo --squash --delete-branch")
	assert.NotContains(t, fakeRunner.Calls, filepath.Join(canonicalRepoRoot, "scripts", "archive.sh"))
}

func TestHandleWorktreeMerge_BlocksOnUnresolvedInvocations(t *testing.T) {
	cases := []struct {
		name         string
		status       store.InvocationStatus
		landingState store.LandingStatus
	}{
		{name: "starting_empty", status: store.InvocationStatusStarting, landingState: ""},
		{name: "running_empty", status: store.InvocationStatusRunning, landingState: ""},
		{name: "finished_pending", status: store.InvocationStatusFinished, landingState: store.LandingStatusPending},
		{name: "failed_pending", status: store.InvocationStatusFailed, landingState: store.LandingStatusPending},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupReadTestEnv(t)
			fakeRunner := testutil.NewFakeCommandRunner()
			env.Server.Runner = fakeRunner

			workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
			canonicalRepoRoot := canonicalTestPath(t, repoRoot)
			setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

			writeWorktreeGuardInvocation(t, env.Store, env.RepoID, worktreeID, "inv-"+tc.name, tc.status, tc.landingState)

			w := doWorktreeRequestWithBody(
				t,
				env,
				http.MethodPost,
				"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
				[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
			)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

			var resp WorktreePRMergeResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.False(t, resp.OK)
			assert.Equal(t, string(errors.EWorktreeHasUnresolvedInvocations), resp.ErrorCode)
			assert.Empty(t, fakeRunner.Calls)
		})
	}
}

func TestHandleWorktreeMerge_AllowsLandedOrDiscardedInvocations(t *testing.T) {
	cases := []struct {
		name         string
		landingState store.LandingStatus
	}{
		{name: "landed", landingState: store.LandingStatusLanded},
		{name: "discarded", landingState: store.LandingStatusDiscarded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupReadTestEnv(t)
			fakeRunner := testutil.NewFakeCommandRunner()
			env.Server.Runner = fakeRunner

			workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
			canonicalRepoRoot := canonicalTestPath(t, repoRoot)
			setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

			writeWorktreeGuardInvocation(t, env.Store, env.RepoID, worktreeID, "inv-"+tc.name, store.InvocationStatusFinished, tc.landingState)

			w := doWorktreeRequestWithBody(
				t,
				env,
				http.MethodPost,
				"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
				[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
			)
			requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)
			requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
				return meta != nil && meta.Status == store.WorktreeMergeStatusSucceeded
			})
			assert.Contains(t, fakeRunner.Calls, "gh pr merge 77 -R test/agent-repo --squash --delete-branch")
		})
	}
}

func TestHandleWorktreeMerge_AttachesToActiveMergeWithSameOptions(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

	removeCmd := "git -C " + canonicalRepoRoot + " worktree remove --force " + workspaceRoot
	runner := newBlockingFakeRunner(removeCmd)
	runner.Responses = fakeRunner.Responses
	env.Server.Runner = runner
	t.Cleanup(runner.Release)

	started := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	startedResp := requireStartedWorktreeMergeResponse(t, started, env.RepoID, worktreeID)

	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("expected first merge attempt to reach archive cleanup")
	}

	attached := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	attachedResp := requireAttachedWorktreeMergeResponse(t, attached, env.RepoID, worktreeID)
	assert.Equal(t, startedResp.Merge.AttemptID, attachedResp.Merge.AttemptID)
	assert.Equal(t, startedResp.RequestID, attachedResp.Merge.RequestID)

	runner.Release()

	mergeMeta := requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusFailed
	})
	assert.Equal(t, store.WorktreeMergeStageArchive, mergeMeta.Stage)
	assert.Equal(t, string(errors.EArchiveFailed), mergeMeta.ErrorCode)
}

func TestHandleWorktreeMerge_BlocksOnBrokenInvocationRecord(t *testing.T) {
	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

	brokenInvocationID := "inv-broken"
	_, err := env.Store.EnsureInvocationDir(env.RepoID, brokenInvocationID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(env.Store.InvocationMetaPath(env.RepoID, brokenInvocationID), []byte("{not json"), 0o644))

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())

	var resp WorktreePRMergeResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EStoreCorrupt), resp.ErrorCode)
	assert.Empty(t, fakeRunner.Calls)
}

func TestHandleWorktreeMerge_ResumesArchiveWhenPRAlreadyMerged(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)
	fakeRunner.Responses["gh pr merge 77 -R test/agent-repo --squash --delete-branch"] = testutil.FakeResponse{ExitCode: 1, Stderr: "must not run"}
	fakeRunner.Responses["gh pr view 77 -R test/agent-repo --json number,url,state,isDraft,mergeable,headRefName"] = testutil.FakeResponse{
		Stdout:   `{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"MERGED","isDraft":false,"mergeable":"MERGEABLE","headRefName":"agency/alpha"}`,
		ExitCode: 0,
	}

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	resp := requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)

	mergeMeta := requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusSucceeded
	})
	assert.Equal(t, store.WorktreeMergeStageCompleted, mergeMeta.Stage)
	assert.Equal(t, filepath.Join(env.Store.IntegrationWorktreeLogsDir(env.RepoID, worktreeID), "archive.log"), resp.Merge.ArchiveLogPath)
	meta, err := env.Store.ReadIntegrationWorktreeMeta(env.RepoID, worktreeID)
	require.NoError(t, err)
	assert.Equal(t, store.WorktreeStateArchived, meta.State)
	require.Contains(t, fakeRunner.Calls, filepath.Join(canonicalRepoRoot, "scripts", "archive.sh"))
	require.Contains(t, fakeRunner.Calls, "git -C "+canonicalRepoRoot+" worktree remove --force "+workspaceRoot)
	assert.False(t, strings.Contains(strings.Join(fakeRunner.Calls, "\n"), "gh pr merge 77 -R test/agent-repo --squash --delete-branch"))
}

func TestHandleWorktreeMerge_FailsWhenArchiveCleanupRemoveFails(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)
	fakeRunner.Responses["git -C "+canonicalRepoRoot+" worktree remove --force "+workspaceRoot] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "remove failed",
	}

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)

	mergeMeta := requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusFailed
	})
	assert.Equal(t, store.WorktreeMergeStageArchive, mergeMeta.Stage)
	assert.Equal(t, string(errors.EArchiveFailed), mergeMeta.ErrorCode)

	meta, err := env.Store.ReadIntegrationWorktreeMeta(env.RepoID, worktreeID)
	require.NoError(t, err)
	assert.Equal(t, store.WorktreeStatePresent, meta.State)
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()

	canonical := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		canonical = resolved
	}
	return canonical
}

func decodeWorktreePRMergeResponse(t *testing.T, w *httptest.ResponseRecorder) WorktreePRMergeResponse {
	t.Helper()

	var resp WorktreePRMergeResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	return resp
}

func requireStartedWorktreeMergeResponse(t *testing.T, w *httptest.ResponseRecorder, repoID, worktreeID string) WorktreePRMergeResponse {
	t.Helper()

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	resp := decodeWorktreePRMergeResponse(t, w)
	require.True(t, resp.OK)
	assert.Equal(t, "started", resp.Action)
	assert.Equal(t, repoID, resp.RepoID)
	assert.Equal(t, worktreeID, resp.IntegrationWorktreeID)
	require.NotNil(t, resp.Merge)
	assert.Equal(t, string(store.WorktreeMergeStatusRunning), resp.Merge.State)
	assert.Equal(t, string(store.WorktreeMergeStagePreflight), resp.Merge.Stage)
	assert.Equal(t, "preparing merge", resp.Merge.StatusSummary)
	assert.Equal(t, resp.RequestID, resp.Merge.RequestID)
	return resp
}

func requireAttachedWorktreeMergeResponse(t *testing.T, w *httptest.ResponseRecorder, repoID, worktreeID string) WorktreePRMergeResponse {
	t.Helper()

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	resp := decodeWorktreePRMergeResponse(t, w)
	require.True(t, resp.OK)
	assert.Equal(t, "attached", resp.Action)
	assert.Equal(t, repoID, resp.RepoID)
	assert.Equal(t, worktreeID, resp.IntegrationWorktreeID)
	require.NotNil(t, resp.Merge)
	assert.Equal(t, string(store.WorktreeMergeStatusRunning), resp.Merge.State)
	return resp
}

func requireEventuallyWorktreeMergeMeta(
	t *testing.T,
	env *readTestEnv,
	worktreeID string,
	predicate func(*store.IntegrationWorktreeMergeMeta) bool,
) *store.IntegrationWorktreeMergeMeta {
	t.Helper()

	var meta *store.IntegrationWorktreeMergeMeta
	var readErr error
	require.Eventually(t, func() bool {
		meta, readErr = env.Store.ReadIntegrationWorktreeMerge(env.RepoID, worktreeID)
		if readErr != nil {
			return false
		}
		return predicate(meta)
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, readErr)
	require.NotNil(t, meta)
	return meta
}

func TestEnsureWorktreeVerifyLogPermissions_AllowsMissingOnRunnerFailurePath(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	logsDir := filepath.Join(t.TempDir(), "missing-logs")
	verifyLogPath := filepath.Join(logsDir, "verify.log")

	err := env.Server.ensureWorktreeVerifyLogPermissions(logsDir, verifyLogPath, true)
	require.NoError(t, err)
}

func TestEnsureWorktreeVerifyLogPermissions_RequiresArtifactsOnSuccessPath(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	logsDir := filepath.Join(t.TempDir(), "missing-logs")
	verifyLogPath := filepath.Join(logsDir, "verify.log")

	err := env.Server.ensureWorktreeVerifyLogPermissions(logsDir, verifyLogPath, false)
	require.Error(t, err)
	assert.Equal(t, errors.EPersistFailed, errors.GetCode(err))
}

func setupWorktreeMergeReadyState(t *testing.T, env *readTestEnv, verifyScriptBody string) (workspaceRoot, repoRoot, worktreeID string) {
	t.Helper()

	worktreeID = "wt-1"
	workspaceRoot = filepath.Join(t.TempDir(), "integration-tree")
	repoRoot = filepath.Join(t.TempDir(), "repo-root")

	require.NoError(t, os.MkdirAll(workspaceRoot, 0o755))
	require.NoError(t, os.MkdirAll(repoRoot, 0o755))

	if verifyScriptBody == "" {
		verifyScriptBody = "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
	}
	writeWorktreeMergeScriptsAndConfig(t, repoRoot, verifyScriptBody)
	writeWorktreeMergeRepoRecord(t, env, repoRoot, repoRoot)

	require.NoError(t, env.Store.UpdateIntegrationWorktreeMeta(env.RepoID, worktreeID, func(meta *store.IntegrationWorktreeMeta) {
		meta.TreePath = workspaceRoot
		meta.Branch = "agency/alpha"
		meta.BaseBranch = "main"
		meta.Name = "alpha"
	}))
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.LandingStatus = store.LandingStatusLanded
	}))

	return workspaceRoot, repoRoot, worktreeID
}

func writeWorktreeMergeScriptsAndConfig(t *testing.T, root, verifyScriptBody string) {
	t.Helper()

	scriptsDir := filepath.Join(root, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755))

	setupScript := "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
	archiveScript := "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "setup.sh"), []byte(setupScript), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "verify.sh"), []byte(verifyScriptBody), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "archive.sh"), []byte(archiveScript), 0o755))

	agencyJSON := `{
  "version": 4,
  "scripts": {
    "setup": {
      "path": "scripts/setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/archive.sh",
      "timeout": "5m"
    }
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "agency.json"), []byte(agencyJSON), 0o644))
}

func writeWorktreeMergeRepoRecord(t *testing.T, env *readTestEnv, preferredRoot, lastSeenRoot string) {
	t.Helper()

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	canonicalRoot := preferredRoot
	if strings.TrimSpace(canonicalRoot) == "" {
		canonicalRoot = lastSeenRoot
	}
	record := store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "test/agent-repo",
		RepoID:           env.RepoID,
		RepoRootLastSeen: lastSeenRoot,
		PreferredRoot:    preferredRoot,
		AgencyJSONPath:   filepath.Join(canonicalRoot, "agency.json"),
		OriginPresent:    true,
		OriginURL:        "git@github.com:test/agent-repo.git",
		OriginHost:       "github.com",
		Capabilities: store.Capabilities{
			GitHubOrigin: true,
			OriginHost:   "github.com",
			GhAuthed:     true,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, env.Store.SaveRepoRecord(record))
}

func setWorktreeMergeHappyRunnerResponses(fakeRunner *testutil.FakeCommandRunner, branch, repoRoot, treePath string) {
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	fakeRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	fakeRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	fakeRunner.Responses["gh pr list --head test:"+branch+" --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN"}]`,
		ExitCode: 0,
	}
	fakeRunner.Responses["gh pr view 77 -R test/agent-repo --json number,url,state,isDraft,mergeable,headRefName"] = testutil.FakeResponse{
		Stdout:   `{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefName":"` + branch + `"}`,
		ExitCode: 0,
	}
	fakeRunner.Responses["gh pr merge 77 -R test/agent-repo --squash --delete-branch"] = testutil.FakeResponse{
		Stdout:   "merged",
		ExitCode: 0,
	}
	fakeRunner.Responses["gh pr view 77 -R test/agent-repo --json state"] = testutil.FakeResponse{
		Stdout:   `{"state":"MERGED"}`,
		ExitCode: 0,
	}
	if repoRoot != "" && treePath != "" {
		repoRoot = canonicalTestPathForHelper(repoRoot)
		fakeRunner.Responses[filepath.Join(repoRoot, "scripts", "archive.sh")] = testutil.FakeResponse{Stdout: "archived\n", ExitCode: 0}
		fakeRunner.Responses["git -C "+repoRoot+" worktree remove --force "+treePath] = testutil.FakeResponse{ExitCode: 0}
	}
}

func canonicalTestPathForHelper(path string) string {
	canonical := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		canonical = resolved
	}
	return canonical
}

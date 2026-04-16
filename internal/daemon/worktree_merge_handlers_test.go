package daemon

import (
	"encoding/json"
	"net/http"
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
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp WorktreePRMergeResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.True(t, resp.OK)
	assert.Equal(t, "wt-1", resp.IntegrationWorktreeID)
	assert.Equal(t, filepath.Join(env.Store.IntegrationWorktreeLogsDir(env.RepoID, "wt-1"), "merge.log"), resp.MergeLogPath)
	assert.Equal(t, filepath.Join(env.Store.IntegrationWorktreeLogsDir(env.RepoID, "wt-1"), "verify.log"), resp.VerifyLogPath)
	assert.Equal(t, filepath.Join(env.Store.IntegrationWorktreeLogsDir(env.RepoID, "wt-1"), "archive.log"), resp.ArchiveLogPath)

	mergeInfo, err := os.Stat(resp.MergeLogPath)
	require.NoError(t, err)
	verifyInfo, err := os.Stat(resp.VerifyLogPath)
	require.NoError(t, err)
	archiveInfo, err := os.Stat(resp.ArchiveLogPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), mergeInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o600), verifyInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o600), archiveInfo.Mode().Perm())
	assert.NotEmpty(t, workspaceRoot)
	meta, err := env.Store.ReadIntegrationWorktreeMeta(env.RepoID, worktreeID)
	require.NoError(t, err)
	assert.Equal(t, store.WorktreeStateArchived, meta.State)
	require.Contains(t, fakeRunner.Calls, filepath.Join(workspaceRoot, "scripts", "archive.sh"))
	require.Contains(t, fakeRunner.Calls, "git -C "+canonicalRepoRoot+" worktree remove --force "+workspaceRoot)
}

func TestHandleWorktreeMerge_UsesLocalAgencyConfigWhenWorktreeHasNone(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	require.NoError(t, os.Remove(filepath.Join(workspaceRoot, "agency.json")))
	localConfigRoot := filepath.Dir(config.LocalAgencyConfigPath(env.Server.ConfigDir, env.RepoID))
	writeWorktreeMergeScriptsAndConfig(t, localConfigRoot, "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")

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
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp WorktreePRMergeResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.True(t, resp.OK)
	require.Contains(t, fakeRunner.Calls, localArchiveScript)
	require.Contains(t, fakeRunner.Calls, "git -C "+canonicalRepoRoot+" worktree remove --force "+workspaceRoot)
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
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			var resp WorktreePRMergeResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.True(t, resp.OK)
			assert.Contains(t, fakeRunner.Calls, "gh pr merge 77 -R test/agent-repo --squash --delete-branch")
		})
	}
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
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp WorktreePRMergeResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.True(t, resp.OK)
	assert.Equal(t, filepath.Join(env.Store.IntegrationWorktreeLogsDir(env.RepoID, worktreeID), "archive.log"), resp.ArchiveLogPath)
	meta, err := env.Store.ReadIntegrationWorktreeMeta(env.RepoID, worktreeID)
	require.NoError(t, err)
	assert.Equal(t, store.WorktreeStateArchived, meta.State)
	require.Contains(t, fakeRunner.Calls, filepath.Join(workspaceRoot, "scripts", "archive.sh"))
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
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	var resp WorktreePRMergeResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EArchiveFailed), resp.ErrorCode)

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
	writeWorktreeMergeScriptsAndConfig(t, workspaceRoot, verifyScriptBody)
	agencyDir := filepath.Join(workspaceRoot, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(
		"## summary\nmerge-ready report\n\n## how to test\ngo test ./...\n",
	), 0o644))
	writeWorktreeMergeRepoRecord(t, env, repoRoot)

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

func writeWorktreeMergeScriptsAndConfig(t *testing.T, workspaceRoot, verifyScriptBody string) {
	t.Helper()

	scriptsDir := filepath.Join(workspaceRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755))

	setupScript := "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
	archiveScript := "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "setup.sh"), []byte(setupScript), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "verify.sh"), []byte(verifyScriptBody), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "archive.sh"), []byte(archiveScript), 0o755))

	agencyJSON := `{
  "version": 1,
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
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "agency.json"), []byte(agencyJSON), 0o644))
}

func writeWorktreeMergeRepoRecord(t *testing.T, env *readTestEnv, repoRoot string) {
	t.Helper()

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	record := store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "test/agent-repo",
		RepoID:           env.RepoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
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
		fakeRunner.Responses[filepath.Join(treePath, "scripts", "archive.sh")] = testutil.FakeResponse{Stdout: "archived\n", ExitCode: 0}
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

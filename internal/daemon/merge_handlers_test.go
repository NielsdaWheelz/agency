package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

type mergeTestResponse struct {
	OK                    bool   `json:"ok"`
	RequestID             string `json:"request_id,omitempty"`
	ErrorCode             string `json:"error_code,omitempty"`
	Message               string `json:"message,omitempty"`
	Hint                  string `json:"hint,omitempty"`
	InvocationID          string `json:"invocation_id,omitempty"`
	RepoID                string `json:"repo_id,omitempty"`
	IntegrationWorktreeID string `json:"integration_worktree_id,omitempty"`
	Branch                string `json:"branch,omitempty"`
	PRNumber              int    `json:"pr_number,omitempty"`
	PRURL                 string `json:"pr_url,omitempty"`
	Strategy              string `json:"strategy,omitempty"`
	MergeLogPath          string `json:"merge_log_path,omitempty"`
}

func TestHandleMerge_RejectsMultipleJSONObjects(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}{"extra":1}`),
	)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvalidArgument), resp.ErrorCode)
}

func TestHandleMerge_ParsesBodyWhenContentLengthUnknown(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupMergeReadyInvocation(t, env, "")
	setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")

	body := []byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`)
	req := httptest.NewRequest(http.MethodPost, "/invocations/inv-1/merge?repo_id="+env.RepoID, bytes.NewReader(body))
	req.ContentLength = -1 // chunked/unknown length should still parse
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.Server.handleInvocations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleMerge_RequiresConfirmation(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupMergeReadyInvocation(t, env, "")
	setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":false}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EConfirmationRequired), resp.ErrorCode)
}

func TestHandleMerge_NotReadyInvocationReturnsTypedError(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvocationStillRunning), resp.ErrorCode)
}

func TestHandleMerge_MissingPRReturnsTypedError(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupMergeReadyInvocation(t, env, "")
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	fakeRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	fakeRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	fakeRunner.Responses["gh pr list --head test:agency/alpha --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[]`,
		ExitCode: 0,
	}
	fakeRunner.Responses["gh pr list --head agency/alpha --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[]`,
		ExitCode: 0,
	}

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	require.Equal(t, http.StatusNotFound, w.Code)

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.ENoPR), resp.ErrorCode)
}

func TestHandleMerge_FallbacksToUnqualifiedHeadLookup(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupMergeReadyInvocation(t, env, "")
	setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")
	fakeRunner.Responses["gh pr list --head test:agency/alpha --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[]`,
		ExitCode: 0,
	}
	fakeRunner.Responses["gh pr list --head agency/alpha --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN"}]`,
		ExitCode: 0,
	}

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleMerge_ClosedPRReturnsTypedError(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupMergeReadyInvocation(t, env, "")
	setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")
	fakeRunner.Responses["gh pr view 77 -R test/agent-repo --json number,url,state,isDraft,mergeable,headRefName"] = testutil.FakeResponse{
		Stdout:   `{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"CLOSED","isDraft":false,"mergeable":"MERGEABLE","headRefName":"agency/alpha"}`,
		ExitCode: 0,
	}

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EPRNotOpen), resp.ErrorCode)
}

func TestHandleMerge_MergeabilityConflictReturnsTypedError(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupMergeReadyInvocation(t, env, "")
	setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")
	fakeRunner.Responses["gh pr view 77 -R test/agent-repo --json number,url,state,isDraft,mergeable,headRefName"] = testutil.FakeResponse{
		Stdout:   `{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN","isDraft":false,"mergeable":"CONFLICTING","headRefName":"agency/alpha"}`,
		ExitCode: 0,
	}

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EPRNotMergeable), resp.ErrorCode)
}

func TestHandleMerge_SuccessWritesPrivateMergeLog(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupMergeReadyInvocation(t, env, "")
	setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
	}

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.True(t, resp.OK)
	assert.Equal(t, "inv-1", resp.InvocationID)
	assert.Equal(t, env.RepoID, resp.RepoID)
	assert.Equal(t, "wt-1", resp.IntegrationWorktreeID)
	assert.Equal(t, "agency/alpha", resp.Branch)
	assert.Equal(t, 77, resp.PRNumber)
	assert.Equal(t, "https://github.com/test/agent-repo/pull/77", resp.PRURL)
	assert.Equal(t, "squash", resp.Strategy)

	mergeLogPath := filepath.Join(env.Store.InvocationDir(env.RepoID, "inv-1"), "merge.log")
	info, err := os.Stat(mergeLogPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestHandleMerge_LogWriteFailureReturnsPersistFailed(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _, _ = setupMergeReadyInvocation(t, env, "")
	setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")

	mergeLogPath := filepath.Join(env.Store.InvocationDir(env.RepoID, "inv-1"), "merge.log")
	require.NoError(t, os.MkdirAll(mergeLogPath, 0o700), "precreate merge.log as a directory")

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EPersistFailed), resp.ErrorCode)
}

func TestHandleMerge_ResponseIncludesRequestIDOnSuccessAndFailure(t *testing.T) {
	t.Parallel()

	t.Run("failure", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		w := env.doInvocationRequestWithBody(
			t,
			http.MethodPost,
			"/invocations/inv-1/merge?repo_id="+env.RepoID,
			[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
		)
		require.Equal(t, http.StatusConflict, w.Code)

		var resp mergeTestResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.False(t, resp.OK)
		assert.NotEmpty(t, resp.RequestID)
		assert.Equal(t, resp.RequestID, w.Header().Get("X-Request-ID"))
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		fakeRunner := testutil.NewFakeCommandRunner()
		env.Server.Runner = fakeRunner

		_, _, _ = setupMergeReadyInvocation(t, env, "")
		setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")

		w := env.doInvocationRequestWithBody(
			t,
			http.MethodPost,
			"/invocations/inv-1/merge?repo_id="+env.RepoID,
			[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
		)
		require.Equal(t, http.StatusOK, w.Code)

		var resp mergeTestResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.OK)
		assert.NotEmpty(t, resp.RequestID)
		assert.Equal(t, resp.RequestID, w.Header().Get("X-Request-ID"))
	})
}

func TestHandleMerge_VerifyEnvUsesRepoAndWorkspaceRoots(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	verifyScript := `#!/usr/bin/env bash
set -euo pipefail
if [ "${AGENCY_REPO_ROOT:-}" != "__REPO_ROOT__" ]; then
  echo "bad repo root: ${AGENCY_REPO_ROOT:-}" >&2
  exit 41
fi
if [ "${AGENCY_WORKSPACE_ROOT:-}" != "__WORKSPACE_ROOT__" ]; then
  echo "bad workspace root: ${AGENCY_WORKSPACE_ROOT:-}" >&2
  exit 42
fi
exit 0
`
	workspaceRoot, repoRoot, _ := setupMergeReadyInvocation(t, env, verifyScript)
	setMergeHappyRunnerResponses(fakeRunner, "agency/alpha")

	verifyScriptPath := filepath.Join(workspaceRoot, "scripts", "verify.sh")
	scriptData, err := os.ReadFile(verifyScriptPath)
	require.NoError(t, err)
	replaced := strings.ReplaceAll(string(scriptData), "__REPO_ROOT__", canonicalizePath(repoRoot))
	replaced = strings.ReplaceAll(replaced, "__WORKSPACE_ROOT__", workspaceRoot)
	require.NoError(t, os.WriteFile(verifyScriptPath, []byte(replaced), 0o755))

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/merge?repo_id="+env.RepoID,
		[]byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
	}

	var resp mergeTestResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.OK)
}

func setupMergeReadyInvocation(t *testing.T, env *readTestEnv, verifyScriptBody string) (workspaceRoot, repoRoot, invocationID string) {
	t.Helper()

	invocationID = "inv-1"
	workspaceRoot = filepath.Join(t.TempDir(), "integration-tree")
	repoRoot = filepath.Join(t.TempDir(), "repo-root")

	require.NoError(t, os.MkdirAll(workspaceRoot, 0o755))
	require.NoError(t, os.MkdirAll(repoRoot, 0o755))

	if verifyScriptBody == "" {
		verifyScriptBody = "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
	}
	writeMergeScriptsAndConfig(t, workspaceRoot, verifyScriptBody)
	writeMergeRepoRecord(t, env, repoRoot)

	require.NoError(t, env.Store.UpdateIntegrationWorktreeMeta(env.RepoID, "wt-1", func(meta *store.IntegrationWorktreeMeta) {
		meta.TreePath = workspaceRoot
		meta.Branch = "agency/alpha"
		meta.ParentBranch = "main"
		meta.Name = "alpha"
	}))

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
		meta.IntegrationWorktreeID = "wt-1"
	}))

	return workspaceRoot, repoRoot, invocationID
}

func writeMergeScriptsAndConfig(t *testing.T, workspaceRoot, verifyScriptBody string) {
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

func writeMergeRepoRecord(t *testing.T, env *readTestEnv, repoRoot string) {
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

func setMergeHappyRunnerResponses(fakeRunner *testutil.FakeCommandRunner, branch string) {
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
}

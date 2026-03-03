package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestHandlePRSync_DirtyWorktreeRejectedWithoutAllowDirty(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _ = setupPRSyncReadyInvocation(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout: " M README.md\n",
	}

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/pr/sync?repo_id="+env.RepoID,
		[]byte(`{}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp PRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EDirtyWorktree), resp.ErrorCode)
}

func TestHandlePRSync_NonFastForwardReturnsForceWithLeaseHint(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _ = setupPRSyncReadyInvocation(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	fakeRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	fakeRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	fakeRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git rev-list --count main..agency/alpha"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
	fakeRunner.Responses["git push -u origin agency/alpha"] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "! [rejected] agency/alpha -> agency/alpha (non-fast-forward)",
	}

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/pr/sync?repo_id="+env.RepoID,
		[]byte(`{}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp PRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EGitPushFailed), resp.ErrorCode)
	assert.Contains(t, resp.Hint, "--force-with-lease")
}

func TestHandlePRSync_ForceWithLeaseUsesPushPolicy(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, reportPath := setupPRSyncReadyInvocation(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	fakeRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	fakeRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	fakeRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git rev-list --count main..agency/alpha"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
	fakeRunner.Responses["git push --force-with-lease -u origin agency/alpha"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n", ExitCode: 0}
	fakeRunner.Responses["gh pr list --head test:agency/alpha --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN"}]`,
		ExitCode: 0,
	}
	fakeRunner.Responses["gh pr edit 77 --body-file "+reportPath] = testutil.FakeResponse{ExitCode: 0}

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/pr/sync?repo_id="+env.RepoID,
		[]byte(`{"force_with_lease":true}`),
	)
	require.Equal(t, http.StatusOK, w.Code)

	var resp PRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.OK)
	assert.Equal(t, "agency/alpha", resp.Branch)
	assert.Equal(t, "updated", resp.PRAction)
	assert.Equal(t, "https://github.com/test/agent-repo/pull/77", resp.PRURL)
	assert.Contains(t, fakeRunner.Calls, "git push --force-with-lease -u origin agency/alpha")
}

func TestHandlePRSync_ResponseIncludesRequestIDOnSuccessAndFailure(t *testing.T) {
	t.Parallel()

	t.Run("failure", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		w := env.doInvocationRequestWithBody(
			t,
			http.MethodPost,
			"/invocations/inv-1/pr/sync?repo_id="+env.RepoID,
			[]byte(`{}`),
		)
		require.Equal(t, http.StatusConflict, w.Code)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
		requestID, ok := payload["request_id"].(string)
		require.True(t, ok, "request_id must be present in failure payload")
		assert.NotEmpty(t, requestID)
		assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		fakeRunner := testutil.NewFakeCommandRunner()
		env.Server.Runner = fakeRunner

		_, reportPath := setupPRSyncReadyInvocation(t, env)
		fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
		fakeRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
		fakeRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
		fakeRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
		fakeRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
		fakeRunner.Responses["git rev-list --count main..agency/alpha"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
		fakeRunner.Responses["git push -u origin agency/alpha"] = testutil.FakeResponse{ExitCode: 0}
		fakeRunner.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n", ExitCode: 0}
		fakeRunner.Responses["gh pr list --head test:agency/alpha --state all --json number,url,state"] = testutil.FakeResponse{
			Stdout:   `[{"number":88,"url":"https://github.com/test/agent-repo/pull/88","state":"OPEN"}]`,
			ExitCode: 0,
		}
		fakeRunner.Responses["gh pr edit 88 --body-file "+reportPath] = testutil.FakeResponse{ExitCode: 0}

		w := env.doInvocationRequestWithBody(
			t,
			http.MethodPost,
			"/invocations/inv-1/pr/sync?repo_id="+env.RepoID,
			[]byte(`{}`),
		)
		require.Equal(t, http.StatusOK, w.Code)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
		requestID, ok := payload["request_id"].(string)
		require.True(t, ok, "request_id must be present in success payload")
		assert.NotEmpty(t, requestID)
		assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
	})
}

func setupPRSyncReadyInvocation(t *testing.T, env *readTestEnv) (string, string) {
	t.Helper()

	treePath := filepath.Join(t.TempDir(), "integration-tree")
	agencyDir := filepath.Join(treePath, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))

	reportPath := filepath.Join(agencyDir, "report.md")
	require.NoError(t, os.WriteFile(reportPath, []byte(
		"## summary\nready for pr sync\n\n## how to test\ngo test ./...\n",
	), 0o644))

	require.NoError(t, env.Store.UpdateIntegrationWorktreeMeta(env.RepoID, "wt-1", func(meta *store.IntegrationWorktreeMeta) {
		meta.TreePath = treePath
		meta.Branch = "agency/alpha"
		meta.ParentBranch = "main"
		meta.Name = "alpha"
	}))

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
		meta.IntegrationWorktreeID = "wt-1"
	}))

	return treePath, reportPath
}

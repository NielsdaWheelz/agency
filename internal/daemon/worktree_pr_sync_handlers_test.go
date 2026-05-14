package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestPRSyncDirtyStatusIgnoresAgencyDirectory(t *testing.T) {
	t.Parallel()

	fakeRunner := testutil.NewFakeCommandRunner()
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout:   "?? .agency/state/runner_status.json\n?? .agency/tmp/pr_body.md\n M README.md\n",
		ExitCode: 0,
	}

	clean, status, err := prSyncDirtyStatus(context.Background(), fakeRunner, "/repo", prSyncNonInteractiveEnv(nil))
	require.NoError(t, err)
	assert.False(t, clean)
	assert.Equal(t, " M README.md", status)

	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout:   "?? .agency/state/runner_status.json\n?? .agency/tmp/pr_body.md\n",
		ExitCode: 0,
	}

	clean, status, err = prSyncDirtyStatus(context.Background(), fakeRunner, "/repo", prSyncNonInteractiveEnv(nil))
	require.NoError(t, err)
	assert.True(t, clean)
	assert.Empty(t, status)
}

func TestHandleWorktreePRSync_MissingRepoIDRemainsInvalidRequest(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	w := doWorktreeRequestWithBody(t, env, http.MethodPost, "/worktrees/wt-1/pr/sync", []byte(`{}`))
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp WorktreePRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_REQUEST", resp.ErrorCode)
	assert.Equal(t, "repo_id query parameter is required", resp.Message)
}

func TestHandleWorktreePRSync_StrictDecodeFailures(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/wt-1/pr/sync?repo_id="+env.RepoID,
		[]byte(`{"allow_dirty":true,"unknown":1}`),
	)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp WorktreePRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvalidArgument), resp.ErrorCode)
	assert.Equal(t, `invalid request body: unknown field "unknown"`, resp.Message)
}

func TestHandleWorktreePRSync_ParsesForceWithLeaseWhenContentLengthUnknown(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_ = setupWorktreeMutationReadyState(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout:   "",
		ExitCode: 0,
	}
	fakeRunner.Responses["gh --version"] = testutil.FakeResponse{
		Stdout:   "gh version 2.0.0\n",
		ExitCode: 0,
	}
	fakeRunner.Responses["gh auth status"] = testutil.FakeResponse{
		Stdout:   "ok\n",
		ExitCode: 0,
	}
	fakeRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git rev-list --count main..agency/alpha"] = testutil.FakeResponse{
		Stdout:   "1\n",
		ExitCode: 0,
	}
	fakeRunner.Responses["git push --force-with-lease -u origin agency/alpha"] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "fatal: push rejected",
	}

	body := []byte(`{"force_with_lease":true}`)
	req := httptest.NewRequest(http.MethodPost, "/worktrees/wt-1/pr/sync?repo_id="+env.RepoID, bytes.NewReader(body))
	req.ContentLength = -1 // chunked/unknown length should still parse options
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.Server.handleWorktrees(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
	var resp WorktreePRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EGitPushFailed), resp.ErrorCode)
	assert.Contains(t, fakeRunner.Calls, "git push --force-with-lease -u origin agency/alpha")
	assert.NotContains(t, fakeRunner.Calls, "git push -u origin agency/alpha")
}

func TestHandleWorktreePRSync_ResponseIncludesRequestIDOnSuccessAndFailure(t *testing.T) {
	t.Parallel()

	t.Run("failure", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		fakeRunner := testutil.NewFakeCommandRunner()
		env.Server.Runner = fakeRunner
		_ = setupWorktreeMutationReadyState(t, env)
		fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
			Stdout: " M README.md\n",
		}
		w := doWorktreeRequestWithBody(
			t,
			env,
			http.MethodPost,
			"/worktrees/wt-1/pr/sync?repo_id="+env.RepoID,
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

		treePath := setupWorktreeMutationReadyState(t, env)
		prBodyPath := filepath.Join(treePath, ".agency", "tmp", "pr_body.md")
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
		fakeRunner.Responses["gh pr edit 88 --body-file "+prBodyPath] = testutil.FakeResponse{ExitCode: 0}

		w := doWorktreeRequestWithBody(
			t,
			env,
			http.MethodPost,
			"/worktrees/wt-1/pr/sync?repo_id="+env.RepoID,
			[]byte(`{}`),
		)
		require.Equal(t, http.StatusOK, w.Code)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
		requestID, ok := payload["request_id"].(string)
		require.True(t, ok, "request_id must be present in success payload")
		assert.NotEmpty(t, requestID)
		assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))

		prBody, err := os.ReadFile(prBodyPath)
		require.NoError(t, err)
		assert.Equal(t, "## summary\nready for mutation\n\n## how to test\ngo test ./...\n", string(prBody))
	})
}

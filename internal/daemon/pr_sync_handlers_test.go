package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestHandlePRSync_MissingRepoIDRemainsInvalidRequest(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/pr/sync",
		[]byte(`{}`),
	)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp PRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_REQUEST", resp.ErrorCode)
	assert.Equal(t, "repo_id query parameter is required", resp.Message)
}

func TestHandlePRSync_StrictDecodeFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		body            []byte
		expectedMessage string
	}{
		{
			name:            "unknown field",
			body:            []byte(`{"allow_dirty":true,"unknown":1}`),
			expectedMessage: `invalid request body: unknown field "unknown"`,
		},
		{
			name:            "trailing data",
			body:            []byte(`{"allow_dirty":true} trailing`),
			expectedMessage: "invalid request body: expected a single JSON object",
		},
		{
			name:            "multiple objects",
			body:            []byte(`{"allow_dirty":true}{"force_with_lease":true}`),
			expectedMessage: "invalid request body: expected a single JSON object",
		},
		{
			name:            "malformed json",
			body:            []byte(`{"allow_dirty":`),
			expectedMessage: "invalid request body: malformed JSON",
		},
		{
			name:            "type mismatch",
			body:            []byte(`{"allow_dirty":"yes"}`),
			expectedMessage: `invalid request body: field "allow_dirty" must be bool`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := setupReadTestEnv(t)
			w := env.doInvocationRequestWithBody(
				t,
				http.MethodPost,
				"/invocations/inv-1/pr/sync?repo_id="+env.RepoID,
				tc.body,
			)
			require.Equal(t, http.StatusBadRequest, w.Code)

			var resp PRSyncResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.False(t, resp.OK)
			assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)
			assert.Equal(t, tc.expectedMessage, resp.Message)
			assert.Empty(t, resp.Hint)
			assert.NotEmpty(t, resp.RequestID)
			assert.Equal(t, resp.RequestID, w.Header().Get("X-Request-ID"))
		})
	}
}

func TestHandlePRSync_ParsesBodyWhenContentLengthUnknown(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _ = setupPRSyncReadyInvocation(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout: " M README.md\n",
	}
	fakeRunner.Responses["gh --version"] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "gh: not found",
	}

	body := []byte(`{"allow_dirty":true}`)
	req := httptest.NewRequest(http.MethodPost, "/invocations/inv-1/pr/sync?repo_id="+env.RepoID, bytes.NewReader(body))
	req.ContentLength = -1 // chunked/unknown length should still parse options
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.Server.handleInvocations(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp PRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EGhNotInstalled), resp.ErrorCode)
	assert.Contains(t, fakeRunner.Calls, "gh --version")
}

func TestHandlePRSync_EmptyBodyRemainsValidWithDefaultOptions(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _ = setupPRSyncReadyInvocation(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout: " M README.md\n",
	}

	w := env.doInvocationRequest(
		t,
		http.MethodPost,
		"/invocations/inv-1/pr/sync?repo_id="+env.RepoID,
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp PRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EDirtyWorktree), resp.ErrorCode)
}

func TestHandlePRSync_ParsesForceWithLeaseWhenContentLengthUnknown(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_, _ = setupPRSyncReadyInvocation(t, env)
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
	req := httptest.NewRequest(http.MethodPost, "/invocations/inv-1/pr/sync?repo_id="+env.RepoID, bytes.NewReader(body))
	req.ContentLength = -1 // chunked/unknown length should still parse options
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.Server.handleInvocations(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
	var resp PRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EGitPushFailed), resp.ErrorCode)
	assert.Contains(t, fakeRunner.Calls, "git push --force-with-lease -u origin agency/alpha")
	assert.NotContains(t, fakeRunner.Calls, "git push -u origin agency/alpha")
}

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
	assert.Equal(t, "report_markdown", resp.ReportSource)
	assert.False(t, resp.ReportFallbackUsed)
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

func TestHandlePRSync_HeadlessStrictReportContractFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prepare  func(t *testing.T, treePath string)
		wantCode errors.Code
	}{
		{
			name: "missing report artifacts",
			prepare: func(t *testing.T, treePath string) {
				t.Helper()
				agencyDir := filepath.Join(treePath, ".agency")
				require.NoError(t, os.Remove(filepath.Join(agencyDir, "report.md")))
				_ = os.Remove(filepath.Join(agencyDir, "report.json"))
			},
			wantCode: errors.EReportMissing,
		},
		{
			name: "malformed json is authoritative failure",
			prepare: func(t *testing.T, treePath string) {
				t.Helper()
				agencyDir := filepath.Join(treePath, ".agency")
				require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{"schema_version":"1.0","summary":`), 0o644))
			},
			wantCode: errors.EReportMalformed,
		},
		{
			name: "oversized json is deterministic failure",
			prepare: func(t *testing.T, treePath string) {
				t.Helper()
				agencyDir := filepath.Join(treePath, ".agency")
				require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), bytes.Repeat([]byte("x"), prSyncMaxReportBytes+1), 0o644))
			},
			wantCode: errors.EReportOversized,
		},
		{
			name: "schema incompatible json is deterministic failure",
			prepare: func(t *testing.T, treePath string) {
				t.Helper()
				agencyDir := filepath.Join(treePath, ".agency")
				require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{
  "schema_version": "9.9",
  "summary": "summary",
  "how_to_test": "go test ./..."
}`), 0o644))
			},
			wantCode: errors.EReportSchemaIncompatible,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := setupReadTestEnv(t)
			fakeRunner := testutil.NewFakeCommandRunner()
			env.Server.Runner = fakeRunner

			treePath, _ := setupPRSyncReadyInvocation(t, env)
			tc.prepare(t, treePath)

			fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
			fakeRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
			fakeRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
			fakeRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
			fakeRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
			fakeRunner.Responses["git rev-list --count main..agency/alpha"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}

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
			assert.Equal(t, string(tc.wantCode), resp.ErrorCode)
		})
	}
}

func TestHandlePRSync_HeadedCompatibilityFallsBackWhenReportInvalid(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	treePath, _ := setupPRSyncReadyInvocation(t, env)
	agencyDir := filepath.Join(treePath, ".agency")
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{"schema_version":"1.0","summary":`), 0o644))
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.Mode = store.RunnerModeHeaded
	}))

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
	fallbackPath := filepath.Join(agencyDir, "pr_sync_fallback.md")
	fakeRunner.Responses["gh pr edit 88 --body-file "+fallbackPath] = testutil.FakeResponse{ExitCode: 0}

	w := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/pr/sync?repo_id="+env.RepoID,
		[]byte(`{}`),
	)
	require.Equal(t, http.StatusOK, w.Code)

	var resp PRSyncResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.OK)
	assert.Equal(t, "updated", resp.PRAction)
	assert.Equal(t, "fallback", resp.ReportSource)
	assert.True(t, resp.ReportFallbackUsed)
	require.NotEmpty(t, resp.ReportDiagnostics)
	assert.Equal(t, "report_malformed", resp.ReportDiagnostics[0].Code)
	assert.Contains(t, fakeRunner.Calls, "gh pr edit 88 --body-file "+fallbackPath)
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

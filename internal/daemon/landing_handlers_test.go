package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestHandleLandDiscardRouting(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	t.Run("GET /land returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invocations/test-inv/land?repo_id=test-repo", nil)
		w := httptest.NewRecorder()

		s.handleInvocations(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("GET /discard returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invocations/test-inv/discard?repo_id=test-repo", nil)
		w := httptest.NewRecorder()

		s.handleInvocations(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("POST /land routes correctly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/land?repo_id=test-repo", nil)
		w := httptest.NewRecorder()

		s.handleInvocations(w, req)

		// Should NOT be 404 "unknown action". It will fail at invocation lookup.
		var resp map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		if code, ok := resp["error_code"].(string); ok {
			assert.NotEqual(t, "E_NOT_FOUND", code, "land route should be recognized")
		}
	})

	t.Run("POST /discard routes correctly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/discard?repo_id=test-repo", nil)
		w := httptest.NewRecorder()

		s.handleInvocations(w, req)

		var resp map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		if code, ok := resp["error_code"].(string); ok {
			assert.NotEqual(t, "E_NOT_FOUND", code, "discard route should be recognized")
		}
	})
}

func TestHandleLandDiscard_ErrorResponseIncludesRequestID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	tests := []string{
		"/invocations/test-inv/land",
		"/invocations/test-inv/discard",
	}
	for _, path := range tests {
		path := path
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			w := httptest.NewRecorder()
			s.handleInvocations(w, req)

			var payload map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&payload), "failed to decode response")
			requestID, ok := payload["request_id"].(string)
			require.True(t, ok, "request_id must be present")
			assert.NotEmpty(t, requestID)
			assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
		})
	}
}

func TestHandleLandDiscard_StrictOptionalBody(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	tests := []string{
		"/invocations/test-inv/land?repo_id=test-repo",
		"/invocations/test-inv/discard?repo_id=test-repo",
	}
	for _, path := range tests {
		path := path
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"unknown":true}`)))
			req.ContentLength = -1
			w := httptest.NewRecorder()

			s.handleInvocations(w, req)

			var payload map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
			assert.Equal(t, "E_INVALID_REQUEST", payload["error_code"])
			assert.Contains(t, payload["message"], `unknown field "unknown"`)
		})
	}
}

func TestHandleDiscardUsesInvocationProfileEnv(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configJSON := `{
		"version": 4,
		"defaults": {"runner": "claude-code", "editor": "code", "execution_profile": "work"},
		"runners": {"claude-code": "/bin/echo"},
		"execution_profiles": {
			"work": {"env": {"AGENCY_SELECTED_PROFILE": "work"}},
			"personal": {"env": {"AGENCY_SELECTED_PROFILE": "personal"}}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(configJSON), 0o644))

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)
	repoID := "repo-1"
	repoRoot := filepath.Join(dataDir, "repo")
	checkoutRoot := filepath.Join(dataDir, "checkouts", repoID)
	integrationPath := filepath.Join(checkoutRoot, "worktrees", "main-wt")
	sandboxPath := filepath.Join(checkoutRoot, "sandboxes", "inv-1")
	require.NoError(t, os.MkdirAll(integrationPath, 0o755))
	require.NoError(t, os.MkdirAll(sandboxPath, 0o755))
	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			"github:owner/repo": {
				RepoID:     repoID,
				Paths:      []string{repoRoot},
				LastSeenAt: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}))

	wtID := "wt-1"
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)
	wtMeta := store.NewIntegrationWorktreeMeta(wtID, "main", repoID, "agency/main", "main", integrationPath, checkoutRoot, "work", time.Now())
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, wtMeta))

	invocationID := "inv-1"
	_, err = st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	invMeta := store.NewInvocationMeta(invocationID, "", wtID, sandboxPath, checkoutRoot, "personal", "agency/sandbox-inv-1", "abc123", "claude-code", store.RunnerModeHeadless, time.Now())
	invMeta.Status = store.InvocationStatusFinished
	invMeta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, invMeta))

	runner := testutil.NewFakeCommandRunner()
	runner.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoRoot + "\n"}
	s := NewServer(st, runner, fsys, configDir)

	req := httptest.NewRequest(http.MethodPost, "/invocations/"+invocationID+"/discard?repo_id="+repoID, nil)
	w := httptest.NewRecorder()
	s.handleInvocations(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for i, call := range runner.Calls {
		switch {
		case call == "git rev-parse --show-toplevel":
			assert.Equal(t, "work", runner.CallEnvs[i]["AGENCY_SELECTED_PROFILE"])
		case strings.Contains(call, "worktree remove --force"), strings.Contains(call, "branch -D"):
			assert.Equal(t, "personal", runner.CallEnvs[i]["AGENCY_SELECTED_PROFILE"])
		}
	}
}

func TestStopInvocationForDiscardUsesProcessGroupLivenessWhenLeaderExited(t *testing.T) {
	pgid := startOrphanedIgnoringSignalProcessGroup(t)

	dataDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), dataDir)
	s.PIDChecker = func(int) bool {
		t.Fatal("discard liveness must check the process group, not the process id")
		return false
	}

	const repoID = "repo-1"
	const invocationID = "inv-orphaned-group"
	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	meta := store.NewInvocationMeta(invocationID, "", "wt-1", "/sandbox", "/checkouts/repo-1", "work", "agency/sandbox-"+invocationID, "abc123", "claude-code", store.RunnerModeHeadless, time.Now())
	meta.Status = store.InvocationStatusRunning
	meta.PGID = &pgid
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = s.stopInvocationForDiscard(ctx, repoID, invocationID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return !isProcessGroupAlive(pgid)
	}, 2*time.Second, 20*time.Millisecond)

	updated, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFailed, updated.Status)
	assert.Equal(t, "discarded", updated.ExitReason)
	assert.Equal(t, "discarded", updated.FailureReason)
	assert.Nil(t, updated.PID)
}

func TestIsProcessGroupAliveDetectsGroupAfterLeaderExited(t *testing.T) {
	pgid := startOrphanedIgnoringSignalProcessGroup(t)

	assert.False(t, IsPIDAlive(pgid), "group leader process should be gone")
	assert.True(t, isProcessGroupAlive(pgid), "process group should still contain a live child")
}

func startOrphanedIgnoringSignalProcessGroup(t *testing.T) int {
	t.Helper()

	const shell = "/bin/sh"
	if _, err := os.Stat(shell); err != nil {
		t.Skipf("%s unavailable: %v", shell, err)
	}
	pid, err := syscall.ForkExec(shell, []string{
		"sh",
		"-c",
		`trap "" INT TERM HUP; while :; do sleep 1; done & exit 0`,
	}, &syscall.ProcAttr{
		Files: []uintptr{0, 1, 2},
		Sys:   &syscall.SysProcAttr{Setpgid: true},
	})
	require.NoError(t, err)
	pgid := pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	})

	var status syscall.WaitStatus
	_, err = syscall.Wait4(pid, &status, 0, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return !IsPIDAlive(pgid) && isProcessGroupAlive(pgid)
	}, time.Second, 10*time.Millisecond)

	return pgid
}

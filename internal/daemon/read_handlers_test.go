package daemon

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// ---------------------------------------------------------------------------
// readTestEnv: shared test environment for read handler tests
// ---------------------------------------------------------------------------

type readTestEnv struct {
	Server *Server
	API    http.Handler
	Store  *store.Store
	RepoID string
}

func (env *readTestEnv) apiHandler() http.Handler {
	if env.API == nil {
		env.API = env.Server.newHTTPHandler()
	}
	return env.API
}

// setupReadTestEnv creates a minimal server with seeded data for read handler tests.
// Seeds:
//
//	1 repo in repo_index.json
//	2 worktrees: wt-1 "alpha" (present), wt-2 "beta" (archived)
//	3 invocations:
//	  inv-1: running, headless, worktree=wt-1, started 10min ago
//	  inv-2: finished, headed, worktree=wt-1, started 5min ago, landed
//	  inv-3: failed, headless, worktree=wt-2, started 1min ago
//
// Clock: fixed at 2026-02-05T12:00:00Z
func setupReadTestEnv(t *testing.T) *readTestEnv {
	t.Helper()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	repoID := "test-repo-read"

	// Fixed clock for deterministic time-based tests (stall detection, etc.)
	now := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	st := store.NewStore(fs.NewRealFS(), dataDir, clock)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	srv.Clock = clock

	// Create repo index
	idx := store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID:     repoID,
				Paths:      []string{"/tmp/repo"},
				LastSeenAt: "2026-02-05T12:00:00Z",
			},
		},
	}
	require.NoError(t, st.SaveRepoIndex(idx))

	// Create worktree wt-1 "alpha" (present)
	_, err := st.EnsureIntegrationWorktreeDir(repoID, "wt-1")
	require.NoError(t, err)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, "wt-1", &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "wt-1",
		Name:          "alpha",
		RepoID:        repoID,
		Branch:        "agency/alpha",
		ParentBranch:  "main",
		TreePath:      "/tmp/worktrees/alpha",
		CreatedAt:     "2026-02-05T10:00:00Z",
		LastUsedAt:    "2026-02-05T11:50:00Z",
		State:         store.WorktreeStatePresent,
	}))

	// Create worktree wt-2 "beta" (archived)
	_, err = st.EnsureIntegrationWorktreeDir(repoID, "wt-2")
	require.NoError(t, err)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, "wt-2", &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "wt-2",
		Name:          "beta",
		RepoID:        repoID,
		Branch:        "agency/beta",
		ParentBranch:  "main",
		TreePath:      "/tmp/worktrees/beta",
		CreatedAt:     "2026-02-05T09:00:00Z",
		LastUsedAt:    "2026-02-05T11:00:00Z",
		State:         store.WorktreeStateArchived,
	}))

	// Create invocation inv-1: running, headless, wt-1, started 10min ago
	semanticWorking := runnerstatus.StatusWorking
	_, err = st.EnsureInvocationDir(repoID, "inv-1")
	require.NoError(t, err)
	require.NoError(t, st.WriteInvocationMeta(repoID, "inv-1", &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          "inv-1",
		IntegrationWorktreeID: "wt-1",
		SandboxPath:           "/tmp/sandbox/inv-1",
		SandboxBranch:         "agency/sandbox-inv-1",
		BaseCommit:            "abc123",
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             now.Add(-10 * time.Minute).Format(time.RFC3339),
		Status:                store.InvocationStatusRunning,
		SemanticStatus:        &semanticWorking,
	}))

	// Create invocation inv-2: finished, headed, wt-1, started 5min ago, landed
	_, err = st.EnsureInvocationDir(repoID, "inv-2")
	require.NoError(t, err)
	require.NoError(t, st.WriteInvocationMeta(repoID, "inv-2", &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          "inv-2",
		InvocationName:        "feature-work",
		IntegrationWorktreeID: "wt-1",
		SandboxPath:           "/tmp/sandbox/inv-2",
		SandboxBranch:         "agency/sandbox-inv-2",
		BaseCommit:            "def456",
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeaded,
		StartedAt:             now.Add(-5 * time.Minute).Format(time.RFC3339),
		FinishedAt:            now.Add(-2 * time.Minute).Format(time.RFC3339),
		Status:                store.InvocationStatusFinished,
		ExitReason:            "exited",
		LandingStatus:         store.LandingStatusLanded,
	}))

	// Create invocation inv-3: failed, headless, wt-2, started 1min ago
	_, err = st.EnsureInvocationDir(repoID, "inv-3")
	require.NoError(t, err)
	require.NoError(t, st.WriteInvocationMeta(repoID, "inv-3", &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          "inv-3",
		IntegrationWorktreeID: "wt-2",
		SandboxPath:           "/tmp/sandbox/inv-3",
		SandboxBranch:         "agency/sandbox-inv-3",
		BaseCommit:            "ghi789",
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             now.Add(-1 * time.Minute).Format(time.RFC3339),
		Status:                store.InvocationStatusFailed,
		ExitReason:            "unknown",
		FailureReason:         "runner_exit_nonzero",
	}))

	return &readTestEnv{
		Server: srv,
		API:    srv.newHTTPHandler(),
		Store:  st,
		RepoID: repoID,
	}
}

// doWorktreeRequest makes a request to the worktrees handler.
func (env *readTestEnv) doWorktreeRequest(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	env.apiHandler().ServeHTTP(w, req)
	return w
}

// doWorktreeRequestWithHeaders makes a request to the worktrees handler with custom headers.
func (env *readTestEnv) doWorktreeRequestWithHeaders(t *testing.T, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	env.apiHandler().ServeHTTP(w, req)
	return w
}

// doInvocationRequest makes a request to the invocations handler.
func (env *readTestEnv) doInvocationRequest(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	env.apiHandler().ServeHTTP(w, req)
	return w
}

// doInvocationRequestWithHeaders makes a request to the invocations handler with custom headers.
func (env *readTestEnv) doInvocationRequestWithHeaders(t *testing.T, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := env.newInvocationRequestWithHeaders(t, method, path, nil, headers)
	w := httptest.NewRecorder()
	env.apiHandler().ServeHTTP(w, req)
	return w
}

func (env *readTestEnv) newInvocationRequestWithHeaders(t *testing.T, method, path string, body []byte, headers map[string]string) *http.Request {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

// doInvocationRequestWithBody makes a request to the invocations handler with a JSON body.
func (env *readTestEnv) doInvocationRequestWithBody(t *testing.T, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := env.newInvocationRequestWithHeaders(t, method, path, body, nil)
	w := httptest.NewRecorder()
	env.apiHandler().ServeHTTP(w, req)
	return w
}

// decodeAPIResponse decodes the API response envelope from a recorder.
func decodeAPIResponse(t *testing.T, w *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var resp APIResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode API response: %s", w.Body.String())
	return resp
}

// decodeData extracts and decodes the Data field from an APIResponse into target.
func decodeData(t *testing.T, resp APIResponse, target interface{}) {
	t.Helper()
	dataBytes, err := json.Marshal(resp.Data)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(dataBytes, target))
}

// ---------------------------------------------------------------------------
// TIER 1: Core read handler tests (Tests 5-19)
// ---------------------------------------------------------------------------

func TestHandleListWorktrees_HappyPath(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)
	assert.NotZero(t, resp.APIVersion)
	assert.NotEmpty(t, resp.RequestID)

	var data ListWorktreesData
	decodeData(t, resp, &data)

	// Default state filter is "present" → only wt-1 "alpha"
	assert.Len(t, data.Worktrees, 1)
	assert.Equal(t, "wt-1", data.Worktrees[0].WorktreeID)
	assert.Equal(t, "alpha", data.Worktrees[0].Name)
}

func TestHandleListWorktrees_StateFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state         string
		expectedCount int
	}{
		{"present", 1},
		{"archived", 1},
		{"all", 2},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()
			env := setupReadTestEnv(t)
			w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id="+env.RepoID+"&state="+tt.state)

			assert.Equal(t, http.StatusOK, w.Code)

			resp := decodeAPIResponse(t, w)
			var data ListWorktreesData
			decodeData(t, resp, &data)

			assert.Len(t, data.Worktrees, tt.expectedCount)
		})
	}
}

func TestHandleListWorktrees_UnknownRepoReturnsNotFound(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id=nonexistent-repo")

	assert.Equal(t, http.StatusNotFound, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_REPO_NOT_FOUND", resp.ErrorCode)
	assert.Contains(t, resp.Message, "repo not found")

	assert.Empty(t, resp.Details)
}

func TestHandleGetWorktree_HappyPath(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees/wt-1?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var dto WorktreeDTO
	decodeData(t, resp, &dto)

	assert.Equal(t, "wt-1", dto.WorktreeID)
	assert.Equal(t, "alpha", dto.Name)
	assert.Equal(t, "present", dto.State)
}

func TestHandleGetWorktree_ByName(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees/alpha?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var dto WorktreeDTO
	decodeData(t, resp, &dto)

	assert.Equal(t, "wt-1", dto.WorktreeID)
}

func TestHandleGetWorktree_NotFound(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees/nonexistent?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusNotFound, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_WORKTREE_NOT_FOUND", resp.ErrorCode)
}

func TestHandleListInvocations_HappyPath(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data ListInvocationsData
	decodeData(t, resp, &data)

	// All 3 invocations (default state=all)
	assert.Len(t, data.Invocations, 3)

	// Sorted by started_at desc: inv-3 (1min ago), inv-2 (5min ago), inv-1 (10min ago)
	assert.Equal(t, "inv-3", data.Invocations[0].InvocationID)
	assert.Equal(t, "inv-2", data.Invocations[1].InvocationID)
	assert.Equal(t, "inv-1", data.Invocations[2].InvocationID)

	// Each should have display_status populated
	for _, inv := range data.Invocations {
		assert.NotEmpty(t, inv.DisplayStatus, "display_status should be populated for %s", inv.InvocationID)
	}
}

func TestHandleListInvocations_StateFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state         string
		expectedCount int
		expectedIDs   []string
	}{
		{"active", 1, []string{"inv-1"}},
		{"finished", 2, []string{"inv-3", "inv-2"}},
		{"all", 3, []string{"inv-3", "inv-2", "inv-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()
			env := setupReadTestEnv(t)
			w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&state="+tt.state)

			assert.Equal(t, http.StatusOK, w.Code)

			resp := decodeAPIResponse(t, w)
			var data ListInvocationsData
			decodeData(t, resp, &data)

			assert.Len(t, data.Invocations, tt.expectedCount)
			var gotIDs []string
			for _, inv := range data.Invocations {
				gotIDs = append(gotIDs, inv.InvocationID)
			}
			assert.Equal(t, tt.expectedIDs, gotIDs)
		})
	}
}

func TestHandleListInvocations_ModeFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode          string
		expectedCount int
	}{
		{"headless", 2},
		{"headed", 1},
		{"all", 3},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			env := setupReadTestEnv(t)
			w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&mode="+tt.mode)

			assert.Equal(t, http.StatusOK, w.Code)

			resp := decodeAPIResponse(t, w)
			var data ListInvocationsData
			decodeData(t, resp, &data)

			assert.Len(t, data.Invocations, tt.expectedCount)
		})
	}
}

func TestHandleGetInvocation_HappyPath(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var dto InvocationDTO
	decodeData(t, resp, &dto)

	assert.Equal(t, "inv-1", dto.InvocationID)
	assert.Equal(t, "running", dto.Status)
	assert.Equal(t, "working", dto.DisplayStatus)
}

func TestInvocationActivityProjection_ConvergesAcrossListShowAndReview(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "inv-1-activity-sandbox")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))
	working := runnerstatus.StatusWorking
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusRunning
		meta.SemanticStatus = &working
	}))

	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		Status:        runnerstatus.StatusWorking,
		UpdatedAt:     "2026-02-05T11:59:30Z",
		Summary:       "waiting on api contract",
		Questions:     []string{},
		Blockers:      []string{},
		Risks:         []string{},
	})

	logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	streamLine := `{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:59:00Z","invocation_id":"inv-1","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"latest activity summary"}}`
	require.NoError(t, os.WriteFile(env.Store.InvocationStreamLogPath(env.RepoID, "inv-1"), []byte(streamLine+"\n"), 0o644))
	require.NoError(t, os.WriteFile(env.Store.InvocationRawLogPath(env.RepoID, "inv-1"), []byte(`{"raw":true}`+"\n"), 0o644))

	listResp := decodeAPIResponse(t, env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID))
	require.True(t, listResp.OK)
	var listData ListInvocationsData
	decodeData(t, listResp, &listData)

	var listed InvocationDTO
	found := false
	for _, inv := range listData.Invocations {
		if inv.InvocationID == "inv-1" {
			listed = inv
			found = true
			break
		}
	}
	require.True(t, found, "expected inv-1 in invocation list")
	require.NotNil(t, listed.LatestActivity)
	require.NotNil(t, listed.Navigation)
	assert.Equal(t, "working", listed.DisplayStatus)
	assert.Equal(t, "waiting on api contract", listed.StatusSummary)
	assert.Equal(t, "stream:1", listed.LatestActivity.TurnID)
	assert.Equal(t, "latest activity summary", listed.LatestActivity.Summary)
	assert.Equal(t, "stream:1", listed.Navigation.LatestTurnID)

	showResp := decodeAPIResponse(t, env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1?repo_id="+env.RepoID))
	require.True(t, showResp.OK)
	var shown InvocationDTO
	decodeData(t, showResp, &shown)
	require.NotNil(t, shown.LatestActivity)
	require.NotNil(t, shown.Navigation)
	assert.Equal(t, listed.DisplayStatus, shown.DisplayStatus)
	assert.Equal(t, listed.StatusSummary, shown.StatusSummary)
	assert.Equal(t, listed.LatestActivity.TurnID, shown.LatestActivity.TurnID)
	assert.Equal(t, listed.LatestActivity.Summary, shown.LatestActivity.Summary)
	assert.Equal(t, listed.Navigation.HistoryCommand, shown.Navigation.HistoryCommand)
	assert.Equal(t, listed.Navigation.DiffCommand, shown.Navigation.DiffCommand)
	assert.Equal(t, listed.Navigation.LatestTurnID, shown.Navigation.LatestTurnID)

	reviewResp := decodeAPIResponse(t, env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/review?repo_id="+env.RepoID))
	require.True(t, reviewResp.OK)
	var review InvocationReviewData
	decodeData(t, reviewResp, &review)
	require.NotNil(t, review.LatestActivity)
	assert.Equal(t, shown.DisplayStatus, review.DisplayStatus)
	assert.Equal(t, shown.StatusSummary, review.StatusSummary)
	assert.Equal(t, shown.LatestActivity.TurnID, review.LatestActivity.TurnID)
	assert.Equal(t, shown.LatestActivity.Summary, review.LatestActivity.Summary)
	assert.Equal(t, shown.Navigation.HistoryCommand, review.Navigation.HistoryCommand)
	assert.Equal(t, shown.Navigation.DiffCommand, review.Navigation.DiffCommand)
	assert.Equal(t, shown.Navigation.LatestTurnID, review.Navigation.LatestTurnID)
}

func TestHandleGetInvocation_UsesInvocationOwnedRunnerSummaryAfterSandboxCleanup(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "inv-1-sandbox-cleanup")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))

	status := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		Status:        runnerstatus.StatusWorking,
		UpdatedAt:     "2026-02-05T11:59:30Z",
		Summary:       "invocation-owned summary survives cleanup",
		Questions:     []string{},
		Blockers:      []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", status)
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", status)

	working := runnerstatus.StatusWorking
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusRunning
		meta.SemanticStatus = &working
	}))
	require.NoError(t, os.RemoveAll(sandboxPath))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var dto InvocationDTO
	decodeData(t, resp, &dto)
	assert.Equal(t, "invocation-owned summary survives cleanup", dto.StatusSummary)
}

func TestHandleGetInvocation_NotFound(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/nonexistent?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusNotFound, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVOCATION_NOT_FOUND", resp.ErrorCode)
}

func TestHandleGetInvocationCheckpoints_HappyPath(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	require.NoError(t, os.MkdirAll(env.Store.InvocationDir(env.RepoID, "inv-1"), 0o700))

	cpFile := &checkpoint.CheckpointsFile{
		SchemaVersion: "1.0",
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                   1,
				CreatedAt:            "2026-02-05T11:51:00Z",
				Diffstat:             "+10 -5",
				SnapshotCommit:       "aaa111",
				IncludesUntracked:    true,
				ChangedPaths:         []string{"README.md", "cmd/main.go"},
				ChangedPathCount:     2,
				ChangedPathTruncated: false,
			},
			{ID: 2, CreatedAt: "2026-02-05T11:52:00Z", Diffstat: "+20 -10", SnapshotCommit: "bbb222", IncludesUntracked: true},
			{ID: 3, CreatedAt: "2026-02-05T11:53:00Z", Diffstat: "+30 -15", SnapshotCommit: "ccc333", IncludesUntracked: false},
		},
	}
	cpData, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(env.Store.InvocationCheckpointsPath(env.RepoID, "inv-1"), cpData, 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checkpoints?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data ListCheckpointsData
	decodeData(t, resp, &data)

	// 3 checkpoints, ordered by ID desc (latest first)
	require.Len(t, data.Checkpoints, 3)
	assert.Equal(t, 3, data.Checkpoints[0].ID)
	assert.Equal(t, 2, data.Checkpoints[1].ID)
	assert.Equal(t, 1, data.Checkpoints[2].ID)
	assert.Equal(t, []string{"README.md", "cmd/main.go"}, data.Checkpoints[2].ChangedPaths)
	assert.Equal(t, 2, data.Checkpoints[2].ChangedPathCount)
	assert.False(t, data.Checkpoints[2].ChangedPathTruncated)
}

func TestHandleGetInvocationCheckpoints_UsesInvocationOwnedAfterSandboxCleanup(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// Seed invocation-owned checkpoints and remove sandbox dir to simulate
	// post-land/discard lifecycle cleanup.
	cpFile := &checkpoint.CheckpointsFile{
		SchemaVersion: "1.0",
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                9,
				CreatedAt:         "2026-02-05T11:59:00Z",
				SnapshotCommit:    "invocation-owned",
				IncludesUntracked: true,
			},
		},
	}
	cpData, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(env.Store.InvocationCheckpointsPath(env.RepoID, "inv-1"), cpData, 0o644))
	require.NoError(t, os.RemoveAll(env.Store.SandboxDir(env.RepoID, "inv-1")))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checkpoints?repo_id="+env.RepoID)
	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data ListCheckpointsData
	decodeData(t, resp, &data)
	require.Len(t, data.Checkpoints, 1)
	assert.Equal(t, 9, data.Checkpoints[0].ID)
	assert.Equal(t, "invocation-owned", data.Checkpoints[0].SnapshotCommit)
}

func TestHandleGetInvocationCheckpoints_Empty(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// inv-2 has no checkpoints.json
	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-2/checkpoints?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data ListCheckpointsData
	decodeData(t, resp, &data)

	assert.NotNil(t, data.Checkpoints)
	assert.Len(t, data.Checkpoints, 0)
}

func TestHandleGetInvocationCheckpoints_MalformedFileReturnsInternalError(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	require.NoError(t, os.MkdirAll(env.Store.InvocationDir(env.RepoID, "inv-1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(env.Store.InvocationDir(env.RepoID, "inv-1"), "checkpoints.json"), []byte("{malformed"), 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checkpoints?repo_id="+env.RepoID)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INTERNAL", resp.ErrorCode)
}

func TestResponseEnvelope_RequestID(t *testing.T) {
	t.Parallel()

	t.Run("custom_request_id", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		w := env.doWorktreeRequestWithHeaders(t, http.MethodGet, "/worktrees?repo_id="+env.RepoID,
			map[string]string{"X-Request-ID": "custom-id"})

		resp := decodeAPIResponse(t, w)
		assert.Equal(t, "custom-id", resp.RequestID)
		assert.Equal(t, resp.RequestID, w.Header().Get("X-Request-ID"))
	})

	t.Run("generated_request_id", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id="+env.RepoID)

		resp := decodeAPIResponse(t, w)
		assert.NotEmpty(t, resp.RequestID)
		// UUID format: 8-4-4-4-12
		assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, resp.RequestID)
		assert.Equal(t, resp.RequestID, w.Header().Get("X-Request-ID"))
	})

	t.Run("invalid_request_id_header_regenerated", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		invalid := "bad id with spaces"
		w := env.doWorktreeRequestWithHeaders(t, http.MethodGet, "/worktrees?repo_id="+env.RepoID,
			map[string]string{"X-Request-ID": invalid})

		resp := decodeAPIResponse(t, w)
		assert.NotEqual(t, invalid, resp.RequestID)
		assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, resp.RequestID)
		assert.Equal(t, resp.RequestID, w.Header().Get("X-Request-ID"))
	})

	t.Run("unknown_invocation_action_echoes_custom_request_id", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		const custom = "trace-id-abc123"
		w := env.doInvocationRequestWithHeaders(
			t,
			http.MethodGet,
			"/invocations/inv-1/unknown?repo_id="+env.RepoID,
			map[string]string{"X-Request-ID": custom},
		)
		require.Equal(t, http.StatusNotFound, w.Code)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
		requestID, ok := payload["request_id"].(string)
		require.True(t, ok, "request_id must be present in error payload")
		assert.Equal(t, custom, requestID)
		assert.Equal(t, custom, w.Header().Get("X-Request-ID"))
	})
}

func TestResponseEnvelope_ErrorFormat(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/nonexistent?repo_id="+env.RepoID)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.NotZero(t, resp.APIVersion)
	assert.NotEmpty(t, resp.ErrorCode)
	assert.NotEmpty(t, resp.Message)
	assert.NotEmpty(t, resp.RequestID)
}

// ---------------------------------------------------------------------------
// TIER 2: Filter helpers (Tests 20-22)
// ---------------------------------------------------------------------------

func TestMatchesWorktreeState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state    store.WorktreeState
		filter   string
		expected bool
	}{
		{store.WorktreeStatePresent, "present", true},
		{store.WorktreeStatePresent, "archived", false},
		{store.WorktreeStatePresent, "all", true},
		{store.WorktreeStateArchived, "present", false},
		{store.WorktreeStateArchived, "archived", true},
		{store.WorktreeStateArchived, "all", true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state)+"_"+tt.filter, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, matchesWorktreeState(tt.state, tt.filter))
		})
	}
}

func TestMatchesInvocationState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   store.InvocationStatus
		landing  store.LandingStatus
		filter   string
		expected bool
	}{
		{"starting_active", store.InvocationStatusStarting, "", "active", true},
		{"running_active", store.InvocationStatusRunning, "", "active", true},
		{"finished_no_landing_active", store.InvocationStatusFinished, "", "active", true},
		{"finished_landed_active", store.InvocationStatusFinished, store.LandingStatusLanded, "active", false},
		{"finished_discarded_active", store.InvocationStatusFinished, store.LandingStatusDiscarded, "active", false},
		{"finished_finished", store.InvocationStatusFinished, "", "finished", true},
		{"failed_finished", store.InvocationStatusFailed, "", "finished", true},
		{"running_finished", store.InvocationStatusRunning, "", "finished", false},
		{"running_all", store.InvocationStatusRunning, "", "all", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, matchesInvocationState(tt.status, tt.landing, tt.filter))
		})
	}
}

func TestMatchesInvocationMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode     store.RunnerMode
		filter   string
		expected bool
	}{
		{store.RunnerModeHeaded, "headed", true},
		{store.RunnerModeHeaded, "headless", false},
		{store.RunnerModeHeaded, "all", true},
		{store.RunnerModeHeadless, "headless", true},
		{store.RunnerModeHeadless, "headed", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode)+"_"+tt.filter, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, matchesInvocationMode(tt.mode, tt.filter))
		})
	}
}

// ---------------------------------------------------------------------------
// TIER 2: Pagination tests (Tests 23-25)
// ---------------------------------------------------------------------------

func TestPaginateWorktrees(t *testing.T) {
	t.Parallel()

	t.Run("empty_list", func(t *testing.T) {
		t.Parallel()
		result, cursor := paginateWorktrees(nil, "", 10)
		assert.Len(t, result, 0)
		assert.Empty(t, cursor)
	})

	t.Run("within_limit", func(t *testing.T) {
		t.Parallel()
		items := []WorktreeDTO{
			{WorktreeID: "wt-1", LastUsedAt: "2026-02-05T11:00:00Z"},
			{WorktreeID: "wt-2", LastUsedAt: "2026-02-05T10:00:00Z"},
			{WorktreeID: "wt-3", LastUsedAt: "2026-02-05T09:00:00Z"},
		}
		result, cursor := paginateWorktrees(items, "", 10)
		assert.Len(t, result, 3)
		assert.Empty(t, cursor)
	})

	t.Run("exceeds_limit", func(t *testing.T) {
		t.Parallel()
		items := make([]WorktreeDTO, 5)
		for i := range items {
			items[i] = WorktreeDTO{
				WorktreeID: "wt-" + string(rune('a'+i)),
				LastUsedAt: time.Date(2026, 2, 5, 11, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			}
		}
		result, cursor := paginateWorktrees(items, "", 2)
		assert.Len(t, result, 2)
		assert.NotEmpty(t, cursor)
	})

	t.Run("cursor_continuation", func(t *testing.T) {
		t.Parallel()
		items := make([]WorktreeDTO, 5)
		for i := range items {
			items[i] = WorktreeDTO{
				WorktreeID: "wt-" + string(rune('a'+i)),
				LastUsedAt: time.Date(2026, 2, 5, 11, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			}
		}
		// Page 1: [wt-a, wt-b]
		result1, cursor1 := paginateWorktrees(items, "", 2)
		assert.Len(t, result1, 2)
		assert.Equal(t, "wt-a", result1[0].WorktreeID)
		assert.Equal(t, "wt-b", result1[1].WorktreeID)
		assert.NotEmpty(t, cursor1)

		// Page 2: exclusive cursor → [wt-c, wt-d]
		result2, cursor2 := paginateWorktrees(items, cursor1, 2)
		assert.Len(t, result2, 2)
		assert.Equal(t, "wt-c", result2[0].WorktreeID)
		assert.Equal(t, "wt-d", result2[1].WorktreeID)
		assert.NotEmpty(t, cursor2)

		// Page 3: [wt-e]
		result3, cursor3 := paginateWorktrees(items, cursor2, 2)
		assert.Len(t, result3, 1)
		assert.Equal(t, "wt-e", result3[0].WorktreeID)
		assert.Empty(t, cursor3)
	})
}

func TestPaginateInvocations(t *testing.T) {
	t.Parallel()

	t.Run("empty_list", func(t *testing.T) {
		t.Parallel()
		result, cursor := paginateInvocations(nil, "", 10)
		assert.Len(t, result, 0)
		assert.Empty(t, cursor)
	})

	t.Run("within_limit", func(t *testing.T) {
		t.Parallel()
		items := []InvocationDTO{
			{InvocationID: "inv-1", StartedAt: "2026-02-05T11:00:00Z"},
			{InvocationID: "inv-2", StartedAt: "2026-02-05T10:00:00Z"},
		}
		result, cursor := paginateInvocations(items, "", 10)
		assert.Len(t, result, 2)
		assert.Empty(t, cursor)
	})

	t.Run("exceeds_limit_with_cursor", func(t *testing.T) {
		t.Parallel()
		items := make([]InvocationDTO, 5)
		for i := range items {
			items[i] = InvocationDTO{
				InvocationID: "inv-" + string(rune('a'+i)),
				StartedAt:    time.Date(2026, 2, 5, 11, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			}
		}
		result1, cursor1 := paginateInvocations(items, "", 2)
		assert.Len(t, result1, 2)
		assert.NotEmpty(t, cursor1)

		result2, _ := paginateInvocations(items, cursor1, 2)
		assert.Len(t, result2, 2)
	})
}

func TestPaginateCheckpoints(t *testing.T) {
	t.Parallel()

	t.Run("empty_list", func(t *testing.T) {
		t.Parallel()
		result, cursor := paginateCheckpoints(nil, "", 10)
		assert.Len(t, result, 0)
		assert.Empty(t, cursor)
	})

	t.Run("within_limit", func(t *testing.T) {
		t.Parallel()
		items := []CheckpointDTO{
			{ID: 5}, {ID: 4}, {ID: 3},
		}
		result, cursor := paginateCheckpoints(items, "", 10)
		assert.Len(t, result, 3)
		assert.Empty(t, cursor)
	})

	t.Run("exceeds_limit_with_cursor", func(t *testing.T) {
		t.Parallel()
		items := []CheckpointDTO{
			{ID: 5}, {ID: 4}, {ID: 3}, {ID: 2}, {ID: 1},
		}
		// Page 1: [5, 4]
		result1, cursor1 := paginateCheckpoints(items, "", 2)
		assert.Len(t, result1, 2)
		assert.Equal(t, 5, result1[0].ID)
		assert.Equal(t, 4, result1[1].ID)
		assert.NotEmpty(t, cursor1)

		// Page 2: exclusive cursor → [3, 2]
		result2, cursor2 := paginateCheckpoints(items, cursor1, 2)
		assert.Len(t, result2, 2)
		assert.Equal(t, 3, result2[0].ID)
		assert.Equal(t, 2, result2[1].ID)
		assert.NotEmpty(t, cursor2)

		// Page 3: [1]
		result3, cursor3 := paginateCheckpoints(items, cursor2, 2)
		assert.Len(t, result3, 1)
		assert.Equal(t, 1, result3[0].ID)
		assert.Empty(t, cursor3)
	})
}

func TestHandleGetInvocationLogs_HappyPath(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// Seed a raw log file for inv-1
	logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	logContent := "{\"event\":\"start\"}\n{\"event\":\"output\",\"data\":\"hello\"}\n"
	require.NoError(t, os.WriteFile(filepath.Join(logsDir, "raw.jsonl"), []byte(logContent), 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/logs?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data InvocationLogsOffsetData
	decodeData(t, resp, &data)

	assert.Equal(t, "raw", data.Kind)
	assert.Equal(t, int64(len(logContent)), data.NextOffset)
	decoded, err := base64.StdEncoding.DecodeString(data.DataB64)
	require.NoError(t, err)
	assert.Equal(t, logContent, string(decoded))
	assert.Equal(t, int64(len(logContent)), data.TotalBytes)
}

func TestHandleGetInvocationLogs_MissingFile(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// inv-2 has no log files
	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-2/logs?repo_id="+env.RepoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data InvocationLogsOffsetData
	decodeData(t, resp, &data)
	assert.Equal(t, "raw", data.Kind)
	assert.Empty(t, data.DataB64)
	assert.Zero(t, data.NextOffset)
	assert.Zero(t, data.TotalBytes)
}

func TestHandleGetInvocationLogs_KindParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind       string
		fileSuffix string
	}{
		{"raw", "raw.jsonl"},
		{"stderr", "stderr.log"},
		{"stream", "stream.jsonl"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()
			env := setupReadTestEnv(t)

			// Create the log file
			logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
			require.NoError(t, os.MkdirAll(logsDir, 0o700))
			content := "content for " + tt.kind + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(logsDir, tt.fileSuffix), []byte(content), 0o644))

			w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/logs?repo_id="+env.RepoID+"&kind="+tt.kind)

			assert.Equal(t, http.StatusOK, w.Code)

			resp := decodeAPIResponse(t, w)
			var data InvocationLogsOffsetData
			decodeData(t, resp, &data)

			assert.Equal(t, tt.kind, data.Kind)
			decoded, err := base64.StdEncoding.DecodeString(data.DataB64)
			require.NoError(t, err)
			assert.Equal(t, content, string(decoded))
		})
	}
}

// ---------------------------------------------------------------------------
// TIER 2: extractDiffstat test (Test 30)
// ---------------------------------------------------------------------------

func TestExtractDiffstat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "summary_line_only",
			input:    " 3 files changed, 42 insertions(+), 15 deletions(-)",
			expected: "3 files changed, 42 insertions(+), 15 deletions(-)",
		},
		{
			name:     "multi_line_stat",
			input:    "foo.go | 10 +++---\n bar.go | 5 ++\n 2 files changed",
			expected: "2 files changed",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, extractDiffstat(tt.input))
		})
	}
}

// ---------------------------------------------------------------------------
// TIER 2: Parameter parsing tests (Tests 32-35)
// ---------------------------------------------------------------------------

func TestParseListWorktreesParams(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/worktrees", nil)
		params, invalid := parseListWorktreesParams(req)
		require.Nil(t, invalid)
		assert.Equal(t, "present", params.State)
		assert.Equal(t, 100, params.Limit)
		assert.Empty(t, params.RepoID)
		assert.Empty(t, params.Cursor)
	})

	t.Run("overrides", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/worktrees?repo_id=r1&state=all&limit=50&cursor=abc", nil)
		params, invalid := parseListWorktreesParams(req)
		require.Nil(t, invalid)
		assert.Equal(t, "r1", params.RepoID)
		assert.Equal(t, "all", params.State)
		assert.Equal(t, 50, params.Limit)
		assert.Equal(t, "abc", params.Cursor)
	})

	t.Run("invalid_state", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/worktrees?state=bogus", nil)
		_, invalid := parseListWorktreesParams(req)
		require.NotNil(t, invalid)
		assert.Equal(t, "state", invalid.Param)
		assert.Equal(t, "bogus", invalid.Value)
		assert.Equal(t, validWorktreeStates, invalid.AllowedValues)
	})

	t.Run("invalid_limit", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/worktrees?limit=0", nil)
		_, invalid := parseListWorktreesParams(req)
		require.NotNil(t, invalid)
		assert.Equal(t, "limit", invalid.Param)
		assert.Equal(t, "0", invalid.Value)
		assert.Nil(t, invalid.AllowedValues)
	})
}

func TestParseListInvocationsParams(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations", nil)
		params, invalid := parseListInvocationsParams(req)
		require.Nil(t, invalid)
		assert.Equal(t, "all", params.State)
		assert.Equal(t, "all", params.Mode)
		assert.Equal(t, 100, params.Limit)
	})

	t.Run("overrides", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations?state=active&mode=headless&limit=25&worktree_ref=alpha", nil)
		params, invalid := parseListInvocationsParams(req)
		require.Nil(t, invalid)
		assert.Equal(t, "active", params.State)
		assert.Equal(t, "headless", params.Mode)
		assert.Equal(t, 25, params.Limit)
		assert.Equal(t, "alpha", params.WorktreeRef)
	})

	t.Run("invalid_mode", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations?mode=bogus", nil)
		_, invalid := parseListInvocationsParams(req)
		require.NotNil(t, invalid)
		assert.Equal(t, "mode", invalid.Param)
		assert.Equal(t, "bogus", invalid.Value)
		assert.Equal(t, validInvocationModes, invalid.AllowedValues)
	})

	t.Run("invalid_limit", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations?limit=0", nil)
		_, invalid := parseListInvocationsParams(req)
		require.NotNil(t, invalid)
		assert.Equal(t, "limit", invalid.Param)
		assert.Equal(t, "0", invalid.Value)
		assert.Nil(t, invalid.AllowedValues)
	})
}

func TestParseGetDiffParams(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations/inv-1/diff", nil)
		params, invalid := parseGetDiffParams(req)
		require.Nil(t, invalid)
		assert.True(t, params.IncludePatch)
		assert.Equal(t, 2097152, params.MaxPatchBytes)
		assert.True(t, params.IncludeUncommitted)
	})

	t.Run("overrides", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations/inv-1/diff?include_patch=false&max_patch_bytes=1000&include_uncommitted=0", nil)
		params, invalid := parseGetDiffParams(req)
		require.Nil(t, invalid)
		assert.False(t, params.IncludePatch)
		assert.Equal(t, 1000, params.MaxPatchBytes)
		assert.False(t, params.IncludeUncommitted)
	})

	t.Run("invalid_max_patch_bytes", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations/inv-1/diff?max_patch_bytes=0", nil)
		_, invalid := parseGetDiffParams(req)
		require.NotNil(t, invalid)
		assert.Equal(t, "max_patch_bytes", invalid.Param)
		assert.Equal(t, "0", invalid.Value)
	})
}

func TestParseGetLogsParams(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations/inv-1/logs", nil)
		params, invalid := parseGetLogsParams(req)
		require.Nil(t, invalid)
		assert.Equal(t, "raw", params.Kind)
		assert.Zero(t, params.Offset)
		assert.Equal(t, 65536, params.Limit)
	})

	t.Run("overrides", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations/inv-1/logs?kind=stderr&offset=128&limit=1024", nil)
		params, invalid := parseGetLogsParams(req)
		require.Nil(t, invalid)
		assert.Equal(t, "stderr", params.Kind)
		assert.Equal(t, int64(128), params.Offset)
		assert.Equal(t, 1024, params.Limit)
	})

	t.Run("invalid_offset", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations/inv-1/logs?offset=-1", nil)
		_, invalid := parseGetLogsParams(req)
		require.NotNil(t, invalid)
		assert.Equal(t, "offset", invalid.Param)
		assert.Equal(t, "-1", invalid.Value)
	})

	t.Run("invalid_limit", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/invocations/inv-1/logs?limit=0", nil)
		_, invalid := parseGetLogsParams(req)
		require.NotNil(t, invalid)
		assert.Equal(t, "limit", invalid.Param)
		assert.Equal(t, "0", invalid.Value)
	})
}

// ---------------------------------------------------------------------------
// TIER 2: Diff integration test (Test 31)
// ---------------------------------------------------------------------------

func TestHandleGetInvocationDiff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Cannot use t.Parallel() because HermeticGitEnv uses t.Setenv.
	testutil.HermeticGitEnv(t)

	cr := exec.NewRealRunner()
	ctx := t.Context()

	// 1. Create a real git repo with an initial commit
	repoDir := t.TempDir()
	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git init failed")

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "base.txt"), []byte("base content\n"), 0o644))
	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)
	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "initial commit"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)

	// Get the initial commit SHA (this will be our base_commit)
	result, err = cr.Run(ctx, "git", []string{"rev-parse", "HEAD"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	baseCommit := strings.TrimSpace(result.Stdout)

	// 2. Make additional commits on top
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "feature.txt"), []byte("new feature\n"), 0o644))
	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "add feature"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)

	// 3. Set up daemon + store with invocation pointing to this repo
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	repoID := "test-repo-diff"

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, cr, fs.NewRealFS(), configDir)
	now := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	srv.Clock = func() time.Time { return now }

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {RepoID: repoID, Paths: []string{repoDir}, LastSeenAt: "2026-02-05T12:00:00Z"},
		},
	}))

	invID := "inv-diff-1"
	_, err = st.EnsureInvocationDir(repoID, invID)
	require.NoError(t, err)
	require.NoError(t, st.WriteInvocationMeta(repoID, invID, &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invID,
		IntegrationWorktreeID: "wt-1",
		SandboxPath:           repoDir,
		SandboxBranch:         "main",
		BaseCommit:            baseCommit,
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             now.Add(-5 * time.Minute).Format(time.RFC3339),
		Status:                store.InvocationStatusRunning,
	}))

	env := &readTestEnv{Server: srv, Store: st, RepoID: repoID}

	// 4. Call the diff handler
	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/"+invID+"/diff?repo_id="+repoID)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data InvocationDiffData
	decodeData(t, resp, &data)

	// 5. Verify diff data
	assert.True(t, data.HasCommits, "expected has_commits=true")
	assert.Equal(t, baseCommit, data.BaseCommit)
	assert.NotEmpty(t, data.SandboxBranchTip)
	assert.NotEqual(t, baseCommit, data.SandboxBranchTip, "tip should differ from base")

	require.NotNil(t, data.CommittedRange)
	assert.NotEmpty(t, data.CommittedRange.Commits, "expected at least one commit in committed_range")
	assert.Equal(t, baseCommit, data.CommittedRange.From)
	assert.Equal(t, data.SandboxBranchTip, data.CommittedRange.To)

	// Verify first commit has valid SHA and summary
	assert.NotEmpty(t, data.CommittedRange.Commits[0].SHA)
	assert.Equal(t, "add feature", data.CommittedRange.Commits[0].Summary)
}

// ---------------------------------------------------------------------------
// TIER 3: Worktree ref filter (Test 37)
// ---------------------------------------------------------------------------

func TestHandleListInvocations_WorktreeRefFilter(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// Filter by worktree_ref=alpha → only inv-1 and inv-2 (both in wt-1 "alpha")
	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&worktree_ref=alpha")

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	var data ListInvocationsData
	decodeData(t, resp, &data)

	// inv-1 and inv-2 are in wt-1 ("alpha"), inv-3 is in wt-2 ("beta")
	assert.Len(t, data.Invocations, 2)
	var gotIDs []string
	for _, inv := range data.Invocations {
		gotIDs = append(gotIDs, inv.InvocationID)
	}
	assert.ElementsMatch(t, []string{"inv-1", "inv-2"}, gotIDs)
}

func TestHandleListInvocations_WorktreeRefFilter_NotFound(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// Filter by nonexistent worktree_ref → should return empty list, not all invocations
	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&worktree_ref=nonexistent")

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	var data ListInvocationsData
	decodeData(t, resp, &data)

	assert.Len(t, data.Invocations, 0, "nonexistent worktree_ref should return empty list")
}

func TestHandleListInvocations_WorktreeIDQueryParamIgnored(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&worktree_id=wt-1")

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	var data ListInvocationsData
	decodeData(t, resp, &data)

	assert.Len(t, data.Invocations, 3)
	assert.ElementsMatch(t, []string{"inv-1", "inv-2", "inv-3"}, []string{
		data.Invocations[0].InvocationID,
		data.Invocations[1].InvocationID,
		data.Invocations[2].InvocationID,
	})
}

// ---------------------------------------------------------------------------
// TIER 3: Handler pagination (Tests 38-40)
// ---------------------------------------------------------------------------

func TestHandleGetInvocationCheckpoints_Pagination(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// Seed 5 checkpoints
	require.NoError(t, os.MkdirAll(env.Store.InvocationDir(env.RepoID, "inv-1"), 0o700))

	cpFile := &checkpoint.CheckpointsFile{
		SchemaVersion: "1.0",
		Checkpoints: []checkpoint.Checkpoint{
			{ID: 1, CreatedAt: "2026-02-05T11:51:00Z", SnapshotCommit: "aaa", IncludesUntracked: true},
			{ID: 2, CreatedAt: "2026-02-05T11:52:00Z", SnapshotCommit: "bbb", IncludesUntracked: true},
			{ID: 3, CreatedAt: "2026-02-05T11:53:00Z", SnapshotCommit: "ccc", IncludesUntracked: true},
			{ID: 4, CreatedAt: "2026-02-05T11:54:00Z", SnapshotCommit: "ddd", IncludesUntracked: true},
			{ID: 5, CreatedAt: "2026-02-05T11:55:00Z", SnapshotCommit: "eee", IncludesUntracked: true},
		},
	}
	cpData, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(env.Store.InvocationCheckpointsPath(env.RepoID, "inv-1"), cpData, 0o644))

	// First page: limit=2
	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checkpoints?repo_id="+env.RepoID+"&limit=2")
	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	var data1 ListCheckpointsData
	decodeData(t, resp, &data1)

	assert.Len(t, data1.Checkpoints, 2)
	assert.Equal(t, 5, data1.Checkpoints[0].ID)
	assert.Equal(t, 4, data1.Checkpoints[1].ID)
	assert.NotEmpty(t, data1.NextCursor)

	// Second page: exclusive cursor → [3, 2]
	w2 := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checkpoints?repo_id="+env.RepoID+"&limit=2&cursor="+data1.NextCursor)
	assert.Equal(t, http.StatusOK, w2.Code)

	resp2 := decodeAPIResponse(t, w2)
	var data2 ListCheckpointsData
	decodeData(t, resp2, &data2)

	assert.Len(t, data2.Checkpoints, 2)
	assert.Equal(t, 3, data2.Checkpoints[0].ID)
	assert.Equal(t, 2, data2.Checkpoints[1].ID)
}

func TestHandleListWorktrees_Pagination(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	repoID := "test-repo-wt-page"

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	srv.Clock = func() time.Time { return time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC) }

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {RepoID: repoID, Paths: []string{"/tmp/repo"}, LastSeenAt: "2026-02-05T12:00:00Z"},
		},
	}))

	// Seed 5 worktrees
	for i := range 5 {
		wtID := "wt-" + string(rune('a'+i))
		_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
		require.NoError(t, err)
		require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, &store.IntegrationWorktreeMeta{
			SchemaVersion: "1.0",
			WorktreeID:    wtID,
			Name:          "name-" + string(rune('a'+i)),
			RepoID:        repoID,
			Branch:        "agency/" + wtID,
			ParentBranch:  "main",
			TreePath:      "/tmp/wt/" + wtID,
			CreatedAt:     "2026-02-05T10:00:00Z",
			LastUsedAt:    time.Date(2026, 2, 5, 11, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			State:         store.WorktreeStatePresent,
		}))
	}

	env := &readTestEnv{Server: srv, Store: st, RepoID: repoID}

	// First page
	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id="+repoID+"&limit=2")
	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	var data ListWorktreesData
	decodeData(t, resp, &data)

	assert.Len(t, data.Worktrees, 2)
	assert.NotEmpty(t, data.NextCursor)

	// Second page
	w2 := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id="+repoID+"&limit=2&cursor="+data.NextCursor)
	assert.Equal(t, http.StatusOK, w2.Code)

	resp2 := decodeAPIResponse(t, w2)
	var data2 ListWorktreesData
	decodeData(t, resp2, &data2)

	assert.Len(t, data2.Worktrees, 2)
}

func TestHandleListInvocations_Pagination(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	repoID := "test-repo-inv-page"

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	now := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	srv.Clock = func() time.Time { return now }

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {RepoID: repoID, Paths: []string{"/tmp/repo"}, LastSeenAt: "2026-02-05T12:00:00Z"},
		},
	}))

	// Seed 5 invocations
	for i := range 5 {
		invID := "inv-" + string(rune('a'+i))
		_, err := st.EnsureInvocationDir(repoID, invID)
		require.NoError(t, err)
		require.NoError(t, st.WriteInvocationMeta(repoID, invID, &store.InvocationMeta{
			SchemaVersion:         "1.0",
			InvocationID:          invID,
			IntegrationWorktreeID: "wt-1",
			SandboxPath:           "/tmp/sandbox/" + invID,
			SandboxBranch:         "agency/sandbox-" + invID,
			BaseCommit:            "abc",
			Runner:                "claude-code",
			Mode:                  store.RunnerModeHeadless,
			StartedAt:             now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			Status:                store.InvocationStatusRunning,
		}))
	}

	env := &readTestEnv{Server: srv, Store: st, RepoID: repoID}

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+repoID+"&limit=2")
	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	var data ListInvocationsData
	decodeData(t, resp, &data)

	assert.Len(t, data.Invocations, 2)
	assert.NotEmpty(t, data.NextCursor)

	// Second page
	w2 := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+repoID+"&limit=2&cursor="+data.NextCursor)
	resp2 := decodeAPIResponse(t, w2)
	var data2 ListInvocationsData
	decodeData(t, resp2, &data2)

	assert.Len(t, data2.Invocations, 2)
}

// ---------------------------------------------------------------------------
// TIER 3: Routing tests (Tests 41-42)
// ---------------------------------------------------------------------------

func TestInvocationsRouting_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"POST_to_list_invocations", http.MethodPost, "/invocations/"},
		{"POST_to_get_invocation", http.MethodPost, "/invocations/inv-1"},
		{"POST_to_diff", http.MethodPost, "/invocations/inv-1/diff"},
		{"POST_to_logs", http.MethodPost, "/invocations/inv-1/logs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := env.doInvocationRequest(t, tt.method, tt.path+"?repo_id="+env.RepoID)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		})
	}
}

func TestCheckpointsRouting(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	t.Run("GET_list_checkpoints", func(t *testing.T) {
		t.Parallel()
		w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checkpoints?repo_id="+env.RepoID)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("GET_checkpoints_unknown_sub_action", func(t *testing.T) {
		t.Parallel()
		w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checkpoints/unknown?repo_id="+env.RepoID)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// ---------------------------------------------------------------------------
// PR-B: Offset-based logs tests
// ---------------------------------------------------------------------------

func TestReadLogFileAtOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		offset    int64
		limit     int
		wantData  string // expected decoded content; empty string = check DataB64 == ""
		wantNext  int64
		wantTotal int64
		wantErr   bool
		noFile    bool // if true, skip creating the log file
	}{
		{
			name:      "full_file",
			offset:    0,
			limit:     65536,
			wantData:  "abcdef",
			wantNext:  6,
			wantTotal: 6,
		},
		{
			name:      "partial_offset_0_limit_2",
			offset:    0,
			limit:     2,
			wantData:  "ab",
			wantNext:  2,
			wantTotal: 6,
		},
		{
			name:      "partial_offset_2_limit_2",
			offset:    2,
			limit:     2,
			wantData:  "cd",
			wantNext:  4,
			wantTotal: 6,
		},
		{
			name:      "beyond_eof",
			offset:    100,
			limit:     65536,
			wantData:  "",
			wantNext:  6,
			wantTotal: 6,
		},
		{
			name:      "limit_clamped_to_max",
			offset:    0,
			limit:     MaxLogChunk + 100,
			wantData:  "abcdef",
			wantNext:  6,
			wantTotal: 6,
		},
		{
			name:    "file_not_found",
			offset:  0,
			limit:   65536,
			noFile:  true,
			wantData: "",
			wantNext: 0,
			wantTotal: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			logPath := filepath.Join(tmpDir, "test.log")

			if !tt.noFile {
				require.NoError(t, os.WriteFile(logPath, []byte("abcdef"), 0o644))
			}

			st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
			srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

			data, err := srv.readLogFileAtOffset(logPath, tt.offset, tt.limit)
			require.NoError(t, err)

			assert.Equal(t, tt.wantNext, data.NextOffset)
			assert.Equal(t, tt.wantTotal, data.TotalBytes)

			if tt.wantData == "" {
				assert.Equal(t, "", data.DataB64)
			} else {
				decoded, decErr := base64.StdEncoding.DecodeString(data.DataB64)
				require.NoError(t, decErr)
				assert.Equal(t, tt.wantData, string(decoded))
			}
		})
	}
}

func TestHandleGetInvocationLogs_OffsetRead(t *testing.T) {
	t.Parallel()

	t.Run("offset_read_happy_path", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		// Seed a raw log file for inv-1
		logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
		require.NoError(t, os.MkdirAll(logsDir, 0o700))
		logContent := "abcdef"
		require.NoError(t, os.WriteFile(filepath.Join(logsDir, "raw.jsonl"), []byte(logContent), 0o644))

		w := env.doInvocationRequest(t, http.MethodGet,
			"/invocations/inv-1/logs?repo_id="+env.RepoID+"&offset=0&limit=2")

		assert.Equal(t, http.StatusOK, w.Code)

		resp := decodeAPIResponse(t, w)
		assert.True(t, resp.OK)

		var data InvocationLogsOffsetData
		decodeData(t, resp, &data)

		assert.Equal(t, "raw", data.Kind)
		assert.Equal(t, int64(2), data.NextOffset)
		assert.Equal(t, int64(6), data.TotalBytes)
		assert.NotEmpty(t, data.DataB64)
	})

	t.Run("offset_read_second_chunk", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
		require.NoError(t, os.MkdirAll(logsDir, 0o700))
		logContent := "abcdef"
		require.NoError(t, os.WriteFile(filepath.Join(logsDir, "raw.jsonl"), []byte(logContent), 0o644))

		w := env.doInvocationRequest(t, http.MethodGet,
			"/invocations/inv-1/logs?repo_id="+env.RepoID+"&offset=2&limit=2")

		assert.Equal(t, http.StatusOK, w.Code)

		resp := decodeAPIResponse(t, w)
		var data InvocationLogsOffsetData
		decodeData(t, resp, &data)

		decoded, decErr := base64.StdEncoding.DecodeString(data.DataB64)
		require.NoError(t, decErr)
		assert.Equal(t, "cd", string(decoded))
		assert.Equal(t, int64(4), data.NextOffset)
	})

	t.Run("offset_beyond_eof", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
		require.NoError(t, os.MkdirAll(logsDir, 0o700))
		logContent := "abcdef"
		require.NoError(t, os.WriteFile(filepath.Join(logsDir, "raw.jsonl"), []byte(logContent), 0o644))

		w := env.doInvocationRequest(t, http.MethodGet,
			"/invocations/inv-1/logs?repo_id="+env.RepoID+"&offset=100&limit=65536")

		assert.Equal(t, http.StatusOK, w.Code)

		resp := decodeAPIResponse(t, w)
		var data InvocationLogsOffsetData
		decodeData(t, resp, &data)

		assert.Equal(t, "", data.DataB64)
		assert.Equal(t, int64(6), data.NextOffset)
	})

	t.Run("offset_negative_returns_error", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		// Seed log file so we get past resolution
		logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
		require.NoError(t, os.MkdirAll(logsDir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(logsDir, "raw.jsonl"), []byte("x"), 0o644))

		w := env.doInvocationRequest(t, http.MethodGet,
			"/invocations/inv-1/logs?repo_id="+env.RepoID+"&offset=-1&limit=65536")

		assert.Equal(t, http.StatusBadRequest, w.Code)

		resp := decodeAPIResponse(t, w)
		assert.False(t, resp.OK)
		assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)
	})

	t.Run("offset_missing_file_returns_empty", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		// inv-2 has no log files; offset mode should now return an empty log payload.
		w := env.doInvocationRequest(t, http.MethodGet,
			"/invocations/inv-2/logs?repo_id="+env.RepoID+"&offset=0&limit=65536")

		assert.Equal(t, http.StatusOK, w.Code)

		resp := decodeAPIResponse(t, w)
		assert.True(t, resp.OK)

		var data InvocationLogsOffsetData
		decodeData(t, resp, &data)
		assert.Equal(t, "raw", data.Kind)
		assert.Empty(t, data.DataB64)
		assert.Zero(t, data.NextOffset)
		assert.Zero(t, data.TotalBytes)
	})

	t.Run("offset_stream_kind_missing_file_returns_empty", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		w := env.doInvocationRequest(t, http.MethodGet,
			"/invocations/inv-1/logs?repo_id="+env.RepoID+"&kind=stream&offset=0&limit=65536")

		assert.Equal(t, http.StatusOK, w.Code)

		resp := decodeAPIResponse(t, w)
		assert.True(t, resp.OK)

		var data InvocationLogsOffsetData
		decodeData(t, resp, &data)
		assert.Equal(t, "stream", data.Kind)
		assert.Empty(t, data.DataB64)
		assert.Zero(t, data.NextOffset)
		assert.Zero(t, data.TotalBytes)
	})

	t.Run("offset_invalid_limit_returns_error", func(t *testing.T) {
		t.Parallel()
		env := setupReadTestEnv(t)

		// Seed log file so we get past resolution
		logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
		require.NoError(t, os.MkdirAll(logsDir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(logsDir, "raw.jsonl"), []byte("x"), 0o644))

		w := env.doInvocationRequest(t, http.MethodGet,
			"/invocations/inv-1/logs?repo_id="+env.RepoID+"&offset=0&limit=0")

		assert.Equal(t, http.StatusBadRequest, w.Code)

		resp := decodeAPIResponse(t, w)
		assert.False(t, resp.OK)
		assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)
	})
}

// ---------------------------------------------------------------------------
// S2 PR-01: Daemon Read API Contract Hardening — Acceptance Tests
// ---------------------------------------------------------------------------

// decodeDetails extracts and decodes the Details field from an APIResponse.
func decodeDetails(t *testing.T, resp APIResponse, target interface{}) {
	t.Helper()
	dataBytes, err := json.Marshal(resp.Details)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(dataBytes, target))
}

func TestHandleListWorktrees_InvalidStateReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id="+env.RepoID+"&state=bogus")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)

	var details InvalidQueryArgumentDetails
	decodeDetails(t, resp, &details)
	assert.Equal(t, "state", details.Param)
	assert.Equal(t, "bogus", details.Value)
	assert.Equal(t, []string{"present", "archived", "all"}, details.AllowedValues)
}

func TestHandleListWorktrees_InvalidLimitReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id="+env.RepoID+"&limit=0")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)
}

func TestHandleListInvocations_InvalidStateReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&state=bogus")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)

	var details InvalidQueryArgumentDetails
	decodeDetails(t, resp, &details)
	assert.Equal(t, "state", details.Param)
	assert.Equal(t, "bogus", details.Value)
	assert.Equal(t, []string{"active", "finished", "all"}, details.AllowedValues)
}

func TestHandleListInvocations_InvalidLimitReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&limit=0")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)
}

func TestHandleListInvocations_InvalidModeReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&mode=bogus")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)

	var details InvalidQueryArgumentDetails
	decodeDetails(t, resp, &details)
	assert.Equal(t, "mode", details.Param)
	assert.Equal(t, "bogus", details.Value)
	assert.Equal(t, []string{"headed", "headless", "all"}, details.AllowedValues)
}

func TestHandleListInvocations_InvalidFiltersFailClosed_DeterministicPrecedence(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&state=badstate&mode=badmode")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)

	// Canonical validation order: state then mode; first invalid wins.
	var details InvalidQueryArgumentDetails
	decodeDetails(t, resp, &details)
	assert.Equal(t, "state", details.Param)
	assert.Equal(t, "badstate", details.Value)
	assert.Equal(t, []string{"active", "finished", "all"}, details.AllowedValues)
}

func TestHandleListWorktrees_InvalidStateFailsBeforeRepoIndexLookup(t *testing.T) {
	t.Parallel()

	// Validation should fail before any repo resolution work happens.
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	srv.Clock = func() time.Time { return time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC) }

	env := &readTestEnv{Server: srv, Store: st, RepoID: ""}

	// No repo_id, no repo index — but invalid state should fail first.
	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?state=bogus")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode,
		"enum validation must run before repo index enumeration")
}

func TestHandleListInvocations_InvalidStateFailsBeforeRepoIndexLookup(t *testing.T) {
	t.Parallel()

	// Validation should fail before any repo resolution work happens.
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	srv.Clock = func() time.Time { return time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC) }

	env := &readTestEnv{Server: srv, Store: st, RepoID: ""}

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?state=bogus")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode,
		"enum validation must run before repo index enumeration")
}

func TestHandleListWorktrees_Limit500Accepted(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees?repo_id="+env.RepoID+"&limit=500")

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)
}

func TestHandleListInvocations_Limit500Accepted(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/?repo_id="+env.RepoID+"&limit=500")

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data ListInvocationsData
	decodeData(t, resp, &data)
	assert.Len(t, data.Invocations, 3)
}

func TestHandleGetWorktree_AmbiguousReturnsCandidates(t *testing.T) {
	t.Parallel()

	// Set up a repo with two worktrees sharing an ID prefix to trigger ambiguous resolution.
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	repoID := "test-repo-ambig-wt"

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	srv.Clock = func() time.Time { return time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC) }

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {RepoID: repoID, Paths: []string{"/tmp/repo"}, LastSeenAt: "2026-02-05T12:00:00Z"},
		},
	}))

	// Two worktrees with same name "alpha" in same repo
	for _, wtID := range []string{"wt-alpha-1", "wt-alpha-2"} {
		_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
		require.NoError(t, err)
		require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, &store.IntegrationWorktreeMeta{
			SchemaVersion: "1.0",
			WorktreeID:    wtID,
			Name:          "alpha",
			RepoID:        repoID,
			Branch:        "agency/" + wtID,
			ParentBranch:  "main",
			TreePath:      "/tmp/wt/" + wtID,
			CreatedAt:     "2026-02-05T10:00:00Z",
			State:         store.WorktreeStatePresent,
		}))
	}

	env := &readTestEnv{Server: srv, Store: st, RepoID: repoID}

	w := env.doWorktreeRequest(t, http.MethodGet, "/worktrees/alpha?repo_id="+repoID)

	assert.Equal(t, http.StatusConflict, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_WORKTREE_ID_AMBIGUOUS", resp.ErrorCode)

	// Verify candidates are present in details
	require.NotNil(t, resp.Details, "ambiguous error must include details with candidates")
	detailsMap, err := json.Marshal(resp.Details)
	require.NoError(t, err)
	var details AmbiguousDetails
	require.NoError(t, json.Unmarshal(detailsMap, &details))
	assert.Len(t, details.Candidates, 2)
}

func TestHandleGetInvocation_AmbiguousReturnsCandidates(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	repoID := "test-repo-ambig-inv"

	now := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time { return now })
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	srv.Clock = func() time.Time { return now }

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {RepoID: repoID, Paths: []string{"/tmp/repo"}, LastSeenAt: "2026-02-05T12:00:00Z"},
		},
	}))

	// Two invocations with same name "shared-run" in same repo
	for _, invID := range []string{"inv-shared-1", "inv-shared-2"} {
		_, err := st.EnsureInvocationDir(repoID, invID)
		require.NoError(t, err)
		require.NoError(t, st.WriteInvocationMeta(repoID, invID, &store.InvocationMeta{
			SchemaVersion:         "1.0",
			InvocationID:          invID,
			InvocationName:        "shared-run",
			IntegrationWorktreeID: "wt-1",
			SandboxPath:           "/tmp/sandbox/" + invID,
			SandboxBranch:         "agency/sandbox-" + invID,
			BaseCommit:            "abc",
			Runner:                "claude-code",
			Mode:                  store.RunnerModeHeadless,
			StartedAt:             now.Add(-5 * time.Minute).Format(time.RFC3339),
			Status:                store.InvocationStatusRunning,
		}))
	}

	env := &readTestEnv{Server: srv, Store: st, RepoID: repoID}

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/shared-run?repo_id="+repoID)

	assert.Equal(t, http.StatusConflict, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVOCATION_ID_AMBIGUOUS", resp.ErrorCode)

	require.NotNil(t, resp.Details, "ambiguous error must include details with candidates")
	detailsMap, err := json.Marshal(resp.Details)
	require.NoError(t, err)
	var details AmbiguousDetails
	require.NoError(t, json.Unmarshal(detailsMap, &details))
	assert.Len(t, details.Candidates, 2)
}

func TestWorktreesRouting_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	tests := []struct {
		name string
		path string
	}{
		{"POST_to_list_worktrees", "/worktrees?repo_id=" + env.RepoID},
		{"POST_to_get_worktree", "/worktrees/wt-1?repo_id=" + env.RepoID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := env.doWorktreeRequest(t, http.MethodPost, tt.path)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

			// Router-level 405 uses writeError (not writeAPIError), so envelope
			// includes ok+error_code but not request_id. Assert the error shape.
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
			assert.Equal(t, false, body["ok"])
			assert.Equal(t, "E_METHOD_NOT_ALLOWED", body["error_code"])
		})
	}
}

// ---------------------------------------------------------------------------
// End S2 PR-01 acceptance tests
// ---------------------------------------------------------------------------

func TestHandleGetInvocationTimeline_UnifiedTypedEntries(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	promptPath := env.Store.InvocationPromptPath(env.RepoID, "inv-1")
	require.NoError(t, os.WriteFile(promptPath, []byte("seed prompt: fix flaky test"), 0o600))
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
	}))

	logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(
		env.Store.InvocationRawLogPath(env.RepoID, "inv-1"),
		[]byte("{\"type\":\"raw\"}\n{\"type\":\"raw2\"}\n"),
		0o644,
	))

	streamEvents := []map[string]any{
		{
			"schema_version": "1.0",
			"seq":            1,
			"timestamp":      "2026-02-05T11:50:10Z",
			"invocation_id":  "inv-1",
			"runner":         "claude-code",
			"kind":           "message",
			"data": map[string]any{
				"role":         "assistant",
				"text":         "looking now",
				"has_tool_use": true,
			},
		},
		{
			"schema_version": "1.0",
			"seq":            2,
			"timestamp":      "2026-02-05T11:50:20Z",
			"invocation_id":  "inv-1",
			"runner":         "claude-code",
			"kind":           "tool_start",
			"data": map[string]any{
				"name":    "shell",
				"command": "go test ./...",
			},
		},
		{
			"schema_version": "1.0",
			"seq":            3,
			"timestamp":      "2026-02-05T11:50:30Z",
			"invocation_id":  "inv-1",
			"runner":         "claude-code",
			"kind":           "tool_end",
			"data": map[string]any{
				"name":      "shell",
				"command":   "go test ./...",
				"exit_code": 0,
			},
		},
	}

	streamPath := env.Store.InvocationStreamLogPath(env.RepoID, "inv-1")
	streamFile, err := os.OpenFile(streamPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	for _, ev := range streamEvents {
		line, mErr := json.Marshal(ev)
		require.NoError(t, mErr)
		_, _ = streamFile.Write(append(line, '\n'))
	}
	require.NoError(t, streamFile.Close())

	invocationEventsPath := env.Store.InvocationEventsPath(env.RepoID, "inv-1")
	require.NoError(t, os.WriteFile(invocationEventsPath, []byte(
		`{"schema_version":"1.0","seq":4,"timestamp":"2026-02-05T11:50:40Z","invocation_id":"inv-1","kind":"agency.checkpoint_applied","data":{"checkpoint_id":2}}`+"\n",
	), 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/timeline?repo_id="+env.RepoID)
	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.True(t, resp.OK)

	var data struct {
		Entries []struct {
			EntryID string         `json:"entry_id"`
			Kind    string         `json:"kind"`
			Data    map[string]any `json:"data"`
		} `json:"entries"`
		NextCursor string `json:"next_cursor"`
	}
	decodeData(t, resp, &data)

	require.NotEmpty(t, data.Entries)
	seenKinds := map[string]bool{}
	toolUseEntries := make([]map[string]any, 0, 2)
	for _, entry := range data.Entries {
		seenKinds[entry.Kind] = true
		if entry.Kind == "tool_use" {
			toolUseEntries = append(toolUseEntries, entry.Data)
		}
	}
	assert.True(t, seenKinds["prompt_seed"], "timeline must include prompt seed context")
	assert.True(t, seenKinds["message"], "timeline must include assistant/user messages")
	assert.True(t, seenKinds["tool_use"], "timeline must include tool-use activity")
	assert.True(t, seenKinds["raw_log_coverage"], "timeline must include raw-log coverage marker")
	require.Len(t, toolUseEntries, 2, "tool_start and tool_end should remain distinct tool_use entries")
	hasInProgress := false
	hasCompleted := false
	for _, toolUseEntry := range toolUseEntries {
		assert.Equal(t, "go test ./...", toolUseEntry["command"])
		if inProgress, ok := toolUseEntry["in_progress"].(bool); ok && inProgress {
			hasInProgress = true
		}
		if exitCode, ok := toolUseEntry["exit_code"].(float64); ok && exitCode == 0 {
			hasCompleted = true
		}
	}
	assert.True(t, hasInProgress, "normalized tool_start row must expose in_progress=true")
	assert.True(t, hasCompleted, "normalized tool_end row must preserve exit_code")
}

func TestHandleGetInvocationTimeline_ReplaySupportsFiveMBLinesAcrossSources(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
	require.NoError(t, os.MkdirAll(logsDir, 0o700))

	largeText := strings.Repeat("x", 5*1024*1024)

	streamEvent := map[string]any{
		"schema_version": "1.0",
		"seq":            1,
		"timestamp":      "2026-02-05T11:50:10Z",
		"invocation_id":  "inv-1",
		"runner":         "claude-code",
		"kind":           "message",
		"data": map[string]any{
			"role": "assistant",
			"text": largeText,
		},
	}
	streamLine, err := json.Marshal(streamEvent)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		env.Store.InvocationStreamLogPath(env.RepoID, "inv-1"),
		append(streamLine, '\n'),
		0o644,
	))

	invocationEvent := map[string]any{
		"schema_version": "1.0",
		"seq":            1,
		"timestamp":      "2026-02-05T11:50:20Z",
		"invocation_id":  "inv-1",
		"kind":           "agency.followup_prompt",
		"data": map[string]any{
			"text": largeText,
		},
	}
	eventLine, err := json.Marshal(invocationEvent)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		env.Store.InvocationEventsPath(env.RepoID, "inv-1"),
		append(eventLine, '\n'),
		0o644,
	))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/timeline?repo_id="+env.RepoID+"&limit=200")
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data struct {
		Entries []struct {
			EntryID string `json:"entry_id"`
		} `json:"entries"`
	}
	decodeData(t, resp, &data)

	entryIDs := make([]string, 0, len(data.Entries))
	for _, entry := range data.Entries {
		entryIDs = append(entryIDs, entry.EntryID)
	}
	assert.Contains(t, entryIDs, "stream:1", "timeline replay must preserve 5MB stream lines accepted by live capture")
	assert.Contains(t, entryIDs, "inv_event:1:agency.followup_prompt", "timeline replay must preserve 5MB invocation-event lines accepted by live capture")
}

func TestHandleGetInvocationTimeline_RejectsCorruptTimelineData(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		streamLine string
		eventLine  string
	}{
		{
			name:       "unsupported_schema_version",
			streamLine: `{"schema_version":"2.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"inv-1","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"unsupported schema"}}` + "\n",
			eventLine:  `{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"inv-1","kind":"agency.followup_prompt","data":{"text":"ok"}}` + "\n",
		},
		{
			name:       "malformed_json",
			streamLine: `{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"inv-1","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"ok"}}` + "\n",
			eventLine:  `{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"inv-1","kind":"agency.followup_prompt","data":{"text":"broken"` + "\n",
		},
		{
			name:       "missing_kind",
			streamLine: `{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"inv-1","runner":"claude-code","kind":"","data":{"role":"assistant","text":"missing-kind"}}` + "\n",
			eventLine:  `{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"inv-1","kind":"","event":"","data":{"text":"missing-kind-and-event"}}` + "\n",
		},
		{
			name:       "oversized_line",
			streamLine: strings.Repeat("x", 9*1024*1024) + "\n",
			eventLine:  strings.Repeat("x", 9*1024*1024) + "\n",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := setupReadTestEnv(t)

			logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
			require.NoError(t, os.MkdirAll(logsDir, 0o700))
			require.NoError(t, os.WriteFile(env.Store.InvocationStreamLogPath(env.RepoID, "inv-1"), []byte(tc.streamLine), 0o644))
			require.NoError(t, os.WriteFile(env.Store.InvocationEventsPath(env.RepoID, "inv-1"), []byte(tc.eventLine), 0o644))

			w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/timeline?repo_id="+env.RepoID)
			assert.Equal(t, http.StatusOK, w.Code)

			resp := decodeAPIResponse(t, w)
			assert.True(t, resp.OK)

			var data struct {
				Entries []struct {
					Kind string `json:"kind"`
				} `json:"entries"`
			}
			decodeData(t, resp, &data)
			require.NotEmpty(t, data.Entries)

			sawParseError := false
			for _, entry := range data.Entries {
				if entry.Kind == "parse_error" {
					sawParseError = true
					break
				}
			}
			assert.True(t, sawParseError, "tolerant replay must preserve a parse_error diagnostic")
		})
	}
}

func TestHandleGetInvocationTimeline_InvocationEventIDsStayUniqueAcrossLineAndSeqFallback(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	eventLines := strings.Join([]string{
		`{"schema_version":"1.0","seq":0,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"inv-1","kind":"agency.followup_prompt","data":{"text":"line-based-id"}}`,
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:21Z","invocation_id":"inv-1","kind":"agency.followup_prompt","data":{"text":"seq-based-id"}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(env.Store.InvocationEventsPath(env.RepoID, "inv-1"), []byte(eventLines), 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/timeline?repo_id="+env.RepoID+"&limit=100")
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data struct {
		Entries []struct {
			EntryID string `json:"entry_id"`
		} `json:"entries"`
	}
	decodeData(t, resp, &data)

	entryIDs := make([]string, 0, len(data.Entries))
	for _, entry := range data.Entries {
		entryIDs = append(entryIDs, entry.EntryID)
	}

	assert.Contains(t, entryIDs, "inv_event:line:1:agency.followup_prompt")
	assert.Contains(t, entryIDs, "inv_event:1:agency.followup_prompt")
}

func TestHandleGetInvocationTimeline_PaginationStableContinuation(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	promptPath := env.Store.InvocationPromptPath(env.RepoID, "inv-1")
	require.NoError(t, os.WriteFile(promptPath, []byte("seed prompt"), 0o600))
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
	}))

	logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(env.Store.InvocationRawLogPath(env.RepoID, "inv-1"), []byte("raw-log\n"), 0o644))

	streamPath := env.Store.InvocationStreamLogPath(env.RepoID, "inv-1")
	streamFile, err := os.OpenFile(streamPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	baseTS := time.Date(2026, 2, 5, 11, 51, 0, 0, time.UTC)
	for i := 1; i <= 6; i++ {
		ev := map[string]any{
			"schema_version": "1.0",
			"seq":            i,
			"timestamp":      baseTS.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			"invocation_id":  "inv-1",
			"runner":         "claude-code",
			"kind":           "message",
			"data": map[string]any{
				"role": "assistant",
				"text": fmt.Sprintf("message-%d", i),
			},
		}
		line, mErr := json.Marshal(ev)
		require.NoError(t, mErr)
		_, _ = streamFile.Write(append(line, '\n'))
	}
	require.NoError(t, streamFile.Close())

	getPage := func(cursor string) ([]string, string) {
		path := "/invocations/inv-1/timeline?repo_id=" + env.RepoID + "&limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		w := env.doInvocationRequest(t, http.MethodGet, path)
		assert.Equal(t, http.StatusOK, w.Code)
		resp := decodeAPIResponse(t, w)
		assert.True(t, resp.OK)

		var data struct {
			Entries []struct {
				EntryID string `json:"entry_id"`
			} `json:"entries"`
			NextCursor string `json:"next_cursor"`
		}
		decodeData(t, resp, &data)

		ids := make([]string, 0, len(data.Entries))
		for _, entry := range data.Entries {
			ids = append(ids, entry.EntryID)
		}
		return ids, data.NextCursor
	}

	firstIDs, c1 := getPage("")
	secondIDs, c2 := getPage(c1)
	thirdIDs, c3 := getPage(c2)
	fourthIDs, _ := getPage(c3)

	paged := append(append(append(firstIDs, secondIDs...), thirdIDs...), fourthIDs...)
	require.NotEmpty(t, paged)

	seen := map[string]bool{}
	for _, id := range paged {
		assert.False(t, seen[id], "pagination must not duplicate entries")
		seen[id] = true
	}

	wAll := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/timeline?repo_id="+env.RepoID+"&limit=100")
	assert.Equal(t, http.StatusOK, wAll.Code)
	respAll := decodeAPIResponse(t, wAll)
	assert.True(t, respAll.OK)

	var allData struct {
		Entries []struct {
			EntryID string `json:"entry_id"`
		} `json:"entries"`
	}
	decodeData(t, respAll, &allData)

	allIDs := make([]string, 0, len(allData.Entries))
	for _, entry := range allData.Entries {
		allIDs = append(allIDs, entry.EntryID)
	}
	assert.Equal(t, allIDs, paged, "cursor pagination must provide deterministic continuation without skip/dup drift")
}

func TestHandleGetInvocationTimeline_InvalidLimitReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	tests := []struct {
		name  string
		limit string
	}{
		{name: "zero", limit: "0"},
		{name: "too_large", limit: "501"},
		{name: "non_numeric", limit: "abc"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := env.doInvocationRequest(t, http.MethodGet,
				"/invocations/inv-1/timeline?repo_id="+env.RepoID+"&limit="+tc.limit)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			resp := decodeAPIResponse(t, w)
			assert.False(t, resp.OK)
			assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)
		})
	}
}

func TestHandleGetInvocationTimeline_OrderDescReturnsReversedEntries(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// Seed 4 stream messages with ascending timestamps.
	logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	streamPath := env.Store.InvocationStreamLogPath(env.RepoID, "inv-1")
	streamFile, err := os.OpenFile(streamPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	baseTS := time.Date(2026, 2, 5, 11, 51, 0, 0, time.UTC)
	for i := 1; i <= 4; i++ {
		ev := map[string]any{
			"schema_version": "1.0",
			"seq":            i,
			"timestamp":      baseTS.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			"invocation_id":  "inv-1",
			"runner":         "claude-code",
			"kind":           "message",
			"data": map[string]any{
				"role": "assistant",
				"text": fmt.Sprintf("msg-%d", i),
			},
		}
		line, mErr := json.Marshal(ev)
		require.NoError(t, mErr)
		_, _ = streamFile.Write(append(line, '\n'))
	}
	require.NoError(t, streamFile.Close())

	// Ascending (default) — first entry should be stream:1.
	wAsc := env.doInvocationRequest(t, http.MethodGet,
		"/invocations/inv-1/timeline?repo_id="+env.RepoID+"&limit=500")
	assert.Equal(t, http.StatusOK, wAsc.Code)
	respAsc := decodeAPIResponse(t, wAsc)
	var ascData InvocationTimelineData
	decodeData(t, respAsc, &ascData)
	require.NotEmpty(t, ascData.Entries)

	// Descending — entries must appear in reverse order.
	wDesc := env.doInvocationRequest(t, http.MethodGet,
		"/invocations/inv-1/timeline?repo_id="+env.RepoID+"&limit=500&order=desc")
	assert.Equal(t, http.StatusOK, wDesc.Code)
	respDesc := decodeAPIResponse(t, wDesc)
	var descData InvocationTimelineData
	decodeData(t, respDesc, &descData)
	require.Equal(t, len(ascData.Entries), len(descData.Entries))

	// Verify descending is exact reverse of ascending.
	for i, entry := range descData.Entries {
		assert.Equal(t, ascData.Entries[len(ascData.Entries)-1-i].EntryID, entry.EntryID,
			"desc entry %d should match reverse of asc", i)
	}
}

func TestHandleGetInvocationTimeline_OrderDescLimit1ReturnsLastEntry(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	// Seed stream messages.
	logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	streamPath := env.Store.InvocationStreamLogPath(env.RepoID, "inv-1")
	streamFile, err := os.OpenFile(streamPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	baseTS := time.Date(2026, 2, 5, 11, 51, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		ev := map[string]any{
			"schema_version": "1.0",
			"seq":            i,
			"timestamp":      baseTS.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			"invocation_id":  "inv-1",
			"runner":         "claude-code",
			"kind":           "message",
			"data": map[string]any{
				"role": "assistant",
				"text": fmt.Sprintf("msg-%d", i),
			},
		}
		line, mErr := json.Marshal(ev)
		require.NoError(t, mErr)
		_, _ = streamFile.Write(append(line, '\n'))
	}
	require.NoError(t, streamFile.Close())

	// Get last entry with order=desc&limit=1.
	w := env.doInvocationRequest(t, http.MethodGet,
		"/invocations/inv-1/timeline?repo_id="+env.RepoID+"&order=desc&limit=1")
	assert.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	var data InvocationTimelineData
	decodeData(t, resp, &data)
	require.Len(t, data.Entries, 1)
	assert.Equal(t, "stream:5", data.Entries[0].EntryID, "order=desc&limit=1 must return chronologically last entry")
}

func TestHandleGetInvocationTimeline_InvalidOrderReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet,
		"/invocations/inv-1/timeline?repo_id="+env.RepoID+"&order=sideways")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)
}

func TestHandleGetInvocationTimeline_OrderDescWithCursorReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet,
		"/invocations/inv-1/timeline?repo_id="+env.RepoID+"&order=desc&cursor=fakecursor")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVALID_ARGUMENT", resp.ErrorCode)
}

func TestHandleControlPlaneFollowUpPrompt_WritesTimelineEntryWithoutNewInvocation(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	before, err := store.ScanInvocationsForRepo(env.Store.DataDir, env.RepoID)
	require.NoError(t, err)
	beforeCount := len(before)

	reqBody, err := json.Marshal(map[string]any{
		"client_request_id": "followup-req-1",
		"prompt":            "investigate retry path",
	})
	require.NoError(t, err)

	w := env.doInvocationRequestWithBody(t, http.MethodPost,
		"/invocations/inv-1/chat?repo_id="+env.RepoID, reqBody)
	assert.Equal(t, http.StatusOK, w.Code)

	var writeResp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&writeResp))
	assert.Equal(t, true, writeResp["ok"])

	after, err := store.ScanInvocationsForRepo(env.Store.DataDir, env.RepoID)
	require.NoError(t, err)
	assert.Equal(t, beforeCount, len(after), "follow-up prompt must not create a new invocation")

	wTimeline := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/timeline?repo_id="+env.RepoID)
	assert.Equal(t, http.StatusOK, wTimeline.Code)
	resp := decodeAPIResponse(t, wTimeline)
	assert.True(t, resp.OK)

	var timeline struct {
		Entries []struct {
			Kind string                 `json:"kind"`
			Data map[string]interface{} `json:"data"`
		} `json:"entries"`
	}
	decodeData(t, resp, &timeline)

	found := false
	for _, entry := range timeline.Entries {
		if entry.Kind == "followup_prompt" && entry.Data["text"] == "investigate retry path" {
			found = true
			break
		}
	}
	assert.True(t, found, "accepted follow-up prompt must appear in unified timeline")
}

func TestHandleControlPlaneFollowUpPrompt_IdempotentRetryNoDuplicateTimelineWrites(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	reqBody, err := json.Marshal(map[string]any{
		"client_request_id": "followup-req-dup",
		"prompt":            "retry-safe follow-up",
	})
	require.NoError(t, err)

	w1 := env.doInvocationRequestWithBody(t, http.MethodPost,
		"/invocations/inv-1/chat?repo_id="+env.RepoID, reqBody)
	assert.Equal(t, http.StatusOK, w1.Code)

	w2 := env.doInvocationRequestWithBody(t, http.MethodPost,
		"/invocations/inv-1/chat?repo_id="+env.RepoID, reqBody)
	assert.Equal(t, http.StatusOK, w2.Code)

	wTimeline := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/timeline?repo_id="+env.RepoID+"&limit=500")
	assert.Equal(t, http.StatusOK, wTimeline.Code)
	resp := decodeAPIResponse(t, wTimeline)
	assert.True(t, resp.OK)

	var timeline struct {
		Entries []struct {
			Kind string                 `json:"kind"`
			Data map[string]interface{} `json:"data"`
		} `json:"entries"`
	}
	decodeData(t, resp, &timeline)

	count := 0
	for _, entry := range timeline.Entries {
		if entry.Kind == "followup_prompt" && entry.Data["client_request_id"] == "followup-req-dup" {
			count++
		}
	}
	assert.Equal(t, 1, count, "duplicate follow-up submissions must not write duplicate timeline entries")
}

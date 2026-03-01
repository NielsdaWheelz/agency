package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

type turnDiffFixture struct {
	env            *readTestEnv
	invocationID   string
	repoID         string
	selectedTurnID string
	baseCommit     string
	cp1Commit      string
	cp2Commit      string
}

func runGit(t *testing.T, cr exec.CommandRunner, repoDir string, args ...string) string {
	t.Helper()
	result, err := cr.Run(context.Background(), "git", args, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git %s failed: %s", strings.Join(args, " "), result.Stderr)
	return strings.TrimSpace(result.Stdout)
}

func setupTurnDiffFixture(t *testing.T) turnDiffFixture {
	t.Helper()
	testutil.HermeticGitEnv(t)

	cr := exec.NewRealRunner()
	repoDir := t.TempDir()

	runGit(t, cr, repoDir, "init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "base.txt"), []byte("base\n"), 0o644))
	runGit(t, cr, repoDir, "add", ".")
	runGit(t, cr, repoDir, "commit", "-m", "base")
	baseCommit := runGit(t, cr, repoDir, "rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "cp1.txt"), []byte("checkpoint one\n"), 0o644))
	runGit(t, cr, repoDir, "add", ".")
	runGit(t, cr, repoDir, "commit", "-m", "checkpoint one")
	cp1Commit := runGit(t, cr, repoDir, "rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "cp2.txt"), []byte("checkpoint two\n"), 0o644))
	runGit(t, cr, repoDir, "add", ".")
	runGit(t, cr, repoDir, "commit", "-m", "checkpoint two")
	cp2Commit := runGit(t, cr, repoDir, "rev-parse", "HEAD")

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	repoID := "test-repo-turn-diff"
	invocationID := "inv-turn-1"

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, cr, fs.NewRealFS(), configDir)
	now := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	srv.Clock = func() time.Time { return now }

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID:     repoID,
				Paths:      []string{repoDir},
				LastSeenAt: now.Format(time.RFC3339),
			},
		},
	}))

	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invocationID,
		IntegrationWorktreeID: "wt-1",
		SandboxPath:           repoDir,
		SandboxBranch:         "main",
		BaseCommit:            baseCommit,
		Runner:                "claude",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             now.Add(-5 * time.Minute).Format(time.RFC3339),
		Status:                store.InvocationStatusRunning,
	}))

	sandboxDir := st.SandboxDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(sandboxDir, 0o700))
	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/1",
				SnapshotCommit:    cp1Commit,
				SandboxHeadSHA:    cp1Commit,
				CreatedAt:         "2026-02-05T11:50:10Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
			{
				ID:                2,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/2",
				SnapshotCommit:    cp2Commit,
				SandboxHeadSHA:    cp2Commit,
				CreatedAt:         "2026-02-05T11:50:30Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(sandboxDir, "checkpoints.json"), cpBytes, 0o644))

	events := strings.Join([]string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"` + invocationID + `","kind":"agency.followup_prompt","data":{"text":"continue"}}`,
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:30Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":2}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(st.InvocationEventsPath(repoID, invocationID), []byte(events), 0o644))

	return turnDiffFixture{
		env:            &readTestEnv{Server: srv, Store: st, RepoID: repoID},
		invocationID:   invocationID,
		repoID:         repoID,
		selectedTurnID: "inv_event:2:agency.followup_prompt",
		baseCommit:     baseCommit,
		cp1Commit:      cp1Commit,
		cp2Commit:      cp2Commit,
	}
}

func writeRunnerStatusForSandbox(t *testing.T, sandboxPath string, status runnerstatus.RunnerStatus) {
	t.Helper()
	stateDir := filepath.Join(sandboxPath, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	payload, err := json.Marshal(status)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), payload, 0o600))
}

func blockingReasonCodes(data map[string]any) []string {
	raw, ok := data["blocking_reasons"].([]any)
	if !ok {
		return nil
	}
	codes := make([]string, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		code, _ := entry["code"].(string)
		if code != "" {
			codes = append(codes, code)
		}
	}
	return codes
}

func TestHandleGetInvocationDiff_TurnSelectorDeterministicMapping(t *testing.T) {
	fixture := setupTurnDiffFixture(t)

	path := "/invocations/" + fixture.invocationID + "/diff?repo_id=" + fixture.repoID + "&turn=" + url.QueryEscape(fixture.selectedTurnID)

	w1 := fixture.env.doInvocationRequest(t, http.MethodGet, path)
	require.Equal(t, http.StatusOK, w1.Code)
	resp1 := decodeAPIResponse(t, w1)
	require.True(t, resp1.OK)

	var data1 map[string]any
	decodeData(t, resp1, &data1)

	turnCtx1, ok := data1["turn_context"].(map[string]any)
	require.True(t, ok, "expected turn_context in diff response")
	selector1, ok := turnCtx1["selector"].(map[string]any)
	require.True(t, ok, "expected selector details in turn_context")
	assert.Equal(t, "single", selector1["kind"])
	assert.Equal(t, fixture.selectedTurnID, selector1["turn_id"])
	assert.Equal(t, float64(1), turnCtx1["start_checkpoint_id"])
	assert.Equal(t, float64(2), turnCtx1["end_checkpoint_id"])
	assert.Equal(t, fixture.cp1Commit, turnCtx1["from_commit"])
	assert.Equal(t, fixture.cp2Commit, turnCtx1["to_commit"])

	committedRange1, ok := data1["committed_range"].(map[string]any)
	require.True(t, ok, "expected committed_range in diff response")
	assert.Equal(t, fixture.cp1Commit, committedRange1["from"])
	assert.Equal(t, fixture.cp2Commit, committedRange1["to"])

	w2 := fixture.env.doInvocationRequest(t, http.MethodGet, path)
	require.Equal(t, http.StatusOK, w2.Code)
	resp2 := decodeAPIResponse(t, w2)
	require.True(t, resp2.OK)

	var data2 map[string]any
	decodeData(t, resp2, &data2)
	turnCtx2, ok := data2["turn_context"].(map[string]any)
	require.True(t, ok, "expected turn_context on repeated request")
	assert.Equal(t, turnCtx1, turnCtx2, "turn selector mapping should be deterministic")
}

func TestHandleGetInvocationDiff_TurnSelectorUnknownTurnReturnsInvalidArgument(t *testing.T) {
	fixture := setupTurnDiffFixture(t)

	path := "/invocations/" + fixture.invocationID + "/diff?repo_id=" + fixture.repoID + "&turn=" + url.QueryEscape("missing-turn-id")
	w := fixture.env.doInvocationRequest(t, http.MethodGet, path)

	require.Equal(t, http.StatusBadRequest, w.Code)
	resp := decodeAPIResponse(t, w)
	require.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvalidArgument), resp.ErrorCode)
}

func TestHandleGetInvocationChecks_BlockedIncludesReasonsAndNavigation(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-blocked-sandbox")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))
	blockedStatus := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		Status:        runnerstatus.StatusBlocked,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "waiting on product decision",
		Blockers:      []string{"need decision on schema version"},
		Questions:     []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForSandbox(t, sandboxPath, blockedStatus)

	blocked := runnerstatus.StatusBlocked
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.SemanticStatus = &blocked
		meta.Status = store.InvocationStatusRunning
	}))

	require.NoError(t, os.WriteFile(env.Store.InvocationEventsPath(env.RepoID, "inv-1"), []byte(
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"inv-1","kind":"agency.followup_prompt","data":{"text":"continue"}}`+"\n",
	), 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checks?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	assert.Equal(t, "blocked", data["readiness"])
	assert.Equal(t, false, data["ready"])

	codes := blockingReasonCodes(data)
	assert.Contains(t, codes, "invocation_active")
	assert.Contains(t, codes, "runner_blocked")

	nav, ok := data["navigation"].(map[string]any)
	require.True(t, ok, "expected navigation context")
	assert.Equal(t, "inv-1", nav["invocation_ref"])
	assert.NotEmpty(t, nav["history_command"])
	assert.NotEmpty(t, nav["latest_turn_id"])
}

func TestHandleGetInvocationChecks_ReadyWhenFinishedAndReviewable(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-ready-sandbox")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))
	readyStatus := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		Status:        runnerstatus.StatusReadyForReview,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "ready for review",
		HowToTest:     "go test ./...",
		Questions:     []string{},
		Blockers:      []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForSandbox(t, sandboxPath, readyStatus)

	ready := runnerstatus.StatusReadyForReview
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusPending
		meta.SemanticStatus = &ready
		meta.FinishedAt = "2026-02-05T11:59:00Z"
	}))

	require.NoError(t, os.WriteFile(env.Store.InvocationEventsPath(env.RepoID, "inv-1"), []byte(
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:58:20Z","invocation_id":"inv-1","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}`+"\n",
	), 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checks?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	assert.Equal(t, "ready", data["readiness"])
	assert.Equal(t, true, data["ready"])
	assert.Empty(t, blockingReasonCodes(data))

	nav, ok := data["navigation"].(map[string]any)
	require.True(t, ok, "expected navigation context")
	assert.Equal(t, "inv-1", nav["invocation_ref"])
	assert.NotEmpty(t, nav["history_command"])
}

func TestHandleGetInvocationChecks_AmbiguousInvocationRefReturnsConflict(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-/checks?repo_id="+env.RepoID)
	require.Equal(t, http.StatusConflict, w.Code)

	resp := decodeAPIResponse(t, w)
	require.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvocationIDAmbiguous), resp.ErrorCode)
	require.NotNil(t, resp.Details)

	detailsMap, err := json.Marshal(resp.Details)
	require.NoError(t, err)
	var details AmbiguousDetails
	require.NoError(t, json.Unmarshal(detailsMap, &details))
	assert.Len(t, details.Candidates, 3)
}

func TestHandleGetInvocationChecks_InvalidRunnerSchemaBlocksReadiness(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-invalid-schema")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))

	invalidSchema := runnerstatus.RunnerStatus{
		SchemaVersion: "9.9",
		Status:        runnerstatus.StatusReadyForReview,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "ready for review",
		HowToTest:     "go test ./...",
		Questions:     []string{},
		Blockers:      []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForSandbox(t, sandboxPath, invalidSchema)

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusPending
		meta.SemanticStatus = nil
		meta.FinishedAt = "2026-02-05T11:59:00Z"
	}))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/checks?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	assert.Equal(t, "blocked", data["readiness"])
	assert.Equal(t, false, data["ready"])

	codes := blockingReasonCodes(data)
	assert.Contains(t, codes, "runner_status_invalid")
	assert.NotContains(t, codes, "runner_status_missing")
}

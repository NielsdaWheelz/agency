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
	writeTestUserConfig(t, configDir)
	repoID := "test-repo-turn-diff"
	invocationID := "inv-turn-1"

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, cr, fs.NewRealFS(), configDir)
	now := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	srv.Clock = func() time.Time { return now }

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
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
		SchemaVersion:         store.SchemaVersion,
		InvocationID:          invocationID,
		IntegrationWorktreeID: "wt-1",
		SandboxPath:           repoDir,
		CheckoutRoot:          filepath.Dir(repoDir),
		ExecutionProfile:      "work",
		SandboxBranch:         "main",
		BaseCommit:            baseCommit,
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             now.Add(-5 * time.Minute).Format(time.RFC3339),
		Status:                store.InvocationStatusRunning,
	}))

	require.NoError(t, os.MkdirAll(st.InvocationDir(repoID, invocationID), 0o700))
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
	require.NoError(t, os.WriteFile(st.InvocationCheckpointsPath(repoID, invocationID), cpBytes, 0o644))

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

func writeRunnerStatusForInvocation(t *testing.T, st *store.Store, repoID, invocationID string, status runnerstatus.RunnerStatus) {
	t.Helper()
	stateDir := filepath.Join(st.InvocationDir(repoID, invocationID), ".agency", "state")
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

func TestHandleGetInvocationDiff_TurnSelectorRejectsNonTurnEntryIDs(t *testing.T) {
	fixture := setupTurnDiffFixture(t)

	path := "/invocations/" + fixture.invocationID + "/diff?repo_id=" + fixture.repoID + "&turn=" + url.QueryEscape("inv_event:1:agency.checkpoint_created")
	w := fixture.env.doInvocationRequest(t, http.MethodGet, path)

	require.Equal(t, http.StatusBadRequest, w.Code)
	resp := decodeAPIResponse(t, w)
	require.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvalidArgument), resp.ErrorCode)
}

func TestHandleGetInvocationDiff_TurnSelectorAssistantTurnUsesCanonicalCheckpointAssociation(t *testing.T) {
	fixture := setupTurnDiffFixture(t)

	require.NoError(t, os.MkdirAll(fixture.env.Store.InvocationLogsDir(fixture.repoID, fixture.invocationID), 0o700))
	streamPath := fixture.env.Store.InvocationStreamLogPath(fixture.repoID, fixture.invocationID)
	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:09Z","invocation_id":"` + fixture.invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"assistant turn before checkpoint"}}`,
	}
	require.NoError(t, os.WriteFile(streamPath, []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))
	fixture.selectedTurnID = "stream:1"

	path := "/invocations/" + fixture.invocationID + "/diff?repo_id=" + fixture.repoID + "&turn=" + url.QueryEscape(fixture.selectedTurnID)
	w := fixture.env.doInvocationRequest(t, http.MethodGet, path)

	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)

	turnCtx, ok := data["turn_context"].(map[string]any)
	require.True(t, ok, "expected turn_context in diff response")
	assert.Equal(t, float64(1), turnCtx["start_checkpoint_id"])
	assert.Equal(t, float64(2), turnCtx["end_checkpoint_id"])
	assert.Equal(t, fixture.cp1Commit, turnCtx["from_commit"])
	assert.Equal(t, fixture.cp2Commit, turnCtx["to_commit"])
}

func TestHandleGetInvocationDiff_TurnSelectorLatestAssistantTurnUsesPreviousCheckpointBoundary(t *testing.T) {
	fixture := setupTurnDiffFixture(t)

	require.NoError(t, os.MkdirAll(fixture.env.Store.InvocationLogsDir(fixture.repoID, fixture.invocationID), 0o700))
	streamPath := fixture.env.Store.InvocationStreamLogPath(fixture.repoID, fixture.invocationID)
	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:40Z","invocation_id":"` + fixture.invocationID + `","runner":"codex","kind":"message","data":{"role":"assistant","text":"latest assistant turn after checkpoint two"}}`,
	}
	require.NoError(t, os.WriteFile(streamPath, []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))
	fixture.selectedTurnID = "stream:1"

	path := "/invocations/" + fixture.invocationID + "/diff?repo_id=" + fixture.repoID + "&turn=" + url.QueryEscape(fixture.selectedTurnID)
	w := fixture.env.doInvocationRequest(t, http.MethodGet, path)

	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)

	turnCtx, ok := data["turn_context"].(map[string]any)
	require.True(t, ok, "expected turn_context in diff response")
	assert.Equal(t, float64(1), turnCtx["start_checkpoint_id"])
	assert.Equal(t, float64(2), turnCtx["end_checkpoint_id"])
	assert.Equal(t, fixture.cp1Commit, turnCtx["from_commit"])
	assert.Equal(t, fixture.cp2Commit, turnCtx["to_commit"])

	committedRange, ok := data["committed_range"].(map[string]any)
	require.True(t, ok, "expected committed_range for selected latest assistant turn")
	assert.Equal(t, fixture.cp1Commit, committedRange["from"])
	assert.Equal(t, fixture.cp2Commit, committedRange["to"])
}

func TestHandleGetInvocationDiff_TurnSelectorLatestAssistantTurnSingleCheckpointUsesBaseBoundary(t *testing.T) {
	fixture := setupTurnDiffFixture(t)

	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + fixture.invocationID + "/1",
				SnapshotCommit:    fixture.cp1Commit,
				SandboxHeadSHA:    fixture.cp1Commit,
				CreatedAt:         "2026-02-05T11:50:10Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(fixture.env.Store.InvocationCheckpointsPath(fixture.repoID, fixture.invocationID), cpBytes, 0o644))

	events := strings.Join([]string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + fixture.invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(fixture.env.Store.InvocationEventsPath(fixture.repoID, fixture.invocationID), []byte(events), 0o644))

	require.NoError(t, os.MkdirAll(fixture.env.Store.InvocationLogsDir(fixture.repoID, fixture.invocationID), 0o700))
	streamPath := fixture.env.Store.InvocationStreamLogPath(fixture.repoID, fixture.invocationID)
	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:40Z","invocation_id":"` + fixture.invocationID + `","runner":"codex","kind":"message","data":{"role":"assistant","text":"latest assistant turn on first checkpoint"}}`,
	}
	require.NoError(t, os.WriteFile(streamPath, []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))
	fixture.selectedTurnID = "stream:1"

	path := "/invocations/" + fixture.invocationID + "/diff?repo_id=" + fixture.repoID + "&turn=" + url.QueryEscape(fixture.selectedTurnID)
	w := fixture.env.doInvocationRequest(t, http.MethodGet, path)

	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)

	turnCtx, ok := data["turn_context"].(map[string]any)
	require.True(t, ok, "expected turn_context in diff response")
	assert.Equal(t, float64(0), turnCtx["start_checkpoint_id"])
	assert.Equal(t, float64(1), turnCtx["end_checkpoint_id"])
	assert.Equal(t, fixture.baseCommit, turnCtx["from_commit"])
	assert.Equal(t, fixture.cp1Commit, turnCtx["to_commit"])

	committedRange, ok := data["committed_range"].(map[string]any)
	require.True(t, ok, "expected committed_range for selected latest assistant turn")
	assert.Equal(t, fixture.baseCommit, committedRange["from"])
	assert.Equal(t, fixture.cp1Commit, committedRange["to"])
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

func TestHandleGetInvocationCheck_BlockedIncludesReasonsAndNavigation(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-blocked-sandbox")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))
	blockedStatus := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		State:         runnerstatus.StateWaiting,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Reason:        runnerstatus.ReasonAwaitingApproval,
		Summary:       "waiting on product decision",
		Questions:     []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", blockedStatus)

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusRunning
	}))

	require.NoError(t, os.WriteFile(env.Store.InvocationEventsPath(env.RepoID, "inv-1"), []byte(
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"inv-1","kind":"agency.followup_prompt","data":{"text":"continue"}}`+"\n",
	), 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/check?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	assert.Equal(t, "waiting", data["state"])
	assert.Equal(t, false, data["pr_sync_eligible"])

	codes := blockingReasonCodes(data)
	assert.Contains(t, codes, "invocation_active")
	assert.Contains(t, codes, "invocation_waiting")

	nav, ok := data["navigation"].(map[string]any)
	require.True(t, ok, "expected navigation context")
	assert.Equal(t, "inv-1", nav["invocation_ref"])
	assert.NotEmpty(t, nav["history_command"])
	assert.NotEmpty(t, nav["latest_turn_id"])
}

func TestHandleGetInvocationCheck_NavigationLatestTurnIDUsesCanonicalTurnProjection(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	logsDir := env.Store.InvocationLogsDir(env.RepoID, "inv-1")
	require.NoError(t, os.MkdirAll(logsDir, 0o700))

	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:00Z","invocation_id":"inv-1","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"canonical latest turn"}}`,
	}
	require.NoError(t, os.WriteFile(env.Store.InvocationStreamLogPath(env.RepoID, "inv-1"), []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))
	require.NoError(t, os.WriteFile(env.Store.InvocationRawLogPath(env.RepoID, "inv-1"), []byte("{\"raw\":true}\n"), 0o644))
	require.NoError(t, os.WriteFile(env.Store.InvocationEventsPath(env.RepoID, "inv-1"), []byte(
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:01Z","invocation_id":"inv-1","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}`+"\n",
	), 0o644))

	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + "inv-1/1",
				SnapshotCommit:    "deadbeef",
				SandboxHeadSHA:    "deadbeef",
				CreatedAt:         "2026-02-05T11:50:01Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(env.Store.InvocationCheckpointsPath(env.RepoID, "inv-1"), cpBytes, 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/check?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	nav, ok := data["navigation"].(map[string]any)
	require.True(t, ok, "expected navigation context")

	assert.Equal(t, "stream:1", nav["latest_turn_id"])
	assert.Contains(t, nav["diff_command"], "--turn stream:1")
}

func TestHandleGetInvocationCheck_NavigationDiffCommandOmitsTurnWhenLatestTurnNotRestorable(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	promptPath := env.Store.InvocationPromptPath(env.RepoID, "inv-1")
	require.NoError(t, os.WriteFile(promptPath, []byte("cursor seed prompt"), 0o600))
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
		meta.Runner = "cursor"
	}))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/check?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	nav, ok := data["navigation"].(map[string]any)
	require.True(t, ok, "expected navigation context")

	assert.Equal(t, "prompt_seed", nav["latest_turn_id"])
	diffCommand, ok := nav["diff_command"].(string)
	require.True(t, ok)
	assert.Equal(t, "agency agent inv-1 diff --repo "+env.RepoID, diffCommand)
	assert.NotContains(t, diffCommand, "--turn")
}

func TestHandleGetInvocationCheck_ReadyWhenFinishedAndCheckable(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-ready-sandbox")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))
	readyStatus := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		State:         runnerstatus.StateSucceeded,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "ready",
		HowToTest:     "go test ./...",
		Questions:     []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", readyStatus)

	integrationTree := filepath.Join(t.TempDir(), "checks-ready-integration-tree")
	require.NoError(t, os.MkdirAll(integrationTree, 0o755))
	require.NoError(t, env.Store.UpdateIntegrationWorktreeMeta(env.RepoID, "wt-1", func(meta *store.IntegrationWorktreeMeta) {
		meta.TreePath = integrationTree
	}))

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
		meta.FinishedAt = "2026-02-05T11:59:00Z"
	}))

	require.NoError(t, os.WriteFile(env.Store.InvocationEventsPath(env.RepoID, "inv-1"), []byte(
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:58:20Z","invocation_id":"inv-1","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}`+"\n",
	), 0o644))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/check?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	assert.Equal(t, "succeeded", data["state"])
	assert.Equal(t, true, data["pr_sync_eligible"])
	assert.Empty(t, blockingReasonCodes(data))

	nav, ok := data["navigation"].(map[string]any)
	require.True(t, ok, "expected navigation context")
	assert.Equal(t, "inv-1", nav["invocation_ref"])
	assert.NotEmpty(t, nav["history_command"])
}

func TestHandleGetInvocationCheck_UsesInvocationOwnedRunnerStatusAfterSandboxCleanup(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-cleanup-sandbox")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))
	readyStatus := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		State:         runnerstatus.StateSucceeded,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "invocation-owned runner status",
		HowToTest:     "go test ./...",
		Questions:     []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", readyStatus)
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", readyStatus)

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusPending
		meta.FinishedAt = "2026-02-05T11:59:00Z"
	}))
	require.NoError(t, os.RemoveAll(sandboxPath))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/check?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)

	codes := blockingReasonCodes(data)
	assert.NotContains(t, codes, "runner_status_unreadable")
	assert.NotContains(t, codes, "runner_status_missing")
	assert.Equal(t, "succeeded", data["runner_state"])
	assert.Equal(t, "invocation-owned runner status", data["runner_summary"])
}

func TestHandleGetInvocationCheck_HeadlessDoesNotRequireWorktreeReport(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-ready-sandbox-without-worktree-status")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))
	readyStatus := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		State:         runnerstatus.StateSucceeded,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "ready",
		HowToTest:     "go test ./...",
		Questions:     []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", readyStatus)

	integrationTree := filepath.Join(t.TempDir(), "integration-tree-missing-report")
	require.NoError(t, os.MkdirAll(integrationTree, 0o755))
	require.NoError(t, env.Store.UpdateIntegrationWorktreeMeta(env.RepoID, "wt-1", func(meta *store.IntegrationWorktreeMeta) {
		meta.TreePath = integrationTree
	}))

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
		meta.FinishedAt = "2026-02-05T11:59:00Z"
		meta.Mode = store.RunnerModeHeadless
	}))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/check?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	assert.Equal(t, "succeeded", data["state"])
	assert.Equal(t, "succeeded", data["runner_state"])
	assert.NotContains(t, blockingReasonCodes(data), "report_missing")
}

func TestHandleGetInvocationCheck_HeadlessOmitsReportMetadata(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-ready-sandbox-report-source")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))
	readyStatus := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		State:         runnerstatus.StateSucceeded,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "ready",
		HowToTest:     "go test ./...",
		Questions:     []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", readyStatus)

	integrationTree := filepath.Join(t.TempDir(), "integration-tree-report-source")
	require.NoError(t, os.MkdirAll(integrationTree, 0o755))
	require.NoError(t, env.Store.UpdateIntegrationWorktreeMeta(env.RepoID, "wt-1", func(meta *store.IntegrationWorktreeMeta) {
		meta.TreePath = integrationTree
	}))

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
		meta.FinishedAt = "2026-02-05T11:59:00Z"
		meta.Mode = store.RunnerModeHeadless
	}))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/check?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	assert.Equal(t, "succeeded", data["state"])
}

func TestHandleGetInvocationCheck_AmbiguousInvocationRefReturnsConflict(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-/check?repo_id="+env.RepoID)
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

func TestHandleGetInvocationCheck_InvalidRunnerSchemaFailsState(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	sandboxPath := filepath.Join(t.TempDir(), "checks-invalid-schema")
	require.NoError(t, os.MkdirAll(sandboxPath, 0o700))

	invalidSchema := runnerstatus.RunnerStatus{
		SchemaVersion: "9.9",
		State:         runnerstatus.StateSucceeded,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "ready",
		HowToTest:     "go test ./...",
		Questions:     []string{},
		Risks:         []string{},
	}
	writeRunnerStatusForInvocation(t, env.Store, env.RepoID, "inv-1", invalidSchema)

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
		meta.SandboxPath = sandboxPath
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusPending
		meta.FinishedAt = "2026-02-05T11:59:00Z"
	}))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/check?repo_id="+env.RepoID)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)

	var data map[string]any
	decodeData(t, resp, &data)
	assert.Equal(t, "failed", data["state"])

	codes := blockingReasonCodes(data)
	assert.Contains(t, codes, "runner_status_invalid")
	assert.NotContains(t, codes, "runner_status_missing")
}

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestAgentDiff_TurnAware_HumanAndJSONAligned(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "diff-turn")
	invocationID := "20260201101010-diff"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	sandboxPath := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID, "tree")

	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/1",
				SnapshotCommit:    "1111111",
				SandboxHeadSHA:    "1111111",
				CreatedAt:         "2026-02-05T11:50:10Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
			{
				ID:                2,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/2",
				SnapshotCommit:    "2222222",
				SandboxHeadSHA:    "2222222",
				CreatedAt:         "2026-02-05T11:50:30Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(st.InvocationCheckpointsPath(repoID, invocationID), cpBytes, 0o644))

	events := "" +
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}` + "\n" +
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"` + invocationID + `","kind":"agency.followup_prompt","data":{"text":"continue"}}` + "\n" +
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:30Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":2}}` + "\n"
	require.NoError(t, os.WriteFile(st.InvocationEventsPath(repoID, invocationID), []byte(events), 0o644))

	daemonRunner.Responses["git -C "+sandboxPath+" rev-parse HEAD"] = testutil.FakeResponse{Stdout: "2222222\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" log --oneline 1111111..2222222"] = testutil.FakeResponse{Stdout: "2222222 checkpoint two\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" diff --stat 1111111..2222222"] = testutil.FakeResponse{Stdout: " cp2.txt | 1 +\n 1 file changed, 1 insertion(+)\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" diff 1111111..2222222"] = testutil.FakeResponse{Stdout: "diff --git a/cp2.txt b/cp2.txt\n+checkpoint two\n"}

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	turnID := "inv_event:2:agency.followup_prompt"
	var humanOut, jsonOut, errOut bytes.Buffer

	err = AgentDiff(context.Background(), cr2, fsys, repoDir, AgentDiffOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		TurnID:          turnID,
		DataDirOverride: dataDir,
	}, &humanOut, &errOut)
	require.NoError(t, err)

	err = AgentDiff(context.Background(), cr2, fsys, repoDir, AgentDiffOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		TurnID:          turnID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &jsonOut, &errOut)
	require.NoError(t, err)

	assert.Contains(t, humanOut.String(), "Turn context:")
	assert.Contains(t, humanOut.String(), "checkpoints:   1 -> 2")
	assert.Contains(t, humanOut.String(), "commit_range:  1111111..2222222")

	var payload daemon.InvocationDiffData
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &payload))
	require.NotNil(t, payload.TurnContext)
	assert.Equal(t, "single", payload.TurnContext.Selector.Kind)
	assert.Equal(t, turnID, payload.TurnContext.Selector.TurnID)
	assert.Equal(t, 1, payload.TurnContext.StartCheckpointID)
	assert.Equal(t, 2, payload.TurnContext.EndCheckpointID)
	require.NotNil(t, payload.CommittedRange)
	assert.Equal(t, "1111111", payload.CommittedRange.From)
	assert.Equal(t, "2222222", payload.CommittedRange.To)
}

func TestAgentDiff_TurnAware_LatestAssistantTurnUsesPreviousCheckpointBoundary(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "diff-turn-latest")
	invocationID := "20260201102020-difflatest"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	sandboxPath := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID, "tree")

	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/1",
				SnapshotCommit:    "1111111",
				SandboxHeadSHA:    "1111111",
				CreatedAt:         "2026-02-05T11:50:10Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
			{
				ID:                2,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/2",
				SnapshotCommit:    "2222222",
				SandboxHeadSHA:    "2222222",
				CreatedAt:         "2026-02-05T11:50:30Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(st.InvocationCheckpointsPath(repoID, invocationID), cpBytes, 0o644))

	events := "" +
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}` + "\n" +
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:30Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":2}}` + "\n"
	require.NoError(t, os.WriteFile(st.InvocationEventsPath(repoID, invocationID), []byte(events), 0o644))

	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))
	stream := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:40Z","invocation_id":"` + invocationID + `","runner":"codex","kind":"message","data":{"role":"assistant","text":"latest codex assistant turn"}}`,
	}
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(repoID, invocationID), []byte(strings.Join(stream, "\n")+"\n"), 0o644))

	daemonRunner.Responses["git -C "+sandboxPath+" rev-parse HEAD"] = testutil.FakeResponse{Stdout: "2222222\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" log --oneline 1111111..2222222"] = testutil.FakeResponse{Stdout: "2222222 checkpoint two\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" diff --stat 1111111..2222222"] = testutil.FakeResponse{Stdout: " cp2.txt | 1 +\n 1 file changed, 1 insertion(+)\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" diff 1111111..2222222"] = testutil.FakeResponse{Stdout: "diff --git a/cp2.txt b/cp2.txt\n+checkpoint two\n"}

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	turnID := "stream:1"
	var humanOut, jsonOut, errOut bytes.Buffer

	err = AgentDiff(context.Background(), cr2, fsys, repoDir, AgentDiffOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		TurnID:          turnID,
		DataDirOverride: dataDir,
	}, &humanOut, &errOut)
	require.NoError(t, err)

	err = AgentDiff(context.Background(), cr2, fsys, repoDir, AgentDiffOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		TurnID:          turnID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &jsonOut, &errOut)
	require.NoError(t, err)

	assert.Contains(t, humanOut.String(), "Turn context:")
	assert.Contains(t, humanOut.String(), "checkpoints:   1 -> 2")
	assert.Contains(t, humanOut.String(), "commit_range:  1111111..2222222")
	assert.NotContains(t, humanOut.String(), "(no changes)")

	var payload daemon.InvocationDiffData
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &payload))
	require.NotNil(t, payload.TurnContext)
	assert.Equal(t, "single", payload.TurnContext.Selector.Kind)
	assert.Equal(t, turnID, payload.TurnContext.Selector.TurnID)
	assert.Equal(t, 1, payload.TurnContext.StartCheckpointID)
	assert.Equal(t, 2, payload.TurnContext.EndCheckpointID)
	require.NotNil(t, payload.CommittedRange)
	assert.Equal(t, "1111111", payload.CommittedRange.From)
	assert.Equal(t, "2222222", payload.CommittedRange.To)
}

func TestAgentDiff_TurnAware_LatestAssistantTurnSingleCheckpointUsesBaseBoundary(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "diff-turn-single-checkpoint")
	invocationID := "20260201103030-diffsingle"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	sandboxPath := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID, "tree")

	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/1",
				SnapshotCommit:    "1111111",
				SandboxHeadSHA:    "1111111",
				CreatedAt:         "2026-02-05T11:50:10Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(st.InvocationCheckpointsPath(repoID, invocationID), cpBytes, 0o644))

	events := "" +
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}` + "\n"
	require.NoError(t, os.WriteFile(st.InvocationEventsPath(repoID, invocationID), []byte(events), 0o644))

	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))
	stream := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:40Z","invocation_id":"` + invocationID + `","runner":"codex","kind":"message","data":{"role":"assistant","text":"latest codex assistant turn with one checkpoint"}}`,
	}
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(repoID, invocationID), []byte(strings.Join(stream, "\n")+"\n"), 0o644))

	baseCommit := "abc123def456"
	daemonRunner.Responses["git -C "+sandboxPath+" rev-parse HEAD"] = testutil.FakeResponse{Stdout: "1111111\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" log --oneline "+baseCommit+"..1111111"] = testutil.FakeResponse{Stdout: "1111111 checkpoint one\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" diff --stat "+baseCommit+"..1111111"] = testutil.FakeResponse{Stdout: " cp1.txt | 1 +\n 1 file changed, 1 insertion(+)\n"}
	daemonRunner.Responses["git -C "+sandboxPath+" diff "+baseCommit+"..1111111"] = testutil.FakeResponse{Stdout: "diff --git a/cp1.txt b/cp1.txt\n+checkpoint one\n"}

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	turnID := "stream:1"
	var humanOut, jsonOut, errOut bytes.Buffer

	err = AgentDiff(context.Background(), cr2, fsys, repoDir, AgentDiffOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		TurnID:          turnID,
		DataDirOverride: dataDir,
	}, &humanOut, &errOut)
	require.NoError(t, err)

	err = AgentDiff(context.Background(), cr2, fsys, repoDir, AgentDiffOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		TurnID:          turnID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &jsonOut, &errOut)
	require.NoError(t, err)

	assert.Contains(t, humanOut.String(), "Turn context:")
	assert.Contains(t, humanOut.String(), "checkpoints:   0 -> 1")
	assert.Contains(t, humanOut.String(), "commit_range:  "+baseCommit+"..1111111")
	assert.NotContains(t, humanOut.String(), "(no changes)")

	var payload daemon.InvocationDiffData
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &payload))
	require.NotNil(t, payload.TurnContext)
	assert.Equal(t, "single", payload.TurnContext.Selector.Kind)
	assert.Equal(t, turnID, payload.TurnContext.Selector.TurnID)
	assert.Equal(t, 0, payload.TurnContext.StartCheckpointID)
	assert.Equal(t, 1, payload.TurnContext.EndCheckpointID)
	require.NotNil(t, payload.CommittedRange)
	assert.Equal(t, baseCommit, payload.CommittedRange.From)
	assert.Equal(t, "1111111", payload.CommittedRange.To)
}

func TestAgentReview_Blocked_HumanAndJSONAligned(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "checks-blocked")
	invocationID := "20260201102020-chkb"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	sandboxPath := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID, "tree")

	blocked := runnerstatus.StatusBlocked
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.SemanticStatus = &blocked
		meta.SandboxPath = sandboxPath
	}))

	stateDir := filepath.Join(sandboxPath, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	rs := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		Status:        runnerstatus.StatusBlocked,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "waiting on API contract decision",
		Blockers:      []string{"need owner sign-off"},
	}
	rsBytes, err := json.Marshal(rs)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), rsBytes, 0o600))

	require.NoError(t, os.WriteFile(st.InvocationEventsPath(repoID, invocationID), []byte(
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"`+invocationID+`","kind":"agency.followup_prompt","data":{"text":"continue"}}`+"\n",
	), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var humanOut, jsonOut, errOut bytes.Buffer
	err = AgentReview(context.Background(), cr2, fsys, repoDir, AgentReviewOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		DataDirOverride: dataDir,
	}, &humanOut, &errOut)
	require.NoError(t, err)

	err = AgentReview(context.Background(), cr2, fsys, repoDir, AgentReviewOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &jsonOut, &errOut)
	require.NoError(t, err)

	assert.Contains(t, humanOut.String(), "Review verdict:       BLOCKED")
	assert.Contains(t, humanOut.String(), "pr_sync_eligible:     no")
	assert.Contains(t, humanOut.String(), "[invocation_active]")
	assert.Contains(t, humanOut.String(), "[runner_blocked]")
	assert.Contains(t, humanOut.String(), "history:")
	assert.Contains(t, humanOut.String(), "diff:")

	var payload daemon.InvocationReviewData
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &payload))
	assert.False(t, payload.Ready)
	assert.Equal(t, "blocked", payload.Readiness)
	assert.False(t, payload.PRSyncEligible)
	assert.NotEmpty(t, payload.Navigation.HistoryCommand)
	assert.NotEmpty(t, payload.Navigation.DiffCommand)

	codes := make([]string, 0, len(payload.BlockingReasons))
	for _, reason := range payload.BlockingReasons {
		codes = append(codes, reason.Code)
	}
	assert.Contains(t, codes, "invocation_active")
	assert.Contains(t, codes, "runner_blocked")
}

func TestParseTurnRange_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{name: "empty accepted", input: "", wantStart: "", wantEnd: "", wantErr: false},
		{name: "valid basic", input: "stream:1..stream:4", wantStart: "stream:1", wantEnd: "stream:4", wantErr: false},
		{name: "valid trims whitespace", input: " stream:1 .. stream:4 ", wantStart: "stream:1", wantEnd: "stream:4", wantErr: false},
		{name: "missing delimiter", input: "stream:1", wantErr: true},
		{name: "missing start", input: "..stream:4", wantErr: true},
		{name: "missing end", input: "stream:1..", wantErr: true},
		{name: "extra delimiter", input: "stream:1..stream:4..stream:9", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start, end, err := parseTurnRange(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				assert.Equal(t, errors.EUsage, errors.GetCode(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantStart, start)
			assert.Equal(t, tc.wantEnd, end)
		})
	}
}

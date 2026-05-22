package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func decodeJSONMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(b), &payload))
	return payload
}

func assertMutationEnvelopeShape(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{
		"ok",
		"error_code",
		"message",
		"hint",
		"request_id",
		"api_version",
		"build_version",
		"client_request_id",
	} {
		_, ok := payload[key]
		assert.True(t, ok, "expected envelope key %q", key)
	}
}

func TestAgentStart_JSONFailurePromptRequiredEnvelope(t *testing.T) {
	_, dataDir, repoID, _, _, fsys := setupAgentTestEnvShort(t, "start-json")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataDir, "config"))

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), testutil.NewFakeCommandRunner(), fsys, "", AgentStartOpts{
		RepoRef:     repoID,
		WorktreeRef: "start-json",
		Mode:        "headless",
		JSON:        true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EPromptRequired), payload["error_code"])
}

func TestAgentStart_JSONFailureDaemonDeclaredEnvelopeIncludesRequestID(t *testing.T) {
	_, dataDir, repoID, _, _, fsys := setupAgentTestEnvShort(t, "start-json-daemon-fail")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataDir, "config"))

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), testutil.NewFakeCommandRunner(), fsys, "", AgentStartOpts{
		RepoRef:     repoID,
		WorktreeRef: "does-not-exist",
		Runner:      "claude-code",
		Mode:        "headless",
		Prompt:      "hello",
		JSON:        true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.NotEmpty(t, payload["request_id"])
}

func TestAgentStop_JSONSuccessEnvelope(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "stop-json")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	invocationID := "20260302171000-stp1"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentStop(context.Background(), cr, fsys, repoDir, AgentStopOpts{
		InvocationRef: invocationID,
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err)

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, invocationID, payload["invocation_id"])
	assert.NotEmpty(t, payload["request_id"])
}

func TestAgentKill_JSONSuccessEnvelope(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "kill-json")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	invocationID := "20260302171500-kll1"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentKill(context.Background(), cr, fsys, repoDir, AgentKillOpts{
		InvocationRef: invocationID,
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err)

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, invocationID, payload["invocation_id"])
	assert.NotEmpty(t, payload["request_id"])
}

func TestAgentLand_JSONFailureEnvelope(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "land-json")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	invocationID := "20260302171800-lnd1"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)
	pid := os.Getpid()
	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.PID = &pid
	}))

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentLand(context.Background(), cr, fsys, repoDir, AgentLandOpts{
		InvocationRef: invocationID,
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EInvocationStillRunning), payload["error_code"])
	assert.NotEmpty(t, payload["request_id"])
}

func TestAgentLand_CleanupModeHumanOutputDoesNotRequireHeads(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnvShort(t, "land-cleanup")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataDir, "config"))

	invocationID := "20260302171900-lnd2"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFinished)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.LandingStatus = store.LandingStatusLanded
		meta.FinishedAt = "2026-03-02T17:19:00Z"
	}))
	stubInvocationCleanupCommands(cr, repoDir, dataDir, repoID, invocationID)

	var stdout, stderr bytes.Buffer
	err := AgentLand(context.Background(), cr, fsys, repoDir, AgentLandOpts{
		InvocationRef: invocationID,
		RepoRef:       repoID,
	}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Successfully completed landing cleanup for invocation "+invocationID)
	assert.Contains(t, out, "mode:        cleanup")
	assert.NotContains(t, out, "head_before")
	assert.NotContains(t, out, "head_after")
	assert.Empty(t, stderr.String())
}

func TestAgentDiscard_JSONSuccessEnvelope(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "discard-json")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	invocationID := "20260302172000-dsc1"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFailed)
	stubInvocationCleanupCommands(daemonRunner, repoDir, dataDir, repoID, invocationID)

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentDiscard(context.Background(), cr, fsys, repoDir, AgentDiscardOpts{
		InvocationRef: invocationID,
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err)

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, invocationID, payload["invocation_id"])
	assert.NotEmpty(t, payload["request_id"])
}

func TestAgentFollowup_JSONFailurePromptRequiredEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := AgentFollowup(context.Background(), testutil.NewFakeCommandRunner(), nil, "", AgentFollowupOpts{
		InvocationRef: "missing",
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EPromptRequired), payload["error_code"])
}

func TestAgentFollowup_JSONFailureDaemonDeclaredEnvelope(t *testing.T) {
	repoDir, dataDir, _, _, _, fsys := setupAgentTestEnvShort(t, "followup-json-daemon-fail")

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentFollowup(context.Background(), cr, fsys, repoDir, AgentFollowupOpts{
		InvocationRef:   "does-not-exist",
		Prompt:          "continue",
		JSON:            true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EInvocationNotFound), payload["error_code"])
	assert.NotEmpty(t, payload["request_id"])
}

func TestAgentFollowup_JSONFailureTransportEnvelope(t *testing.T) {
	missingDataDir := filepath.Join(t.TempDir(), "missing")

	var stdout, stderr bytes.Buffer
	err := AgentFollowup(context.Background(), testutil.NewFakeCommandRunner(), nil, "", AgentFollowupOpts{
		InvocationRef:   "any",
		Prompt:          "continue",
		JSON:            true,
		DataDirOverride: missingDataDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "json transport failures must remain machine-readable")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EDaemonStartFailed), payload["error_code"])
}

func TestAgentRestore_JSONFailureValidationEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := AgentRestore(context.Background(), testutil.NewFakeCommandRunner(), nil, "", AgentRestoreOpts{
		InvocationRef: "inv-123",
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json validation failures should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EUsage), payload["error_code"])
}

func TestAgentRestore_JSONFailureDaemonDeclaredEnvelope(t *testing.T) {
	repoDir, dataDir, _, _, _, fsys := setupAgentTestEnvShort(t, "restore-json-daemon-fail")
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataDir, "config"))

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentRestore(context.Background(), cr, fsys, repoDir, AgentRestoreOpts{
		InvocationRef:   "does-not-exist",
		CheckpointID:    1,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EInvocationNotFound), payload["error_code"])
	assert.NotEmpty(t, payload["request_id"])
}

func TestAgentRestore_JSONFailureTransportEnvelope(t *testing.T) {
	missingDataDir := filepath.Join(t.TempDir(), "missing")

	var stdout, stderr bytes.Buffer
	err := AgentRestore(context.Background(), testutil.NewFakeCommandRunner(), nil, "", AgentRestoreOpts{
		InvocationRef:   "any",
		CheckpointID:    1,
		JSON:            true,
		DataDirOverride: missingDataDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "json transport failures must remain machine-readable")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EDaemonStartFailed), payload["error_code"])
}

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

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
	repoDir, dataDir, _, _, _, fsys := setupAgentTestEnvShort(t, "start-json")
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), cr, fsys, repoDir, AgentStartOpts{
		WorktreeRef: "start-json",
		Headless:    true,
		JSON:        true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EPromptRequired), payload["error_code"])
}

func TestAgentStart_JSONFailureDaemonDeclaredEnvelopeIncludesRequestID(t *testing.T) {
	repoDir, dataDir, _, _, _, fsys := setupAgentTestEnvShort(t, "start-json-daemon-fail")
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), cr, fsys, repoDir, AgentStartOpts{
		WorktreeRef: "does-not-exist",
		Runner:      "claude-code",
		Headless:    true,
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

func TestAgentDiscard_JSONSuccessEnvelope(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "discard-json")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	invocationID := "20260302172000-dsc1"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFailed)

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

func TestAgentChat_JSONFailurePromptRequiredEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := AgentChat(context.Background(), testutil.NewFakeCommandRunner(), nil, "", AgentChatOpts{
		InvocationRef: "missing",
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EPromptRequired), payload["error_code"])
}

func TestAgentChat_JSONFailureDaemonDeclaredEnvelope(t *testing.T) {
	repoDir, dataDir, _, _, _, fsys := setupAgentTestEnvShort(t, "chat-json-daemon-fail")

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentChat(context.Background(), cr, fsys, repoDir, AgentChatOpts{
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

func TestAgentChat_JSONFailureTransportEnvelope(t *testing.T) {
	missingDataDir := filepath.Join(t.TempDir(), "missing")

	var stdout, stderr bytes.Buffer
	err := AgentChat(context.Background(), testutil.NewFakeCommandRunner(), nil, "", AgentChatOpts{
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

func TestAgentRestart_JSONFailureValidationEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := AgentRestart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", AgentRestartOpts{
		InvocationRef: "inv-123",
		CheckpointID:  0,
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json validation failures should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EUsage), payload["error_code"])
}

func TestAgentRestart_JSONFailureDaemonDeclaredEnvelope(t *testing.T) {
	repoDir, dataDir, _, _, _, fsys := setupAgentTestEnvShort(t, "restart-json-daemon-fail")

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentRestart(context.Background(), cr, fsys, repoDir, AgentRestartOpts{
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

func TestAgentRestart_JSONFailureTransportEnvelope(t *testing.T) {
	missingDataDir := filepath.Join(t.TempDir(), "missing")

	var stdout, stderr bytes.Buffer
	err := AgentRestart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", AgentRestartOpts{
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

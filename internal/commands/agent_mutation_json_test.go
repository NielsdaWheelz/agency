package commands

import (
	"bytes"
	"context"
	"encoding/json"
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
}

func TestAgentLand_JSONFailureEnvelope(t *testing.T) {
	repoDir, dataDir, _, _, _, fsys := setupAgentTestEnvShort(t, "land-json")
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentLand(context.Background(), cr, fsys, repoDir, AgentLandOpts{
		InvocationRef: "does-not-exist",
		JSON:          true,
	}, &stdout, &stderr)
	require.NoError(t, err, "json failure mode should not return a human-formatted error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EInvocationNotFound), payload["error_code"])
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
}

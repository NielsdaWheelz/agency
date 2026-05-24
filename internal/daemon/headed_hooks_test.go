package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/store"
)

func TestHandleHeadedHook_StoresPayloadAndImportsTranscript(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	transcriptPath := filepath.Join(t.TempDir(), "claude-transcript.jsonl")
	transcript := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done from headed transcript."}]}}` + "\n"
	require.NoError(t, os.WriteFile(transcriptPath, []byte(transcript), 0o600))

	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Bash",
		"transcript_path": transcriptPath,
	})
	require.NoError(t, err)

	w := env.doInvocationRequestWithBody(t, http.MethodPost,
		"/invocations/inv-2/headed_hook?repo_id="+env.RepoID+"&runner=claude-code", payload)
	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)
	var data HeadedHookIngestData
	decodeData(t, resp, &data)
	assert.Equal(t, "inv-2", data.InvocationID)
	assert.Equal(t, []string{transcriptPath}, data.TranscriptPaths)
	assert.Equal(t, int64(len(transcript)), data.ImportedBytes)

	hooksData, err := os.ReadFile(env.Store.InvocationHooksLogPath(env.RepoID, "inv-2"))
	require.NoError(t, err)
	assert.JSONEq(t, string(payload), strings.TrimSpace(string(hooksData)))

	rawData, err := os.ReadFile(env.Store.InvocationRawLogPath(env.RepoID, "inv-2"))
	require.NoError(t, err)
	assert.Equal(t, transcript, string(rawData))

	streamData, err := os.ReadFile(env.Store.InvocationStreamLogPath(env.RepoID, "inv-2"))
	require.NoError(t, err)
	assert.Contains(t, string(streamData), "Done from headed transcript.")

	w = env.doInvocationRequestWithBody(t, http.MethodPost,
		"/invocations/inv-2/headed_hook?repo_id="+env.RepoID+"&runner=claude-code", payload)
	assert.Equal(t, http.StatusOK, w.Code)
	resp = decodeAPIResponse(t, w)
	require.True(t, resp.OK)
	decodeData(t, resp, &data)
	assert.Zero(t, data.ImportedBytes)

	rawDataAfterReplay, err := os.ReadFile(env.Store.InvocationRawLogPath(env.RepoID, "inv-2"))
	require.NoError(t, err)
	assert.Equal(t, rawData, rawDataAfterReplay)
}

func TestHandleHeadedHook_CodexStopSynthesizesFinalMessage(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	_, err := env.Store.UpdateInvocationMeta(env.RepoID, "inv-2", func(meta *store.InvocationMeta) {
		meta.Runner = "codex"
	})
	require.NoError(t, err)

	payload := []byte(`{"hook_event_name":"Stop","session_id":"thread-1","last_assistant_message":"Ready for check from headed Codex."}`)
	w := env.doInvocationRequestWithBody(t, http.MethodPost,
		"/invocations/inv-2/headed_hook?repo_id="+env.RepoID+"&runner=codex", payload)
	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)
	var data HeadedHookIngestData
	decodeData(t, resp, &data)
	assert.NotZero(t, data.ImportedBytes)

	rawData, err := os.ReadFile(env.Store.InvocationRawLogPath(env.RepoID, "inv-2"))
	require.NoError(t, err)
	assert.Contains(t, string(rawData), "Ready for check from headed Codex.")

	streamData, err := os.ReadFile(env.Store.InvocationStreamLogPath(env.RepoID, "inv-2"))
	require.NoError(t, err)
	assert.Contains(t, string(streamData), "Ready for check from headed Codex.")

	meta, err := env.Store.ReadInvocationMeta(env.RepoID, "inv-2")
	require.NoError(t, err)
	assert.Equal(t, "inv-2", meta.InvocationID)
}

func TestHandleHeadedHook_ImportsCodexTranscriptWithoutSemanticStatus(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	_, err := env.Store.UpdateInvocationMeta(env.RepoID, "inv-2", func(meta *store.InvocationMeta) {
		meta.Runner = "codex"
	})
	require.NoError(t, err)

	transcriptPath := filepath.Join(t.TempDir(), "codex-transcript.jsonl")
	transcript := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-1"}`,
		`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"pwd","aggregated_output":"","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"pwd","aggregated_output":"/tmp/work\n","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Done from headed Codex transcript."}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(transcriptPath, []byte(transcript), 0o600))

	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"transcript_path": transcriptPath,
	})
	require.NoError(t, err)

	w := env.doInvocationRequestWithBody(t, http.MethodPost,
		"/invocations/inv-2/headed_hook?repo_id="+env.RepoID+"&runner=codex", payload)
	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeAPIResponse(t, w)
	require.True(t, resp.OK)
	var data HeadedHookIngestData
	decodeData(t, resp, &data)
	assert.Equal(t, int64(len(transcript)), data.ImportedBytes)

	rawData, err := os.ReadFile(env.Store.InvocationRawLogPath(env.RepoID, "inv-2"))
	require.NoError(t, err)
	assert.Equal(t, transcript, string(rawData))

	streamData, err := os.ReadFile(env.Store.InvocationStreamLogPath(env.RepoID, "inv-2"))
	require.NoError(t, err)
	assert.Contains(t, string(streamData), `"kind":"session_start"`)
	assert.Contains(t, string(streamData), `"kind":"tool_start"`)
	assert.Contains(t, string(streamData), `"kind":"tool_end"`)
	assert.Contains(t, string(streamData), "Done from headed Codex transcript.")

	meta, err := env.Store.ReadInvocationMeta(env.RepoID, "inv-2")
	require.NoError(t, err)
	assert.Equal(t, "inv-2", meta.InvocationID)
}

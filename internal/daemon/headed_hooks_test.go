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

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
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
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-2", func(meta *store.InvocationMeta) {
		meta.Runner = "codex"
	}))

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
	require.NotNil(t, meta.SemanticStatus)
	assert.Equal(t, runnerstatus.StatusReady, *meta.SemanticStatus)
}

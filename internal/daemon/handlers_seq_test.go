package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type oversizedStreamEvent struct {
	SchemaVersion string         `json:"schema_version"`
	Seq           uint64         `json:"seq"`
	Timestamp     string         `json:"timestamp"`
	InvocationID  string         `json:"invocation_id"`
	Runner        string         `json:"runner"`
	Kind          string         `json:"kind"`
	Data          map[string]any `json:"data"`
}

func TestLoadMaxStreamSeq_ExtractsSeqFromOversizedLinePrefix(t *testing.T) {
	t.Parallel()

	streamPath := filepath.Join(t.TempDir(), "stream.jsonl")
	oversized := oversizedStreamEvent{
		SchemaVersion: "1.0",
		Seq:           42,
		Timestamp:     "2026-02-05T11:50:10Z",
		InvocationID:  "inv-1",
		Runner:        "claude-code",
		Kind:          "message",
		Data: map[string]any{
			"text": strings.Repeat("x", maxTimelineLineBytes+1024),
		},
	}
	line, err := json.Marshal(oversized)
	require.NoError(t, err)
	require.Greater(t, len(line), maxTimelineLineBytes)
	require.NoError(t, os.WriteFile(streamPath, append(line, '\n'), 0o644))

	assert.Equal(t, uint64(42), loadMaxStreamSeq(streamPath))
}

func TestLoadMaxStreamSeq_ContinuesAfterOversizedRows(t *testing.T) {
	t.Parallel()

	streamPath := filepath.Join(t.TempDir(), "stream.jsonl")
	oversized := oversizedStreamEvent{
		SchemaVersion: "1.0",
		Seq:           3,
		Timestamp:     "2026-02-05T11:50:10Z",
		InvocationID:  "inv-1",
		Runner:        "claude-code",
		Kind:          "message",
		Data: map[string]any{
			"text": strings.Repeat("x", maxTimelineLineBytes+1024),
		},
	}
	oversizedLine, err := json.Marshal(oversized)
	require.NoError(t, err)
	require.Greater(t, len(oversizedLine), maxTimelineLineBytes)

	validLine := []byte(`{"schema_version":"1.0","seq":9,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"inv-1","runner":"claude-code","kind":"message","data":{"text":"ok"}}` + "\n")
	require.NoError(t, os.WriteFile(streamPath, append(append(oversizedLine, '\n'), validLine...), 0o644))

	assert.Equal(t, uint64(9), loadMaxStreamSeq(streamPath))
}

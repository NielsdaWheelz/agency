package invocationevents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type persistedEvent struct {
	Seq  uint64         `json:"seq"`
	Kind string         `json:"kind"`
	Data map[string]any `json:"data"`
}

func readPersistedEvents(t *testing.T, eventsPath string) []persistedEvent {
	t.Helper()

	f, err := os.Open(eventsPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var events []persistedEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev persistedEvent
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &ev))
		events = append(events, ev)
	}
	require.NoError(t, scanner.Err())
	return events
}

func TestWriter_AppendConcurrentCrossSurfaceMonotonicSeq(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer := NewWriter(func() time.Time {
		return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	})

	const perProducer = 40
	kinds := []string{
		"agency.followup_prompt",
		"agency.checkpoint_created",
		"agency.land_started",
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(kinds)*perProducer)
	for producerIdx, kind := range kinds {
		producerIdx := producerIdx
		kind := kind
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				_, err := writer.Append(
					eventsPath,
					"inv-concurrent",
					kind,
					map[string]any{
						"producer": producerIdx,
						"index":    i,
					},
					AppendOptions{},
				)
				if err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	events := readPersistedEvents(t, eventsPath)
	require.Len(t, events, len(kinds)*perProducer)
	for i, ev := range events {
		assert.Equal(t, uint64(i+1), ev.Seq, "file order should match monotonic sequence allocation")
		assert.NotEmpty(t, ev.Kind)
	}
}

func TestWriter_Append_IdempotentFollowUpClientRequestID(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer := NewWriter(time.Now)

	first, err := writer.Append(
		eventsPath,
		"inv-followup",
		"agency.followup_prompt",
		map[string]any{
			"text":              "first follow-up",
			"client_request_id": "req-1",
		},
		AppendOptions{
			IdempotencyDataKey:   "client_request_id",
			IdempotencyDataValue: "req-1",
		},
	)
	require.NoError(t, err)
	assert.False(t, first.AlreadyApplied)
	assert.Equal(t, uint64(1), first.Seq)

	second, err := writer.Append(
		eventsPath,
		"inv-followup",
		"agency.followup_prompt",
		map[string]any{
			"text":              "retry payload",
			"client_request_id": "req-1",
		},
		AppendOptions{
			IdempotencyDataKey:   "client_request_id",
			IdempotencyDataValue: "req-1",
		},
	)
	require.NoError(t, err)
	assert.True(t, second.AlreadyApplied)
	assert.Equal(t, first.Seq, second.Seq)

	events := readPersistedEvents(t, eventsPath)
	require.Len(t, events, 1)
	assert.Equal(t, uint64(1), events[0].Seq)
	assert.Equal(t, "agency.followup_prompt", events[0].Kind)
}

func TestWriter_Append_UsesPrivatePermissions(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "nested", "events.jsonl")
	writer := NewWriter(time.Now)

	_, err := writer.Append(
		eventsPath,
		"inv-perms",
		"agency.checkpoint_created",
		map[string]any{"checkpoint_id": 1},
		AppendOptions{},
	)
	require.NoError(t, err)

	dirInfo, err := os.Stat(filepath.Dir(eventsPath))
	require.NoError(t, err)
	fileInfo, err := os.Stat(eventsPath)
	require.NoError(t, err)

	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

func TestWriter_Append_ContinuesAfterOversizedLegacyRows(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	legacyOversized := eventLine{
		SchemaVersion: "1.0",
		Seq:           5,
		Timestamp:     "2026-03-01T12:00:00Z",
		InvocationID:  "inv-legacy",
		Kind:          "agency.followup_prompt",
		Data: map[string]any{
			"text": strings.Repeat("x", maxEventLineBytes+1024),
		},
	}
	legacyLine, err := json.Marshal(legacyOversized)
	require.NoError(t, err)
	require.Greater(t, len(legacyLine), maxEventLineBytes)

	validLine := []byte(`{"schema_version":"1.0","seq":6,"timestamp":"2026-03-01T12:00:01Z","invocation_id":"inv-legacy","kind":"agency.followup_prompt","data":{"client_request_id":"req-existing"}}` + "\n")
	require.NoError(t, os.WriteFile(eventsPath, append(append(legacyLine, '\n'), validLine...), 0o600))

	writer := NewWriter(time.Now)
	result, err := writer.Append(
		eventsPath,
		"inv-legacy",
		"agency.followup_prompt",
		map[string]any{"client_request_id": "req-new"},
		AppendOptions{
			IdempotencyDataKey:   "client_request_id",
			IdempotencyDataValue: "req-new",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, uint64(7), result.Seq)
	assert.False(t, result.AlreadyApplied)
}

func TestWriter_Append_OversizedLegacyTailPreservesSeq(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	legacyOversized := eventLine{
		SchemaVersion: "1.0",
		Seq:           9,
		Timestamp:     "2026-03-01T12:00:00Z",
		InvocationID:  "inv-tail",
		Kind:          "agency.followup_prompt",
		Data: map[string]any{
			"text": strings.Repeat("y", maxEventLineBytes+1024),
		},
	}
	legacyLine, err := json.Marshal(legacyOversized)
	require.NoError(t, err)
	require.Greater(t, len(legacyLine), maxEventLineBytes)
	require.NoError(t, os.WriteFile(eventsPath, append(legacyLine, '\n'), 0o600))

	writer := NewWriter(time.Now)
	result, err := writer.Append(
		eventsPath,
		"inv-tail",
		"agency.followup_prompt",
		map[string]any{"text": "fresh"},
		AppendOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, uint64(10), result.Seq)
}

func TestWriter_Append_RejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer := NewWriter(time.Now)

	_, err := writer.Append(
		eventsPath,
		"inv-oversized",
		"agency.followup_prompt",
		map[string]any{
			"text": strings.Repeat("z", maxEventLineBytes+1024),
		},
		AppendOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("%d", maxEventLineBytes))
}

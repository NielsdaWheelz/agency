package eventlog

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
	SchemaVersion string         `json:"schema_version"`
	Seq           uint64         `json:"seq"`
	Kind          string         `json:"kind"`
	Data          map[string]any `json:"data"`
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
	writer := NewWriter("invocation_id", func() time.Time {
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

func TestWriter_Append_RecordsConfiguredIDField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		idField  string
		entityID string
	}{
		{"invocation_id", "inv-1"},
		{"worktree_id", "wt-1"},
		{"task_id", "task-1"},
		{"repo_id", "repo-1"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.idField, func(t *testing.T) {
			t.Parallel()

			eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
			writer := NewWriter(tc.idField, func() time.Time {
				return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
			})

			_, err := writer.Append(eventsPath, tc.entityID, "agency.kind", map[string]any{"k": "v"}, AppendOptions{})
			require.NoError(t, err)

			f, err := os.Open(eventsPath)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			var line map[string]any
			scanner := bufio.NewScanner(f)
			require.True(t, scanner.Scan())
			require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))

			assert.Equal(t, tc.entityID, line[tc.idField])
			assert.Equal(t, "1.0", line["schema_version"])
			assert.Equal(t, "agency.kind", line["kind"])
		})
	}
}

func TestWriter_Append_IdempotentClientRequestID(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer := NewWriter("invocation_id", time.Now)

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
	writer := NewWriter("invocation_id", time.Now)

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

func TestWriter_Append_RejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer := NewWriter("invocation_id", time.Now)

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

package repoevents

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
	Seq    uint64         `json:"seq"`
	RepoID string         `json:"repo_id"`
	Kind   string         `json:"kind"`
	Data   map[string]any `json:"data"`
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
		"agency.repo_seeded",
		"agency.repo_updated",
		"agency.repo_synced",
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
					"repo-concurrent",
					kind,
					map[string]any{
						"producer": producerIdx,
						"index":    i,
					},
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
		assert.Equal(t, "repo-concurrent", ev.RepoID)
		assert.NotEmpty(t, ev.Kind)
	}
}

func TestWriter_Append_UsesPrivatePermissions(t *testing.T) {
	t.Parallel()

	eventsPath := filepath.Join(t.TempDir(), "nested", "events.jsonl")
	writer := NewWriter(time.Now)

	_, err := writer.Append(
		eventsPath,
		"repo-perms",
		"agency.repo_seeded",
		map[string]any{"repo_id": "repo-perms"},
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
	writer := NewWriter(time.Now)

	_, err := writer.Append(
		eventsPath,
		"repo-oversized",
		"agency.repo_seeded",
		map[string]any{
			"text": strings.Repeat("z", maxEventLineBytes+1024),
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("%d", maxEventLineBytes))
}

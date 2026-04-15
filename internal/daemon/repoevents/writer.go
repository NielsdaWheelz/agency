package repoevents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/jsonl"
)

const maxEventLineBytes = stream.MaxLineSize

// AppendResult describes the outcome of an append attempt.
type AppendResult struct {
	Seq uint64
}

// Appender appends repo-scoped events.
type Appender interface {
	Append(eventsPath, repoID, kind string, data map[string]any) (AppendResult, error)
}

// Writer appends repo events with process-wide in-memory locking per events path.
type Writer struct {
	clock func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewWriter creates a writer using the provided clock.
func NewWriter(clock func() time.Time) *Writer {
	if clock == nil {
		clock = time.Now
	}
	return &Writer{
		clock: clock,
		locks: make(map[string]*sync.Mutex),
	}
}

type eventLine struct {
	SchemaVersion string         `json:"schema_version"`
	Seq           uint64         `json:"seq"`
	Timestamp     string         `json:"timestamp"`
	RepoID        string         `json:"repo_id"`
	Kind          string         `json:"kind"`
	Data          map[string]any `json:"data,omitempty"`
}

// Append appends one repo event under a single sequencing authority.
func (w *Writer) Append(eventsPath, repoID, kind string, data map[string]any) (AppendResult, error) {
	if eventsPath == "" {
		return AppendResult{}, fmt.Errorf("events path is required")
	}
	if repoID == "" {
		return AppendResult{}, fmt.Errorf("repo id is required")
	}
	if kind == "" {
		return AppendResult{}, fmt.Errorf("event kind is required")
	}

	lock := w.lockForPath(eventsPath)
	lock.Lock()
	defer lock.Unlock()

	if err := ensurePrivateEventsPath(eventsPath); err != nil {
		return AppendResult{}, err
	}

	seq, err := nextSeq(eventsPath)
	if err != nil {
		return AppendResult{}, err
	}

	line := eventLine{
		SchemaVersion: "1.0",
		Seq:           seq,
		Timestamp:     w.clock().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		Kind:          kind,
		Data:          data,
	}

	payload, err := json.Marshal(line)
	if err != nil {
		return AppendResult{}, err
	}
	if len(payload)+1 > maxEventLineBytes {
		return AppendResult{}, fmt.Errorf("event payload exceeds max line size of %d bytes", maxEventLineBytes)
	}
	payload = append(payload, '\n')

	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return AppendResult{}, err
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(payload); err != nil {
		return AppendResult{}, err
	}
	if err := f.Sync(); err != nil {
		return AppendResult{}, err
	}
	if err := os.Chmod(eventsPath, 0o600); err != nil {
		return AppendResult{}, err
	}

	return AppendResult{Seq: seq}, nil
}

func (w *Writer) lockForPath(eventsPath string) *sync.Mutex {
	key := canonicalEventPath(eventsPath)

	w.mu.Lock()
	defer w.mu.Unlock()

	if existing, ok := w.locks[key]; ok {
		return existing
	}
	lock := &sync.Mutex{}
	w.locks[key] = lock
	return lock
}

func canonicalEventPath(eventsPath string) string {
	clean := filepath.Clean(eventsPath)
	abs, err := filepath.Abs(clean)
	if err != nil {
		abs = clean
	}

	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}

	parent := filepath.Dir(abs)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolvedParent, filepath.Base(abs))
	}

	return abs
}

func ensurePrivateEventsPath(eventsPath string) error {
	dir := filepath.Dir(eventsPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	return nil
}

func nextSeq(eventsPath string) (uint64, error) {
	f, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}
	defer func() { _ = f.Close() }()

	var maxSeq uint64
	visitErr := jsonl.Visit(f, maxEventLineBytes, jsonl.VisitOptions{OversizedPrefixBytes: 1024}, func(scanned jsonl.Line) error {
		if scanned.Oversized {
			if seq, ok := jsonl.ExtractUintField(scanned.Bytes, "seq"); ok && seq > maxSeq {
				maxSeq = seq
			}
			return nil
		}

		var persisted eventLine
		if err := json.Unmarshal(scanned.Bytes, &persisted); err != nil {
			return nil
		}
		if persisted.Seq > maxSeq {
			maxSeq = persisted.Seq
		}
		return nil
	})
	if visitErr != nil {
		return 0, visitErr
	}

	return maxSeq + 1, nil
}

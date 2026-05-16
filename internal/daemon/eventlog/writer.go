// Package eventlog owns the daemon mutation event-log mechanism: atomic,
// locked, private-permission JSONL appends with monotonic sequencing and
// optional idempotency. Domain packages own their event-kind constants and
// construct a Writer with the entity-id field name they record.
package eventlog

import (
	"bytes"
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

// idFieldPlaceholder is the JSON key carried by eventLine.EntityID before it is
// rewritten to the writer's configured entity-id field name.
const idFieldPlaceholder = "entity_id"

// AppendOptions configures optional idempotency behavior for an append.
type AppendOptions struct {
	// IdempotencyDataKey is the data key used to detect duplicates.
	// If empty, no idempotency check is performed.
	IdempotencyDataKey string

	// IdempotencyDataValue is the data value used to detect duplicates.
	IdempotencyDataValue string
}

// AppendResult describes the outcome of an append attempt.
type AppendResult struct {
	Seq            uint64
	AlreadyApplied bool
}

// Appender appends entity-scoped events. It is the seam callers depend on so
// tests can substitute a failing implementation.
type Appender interface {
	Append(eventsPath, entityID, kind string, data map[string]any, opts AppendOptions) (AppendResult, error)
}

// Writer appends events with process-wide in-memory locking per events path.
// The entity-id field name is fixed at construction.
type Writer struct {
	idField string
	clock   func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewWriter creates a writer that records the entity id under idField (for
// example "task_id") using the provided clock.
func NewWriter(idField string, clock func() time.Time) *Writer {
	if clock == nil {
		clock = time.Now
	}
	return &Writer{
		idField: idField,
		clock:   clock,
		locks:   make(map[string]*sync.Mutex),
	}
}

type eventLine struct {
	SchemaVersion string         `json:"schema_version"`
	Seq           uint64         `json:"seq"`
	Timestamp     string         `json:"timestamp"`
	EntityID      string         `json:"entity_id"`
	Kind          string         `json:"kind"`
	Data          map[string]any `json:"data,omitempty"`
}

// Append appends one event under a single sequencing authority.
func (w *Writer) Append(eventsPath, entityID, kind string, data map[string]any, opts AppendOptions) (AppendResult, error) {
	if eventsPath == "" {
		return AppendResult{}, fmt.Errorf("events path is required")
	}
	if entityID == "" {
		return AppendResult{}, fmt.Errorf("entity id is required")
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

	maxSeq, duplicateSeq, duplicateFound, err := scanExistingEvents(eventsPath, kind, opts)
	if err != nil {
		return AppendResult{}, err
	}
	if duplicateFound {
		return AppendResult{Seq: duplicateSeq, AlreadyApplied: true}, nil
	}

	seq := maxSeq + 1
	payload, err := json.Marshal(eventLine{
		SchemaVersion: "1.0",
		Seq:           seq,
		Timestamp:     w.clock().UTC().Format(time.RFC3339),
		EntityID:      entityID,
		Kind:          kind,
		Data:          data,
	})
	if err != nil {
		return AppendResult{}, err
	}
	payload = bytes.Replace(payload,
		[]byte(`"`+idFieldPlaceholder+`":`),
		[]byte(`"`+w.idField+`":`), 1)
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

	// For existing files, resolve fully to collapse symlink aliases.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}

	// For not-yet-created files, resolve the parent when possible.
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

func scanExistingEvents(eventsPath, kind string, opts AppendOptions) (maxSeq, duplicateSeq uint64, duplicateFound bool, err error) {
	f, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	defer func() { _ = f.Close() }()

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
		if opts.IdempotencyDataKey == "" {
			return nil
		}
		if persisted.Kind != kind || persisted.Data == nil {
			return nil
		}
		if value, ok := persisted.Data[opts.IdempotencyDataKey].(string); ok && value == opts.IdempotencyDataValue {
			duplicateFound = true
			duplicateSeq = persisted.Seq
		}
		return nil
	})
	if visitErr != nil {
		return 0, 0, false, visitErr
	}
	if duplicateFound {
		return maxSeq, duplicateSeq, true, nil
	}
	return maxSeq, 0, false, nil
}

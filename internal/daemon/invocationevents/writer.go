package invocationevents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxEventLineBytes = 4 * 1024 * 1024

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

// Appender appends invocation events.
type Appender interface {
	Append(eventsPath, invocationID, kind string, data map[string]any, opts AppendOptions) (AppendResult, error)
}

// Writer appends invocation events with process-wide in-memory locking per
// invocation events path.
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
	InvocationID  string         `json:"invocation_id"`
	Kind          string         `json:"kind"`
	Data          map[string]any `json:"data,omitempty"`
}

// Append appends one invocation event under a single sequencing authority.
func (w *Writer) Append(eventsPath, invocationID, kind string, data map[string]any, opts AppendOptions) (AppendResult, error) {
	if eventsPath == "" {
		return AppendResult{}, fmt.Errorf("events path is required")
	}
	if invocationID == "" {
		return AppendResult{}, fmt.Errorf("invocation id is required")
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
		return AppendResult{
			Seq:            duplicateSeq,
			AlreadyApplied: true,
		}, nil
	}

	seq := maxSeq + 1
	line := eventLine{
		SchemaVersion: "1.0",
		Seq:           seq,
		Timestamp:     w.clock().UTC().Format(time.RFC3339),
		InvocationID:  invocationID,
		Kind:          kind,
		Data:          data,
	}

	payload, err := json.Marshal(line)
	if err != nil {
		return AppendResult{}, err
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

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxEventLineBytes)
	for scanner.Scan() {
		var line eventLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Seq > maxSeq {
			maxSeq = line.Seq
		}
		if opts.IdempotencyDataKey == "" {
			continue
		}
		if line.Kind != kind || line.Data == nil {
			continue
		}
		if value, ok := line.Data[opts.IdempotencyDataKey].(string); ok && value == opts.IdempotencyDataValue {
			return maxSeq, line.Seq, true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, false, err
	}
	return maxSeq, 0, false, nil
}

package stream

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

// MaxLineSize is the maximum size of a single line (8 MB).
// Lines exceeding this are written to raw.jsonl but not parsed.
const MaxLineSize = 8 * 1024 * 1024

// ParseErrorThrottleCount is how many parse errors before throttling emission.
const ParseErrorThrottleCount = 10

// ParseErrorThrottleInterval is the minimum time between parse_error events.
const ParseErrorThrottleInterval = 5 * time.Second

var (
	// ErrRawLogWriteFailed indicates raw.jsonl persistence failed.
	ErrRawLogWriteFailed = errors.New("raw log write failed")

	// ErrNormalizedStreamWriteFailed indicates stream.jsonl persistence failed.
	ErrNormalizedStreamWriteFailed = errors.New("normalized stream write failed")
)

// Parser handles line-by-line parsing of runner output.
type Parser struct {
	// InvocationID is the invocation being parsed.
	InvocationID string

	// Runner is the runner id associated with this parser instance.
	Runner string

	// Adapter is the runner-specific parser.
	Adapter Adapter

	// Clock returns the current time (injectable for testing).
	Clock func() time.Time

	// mu protects mutable state.
	mu sync.Mutex

	// seq is the monotonic sequence counter for normalized events.
	seq uint64

	// semanticStatus is the current semantic status.
	semanticStatus *runnerstatus.Status

	// semanticStatusUpdatedAt is when semantic status was last updated.
	semanticStatusUpdatedAt time.Time

	// lastOutputAt tracks when the last output was received.
	lastOutputAt atomic.Int64

	// parseErrorCount tracks the total number of parse errors.
	parseErrorCount int

	// lastParseErrorEmit is when the last parse_error event was emitted.
	lastParseErrorEmit time.Time

	// stopped indicates the parser has been stopped.
	stopped bool

	// checkpointNotify is called when a mutating tool completes.
	// Set via SetCheckpointNotify before calling StreamAndParse.
	checkpointNotify func(CheckpointNotification)

	// sessionStartNotify is called when a session_start event contains an
	// explicit session/thread identifier.
	// Set via SetSessionStartNotify before calling StreamAndParse.
	sessionStartNotify func(SessionStartNotification)

	// pendingMutatingTools tracks mutating tool names from the latest assistant
	// message, used to emit a checkpoint notification after the tool results arrive.
	pendingMutatingTools []string
}

// NewParser creates a new parser for the given runner.
func NewParser(invocationID, runner string, clock func() time.Time) *Parser {
	adapter := GetAdapter(runner)
	return &Parser{
		InvocationID: invocationID,
		Runner:       runner,
		Adapter:      adapter,
		Clock:        clock,
	}
}

// SetInitialSeq seeds the parser sequence counter.
// Used when appending to an existing stream log (e.g., restart-in-place).
func (p *Parser) SetInitialSeq(seq uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq = seq
}

// GetSemanticStatus returns the current semantic status.
func (p *Parser) GetSemanticStatus() *runnerstatus.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.semanticStatus
}

// GetSemanticStatusUpdatedAt returns when the semantic status was last updated.
func (p *Parser) GetSemanticStatusUpdatedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.semanticStatusUpdatedAt
}

// GetLastOutputAt returns the last output timestamp as UnixNano.
func (p *Parser) GetLastOutputAt() int64 {
	return p.lastOutputAt.Load()
}

// SetCheckpointNotify registers a callback invoked when a mutating tool completes.
// Must be called before StreamAndParse. Safe to call with nil (no-op).
func (p *Parser) SetCheckpointNotify(fn func(CheckpointNotification)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.checkpointNotify = fn
}

// SetSessionStartNotify registers a callback invoked when a parsed session_start
// event includes a session/thread identifier. Safe to call with nil (no-op).
func (p *Parser) SetSessionStartNotify(fn func(SessionStartNotification)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionStartNotify = fn
}

// Stop stops the parser.
func (p *Parser) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
}

// StreamAndParse reads from reader line-by-line, writes verbatim to rawFile,
// parses each line, and writes normalized events to streamFile.
// This function blocks until EOF or error.
func (p *Parser) StreamAndParse(reader io.Reader, rawFile, streamFile *os.File) error {
	if p.Adapter == nil {
		// No adapter - just stream raw output without parsing
		return p.streamRawOnly(reader, rawFile)
	}

	bufReader := bufio.NewReader(reader)

	for {
		p.mu.Lock()
		stopped := p.stopped
		p.mu.Unlock()
		if stopped {
			return nil
		}

		// Read one logical line while persisting raw bytes incrementally.
		line, oversized, hasData, err := p.readAndPersistLine(bufReader, rawFile)
		if hasData {
			// Update last output timestamp
			p.lastOutputAt.Store(p.Clock().UnixNano())

			if oversized {
				// Line too large - emit parse_error event
				if parseErr := p.emitParseError(streamFile, "line_too_large"); parseErr != nil {
					return parseErr
				}
			} else if parseErr := p.parseAndWriteLine(line, streamFile); parseErr != nil {
				return parseErr
			}
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// readAndPersistLine reads one logical line while writing every chunk directly
// to rawFile. Parsing payload buffering is capped at MaxLineSize, and oversized
// lines are drained without retaining full line bytes in memory.
func (p *Parser) readAndPersistLine(reader *bufio.Reader, rawFile *os.File) ([]byte, bool, bool, error) {
	line := make([]byte, 0, 4096)
	oversized := false
	hasData := false

	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			hasData = true
			if _, writeErr := rawFile.Write(chunk); writeErr != nil {
				return nil, false, hasData, fmt.Errorf("%w: %v", ErrRawLogWriteFailed, writeErr)
			}

			if !oversized {
				if len(line)+len(chunk) <= MaxLineSize {
					line = append(line, chunk...)
				} else {
					oversized = true
					line = nil
				}
			}
		}

		if err == nil {
			return line, oversized, hasData, nil
		}

		if err == bufio.ErrBufferFull {
			continue
		}

		if err == io.EOF {
			if hasData {
				return line, oversized, hasData, io.EOF
			}
			return nil, false, false, io.EOF
		}

		return line, oversized, hasData, err
	}
}

// parseAndWriteLine parses a single line and writes normalized events.
func (p *Parser) parseAndWriteLine(line []byte, streamFile *os.File) error {
	// Strip trailing newline for parsing
	trimmedLine := line
	if len(trimmedLine) > 0 && trimmedLine[len(trimmedLine)-1] == '\n' {
		trimmedLine = trimmedLine[:len(trimmedLine)-1]
	}
	if len(trimmedLine) > 0 && trimmedLine[len(trimmedLine)-1] == '\r' {
		trimmedLine = trimmedLine[:len(trimmedLine)-1]
	}

	if len(trimmedLine) == 0 {
		return nil
	}

	result, err := p.Adapter.ParseLine(trimmedLine)
	if err != nil {
		// Parse error - emit parse_error event
		return p.emitParseError(streamFile, "json_parse_error")
	}

	if result == nil {
		return nil
	}

	// Write normalized events
	for _, event := range result.Events {
		if writeErr := p.writeNormalizedEvent(event, streamFile); writeErr != nil {
			return writeErr
		}

		// Detect mutating tools and emit checkpoint notification.
		p.maybeNotifyCheckpoint(event)
		p.maybeNotifySessionStart(event)
	}

	// Update semantic status if provided
	if result.SemanticStatus != nil {
		p.updateSemanticStatus(result.SemanticStatus)
	}
	return nil
}

// writeNormalizedEvent writes a normalized event to stream.jsonl.
func (p *Parser) writeNormalizedEvent(event *NormalizedEvent, streamFile *os.File) error {
	p.mu.Lock()
	p.seq++
	event.Seq = p.seq
	event.SchemaVersion = SchemaVersion
	event.Timestamp = p.Clock().UTC().Format(time.RFC3339)
	event.InvocationID = p.InvocationID
	event.Runner = p.Runner
	p.mu.Unlock()

	data, err := event.Marshal()
	if err != nil {
		return fmt.Errorf("marshal normalized event: %w", err)
	}

	if _, err := streamFile.Write(data); err != nil {
		return fmt.Errorf("%w: %v", ErrNormalizedStreamWriteFailed, err)
	}
	return nil
}

// updateSemanticStatus updates the semantic status.
func (p *Parser) updateSemanticStatus(status *runnerstatus.Status) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.semanticStatus = status
	p.semanticStatusUpdatedAt = p.Clock()
}

// emitParseError emits a parse_error event with throttling.
func (p *Parser) emitParseError(streamFile *os.File, reason string) error {
	p.mu.Lock()
	p.parseErrorCount++
	count := p.parseErrorCount
	lastEmit := p.lastParseErrorEmit
	now := p.Clock()

	// Throttle emission: emit on first error, then every 10 errors or 5 seconds
	shouldEmit := count == 1 ||
		(count%ParseErrorThrottleCount == 0) ||
		(now.Sub(lastEmit) >= ParseErrorThrottleInterval)

	if shouldEmit {
		p.lastParseErrorEmit = now
	}
	p.mu.Unlock()

	if !shouldEmit {
		return nil
	}

	event := &NormalizedEvent{
		Kind: EventKindParseError,
		Data: map[string]interface{}{
			"parse_error_count": count,
			"reason":            reason,
		},
	}

	return p.writeNormalizedEvent(event, streamFile)
}

// streamRawOnly streams data to rawFile without parsing.
// Used when no adapter is available for the runner.
func (p *Parser) streamRawOnly(reader io.Reader, rawFile *os.File) error {
	buf := make([]byte, 4096)
	for {
		p.mu.Lock()
		stopped := p.stopped
		p.mu.Unlock()
		if stopped {
			return nil
		}

		n, err := reader.Read(buf)
		if n > 0 {
			if _, writeErr := rawFile.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("%w: %v", ErrRawLogWriteFailed, writeErr)
			}
			p.lastOutputAt.Store(p.Clock().UnixNano())
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// maybeNotifyCheckpoint checks if a normalized event represents a mutating tool
// completion and emits a checkpoint notification if so.
//
// Strategy: when an assistant message with has_tool_use=true arrives, we record
// the mutating tool names. When a subsequent user message (tool result) arrives,
// we emit the notification. This ensures the checkpoint captures the tool's
// effect on the filesystem.
func (p *Parser) maybeNotifyCheckpoint(event *NormalizedEvent) {
	p.mu.Lock()
	notifyFn := p.checkpointNotify
	p.mu.Unlock()

	if notifyFn == nil {
		return
	}

	if event.Kind == EventKindMessage {
		role, _ := event.Data["role"].(string)
		hasToolUse, _ := event.Data["has_tool_use"].(bool)

		if role == "assistant" && hasToolUse {
			// Record mutating tools from this assistant turn
			var mutating []string
			if toolNames, ok := event.Data["tool_names"].([]string); ok {
				for _, name := range toolNames {
					if MutatingStreamTools[name] {
						mutating = append(mutating, name)
					}
				}
			} else if toolNamesIface, ok := event.Data["tool_names"].([]interface{}); ok {
				for _, nameIface := range toolNamesIface {
					if name, ok := nameIface.(string); ok && MutatingStreamTools[name] {
						mutating = append(mutating, name)
					}
				}
			}
			p.mu.Lock()
			p.pendingMutatingTools = mutating
			p.mu.Unlock()
		} else if role == "user" {
			// User message (tool result) — emit notification if we have pending tools
			p.mu.Lock()
			pending := p.pendingMutatingTools
			p.pendingMutatingTools = nil
			p.mu.Unlock()

			if len(pending) > 0 {
				notifyFn(CheckpointNotification{
					ToolName:  pending[0],
					ToolNames: pending,
					Seq:       event.Seq,
				})
			}
		}
	}
}

func (p *Parser) maybeNotifySessionStart(event *NormalizedEvent) {
	if event.Kind != EventKindSessionStart {
		return
	}

	p.mu.Lock()
	notifyFn := p.sessionStartNotify
	p.mu.Unlock()
	if notifyFn == nil {
		return
	}

	sessionID := ""
	if v, ok := event.Data["session_id"].(string); ok {
		sessionID = strings.TrimSpace(v)
	}
	if sessionID == "" {
		if v, ok := event.Data["thread_id"].(string); ok {
			sessionID = strings.TrimSpace(v)
		}
	}
	if sessionID == "" {
		return
	}

	notifyFn(SessionStartNotification{
		SessionID: sessionID,
		Seq:       event.Seq,
	})
}

// ClearSemanticStatus clears the semantic status (called when lifecycle becomes failed).
func (p *Parser) ClearSemanticStatus() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.semanticStatus = nil
	p.semanticStatusUpdatedAt = p.Clock()
}

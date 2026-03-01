package stream

import (
	"bufio"
	"io"
	"os"
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

// Parser handles line-by-line parsing of runner output.
type Parser struct {
	// InvocationID is the invocation being parsed.
	InvocationID string

	// Runner is the runner type (claude, codex).
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

		// Read a line (including the newline)
		line, err := p.readLine(bufReader)
		if len(line) > 0 {
			// Always write to raw.jsonl verbatim (best-effort)
			_, _ = rawFile.Write(line)

			// Update last output timestamp
			p.lastOutputAt.Store(p.Clock().UnixNano())

			// Parse and write to stream.jsonl if line is not too large
			if len(line) <= MaxLineSize {
				p.parseAndWriteLine(line, streamFile)
			} else {
				// Line too large - emit parse_error event
				p.emitParseError(streamFile, "line_too_large")
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

// readLine reads a single line from the reader, handling the case where
// the final line may not have a trailing newline.
func (p *Parser) readLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err == nil {
		return line, nil
	}

	if err == io.EOF {
		if len(line) > 0 {
			return line, io.EOF
		}
		return nil, io.EOF
	}

	return line, err
}

// parseAndWriteLine parses a single line and writes normalized events.
func (p *Parser) parseAndWriteLine(line []byte, streamFile *os.File) {
	// Strip trailing newline for parsing
	trimmedLine := line
	if len(trimmedLine) > 0 && trimmedLine[len(trimmedLine)-1] == '\n' {
		trimmedLine = trimmedLine[:len(trimmedLine)-1]
	}
	if len(trimmedLine) > 0 && trimmedLine[len(trimmedLine)-1] == '\r' {
		trimmedLine = trimmedLine[:len(trimmedLine)-1]
	}

	if len(trimmedLine) == 0 {
		return
	}

	result, err := p.Adapter.ParseLine(trimmedLine)
	if err != nil {
		// Parse error - emit parse_error event
		p.emitParseError(streamFile, "json_parse_error")
		return
	}

	if result == nil {
		return
	}

	// Write normalized events
	for _, event := range result.Events {
		p.writeNormalizedEvent(event, streamFile)
	}

	// Update semantic status if provided
	if result.SemanticStatus != nil {
		p.updateSemanticStatus(result.SemanticStatus)
	}
}

// writeNormalizedEvent writes a normalized event to stream.jsonl.
func (p *Parser) writeNormalizedEvent(event *NormalizedEvent, streamFile *os.File) {
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
		return
	}

	_, _ = streamFile.Write(data)
}

// updateSemanticStatus updates the semantic status.
func (p *Parser) updateSemanticStatus(status *runnerstatus.Status) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.semanticStatus = status
	p.semanticStatusUpdatedAt = p.Clock()
}

// emitParseError emits a parse_error event with throttling.
func (p *Parser) emitParseError(streamFile *os.File, reason string) {
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
		return
	}

	event := &NormalizedEvent{
		Kind: EventKindParseError,
		Data: map[string]interface{}{
			"parse_error_count": count,
			"reason":            reason,
		},
	}

	p.writeNormalizedEvent(event, streamFile)
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
			_, _ = rawFile.Write(buf[:n])
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

// ClearSemanticStatus clears the semantic status (called when lifecycle becomes failed).
func (p *Parser) ClearSemanticStatus() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.semanticStatus = nil
	p.semanticStatusUpdatedAt = p.Clock()
}

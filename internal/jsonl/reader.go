package jsonl

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
)

// Line describes one logical JSONL row.
type Line struct {
	// Number is the 1-indexed logical line number.
	Number int

	// Bytes is the line payload with trailing line endings removed.
	// For oversized lines this contains only the configured prefix.
	Bytes []byte

	// Oversized is true when the logical line exceeded MaxLineBytes.
	Oversized bool
}

// VisitOptions controls oversized-line handling.
type VisitOptions struct {
	// OversizedPrefixBytes controls how many bytes are retained for oversized
	// lines. Set to 0 to retain no prefix bytes.
	OversizedPrefixBytes int
}

// Visit walks logical JSONL lines from r while tolerating oversized rows.
// Oversized rows are surfaced to visitFn with Line.Oversized=true and do not
// abort iteration.
func Visit(r io.Reader, maxLineBytes int, opts VisitOptions, visitFn func(Line) error) error {
	if r == nil {
		return fmt.Errorf("jsonl reader is required")
	}
	if visitFn == nil {
		return fmt.Errorf("jsonl visit callback is required")
	}
	if maxLineBytes <= 0 {
		return fmt.Errorf("max line bytes must be positive")
	}

	prefixBytes := opts.OversizedPrefixBytes
	if prefixBytes < 0 {
		prefixBytes = 0
	}

	reader := bufio.NewReader(r)
	lineNumber := 0
	for {
		line, prefix, oversized, hasData, err := readLogicalLine(reader, maxLineBytes, prefixBytes)
		if hasData {
			lineNumber++
			payload := line
			if oversized {
				payload = prefix
			}
			if visitErr := visitFn(Line{
				Number:    lineNumber,
				Bytes:     trimLineEndings(payload),
				Oversized: oversized,
			}); visitErr != nil {
				return visitErr
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

func readLogicalLine(reader *bufio.Reader, maxLineBytes int, prefixBytes int) ([]byte, []byte, bool, bool, error) {
	line := make([]byte, 0, 4096)
	prefix := make([]byte, 0, min(prefixBytes, 4096))
	logicalLen := 0
	oversized := false
	hasData := false

	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			hasData = true
			if prefixBytes > 0 && len(prefix) < prefixBytes {
				remaining := prefixBytes - len(prefix)
				if remaining > len(chunk) {
					remaining = len(chunk)
				}
				prefix = append(prefix, chunk[:remaining]...)
			}

			if !oversized {
				chunkLogicalLen := logicalChunkLength(chunk)
				if logicalLen+chunkLogicalLen <= maxLineBytes {
					line = append(line, chunk...)
					logicalLen += chunkLogicalLen
				} else {
					oversized = true
					line = nil
				}
			}
		}

		switch err {
		case nil:
			return line, prefix, oversized, hasData, nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if hasData {
				return line, prefix, oversized, hasData, io.EOF
			}
			return nil, nil, false, false, io.EOF
		default:
			return line, prefix, oversized, hasData, err
		}
	}
}

func logicalChunkLength(chunk []byte) int {
	n := len(chunk)
	if n == 0 {
		return 0
	}
	if chunk[n-1] == '\n' {
		n--
		if n > 0 && chunk[n-1] == '\r' {
			n--
		}
	}
	return n
}

func trimLineEndings(line []byte) []byte {
	trimmed := line
	for len(trimmed) > 0 {
		last := trimmed[len(trimmed)-1]
		if last != '\n' && last != '\r' {
			break
		}
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ExtractUintField scans a JSON prefix for a numeric object field.
// It tolerates truncated JSON as long as `"field":<digits>` is present.
func ExtractUintField(line []byte, field string) (uint64, bool) {
	field = strconv.Quote(field)
	if field == `""` || len(line) == 0 {
		return 0, false
	}

	needle := []byte(field)
	offset := 0
	for {
		idx := bytes.Index(line[offset:], needle)
		if idx < 0 {
			return 0, false
		}
		pos := offset + idx + len(needle)

		for pos < len(line) && isJSONWhitespace(line[pos]) {
			pos++
		}
		if pos >= len(line) || line[pos] != ':' {
			offset = offset + idx + len(needle)
			continue
		}
		pos++
		for pos < len(line) && isJSONWhitespace(line[pos]) {
			pos++
		}
		start := pos
		for pos < len(line) && line[pos] >= '0' && line[pos] <= '9' {
			pos++
		}
		if start == pos {
			offset = offset + idx + len(needle)
			continue
		}
		value, err := strconv.ParseUint(string(line[start:pos]), 10, 64)
		if err != nil {
			offset = offset + idx + len(needle)
			continue
		}
		return value, true
	}
}

func isJSONWhitespace(ch byte) bool {
	switch ch {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}

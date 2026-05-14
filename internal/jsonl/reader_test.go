package jsonl

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVisit_ContinuesAfterOversizedLines(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", 16)
	maxLineBytes := len(`{"seq":1}`)
	input := strings.Join([]string{
		`{"seq":1}`,
		oversized,
		`{"seq":2}`,
	}, "\n") + "\n"

	var seen []Line
	err := Visit(strings.NewReader(input), maxLineBytes, VisitOptions{OversizedPrefixBytes: 5}, func(line Line) error {
		clone := line
		clone.Bytes = append([]byte(nil), line.Bytes...)
		seen = append(seen, clone)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 3)

	assert.Equal(t, 1, seen[0].Number)
	assert.False(t, seen[0].Oversized)
	assert.Equal(t, `{"seq":1}`, string(seen[0].Bytes))

	assert.Equal(t, 2, seen[1].Number)
	assert.True(t, seen[1].Oversized)
	assert.Equal(t, oversized[:5], string(seen[1].Bytes))

	assert.Equal(t, 3, seen[2].Number)
	assert.False(t, seen[2].Oversized)
	assert.Equal(t, `{"seq":2}`, string(seen[2].Bytes))
}

func TestVisit_OversizedFinalLineWithoutNewline(t *testing.T) {
	t.Parallel()

	maxLineBytes := len(`{"seq":1}`)
	input := `{"seq":1}` + "\n" + strings.Repeat("y", 32)

	var seen []Line
	err := Visit(strings.NewReader(input), maxLineBytes, VisitOptions{OversizedPrefixBytes: 4}, func(line Line) error {
		clone := line
		clone.Bytes = append([]byte(nil), line.Bytes...)
		seen = append(seen, clone)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 2)

	assert.False(t, seen[0].Oversized)
	assert.True(t, seen[1].Oversized)
	assert.Equal(t, "yyyy", string(seen[1].Bytes))
}

func TestExtractUintField_FromPartialJSONPrefix(t *testing.T) {
	t.Parallel()

	line := []byte(`{"schema_version":"1.0","seq":123456,"timestamp":"2026-02-05T11:50:10Z","kind":"mess`)
	seq, ok := ExtractUintField(line, "seq")
	require.True(t, ok)
	assert.Equal(t, uint64(123456), seq)

	_, ok = ExtractUintField(line, "missing")
	assert.False(t, ok)
}

func TestVisit_LineLengthBoundaryExcludesTrailingNewline(t *testing.T) {
	t.Parallel()

	lineAtLimit := `{"seq":1}`
	lineOverLimit := `{"seq":22}`
	maxLineBytes := len(lineAtLimit)

	input := strings.Join([]string{
		lineAtLimit,
		lineOverLimit,
	}, "\n") + "\n"

	var seen []Line
	err := Visit(strings.NewReader(input), maxLineBytes, VisitOptions{OversizedPrefixBytes: 4}, func(line Line) error {
		clone := line
		clone.Bytes = append([]byte(nil), line.Bytes...)
		seen = append(seen, clone)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 2)

	assert.False(t, seen[0].Oversized, "line equal to max bytes should not be oversized")
	assert.Equal(t, lineAtLimit, string(seen[0].Bytes))

	assert.True(t, seen[1].Oversized, "line above max bytes should be oversized")
	assert.Equal(t, lineOverLimit[:4], string(seen[1].Bytes))
}

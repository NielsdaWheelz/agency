package jsonl

import (
	"bytes"
	"strings"
	"testing"

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
		clone.Bytes = bytes.Clone(line.Bytes)
		seen = append(seen, clone)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 3)

	want := []struct {
		number    int
		oversized bool
		bytes     string
	}{
		{number: 1, bytes: `{"seq":1}`},
		{number: 2, oversized: true, bytes: oversized[:5]},
		{number: 3, bytes: `{"seq":2}`},
	}
	for i, line := range seen {
		if line.Number != want[i].number || line.Oversized != want[i].oversized || string(line.Bytes) != want[i].bytes {
			t.Fatalf("line %d = {number:%d oversized:%v bytes:%q}, want {number:%d oversized:%v bytes:%q}",
				i, line.Number, line.Oversized, string(line.Bytes), want[i].number, want[i].oversized, want[i].bytes)
		}
	}
}

func TestVisit_OversizedFinalLineWithoutNewline(t *testing.T) {
	t.Parallel()

	maxLineBytes := len(`{"seq":1}`)
	input := `{"seq":1}` + "\n" + strings.Repeat("y", 32)

	var seen []Line
	err := Visit(strings.NewReader(input), maxLineBytes, VisitOptions{OversizedPrefixBytes: 4}, func(line Line) error {
		clone := line
		clone.Bytes = bytes.Clone(line.Bytes)
		seen = append(seen, clone)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 2)

	if seen[0].Oversized {
		t.Fatalf("first line oversized = true, want false")
	}
	if !seen[1].Oversized {
		t.Fatalf("second line oversized = false, want true")
	}
	if got := string(seen[1].Bytes); got != "yyyy" {
		t.Fatalf("second line bytes = %q, want %q", got, "yyyy")
	}
}

func TestExtractUintField_FromPartialJSONPrefix(t *testing.T) {
	t.Parallel()

	line := []byte(`{"schema_version":"1.0","seq":123456,"timestamp":"2026-02-05T11:50:10Z","kind":"mess`)
	seq, ok := ExtractUintField(line, "seq")
	require.True(t, ok)
	if seq != 123456 {
		t.Fatalf("seq = %d, want 123456", seq)
	}

	_, ok = ExtractUintField(line, "missing")
	if ok {
		t.Fatalf("missing field was reported present")
	}
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
		clone.Bytes = bytes.Clone(line.Bytes)
		seen = append(seen, clone)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 2)

	if seen[0].Oversized {
		t.Fatalf("line equal to max bytes should not be oversized")
	}
	if got := string(seen[0].Bytes); got != lineAtLimit {
		t.Fatalf("line at limit bytes = %q, want %q", got, lineAtLimit)
	}

	if !seen[1].Oversized {
		t.Fatalf("line above max bytes should be oversized")
	}
	if got := string(seen[1].Bytes); got != lineOverLimit[:4] {
		t.Fatalf("line over limit bytes = %q, want %q", got, lineOverLimit[:4])
	}
}

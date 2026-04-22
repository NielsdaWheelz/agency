package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTimelineJSONL_DegradesMalformedLinesIntoParseErrors(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "stream.jsonl")
	content := strings.Join([]string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","kind":"message","data":{"role":"assistant","text":"ok"}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:20Z","kind":"","data":{"role":"assistant","text":"broken"}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	entries, err := readTimelineJSONL(path, "2026-02-05T11:50:00Z", "stream", timelineSourceRankStream, buildStreamTimelineEntry)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "message", entries[0].dto.Kind)
	assert.Equal(t, "parse_error", entries[1].dto.Kind)
}

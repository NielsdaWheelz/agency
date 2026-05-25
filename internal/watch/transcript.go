package watch

import (
	"bytes"
	"context"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/render"
)

func loadInvocationTranscript(ctx context.Context, client *daemonclient.Client, invocationID, repoID string) (string, error) {
	timelineEntries, err := loadAllTimelineEntries(ctx, client, invocationID, repoID, "transcript")
	if err != nil {
		return "", err
	}

	entries := make([]render.TranscriptEntry, 0, len(timelineEntries))
	for _, entry := range timelineEntries {
		entries = append(entries, render.TranscriptEntry{
			Kind:      entry.Kind,
			Timestamp: entry.Timestamp,
			Data:      entry.Data,
		})
	}

	var buf bytes.Buffer
	if err := render.WriteTranscript(&buf, entries, render.TranscriptOpts{NoColor: true}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m model) renderTranscript() string {
	width := m.viewWidth()
	lines := m.renderPageHeader("transcript")
	if line := m.styledActionLine(width); line != "" {
		lines = append(lines, line, "")
	}
	lines = appendPageError(lines, "transcript", m.transcriptError, width)
	lines = appendPageLoading(lines, "transcript", m.transcriptLoading)
	lines = append(lines, m.renderScrollViewport(transcriptLines(m.transcriptContent), m.transcriptScroll)...)
	lines = append(lines, "")
	lines = append(lines, warningStyle.Render("j/k move • a attach • d review • x actions • l logs • r refresh • b back • q quit"))
	return strings.Join(lines, "\n")
}

func transcriptLines(content string) []string {
	return contentLines(content, "(no transcript entries yet)")
}

func (m model) maxTranscriptScroll() int {
	return m.pageMaxScroll(transcriptLines(m.transcriptContent))
}

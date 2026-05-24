package watch

import (
	"bytes"
	"context"
	"strconv"
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
	width := m.width
	if width <= 0 {
		width = 120
	}

	lines := m.renderPageHeader("transcript")
	if line := m.styledActionLine(width); line != "" {
		lines = append(lines, line, "")
	}
	if m.transcriptError != "" {
		lines = append(lines, errorStyle.Render("transcript error: "+truncateWithEllipsis(m.transcriptError, width-4)))
		lines = append(lines, "")
	}
	if m.transcriptLoading {
		lines = append(lines, warningStyle.Render("loading transcript..."))
		lines = append(lines, "")
	}

	transcriptLines := transcriptLines(m.transcriptContent)
	visible := m.transcriptVisibleLines()
	start := 0
	end := len(transcriptLines)
	if len(transcriptLines) > visible {
		start = clamp(m.transcriptScroll, 0, max(0, len(transcriptLines)-visible))
		end = clamp(start+visible, 0, len(transcriptLines))
	}

	for _, line := range transcriptLines[start:end] {
		lines = append(lines, truncateWithEllipsis(line, width))
	}
	if len(transcriptLines) > visible {
		lines = append(lines, "")
		lines = append(lines, warningStyle.Render(
			"showing "+truncateWithEllipsis(strconv.Itoa(start+1)+"-"+strconv.Itoa(end)+" of "+strconv.Itoa(len(transcriptLines)), width-12),
		))
	}
	lines = append(lines, "")
	lines = append(lines, warningStyle.Render("j/k move • a attach • d review • x actions • l logs • r refresh • b back • q quit"))
	return strings.Join(lines, "\n")
}

func transcriptLines(content string) []string {
	return contentLines(content, "(no transcript entries yet)")
}

func (m model) maxTranscriptScroll() int {
	lines := transcriptLines(m.transcriptContent)
	visible := m.transcriptVisibleLines()
	if len(lines) <= visible {
		return 0
	}
	return len(lines) - visible
}

func (m model) transcriptVisibleLines() int {
	height := m.height
	if height <= 0 {
		height = 36
	}
	visible := height - 8
	if visible < 5 {
		visible = 5
	}
	return visible
}

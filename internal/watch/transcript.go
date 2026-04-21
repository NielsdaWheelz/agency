package watch

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/render"
)

func loadInvocationTranscript(ctx context.Context, client *daemonclient.Client, invocationID, repoID string) (string, error) {
	if client == nil {
		return "", errors.New(errors.EInternal, "watch runtime requires a daemon client")
	}
	if strings.TrimSpace(invocationID) == "" || strings.TrimSpace(repoID) == "" {
		return "", errors.New(errors.EInvalidArgument, "transcript page requires an invocation and repo")
	}

	entries := make([]render.TranscriptEntry, 0, 128)
	cursor := ""
	for {
		result, err := client.GetInvocationTimeline(ctx, invocationID, repoID, daemonclient.GetInvocationTimelineOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return "", err
		}
		for _, entry := range result.Data.Entries {
			entries = append(entries, render.TranscriptEntry{
				Kind:      entry.Kind,
				Timestamp: entry.Timestamp,
				Data:      entry.Data,
			})
		}
		if len(entries) > maxHistoryEntries {
			return "", errors.New(errors.EInvalidArgument, fmt.Sprintf("interactive transcript view supports at most %d timeline entries", maxHistoryEntries))
		}
		if result.Data.NextCursor == "" {
			break
		}
		if result.Data.NextCursor == cursor {
			return "", errors.New(errors.EInternal, "timeline pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
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
	if m.lastActionMessage != "" {
		actionLine := "action: " + truncateWithEllipsis(m.lastActionMessage, width-10)
		switch {
		case m.lastActionError:
			lines = append(lines, errorStyle.Render(actionLine))
		case m.actionRunning:
			lines = append(lines, warningStyle.Render(actionLine))
		default:
			lines = append(lines, actionStyle.Render(actionLine))
		}
		lines = append(lines, "")
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
	if strings.TrimSpace(content) == "" {
		return []string{"(no transcript entries yet)"}
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []string{"(no transcript entries yet)"}
	}
	return lines
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

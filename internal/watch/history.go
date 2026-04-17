package watch

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/render"
)

const maxHistoryEntries = 2000

var (
	historyHeaderStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	historySelectedStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	historyMarkerStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	historyCheckpointStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	historyToolCallStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	historyDimStyle        = lipgloss.NewStyle().Faint(true)
	historySeparatorStyle  = lipgloss.NewStyle().Faint(true)
	historyHelpStyle       = lipgloss.NewStyle().Faint(true)
)

func loadHistoryTurns(ctx context.Context, client *daemonclient.Client, invocationID, repoID string) ([]daemon.Turn, error) {
	if client == nil {
		return nil, errors.New(errors.EInternal, "watch runtime requires a daemon client")
	}
	if strings.TrimSpace(invocationID) == "" || strings.TrimSpace(repoID) == "" {
		return nil, errors.New(errors.EInvalidArgument, "history page requires an invocation and repo")
	}

	entries := make([]daemon.TimelineEntryDTO, 0, 128)
	cursor := ""
	for {
		result, err := client.GetInvocationTimeline(ctx, invocationID, repoID, daemonclient.GetInvocationTimelineOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		entries = append(entries, result.Data.Entries...)
		if len(entries) > maxHistoryEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history view supports at most %d timeline entries", maxHistoryEntries),
				map[string]string{
					"hint": "narrow invocation scope or use explicit --checkpoint <id>",
				},
			)
		}
		if result.Data.NextCursor == "" {
			break
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "timeline pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
	}

	checkpoints := make([]daemon.CheckpointDTO, 0, 32)
	cursor = ""
	for {
		result, err := client.ListCheckpoints(ctx, invocationID, repoID, daemonclient.ListCheckpointsOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, result.Data.Checkpoints...)
		if len(checkpoints) > maxHistoryEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history view supports at most %d checkpoints", maxHistoryEntries),
				map[string]string{
					"hint": "use explicit --checkpoint <id> for very large histories",
				},
			)
		}
		if result.Data.NextCursor == "" {
			break
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "checkpoint pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
	}

	turns := daemon.ProjectTimelineTurns(entries, checkpoints)
	if len(turns) == 0 {
		return nil, errors.New(errors.ECheckpointNotFound, "no history entries available")
	}
	return turns, nil
}

func (m model) renderHistory() string {
	width := m.width
	if width <= 0 {
		width = 120
	}

	lines := []string{
		m.renderHistoryHeader(fmt.Sprintf("invocation history  %s", m.selectedInvocationID)),
		"",
	}

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
	if m.historyError != "" {
		lines = append(lines, errorStyle.Render("history error: "+truncateWithEllipsis(m.historyError, width-4)))
		lines = append(lines, "")
	}
	if m.historyLoading {
		lines = append(lines, warningStyle.Render("loading history..."))
		lines = append(lines, "")
	}
	if len(m.historyTurns) == 0 {
		lines = append(lines, "no history entries available")
		lines = append(lines, "")
		lines = append(lines, m.renderHistoryHelp("j/k move • enter restore • l logs • r refresh • b back • q quit"))
		return strings.Join(lines, "\n")
	}

	var builder strings.Builder
	builder.WriteString(strings.Join(lines, "\n"))
	for index, turn := range m.historyTurns {
		m.renderHistoryTurn(&builder, index, turn, width)
	}
	builder.WriteString("\n")
	builder.WriteString(m.renderHistoryHelp("j/k move • enter restore • l logs • r refresh • b back • q quit"))
	return builder.String()
}

func (m model) renderHistoryTurn(builder *strings.Builder, index int, turn daemon.Turn, width int) {
	isSelected := index == m.historySelectedIndex
	marker := "  "
	if isSelected {
		marker = m.renderHistoryMarker("> ")
	}

	timestamp := strings.TrimSpace(turn.ShortTimestamp)
	if timestamp == "" {
		timestamp = strings.TrimSpace(turn.Timestamp)
	}
	if timestamp == "" {
		timestamp = "-"
	}

	header := marker + "[" + render.NormalizeActivityKind(string(turn.Kind)) + "] (" + timestamp + ")"
	if turn.Restorable && turn.CheckpointID > 0 {
		header += " " + m.renderHistoryCheckpoint("cp:"+fmt.Sprintf("%d", turn.CheckpointID))
	}
	visibleLen := historyVisibleLen(header)
	remaining := width - visibleLen - 1
	if remaining > 2 {
		header += " " + m.renderHistorySeparator(strings.Repeat("─", remaining))
	}
	if !turn.Restorable && !isSelected {
		header = m.renderHistoryDim(header)
	}
	if isSelected {
		header = m.renderHistorySelected(header)
	}
	builder.WriteString(header)
	builder.WriteString("\n")

	summaryText := render.ActivitySummaryText(string(turn.Kind), turn.Summary)
	if summaryText != "" {
		summaryLine := "    " + historyTruncate(summaryText, width-4)
		if !turn.Restorable && !isSelected {
			summaryLine = m.renderHistoryDim(summaryLine)
		}
		builder.WriteString(summaryLine)
		builder.WriteString("\n")
	}

	if turn.Restorable && len(turn.CheckpointChangedPaths) > 0 {
		pathsSummary := render.FormatChangedPathSummary(turn.CheckpointChangedPaths, turn.CheckpointChangedCount, turn.CheckpointPathsTrimmed)
		builder.WriteString("    files: ")
		builder.WriteString(historyTruncate(pathsSummary, width-11))
		builder.WriteString("\n")
	}

	for _, toolCall := range turn.ToolCalls {
		builder.WriteString("    ")
		builder.WriteString(m.renderHistoryToolCall(render.FormatToolCallSummary(toolCall.Name, toolCall.Command, toolCall.HasExit, toolCall.ExitCode)))
		builder.WriteString("\n")
	}

	if index < len(m.historyTurns)-1 {
		builder.WriteString("\n")
	}
}

func historyVisibleLen(value string) int {
	visibleLen := 0
	inEscape := false
	for _, r := range value {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		visibleLen++
	}
	return visibleLen
}

func historyTruncate(value string, maxWidth int) string {
	trimmed := strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	return truncateWithEllipsis(trimmed, maxWidth)
}

func (m model) renderHistoryHeader(value string) string {
	return historyHeaderStyle.Render(value)
}

func (m model) renderHistorySelected(value string) string {
	return historySelectedStyle.Render(value)
}

func (m model) renderHistoryMarker(value string) string {
	return historyMarkerStyle.Render(value)
}

func (m model) renderHistoryCheckpoint(value string) string {
	return historyCheckpointStyle.Render(value)
}

func (m model) renderHistoryToolCall(value string) string {
	return historyToolCallStyle.Render(value)
}

func (m model) renderHistoryDim(value string) string {
	return historyDimStyle.Render(value)
}

func (m model) renderHistorySeparator(value string) string {
	return historySeparatorStyle.Render(value)
}

func (m model) renderHistoryHelp(value string) string {
	return historyHelpStyle.Render(value)
}

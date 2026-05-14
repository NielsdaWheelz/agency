package watch

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
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
	entries, err := loadAllTimelineEntries(ctx, client, invocationID, repoID, "history")
	if err != nil {
		return nil, err
	}
	checkpoints, err := loadAllCheckpoints(ctx, client, invocationID, repoID)
	if err != nil {
		return nil, err
	}
	turns := daemon.ProjectTimelineTurns(entries, checkpoints)
	return turns, nil
}

func (m model) renderHistory() string {
	width := m.width
	if width <= 0 {
		width = 120
	}

	lines := m.renderHistoryHeaderLines(width)
	lines = append(lines, "")
	lines = append(lines, m.renderTransientActionPanel(width)...)

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
		lines = append(lines, historyHelpStyle.Render("j/k move • enter restore • d review • t transcript • l logs • a attach • x actions • r refresh • b back • q quit"))
		return strings.Join(lines, "\n")
	}

	var builder strings.Builder
	builder.WriteString(strings.Join(lines, "\n"))
	for index, turn := range m.historyTurns {
		m.renderHistoryTurn(&builder, index, turn, width)
	}
	builder.WriteString("\n")
	builder.WriteString(historyHelpStyle.Render("j/k move • enter restore • d review • t transcript • l logs • a attach • x actions • r refresh • b back • q quit"))
	return builder.String()
}

func (m model) renderHistoryHeaderLines(width int) []string {
	primary, secondary := m.historyHeaderContext()
	lines := []string{historyHeaderStyle.Render(truncateWithEllipsis(primary, width))}
	if secondary != "" {
		lines = append(lines, historyHelpStyle.Render(truncateWithEllipsis(secondary, width)))
	}
	return lines
}

func (m model) historyHeaderContext() (string, string) {
	primaryParts := make([]string, 0, 3)
	secondaryParts := []string{"history"}

	if inv, ok := m.historyContextInvocation(); ok {
		if label := historyAgentLabel(inv); label != "" {
			primaryParts = append(primaryParts, label)
		}
		if label := m.worktreeDisplay(inv.WorktreeName, inv.WorktreeID); label != "-" {
			primaryParts = append(primaryParts, label)
		}
		if label := m.repoDisplay(inv.RepoName, inv.RepoID); label != "-" {
			primaryParts = append(primaryParts, label)
		}
		if invocationID := strings.TrimSpace(inv.InvocationID); invocationID != "" {
			secondaryParts = append(secondaryParts, "invocation "+invocationID)
		}
	} else {
		if label := m.historyRepoLabel(m.selectedRepoID); label != "" {
			primaryParts = append(primaryParts, label)
		}
		if invocationID := strings.TrimSpace(m.selectedInvocationID); invocationID != "" {
			secondaryParts = append(secondaryParts, "invocation "+invocationID)
		}
	}

	primary := strings.Join(primaryParts, " / ")
	if primary == "" {
		primary = "history"
	}

	secondary := strings.Join(secondaryParts, " / ")
	if secondary == "history" || secondary == primary {
		secondary = ""
	}

	return primary, secondary
}

func (m model) historyContextInvocation() (daemon.InvocationDTO, bool) {
	invocationID := strings.TrimSpace(m.selectedInvocationID)
	if invocationID != "" {
		for _, inv := range m.snapshot.Invocations {
			if inv.InvocationID == invocationID {
				return inv, true
			}
		}
		return daemon.InvocationDTO{}, false
	}
	if len(m.snapshot.Invocations) == 0 {
		return daemon.InvocationDTO{}, false
	}
	idx := clamp(m.selectedIndex, 0, len(m.snapshot.Invocations)-1)
	return m.snapshot.Invocations[idx], true
}

func (m model) historyRepoLabel(repoID string) string {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return ""
	}
	for _, repo := range m.snapshot.Repos {
		if repo.RepoID != repoID {
			continue
		}
		label := strings.TrimSpace(repo.RepoKey)
		if label != "" {
			return label
		}
		return repo.RepoID
	}
	return repoID
}

func historyAgentLabel(inv daemon.InvocationDTO) string {
	if name := strings.TrimSpace(inv.InvocationName); name != "" {
		return name
	}
	runner := strings.TrimSpace(inv.Runner)
	mode := strings.TrimSpace(inv.Mode)
	switch {
	case runner != "" && mode != "":
		return runner + "/" + mode
	case runner != "":
		return runner
	case mode != "":
		return mode
	default:
		return ""
	}
}

func (m model) renderHistoryTurn(builder *strings.Builder, index int, turn daemon.Turn, width int) {
	isSelected := index == m.historySelectedIndex
	marker := "  "
	if isSelected {
		marker = historyMarkerStyle.Render("> ")
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
		header += " " + historyCheckpointStyle.Render("cp:"+fmt.Sprintf("%d", turn.CheckpointID))
	}
	visibleLen := historyVisibleLen(header)
	remaining := width - visibleLen - 1
	if remaining > 2 {
		header += " " + historySeparatorStyle.Render(strings.Repeat("─", remaining))
	}
	if !turn.Restorable && !isSelected {
		header = historyDimStyle.Render(header)
	}
	if isSelected {
		header = historySelectedStyle.Render(header)
	}
	builder.WriteString(header)
	builder.WriteString("\n")

	summaryText := render.ActivitySummaryText(string(turn.Kind), turn.Summary)
	if summaryText != "" {
		summaryLine := "    " + historyTruncate(summaryText, width-4)
		if !turn.Restorable && !isSelected {
			summaryLine = historyDimStyle.Render(summaryLine)
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
		builder.WriteString(historyToolCallStyle.Render(render.FormatToolCallSummary(toolCall.Name, toolCall.Command, toolCall.HasExit, toolCall.ExitCode)))
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

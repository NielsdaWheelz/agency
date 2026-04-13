package watch

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/render"
)

func (m model) renderWorkspace() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 36
	}

	readyCount, blockedCount, unknownCount := readinessCounts(m.snapshot.Invocations, m.snapshot.Reviews)
	headerParts := []string{
		"agency watch",
		fmt.Sprintf("repos:%d", len(m.snapshot.Repos)),
		fmt.Sprintf("worktrees:%d", len(m.snapshot.Worktrees)),
		fmt.Sprintf("invocations:%d", len(m.snapshot.Invocations)),
		fmt.Sprintf("ready:%d", readyCount),
		fmt.Sprintf("blocked:%d", blockedCount),
		fmt.Sprintf("unknown:%d", unknownCount),
	}
	if m.refreshing {
		headerParts = append(headerParts, "refreshing")
	}
	if m.actionRunning {
		headerParts = append(headerParts, "action-running")
	}
	if !m.snapshot.UpdatedAt.IsZero() {
		headerParts = append(headerParts, "updated:"+m.snapshot.UpdatedAt.Format(time.RFC3339))
	}
	header := headerStyle.Render(strings.Join(headerParts, "  "))

	contentHeight := height - 6
	if contentHeight < 12 {
		contentHeight = 12
	}
	body := m.renderPanels(width, contentHeight)

	footerLines := make([]string, 0, 3)
	if m.lastActionMessage != "" {
		actionLine := "action: " + truncateWithEllipsis(m.lastActionMessage, width-10)
		switch {
		case m.lastActionError:
			footerLines = append(footerLines, errorStyle.Render(actionLine))
		case m.actionRunning:
			footerLines = append(footerLines, warningStyle.Render(actionLine))
		default:
			footerLines = append(footerLines, actionStyle.Render(actionLine))
		}
	}
	if m.lastError != "" {
		footerLines = append(footerLines, errorStyle.Render("refresh error: "+truncateWithEllipsis(m.lastError, width-4)+" (auto-retrying)"))
	}
	if len(m.snapshot.Warnings) > 0 {
		warning := fmt.Sprintf("warnings: %d (first: %s)", len(m.snapshot.Warnings), truncateWithEllipsis(m.snapshot.Warnings[0], width-20))
		footerLines = append(footerLines, warningStyle.Render(warning))
	}
	footerLines = append(footerLines, m.help.View(m.keys))

	return strings.Join([]string{
		header,
		body,
		strings.Join(footerLines, "\n"),
	}, "\n")
}

func (m model) renderPanels(width, contentHeight int) string {
	leftWidth := width / 2
	if leftWidth < minPanelWidth {
		leftWidth = minPanelWidth
	}
	rightWidth := width - leftWidth - 1
	if rightWidth < minPanelWidth {
		rightWidth = minPanelWidth
		leftWidth = width - rightWidth - 1
	}

	// Extremely narrow terminals can't fit two fixed-width panels side by side.
	// Fall back to stacked panels to keep the workspace usable.
	if leftWidth < minPanelWidth {
		panelWidth := max(1, width-2)
		leftHeight := max(6, contentHeight/2)
		rightHeight := max(6, contentHeight-leftHeight)
		leftPanel := panelStyle.Width(panelWidth).Height(leftHeight).Render(m.renderInvocationsPanel(max(1, panelWidth-2)))
		rightPanel := panelStyle.Width(panelWidth).Height(rightHeight).Render(m.renderDetailsPanel(max(1, panelWidth-2)))
		return lipgloss.JoinVertical(lipgloss.Left, leftPanel, rightPanel)
	}

	leftPanel := panelStyle.Width(leftWidth).Height(contentHeight).Render(m.renderInvocationsPanel(max(1, leftWidth-2)))
	rightPanel := panelStyle.Width(rightWidth).Height(contentHeight).Render(m.renderDetailsPanel(max(1, rightWidth-2)))
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
}

func (m model) renderInvocationsPanel(width int) string {
	if len(m.snapshot.Invocations) == 0 {
		return "invocations\n\n(no invocations found)"
	}

	worktreeNames := make(map[string]string, len(m.snapshot.Worktrees))
	for _, wt := range m.snapshot.Worktrees {
		worktreeNames[wt.WorktreeID] = wt.Name
	}

	lines := []string{"invocations", ""}
	maxRows := 18
	start, end := windowForSelection(len(m.snapshot.Invocations), m.selectedIndex, maxRows)
	for idx := start; idx < end; idx++ {
		inv := m.snapshot.Invocations[idx]
		indicator := " "
		if idx == m.selectedIndex {
			indicator = ">"
		}

		readiness := "UNKNOWN"
		if review, ok := m.snapshot.Reviews[inv.InvocationID]; ok {
			if review.Ready || review.Readiness == "ready" {
				readiness = "READY"
			} else {
				readiness = "BLOCKED"
			}
		}

		worktreeName := worktreeNames[inv.WorktreeID]
		if worktreeName == "" {
			worktreeName = inv.WorktreeID
		}
		displayStatus := inv.DisplayStatus
		if strings.TrimSpace(displayStatus) == "" {
			displayStatus = inv.Status
		}
		activitySummary := strings.TrimSpace(inv.StatusSummary)
		if activity := inv.LatestActivity; activity != nil {
			toolCount := activity.ToolCallCount
			if toolCount == 0 {
				toolCount = len(activity.ToolCalls)
			}
			if kind := strings.TrimSpace(activity.Kind); kind != "" || strings.TrimSpace(activity.Summary) != "" || toolCount > 0 || activity.CheckpointID > 0 {
				activitySummary = render.FormatActivityWithExtras(
					activity.Kind,
					activity.Summary,
					toolCount,
					activity.CheckpointID,
					activity.Restorable,
				)
			}
		}
		rowTail := worktreeName
		if activitySummary != "" {
			rowTail = worktreeName + " | " + activitySummary
		}

		row := fmt.Sprintf(
			"%s %-8s %-10s %-9s %-14s %s",
			indicator,
			readiness,
			shortID(inv.InvocationID, 10),
			inv.Runner,
			truncateWithEllipsis(displayStatus, 14),
			truncateWithEllipsis(rowTail, max(1, width-55)),
		)
		row = truncateWithEllipsis(row, width)
		if idx == m.selectedIndex {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)
	}

	if start > 0 || end < len(m.snapshot.Invocations) {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("showing %d-%d of %d", start+1, end, len(m.snapshot.Invocations)))
	}

	return strings.Join(lines, "\n")
}

func (m model) renderDetailsPanel(width int) string {
	lines := []string{"invocation details", ""}

	selected, ok := m.selectedInvocation()
	if !ok {
		lines = append(lines, "select an invocation to view readiness details")
		return strings.Join(lines, "\n")
	}

	review, hasReview := m.snapshot.Reviews[selected.InvocationID]

	lines = append(lines, fmt.Sprintf("invocation_id: %s", selected.InvocationID))
	lines = append(lines, fmt.Sprintf("repo_id:       %s", selected.RepoID))
	lines = append(lines, fmt.Sprintf("worktree_id:   %s", selected.WorktreeID))
	lines = append(lines, fmt.Sprintf("runner/mode:   %s / %s", selected.Runner, selected.Mode))

	displayStatus := selected.DisplayStatus
	if strings.TrimSpace(displayStatus) == "" {
		displayStatus = selected.Status
	}
	lines = append(lines, fmt.Sprintf("status:        %s", displayStatus))
	if summary := strings.TrimSpace(selected.StatusSummary); summary != "" {
		lines = append(lines, fmt.Sprintf("status_summary: %s", summary))
	}
	if activity := selected.LatestActivity; activity != nil {
		toolCount := activity.ToolCallCount
		if toolCount == 0 {
			toolCount = len(activity.ToolCalls)
		}
		if kind := strings.TrimSpace(activity.Kind); kind != "" || strings.TrimSpace(activity.Summary) != "" || toolCount > 0 || activity.CheckpointID > 0 {
			latestLabel := render.FormatActivityWithExtras(
				activity.Kind,
				activity.Summary,
				toolCount,
				activity.CheckpointID,
				activity.Restorable,
			)
			latestTurnID := strings.TrimSpace(activity.TurnID)
			if latestTurnID != "" {
				lines = append(lines, fmt.Sprintf("latest_activity: [%s] %s", latestTurnID, latestLabel))
			} else {
				lines = append(lines, fmt.Sprintf("latest_activity: %s", latestLabel))
			}
			for _, tool := range activity.ToolCalls {
				lines = append(lines, "latest_tool: "+render.FormatToolCallSummary(tool.Name, tool.Command, tool.HasExit, tool.ExitCode))
			}
			if activity.CheckpointID > 0 {
				lines = append(lines, fmt.Sprintf("latest_checkpoint: %d", activity.CheckpointID))
			}
			if description := strings.TrimSpace(activity.CheckpointDescription); description != "" {
				lines = append(lines, "latest_checkpoint_desc: "+description)
			}
			if diffstat := strings.TrimSpace(activity.CheckpointDiffstat); diffstat != "" {
				lines = append(lines, "latest_checkpoint_diff: "+diffstat)
			}
			if pathsSummary := render.FormatChangedPathSummary(activity.CheckpointChangedPaths, activity.CheckpointChangedCount, activity.CheckpointPathsTrimmed); pathsSummary != "" {
				lines = append(lines, "latest_checkpoint_paths: "+pathsSummary)
			}
		}
	}
	lines = append(lines, "")

	if !hasReview {
		lines = append(lines, warningStyle.Render("review data unavailable (retrying)"))
		return truncateLines(lines, width)
	}

	verdict := blockedStyle.Render("BLOCKED")
	if review.Ready || review.Readiness == "ready" {
		verdict = readyStyle.Render("READY")
	}
	lines = append(lines, "verdict:       "+verdict)
	lines = append(lines, fmt.Sprintf("pr_sync_eligible: %t", review.PRSyncEligible))
	if review.ReportSource != "" {
		lines = append(lines, fmt.Sprintf("report_source: %s", review.ReportSource))
	}
	lines = append(lines, "")
	lines = append(lines, "blocking reasons:")
	if len(review.BlockingReasons) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, reason := range review.BlockingReasons {
			lines = append(lines, fmt.Sprintf("  - [%s] %s", reason.Code, reason.Message))
			if strings.TrimSpace(reason.Hint) != "" {
				lines = append(lines, fmt.Sprintf("      hint: %s", reason.Hint))
			}
		}
	}

	if len(review.ReportDiagnostics) > 0 {
		lines = append(lines, "")
		lines = append(lines, "report diagnostics:")
		for _, diagnostic := range review.ReportDiagnostics {
			lines = append(lines, fmt.Sprintf("  - [%s] %s", diagnostic.Code, diagnostic.Message))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "navigation:")
	lines = append(lines, "  history: "+review.Navigation.HistoryCommand)
	if review.Navigation.DiffCommand != "" {
		lines = append(lines, "  diff:    "+review.Navigation.DiffCommand)
	}
	if review.Navigation.PRSyncCommand != "" {
		lines = append(lines, "  pr_sync: "+review.Navigation.PRSyncCommand)
	}
	if review.Navigation.LatestTurnID != "" {
		lines = append(lines, "  turn:    "+review.Navigation.LatestTurnID)
	}

	return truncateLines(lines, width)
}

func (m model) selectedInvocation() (daemon.InvocationDTO, bool) {
	if len(m.snapshot.Invocations) == 0 {
		return daemon.InvocationDTO{}, false
	}
	idx := clamp(m.selectedIndex, 0, len(m.snapshot.Invocations)-1)
	return m.snapshot.Invocations[idx], true
}

func (m *model) moveSelection(delta int) {
	if len(m.snapshot.Invocations) == 0 {
		m.selectedIndex = 0
		m.selectedInvocationID = ""
		return
	}
	next := clamp(m.selectedIndex+delta, 0, len(m.snapshot.Invocations)-1)
	m.selectedIndex = next
	m.selectedInvocationID = m.snapshot.Invocations[next].InvocationID
}

func (m *model) reconcileSelection() {
	if len(m.snapshot.Invocations) == 0 {
		m.selectedIndex = 0
		m.selectedInvocationID = ""
		return
	}

	if m.selectedInvocationID != "" {
		for idx, inv := range m.snapshot.Invocations {
			if inv.InvocationID == m.selectedInvocationID {
				m.selectedIndex = idx
				return
			}
		}
	}

	m.selectedIndex = clamp(m.selectedIndex, 0, len(m.snapshot.Invocations)-1)
	m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
}

func readinessCounts(invocations []daemon.InvocationDTO, reviews map[string]daemon.InvocationReviewData) (ready int, blocked int, unknown int) {
	for _, inv := range invocations {
		review, ok := reviews[inv.InvocationID]
		if !ok {
			unknown++
			continue
		}
		if review.Ready || review.Readiness == "ready" {
			ready++
			continue
		}
		blocked++
	}
	return ready, blocked, unknown
}

func windowForSelection(total, selected, size int) (start, end int) {
	if total <= 0 {
		return 0, 0
	}
	if size <= 0 || size >= total {
		return 0, total
	}

	selected = clamp(selected, 0, total-1)
	half := size / 2
	start = selected - half
	if start < 0 {
		start = 0
	}
	end = start + size
	if end > total {
		end = total
		start = end - size
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

func truncateLines(lines []string, width int) string {
	if width <= 0 {
		return strings.Join(lines, "\n")
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, truncateWithEllipsis(line, width))
	}
	return strings.Join(out, "\n")
}

func truncateWithEllipsis(value string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxWidth {
		return value
	}
	if maxWidth <= 3 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-3]) + "..."
}

func shortID(value string, maxLen int) string {
	runes := []rune(value)
	if maxLen <= 0 || len(runes) <= maxLen {
		return value
	}
	return string(runes[:maxLen])
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

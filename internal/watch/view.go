package watch

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

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

	readyCount, blockedCount, unknownCount := readinessCounts(m.snapshot.Invocations, m.snapshot.Checks)
	headerParts := []string{
		"agency watch",
		fmt.Sprintf("repos:%d", len(m.snapshot.Repos)),
		fmt.Sprintf("worktrees:%d", len(m.snapshot.Worktrees)),
		fmt.Sprintf("invocations:%d", len(m.snapshot.Invocations)),
		fmt.Sprintf("ready:%d", readyCount),
		fmt.Sprintf("blocked:%d", blockedCount),
		fmt.Sprintf("unknown:%d", unknownCount),
	}
	if m.workspaceLoading {
		headerParts = append(headerParts, "refreshing")
	}
	if m.actionRunning {
		headerParts = append(headerParts, "action-running")
	}
	if !m.snapshot.UpdatedAt.IsZero() {
		headerParts = append(headerParts, "updated:"+m.snapshot.UpdatedAt.Format(time.RFC3339))
	}

	contentHeight := height - 6
	if contentHeight < 12 {
		contentHeight = 12
	}

	lines := []string{
		headerStyle.Render(strings.Join(headerParts, "  ")),
		m.renderWorkspacePanels(width, contentHeight),
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
	}
	if m.workspaceError != "" {
		lines = append(lines, errorStyle.Render("refresh error: "+truncateWithEllipsis(m.workspaceError, width-4)+" (auto-retrying)"))
	}
	if len(m.snapshot.Warnings) > 0 {
		lines = append(lines, warningStyle.Render(
			fmt.Sprintf(
				"warnings: %d (first: %s)",
				len(m.snapshot.Warnings),
				truncateWithEllipsis(m.snapshot.Warnings[0], width-20),
			),
		))
	}
	lines = append(lines, warningStyle.Render("j/k move • enter attach • o open • p pr sync • h history • l logs • r refresh • q quit"))

	return strings.Join(lines, "\n")
}

func (m model) renderWorkspacePanels(width, contentHeight int) string {
	leftWidth := width / 2
	if leftWidth < minPanelWidth {
		leftWidth = minPanelWidth
	}
	rightWidth := width - leftWidth - 1
	if rightWidth < minPanelWidth {
		rightWidth = minPanelWidth
		leftWidth = width - rightWidth - 1
	}

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

	repoLabels := make(map[string]string, len(m.snapshot.Repos))
	for _, repo := range m.snapshot.Repos {
		label := strings.TrimSpace(repo.RepoKey)
		if label == "" {
			label = repo.RepoID
		}
		repoLabels[repo.RepoID] = label
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

		prefix := " "
		if idx == m.selectedIndex {
			prefix = ">"
		}

		verdict := inv.DisplayStatus
		if strings.TrimSpace(verdict) == "" {
			verdict = inv.Status
		}
		if check, ok := m.snapshot.Checks[inv.InvocationID]; ok {
			if check.Ready || check.Readiness == "ready" {
				verdict = "READY"
			} else {
				verdict = "BLOCKED"
			}
		}

		location := worktreeNames[inv.WorktreeID]
		if location == "" {
			location = inv.WorktreeID
		}
		repoLabel := repoLabels[inv.RepoID]
		if repoLabel != "" && location != "" {
			location = repoLabel + " / " + location
		} else if repoLabel != "" {
			location = repoLabel
		}

		summary := strings.TrimSpace(inv.StatusSummary)
		if activity := inv.LatestActivity; activity != nil {
			toolCount := activity.ToolCallCount
			if toolCount == 0 {
				toolCount = len(activity.ToolCalls)
			}
			if strings.TrimSpace(activity.Kind) != "" || strings.TrimSpace(activity.Summary) != "" || toolCount > 0 || activity.CheckpointID > 0 {
				summary = render.FormatActivityWithExtras(
					activity.Kind,
					activity.Summary,
					toolCount,
					activity.CheckpointID,
					activity.Restorable,
				)
			}
		}

		attention := ""
		if len(inv.AttentionFlags) > 0 {
			attention = " [" + strings.Join(inv.AttentionFlags, ",") + "]"
		}

		row := truncateWithEllipsis(
			fmt.Sprintf(
				"%s %-9s %-12s %s%s",
				prefix,
				verdict,
				truncateWithEllipsis(inv.Runner+"/"+inv.Mode, 12),
				truncateWithEllipsis(location, max(1, width/3)),
				attention,
			),
			width,
		)
		if idx == m.selectedIndex {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)

		if summary != "" {
			detail := "  " + truncateWithEllipsis(summary, width-2)
			if idx == m.selectedIndex {
				detail = selectedRowStyle.Render(detail)
			}
			lines = append(lines, detail)
		}
	}

	if start > 0 || end < len(m.snapshot.Invocations) {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("showing %d-%d of %d", start+1, end, len(m.snapshot.Invocations)))
	}

	return strings.Join(lines, "\n")
}

func (m model) renderDetailsPanel(width int) string {
	lines := []string{"selected invocation", ""}

	selected, ok := m.selectedInvocation()
	if !ok {
		lines = append(lines, "select an invocation to view readiness and actions")
		return strings.Join(lines, "\n")
	}

	check, hasCheck := m.snapshot.Checks[selected.InvocationID]
	if !hasCheck {
		lines = append(lines, warningStyle.Render("check data unavailable (retrying)"))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("invocation: %s", selected.InvocationID))
		lines = append(lines, fmt.Sprintf("status:     %s", selected.DisplayStatus))
		return truncateLines(lines, width)
	}

	readiness := blockedStyle.Render("BLOCKED")
	if check.Ready || check.Readiness == "ready" {
		readiness = readyStyle.Render("READY")
	}

	lines = append(lines, "readiness: "+readiness)
	status := strings.TrimSpace(check.DisplayStatus)
	if status == "" {
		status = strings.TrimSpace(selected.DisplayStatus)
	}
	if status == "" {
		status = strings.TrimSpace(selected.Status)
	}
	lines = append(lines, fmt.Sprintf("status:    %s", status))
	if len(selected.AttentionFlags) > 0 {
		lines = append(lines, "attention: "+strings.Join(selected.AttentionFlags, ", "))
	}

	summary := strings.TrimSpace(check.StatusSummary)
	if summary == "" {
		summary = strings.TrimSpace(check.RunnerSummary)
	}
	if summary != "" {
		lines = append(lines, "summary:   "+summary)
	}

	if len(check.BlockingReasons) > 0 {
		lines = append(lines, "why:       "+check.BlockingReasons[0].Message)
		if hint := strings.TrimSpace(check.BlockingReasons[0].Hint); hint != "" {
			lines = append(lines, "hint:      "+hint)
		}
	} else {
		lines = append(lines, "why:       no blocking reasons")
	}

	nextAction := "h for history, o to open sandbox"
	switch {
	case check.Ready && check.PRSyncEligible:
		nextAction = "p to sync the PR"
	case selected.Mode == "headed" && selected.Status == "running":
		nextAction = "enter to attach to the running session"
	case strings.TrimSpace(selected.WorktreeID) != "":
		nextAction = "h for history, then restore or inspect logs"
	}
	lines = append(lines, "next:      "+nextAction)
	lines = append(lines, fmt.Sprintf("pr_sync:   %t", check.PRSyncEligible))
	if check.HowToTest != "" {
		lines = append(lines, "how_to_test: "+check.HowToTest)
	}

	if activity := check.LatestActivity; activity != nil {
		toolCount := activity.ToolCallCount
		if toolCount == 0 {
			toolCount = len(activity.ToolCalls)
		}
		if strings.TrimSpace(activity.Kind) != "" || strings.TrimSpace(activity.Summary) != "" || toolCount > 0 || activity.CheckpointID > 0 {
			latest := render.FormatActivityWithExtras(
				activity.Kind,
				activity.Summary,
				toolCount,
				activity.CheckpointID,
				activity.Restorable,
			)
			if turnID := strings.TrimSpace(activity.TurnID); turnID != "" {
				lines = append(lines, "latest:    ["+turnID+"] "+latest)
			} else {
				lines = append(lines, "latest:    "+latest)
			}
			for _, tool := range activity.ToolCalls {
				lines = append(lines, "tool:      "+render.FormatToolCallSummary(tool.Name, tool.Command, tool.HasExit, tool.ExitCode))
			}
			if paths := render.FormatChangedPathSummary(activity.CheckpointChangedPaths, activity.CheckpointChangedCount, activity.CheckpointPathsTrimmed); paths != "" {
				lines = append(lines, "files:     "+paths)
			}
		}
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("invocation_id: %s", selected.InvocationID))
	lines = append(lines, fmt.Sprintf("repo_id:       %s", selected.RepoID))
	lines = append(lines, fmt.Sprintf("worktree_id:   %s", selected.WorktreeID))
	lines = append(lines, fmt.Sprintf("runner/mode:   %s / %s", selected.Runner, selected.Mode))

	if check.Navigation.HistoryCommand != "" {
		lines = append(lines, "history: "+check.Navigation.HistoryCommand)
	}
	if check.Navigation.LatestTurnID != "" {
		lines = append(lines, "turn:    "+check.Navigation.LatestTurnID)
	}

	return truncateLines(lines, width)
}

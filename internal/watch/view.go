package watch

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/ids"
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

	headerParts := []string{
		"agency watch",
		fmt.Sprintf("agents:%d", len(m.snapshot.Invocations)),
		fmt.Sprintf("worktrees:%d", len(m.snapshot.Worktrees)),
		fmt.Sprintf("repos:%d", len(m.snapshot.Repos)),
	}
	if m.workspaceLoading {
		headerParts = append(headerParts, "refreshing")
	}
	if m.actionRunning {
		headerParts = append(headerParts, "action-running")
	}
	if !m.snapshot.UpdatedAt.IsZero() {
		headerParts = append(headerParts, "updated:"+m.snapshot.UpdatedAt.Format(time.Kitchen))
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
			fmt.Sprintf("warnings: %d (first: %s)", len(m.snapshot.Warnings), truncateWithEllipsis(m.snapshot.Warnings[0], width-20)),
		))
	}
	lines = append(lines, warningStyle.Render("j/k move • enter default • x actions • h history • l logs • o open • p pr sync • r refresh • q quit"))
	return strings.Join(lines, "\n")
}

func (m model) renderWorkspacePanels(width, contentHeight int) string {
	leftWidth := width * 3 / 5
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
		leftPanel := panelStyle.Width(panelWidth).Height(leftHeight).Render(m.renderInvocationsPanel(max(1, panelWidth-2), max(6, leftHeight-2)))
		rightPanel := panelStyle.Width(panelWidth).Height(rightHeight).Render(m.renderDetailsPanel(max(1, panelWidth-2)))
		return lipgloss.JoinVertical(lipgloss.Left, leftPanel, rightPanel)
	}

	leftPanel := panelStyle.Width(leftWidth).Height(contentHeight).Render(m.renderInvocationsPanel(max(1, leftWidth-2), max(6, contentHeight-2)))
	rightPanel := panelStyle.Width(rightWidth).Height(contentHeight).Render(m.renderDetailsPanel(max(1, rightWidth-2)))
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
}

func (m model) renderInvocationsPanel(width, height int) string {
	if len(m.snapshot.Invocations) == 0 {
		return "agents\n\n(no agents found)"
	}

	stateWidth := 10
	agentWidth := max(16, width/5)
	worktreeWidth := max(14, width/6)
	repoWidth := max(10, width/8)
	latestWidth := max(12, width-stateWidth-agentWidth-worktreeWidth-repoWidth-8)

	lines := []string{
		"agents",
		"",
		dimStyle.Render(fmt.Sprintf("  %-*s %-*s %-*s %-*s %s", stateWidth, "STATE", agentWidth, "AGENT", worktreeWidth, "WORKTREE", repoWidth, "REPO", "LATEST")),
	}

	maxRows := max(4, height-5)
	start, end := windowForSelection(len(m.snapshot.Invocations), m.selectedIndex, maxRows)
	for idx := start; idx < end; idx++ {
		inv := m.snapshot.Invocations[idx]
		prefix := " "
		if idx == m.selectedIndex {
			prefix = ">"
		}

		row := fmt.Sprintf(
			"%s %-*s %-*s %-*s %-*s %s",
			prefix,
			stateWidth, truncateWithEllipsis(m.invocationState(inv), stateWidth),
			agentWidth, truncateWithEllipsis(m.agentDisplay(inv), agentWidth),
			worktreeWidth, truncateWithEllipsis(m.worktreeDisplay(inv.WorktreeID), worktreeWidth),
			repoWidth, truncateWithEllipsis(m.repoDisplay(inv.RepoID), repoWidth),
			truncateWithEllipsis(m.latestSummary(inv), latestWidth),
		)
		row = truncateWithEllipsis(row, width)
		if idx == m.selectedIndex {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)
	}

	if start > 0 || end < len(m.snapshot.Invocations) {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(m.snapshot.Invocations))))
	}

	return strings.Join(lines, "\n")
}

func (m model) renderDetailsPanel(width int) string {
	lines := []string{"selected", ""}

	selected, ok := m.selectedInvocation()
	if !ok {
		lines = append(lines, "select an agent to inspect it")
		return strings.Join(lines, "\n")
	}

	state := m.invocationState(selected)
	if len(selected.AttentionFlags) > 0 {
		state += " [" + strings.Join(selected.AttentionFlags, ", ") + "]"
	}

	latest := m.latestSummary(selected)
	if latest == "" {
		latest = "no recent activity"
	}

	lines = append(lines, "Agent:      "+m.agentDisplay(selected))
	lines = append(lines, "Worktree:   "+m.worktreeDisplay(selected.WorktreeID))
	lines = append(lines, "Repo:       "+m.repoDisplay(selected.RepoID))
	lines = append(lines, "Runner:     "+selected.Runner+" / "+selected.Mode)
	lines = append(lines, "State:      "+state)
	if strings.TrimSpace(selected.Reason) != "" {
		lines = append(lines, "Reason:     "+selected.Reason)
	}
	lines = append(lines, "Latest:     "+latest)
	lines = append(lines, "Next:       "+m.nextActionSummary(selected))
	lines = append(lines, "")
	lines = append(lines, m.renderActionPanel(width)...)
	lines = append(lines, "")
	lines = append(lines, "IDs:        "+selected.InvocationID+" · "+firstNonEmpty(selected.WorktreeID, "-")+" · "+selected.RepoID)
	return truncateLines(lines, width)
}

func (m model) renderPageHeader(title string) []string {
	lines := []string{headerStyle.Render(title)}

	selected, ok := m.selectedInvocation()
	if !ok {
		lines = append(lines, dimStyle.Render("agent unavailable"))
		lines = append(lines, "")
		return lines
	}

	lines = append(lines, dimStyle.Render(
		"agent "+m.agentDisplay(selected)+"  worktree "+m.worktreeDisplay(selected.WorktreeID)+"  repo "+m.repoDisplay(selected.RepoID),
	))
	lines = append(lines, dimStyle.Render(
		"state "+m.invocationState(selected)+"  runner "+selected.Runner+"/"+selected.Mode,
	))
	lines = append(lines, "")
	lines = append(lines, m.renderTransientActionPanel(m.width)...)
	return lines
}

func (m model) renderActionPanel(width int) []string {
	lines := make([]string, 0, 16)
	switch {
	case m.followupInput:
		lines = append(lines, "Follow-up:")
		lines = append(lines, "  prompt: "+truncateWithEllipsis(m.followupText, max(1, width-10)))
		lines = append(lines, dimStyle.Render("  enter send • esc cancel"))
	case m.confirmAction != "":
		lines = append(lines, "Confirm:")
		lines = append(lines, "  "+string(m.confirmAction)+" "+m.selectedActionTarget())
		lines = append(lines, dimStyle.Render("  y confirm • esc cancel"))
	case m.actionMenuOpen:
		lines = append(lines, "Actions:")
		if m.canStartAction(actionAttach) {
			lines = append(lines, "  a attach")
		}
		if m.canStartAction(actionOpen) {
			lines = append(lines, "  o open sandbox")
		}
		if m.canStartAction(actionStop) {
			lines = append(lines, "  s stop invocation")
		}
		if m.canStartAction(actionKill) {
			lines = append(lines, "  k kill invocation")
		}
		if m.canStartAction(actionLand) {
			lines = append(lines, "  n land changes")
		}
		if m.canStartAction(actionDiscard) {
			lines = append(lines, "  d discard changes")
		}
		if m.canStartAction(actionFollowup) {
			lines = append(lines, "  f send follow-up")
		}
		if m.canStartAction(actionRecreate) {
			lines = append(lines, "  c recreate headed session")
		}
		if m.canStartAction(actionPRSync) {
			lines = append(lines, "  p sync PR")
		}
		if m.canStartAction(actionPRMerge) {
			lines = append(lines, "  m merge PR")
		}
		if m.canStartAction(actionRebase) {
			lines = append(lines, "  b rebase worktree")
		}
		lines = append(lines, dimStyle.Render("  esc cancel"))
	default:
		lines = append(lines, "Actions:")
		if m.canStartAction(actionAttach) {
			lines = append(lines, "  enter attach")
		} else {
			lines = append(lines, "  enter open actions")
		}
		lines = append(lines, "  x more actions")
		lines = append(lines, "  h history • l logs")
	}
	return lines
}

func (m model) renderTransientActionPanel(width int) []string {
	if !m.actionMenuOpen && m.confirmAction == "" && !m.followupInput {
		return nil
	}
	lines := m.renderActionPanel(width)
	lines = append(lines, "")
	return lines
}

func (m model) selectedActionTarget() string {
	selected, ok := m.selectedInvocation()
	if !ok {
		return "selected invocation"
	}
	return m.agentDisplay(selected) + " / " + m.worktreeDisplay(selected.WorktreeID) + " / " + m.repoDisplay(selected.RepoID)
}

func (m model) invocationState(inv daemon.InvocationDTO) string {
	check, ok := m.snapshot.Checks[inv.InvocationID]
	if ok && strings.TrimSpace(check.State) != "" {
		return check.State
	}
	if strings.TrimSpace(inv.State) != "" {
		return inv.State
	}
	return "-"
}

func (m model) latestSummary(inv daemon.InvocationDTO) string {
	check, ok := m.snapshot.Checks[inv.InvocationID]
	if ok {
		if latest := latestSummaryFromActivity(check.LatestActivity); latest != "" {
			return latest
		}
		if summary := strings.TrimSpace(check.StatusSummary); summary != "" {
			return summary
		}
	}
	if latest := latestSummaryFromActivity(inv.LatestActivity); latest != "" {
		return latest
	}
	return strings.TrimSpace(inv.StatusSummary)
}

func latestSummaryFromActivity(activity *daemon.InvocationLatestActivity) string {
	if activity == nil {
		return ""
	}
	toolCount := activity.ToolCallCount
	if toolCount == 0 {
		toolCount = len(activity.ToolCalls)
	}
	if strings.TrimSpace(activity.Kind) == "" &&
		strings.TrimSpace(activity.Summary) == "" &&
		toolCount == 0 &&
		activity.CheckpointID == 0 {
		return ""
	}
	return render.FormatActivityWithExtras(
		activity.Kind,
		activity.Summary,
		toolCount,
		activity.CheckpointID,
		activity.Restorable,
	)
}

func (m model) nextActionSummary(inv daemon.InvocationDTO) string {
	switch {
	case m.followupInput:
		return "type a follow-up prompt and press enter"
	case m.confirmAction != "":
		return "confirm the selected action or cancel it"
	case m.actionMenuOpen:
		return "choose an action key from the list"
	case m.canStartAction(actionAttach):
		return "attach to the running session"
	case m.canStartAction(actionLand):
		return "land the agent changes"
	case m.canStartAction(actionPRSync):
		return "sync the PR"
	default:
		return "open actions, inspect history, or read logs"
	}
}

func (m model) agentDisplay(inv daemon.InvocationDTO) string {
	name := strings.TrimSpace(inv.InvocationName)
	if name == "" {
		return shortID(inv.InvocationID, 12)
	}
	return name + " (" + shortID(inv.InvocationID, 12) + ")"
}

func (m model) worktreeDisplay(worktreeID string) string {
	worktreeID = strings.TrimSpace(worktreeID)
	if worktreeID == "" {
		return "-"
	}
	for _, wt := range m.snapshot.Worktrees {
		if wt.WorktreeID != worktreeID {
			continue
		}
		name := strings.TrimSpace(wt.Name)
		if name == "" {
			return shortID(worktreeID, 12)
		}
		return name + " (" + shortID(worktreeID, 12) + ")"
	}
	return shortID(worktreeID, 12)
}

func (m model) repoDisplay(repoID string) string {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return "-"
	}
	for _, repo := range m.snapshot.Repos {
		if repo.RepoID != repoID {
			continue
		}
		label := ids.RepoShortName(repo.RepoKey)
		if label == "" {
			label = strings.TrimSpace(repo.RepoKey)
		}
		if label == "" {
			label = shortID(repo.RepoID, 12)
		}
		return label + " (" + shortID(repo.RepoID, 12) + ")"
	}
	return shortID(repoID, 12)
}

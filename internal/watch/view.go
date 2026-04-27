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

	headerParts := []string{
		"agency watch",
		m.workspaceScopeLabel(),
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
	switch m.workspaceFocus {
	case workspacePaneRepos:
		lines = append(lines, warningStyle.Render("tab focus • j/k move • enter scope repo • b/esc broaden • r refresh • q quit"))
	case workspacePaneWorktrees:
		lines = append(lines, warningStyle.Render("tab focus • j/k move • enter scope worktree • b/esc broaden • r refresh • q quit"))
	case workspacePaneAgents:
		lines = append(lines, warningStyle.Render("tab focus • j/k move • enter default • d review • x actions • h history • l logs • o open • p pr sync • b/esc broaden • r refresh • q quit"))
	case "":
		lines = append(lines, warningStyle.Render("tab focus • j/k move • enter default • r refresh • q quit"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderWorkspacePanels(width, contentHeight int) string {
	repoWidth := max(18, width/7)
	worktreeWidth := max(24, width/5)
	detailsWidth := max(34, width/4)
	agentsWidth := width - repoWidth - worktreeWidth - detailsWidth - 3
	if width < 120 || agentsWidth < 40 {
		panelWidth := max(1, width-2)
		listHeight := max(6, contentHeight/4)
		detailHeight := max(8, contentHeight-listHeight*3)
		reposPanel := panelStyle.Width(panelWidth).Height(listHeight).Render(m.renderReposPanel(max(1, panelWidth-2), max(4, listHeight-2)))
		worktreesPanel := panelStyle.Width(panelWidth).Height(listHeight).Render(m.renderWorktreesPanel(max(1, panelWidth-2), max(4, listHeight-2)))
		agentsPanel := panelStyle.Width(panelWidth).Height(listHeight).Render(m.renderAgentsPanel(max(1, panelWidth-2), max(4, listHeight-2)))
		detailsPanel := panelStyle.Width(panelWidth).Height(detailHeight).Render(m.renderDetailsPanel(max(1, panelWidth-2)))
		return lipgloss.JoinVertical(lipgloss.Left, reposPanel, worktreesPanel, agentsPanel, detailsPanel)
	}

	reposPanel := panelStyle.Width(repoWidth).Height(contentHeight).Render(m.renderReposPanel(max(1, repoWidth-2), max(6, contentHeight-2)))
	worktreesPanel := panelStyle.Width(worktreeWidth).Height(contentHeight).Render(m.renderWorktreesPanel(max(1, worktreeWidth-2), max(6, contentHeight-2)))
	agentsPanel := panelStyle.Width(agentsWidth).Height(contentHeight).Render(m.renderAgentsPanel(max(1, agentsWidth-2), max(6, contentHeight-2)))
	detailsPanel := panelStyle.Width(detailsWidth).Height(contentHeight).Render(m.renderDetailsPanel(max(1, detailsWidth-2)))
	return lipgloss.JoinHorizontal(lipgloss.Top, reposPanel, worktreesPanel, agentsPanel, detailsPanel)
}

func (m model) renderReposPanel(width, height int) string {
	lines := []string{m.workspacePaneTitle(workspacePaneRepos, "Repos"), ""}

	maxRows := max(3, height-3)
	start, end := windowForSelection(len(m.snapshot.Repos)+1, m.selectedRepoIndex, maxRows)
	for idx := start; idx < end; idx++ {
		label := "all repos"
		active := strings.TrimSpace(m.activeRepoID) == ""
		if idx > 0 {
			repo := m.snapshot.Repos[idx-1]
			name := strings.TrimSpace(repo.RepoKey)
			if name == "" {
				name = strings.TrimSpace(repo.RepoName)
			}
			label = m.repoDisplay(name, repo.RepoID)
			active = repo.RepoID == m.activeRepoID
		}

		prefix := "  "
		if idx == m.selectedRepoIndex {
			prefix = "> "
		}
		if active {
			prefix = prefix[:1] + "*"
		}
		row := truncateWithEllipsis(prefix+" "+label, width)
		if idx == m.selectedRepoIndex && m.workspaceFocus == workspacePaneRepos {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)
	}
	if len(m.snapshot.Repos) == 0 {
		lines = append(lines, dimStyle.Render("  no repos"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderWorktreesPanel(width, height int) string {
	lines := []string{m.workspacePaneTitle(workspacePaneWorktrees, "Worktrees"), ""}

	maxRows := max(3, height-3)
	start, end := windowForSelection(len(m.snapshot.Worktrees)+1, m.selectedWorktreeIndex, maxRows)
	for idx := start; idx < end; idx++ {
		label := "all worktrees"
		active := strings.TrimSpace(m.activeWorktreeID) == ""
		if idx > 0 {
			wt := m.snapshot.Worktrees[idx-1]
			label = m.worktreeDisplay(wt.WorktreeName, wt.WorktreeID)
			if strings.TrimSpace(m.activeRepoID) == "" {
				label += " / " + m.repoDisplay(wt.RepoName, wt.RepoID)
			}
			active = wt.WorktreeID == m.activeWorktreeID
		}

		prefix := "  "
		if idx == m.selectedWorktreeIndex {
			prefix = "> "
		}
		if active {
			prefix = prefix[:1] + "*"
		}
		row := truncateWithEllipsis(prefix+" "+label, width)
		if idx == m.selectedWorktreeIndex && m.workspaceFocus == workspacePaneWorktrees {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)
	}
	if len(m.snapshot.Worktrees) == 0 {
		lines = append(lines, dimStyle.Render("  no worktrees"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderAgentsPanel(width, height int) string {
	if len(m.snapshot.Invocations) == 0 {
		return strings.Join([]string{m.workspacePaneTitle(workspacePaneAgents, "Agents"), "", "  no agents"}, "\n")
	}

	stateWidth := 10
	agentWidth := max(16, width/5)
	worktreeWidth := max(14, width/6)
	repoWidth := max(10, width/8)
	latestWidth := max(12, width-stateWidth-agentWidth-worktreeWidth-repoWidth-8)

	lines := []string{
		m.workspacePaneTitle(workspacePaneAgents, "Agents"),
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
			worktreeWidth, truncateWithEllipsis(m.worktreeDisplay(inv.WorktreeName, inv.WorktreeID), worktreeWidth),
			repoWidth, truncateWithEllipsis(m.repoDisplay(inv.RepoName, inv.RepoID), repoWidth),
			truncateWithEllipsis(m.latestSummary(inv), latestWidth),
		)
		row = truncateWithEllipsis(row, width)
		if idx == m.selectedIndex && m.workspaceFocus == workspacePaneAgents {
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
	lines := []string{"Selected", ""}

	selected, ok := m.selectedInvocation()
	if !ok {
		lines = append(lines, "select an agent to inspect it")
		return strings.Join(lines, "\n")
	}

	state := m.invocationState(selected)
	if reason := strings.TrimSpace(selected.Reason); reason != "" {
		state += " (" + reason + ")"
	}

	latest := m.latestSummary(selected)
	if latest == "" {
		latest = "no recent activity"
	}

	agent := strings.TrimSpace(selected.InvocationName)
	if agent == "" {
		agent = strings.TrimSpace(selected.InvocationID)
	} else {
		agent += " (" + selected.InvocationID + ")"
	}

	lines = append(lines, "Agent:      "+agent)
	lines = append(lines, "Worktree:   "+m.worktreeDisplay(selected.WorktreeName, selected.WorktreeID))
	lines = append(lines, "Repo:       "+m.repoDisplay(selected.RepoName, selected.RepoID))
	lines = append(lines, "Runner:     "+selected.Runner+" / "+selected.Mode)
	lines = append(lines, "State:      "+state)
	lines = append(lines, "Latest:     "+latest)
	lines = append(lines, m.renderSessionDetailLines(width, selected)...)
	lines = append(lines, "")
	lines = append(lines, m.renderActionPanel(width)...)
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
		"agent "+m.agentDisplay(selected)+"  worktree "+m.worktreeDisplay(selected.WorktreeName, selected.WorktreeID)+"  repo "+m.repoDisplay(selected.RepoName, selected.RepoID),
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
		lines = append(lines, "  d review diff")
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

func (m model) renderSessionDetailLines(width int, selected daemon.InvocationDTO) []string {
	lines := make([]string, 0, 12)
	if strings.TrimSpace(selected.Mode) != "headed" {
		lines = append(lines, "Session:     n/a (headless)")
		return lines
	}

	switch {
	case m.sessionLoader == nil:
		lines = append(lines, "Session:     unavailable")
	case m.selectedSessionLoading:
		lines = append(lines, "Session:     loading...")
	case strings.TrimSpace(m.selectedSessionError) != "":
		lines = append(lines, "Session:     error")
		lines = append(lines, "Session err: "+truncateWithEllipsis(m.selectedSessionError, max(1, width-13)))
	default:
		status := strings.TrimSpace(m.selectedSession.SessionStatus)
		if status == "" {
			status = "unknown"
		}
		lines = append(lines, "Session:     "+status)
		if sessionName := strings.TrimSpace(m.selectedSession.TmuxSession); sessionName != "" {
			lines = append(lines, "Tmux:        "+sessionName)
		}
		clientCount := m.selectedSession.ClientCount
		if clientCount <= 0 {
			clientCount = len(m.selectedSession.ConnectedClients)
		}
		lines = append(lines, fmt.Sprintf("Clients:     %d", clientCount))
		if attachCommand := strings.TrimSpace(m.selectedSession.AttachCommand); attachCommand != "" {
			lines = append(lines, "Attach:      "+attachCommand)
		}
		recreate := "no"
		if m.selectedSessionCanRecreate() {
			recreate = "yes"
		}
		lines = append(lines, "Recreate:    "+recreate)
		if strings.EqualFold(strings.TrimSpace(m.selectedSession.SessionStatus), "missing") && m.selectedSession.RecreateAvailable {
			lines = append(lines, "Hint:        "+truncateWithEllipsis("use recreate to start a new headed session in the same sandbox", max(1, width-13)))
		}
		if len(m.selectedSession.ConnectedClients) > 0 {
			lines = append(lines, "Connected:")
			for idx, client := range m.selectedSession.ConnectedClients {
				if idx == 5 {
					lines = append(lines, fmt.Sprintf("  ... %d more", len(m.selectedSession.ConnectedClients)-idx))
					break
				}
				label := strings.TrimSpace(client.Name)
				if label == "" {
					label = fmt.Sprintf("client %d", idx+1)
				}
				if client.ReadOnly {
					label += " (read-only)"
				}
				lines = append(lines, "  - "+label)
			}
		}
	}

	return lines
}

func (m model) workspaceScopeLabel() string {
	repoLabel := "all repos"
	if strings.TrimSpace(m.activeRepoID) != "" {
		repoLabel = m.repoDisplay("", m.activeRepoID)
		for _, repo := range m.snapshot.Repos {
			if repo.RepoID == m.activeRepoID {
				name := strings.TrimSpace(repo.RepoKey)
				if name == "" {
					name = strings.TrimSpace(repo.RepoName)
				}
				repoLabel = m.repoDisplay(name, repo.RepoID)
				break
			}
		}
	}

	worktreeLabel := "all worktrees"
	if strings.TrimSpace(m.activeWorktreeID) != "" {
		worktreeLabel = m.worktreeDisplay("", m.activeWorktreeID)
		for _, wt := range m.snapshot.Worktrees {
			if wt.WorktreeID == m.activeWorktreeID {
				worktreeLabel = m.worktreeDisplay(wt.WorktreeName, wt.WorktreeID)
				break
			}
		}
	}

	return repoLabel + " / " + worktreeLabel
}

func (m model) workspacePaneTitle(pane workspacePane, title string) string {
	if m.workspaceFocus == pane {
		return "> " + title
	}
	return title
}

func (m model) selectedActionTarget() string {
	selected, ok := m.selectedInvocation()
	if !ok {
		return "selected invocation"
	}
	return m.agentDisplay(selected) + " / " + m.worktreeDisplay(selected.WorktreeName, selected.WorktreeID) + " / " + m.repoDisplay(selected.RepoName, selected.RepoID)
}

func (m model) invocationState(inv daemon.InvocationDTO) string {
	if strings.TrimSpace(inv.State) != "" {
		return inv.State
	}
	return "-"
}

func (m model) latestSummary(inv daemon.InvocationDTO) string {
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

func (m model) agentDisplay(inv daemon.InvocationDTO) string {
	name := strings.TrimSpace(inv.InvocationName)
	if name == "" {
		return shortID(inv.InvocationID, 12)
	}
	return name + " (" + shortID(inv.InvocationID, 12) + ")"
}

func (m model) worktreeDisplay(name, worktreeID string) string {
	if strings.TrimSpace(name) != "" || strings.TrimSpace(worktreeID) == "" {
		return displayWorktree(name, worktreeID)
	}
	for _, wt := range m.snapshot.Worktrees {
		if wt.WorktreeID == worktreeID {
			return displayWorktree(wt.WorktreeName, wt.WorktreeID)
		}
	}
	return displayWorktree(name, worktreeID)
}

func (m model) repoDisplay(name, repoID string) string {
	if strings.TrimSpace(name) != "" || strings.TrimSpace(repoID) == "" {
		return displayRepo(name, repoID)
	}
	for _, repo := range m.snapshot.Repos {
		if repo.RepoID == repoID {
			return displayRepo(repo.RepoKey, repo.RepoID)
		}
	}
	return displayRepo(name, repoID)
}

func displayWorktree(name, worktreeID string) string {
	worktreeID = strings.TrimSpace(worktreeID)
	name = strings.TrimSpace(name)
	if name == "" {
		if worktreeID == "" {
			return "-"
		}
		return worktreeID
	}
	if worktreeID == "" {
		return name
	}
	return name + " (" + worktreeID + ")"
}

func displayRepo(name, repoID string) string {
	repoID = strings.TrimSpace(repoID)
	name = strings.TrimSpace(name)
	if name == "" {
		if repoID == "" {
			return "-"
		}
		return repoID
	}
	if repoID == "" {
		return name
	}
	return name + " (" + repoID + ")"
}

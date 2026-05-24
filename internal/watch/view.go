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
		"agent-state:" + m.invocationStateFilter,
		"worktrees:" + m.worktreeStateFilter,
		fmt.Sprintf("agents:%d", len(m.visibleInvocations())),
		fmt.Sprintf("worktrees:%d", len(m.visibleWorktrees())),
		fmt.Sprintf("repos:%d", len(m.visibleRepos())),
	}
	if m.workspaceLayoutMode != "" {
		headerParts = append(headerParts, "layout:"+m.workspaceLayoutMode)
	}
	if filter := m.workspaceFilterText(); filter != "" {
		headerParts = append(headerParts, "filter:"+filter)
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

	messageLines := make([]string, 0, 2)
	if m.lastActionMessage != "" {
		actionLine := "action: " + truncateWithEllipsis(m.lastActionMessage, width-10)
		switch {
		case m.lastActionError:
			messageLines = append(messageLines, errorStyle.Render(actionLine))
		case m.actionRunning:
			messageLines = append(messageLines, warningStyle.Render(actionLine))
		default:
			messageLines = append(messageLines, actionStyle.Render(actionLine))
		}
	}
	if m.workspaceError != "" {
		messageLines = append(messageLines, errorStyle.Render("refresh error: "+truncateWithEllipsis(m.workspaceError, width-4)+" (auto-retrying)"))
	}

	contentHeight := height - 2 - len(messageLines)
	if contentHeight < 1 {
		contentHeight = 1
	}

	lines := []string{
		headerStyle.Render(truncateWithEllipsis(strings.Join(headerParts, "  "), width)),
		m.renderWorkspacePanels(width, contentHeight),
	}
	lines = append(lines, messageLines...)
	lines = append(lines, warningStyle.Render(truncateWithEllipsis(m.workspaceHelpLine(), width)))
	return strings.Join(lines, "\n")
}

func (m model) renderWorkspacePanels(width, contentHeight int) string {
	panelWidth := max(1, width-2)
	panelHeight := max(1, contentHeight)

	if m.workspaceLayoutMode == "detail" {
		return panelStyle.Width(panelWidth).Height(panelHeight).Render(m.renderDetailsPanel(max(1, panelWidth-2)))
	}

	if m.workspaceLayoutMode == "focused" || width < 120 || contentHeight < 18 {
		switch m.workspaceFocus {
		case workspacePaneRepos:
			return panelStyle.Width(panelWidth).Height(panelHeight).Render(m.renderReposPanel(max(1, panelWidth-2), max(1, panelHeight-2)))
		case workspacePaneWorktrees:
			return panelStyle.Width(panelWidth).Height(panelHeight).Render(m.renderWorktreesPanel(max(1, panelWidth-2), max(1, panelHeight-2)))
		default:
			return panelStyle.Width(panelWidth).Height(panelHeight).Render(m.renderAgentsPanel(max(1, panelWidth-2), max(1, panelHeight-2)))
		}
	}

	if width < 132 {
		agentsHeight := contentHeight - 5
		if agentsHeight < 6 {
			agentsHeight = contentHeight
		}
		agentsPanel := panelStyle.Width(panelWidth).Height(agentsHeight).Render(m.renderAgentsPanel(max(1, panelWidth-2), max(1, agentsHeight-2)))
		if agentsHeight == contentHeight {
			return agentsPanel
		}
		previewPanel := panelStyle.Width(panelWidth).Height(max(1, contentHeight-agentsHeight)).Render(m.renderSelectedPreview(max(1, panelWidth-2), max(1, contentHeight-agentsHeight-2)))
		return lipgloss.JoinVertical(lipgloss.Left, agentsPanel, previewPanel)
	}

	scopeWidth := max(28, width/5)
	detailsWidth := max(36, width/4)
	agentsWidth := width - scopeWidth - detailsWidth - 2
	if agentsWidth < 52 {
		return panelStyle.Width(panelWidth).Height(panelHeight).Render(m.renderAgentsPanel(max(1, panelWidth-2), max(1, panelHeight-2)))
	}

	scopePanel := panelStyle.Width(scopeWidth).Height(contentHeight).Render(m.renderScopePanel(max(1, scopeWidth-2), max(1, contentHeight-2)))
	agentsPanel := panelStyle.Width(agentsWidth).Height(contentHeight).Render(m.renderAgentsPanel(max(1, agentsWidth-2), max(1, contentHeight-2)))
	detailsPanel := panelStyle.Width(detailsWidth).Height(contentHeight).Render(m.renderDetailsPanel(max(1, detailsWidth-2)))
	return lipgloss.JoinHorizontal(lipgloss.Top, scopePanel, agentsPanel, detailsPanel)
}

func (m model) renderScopePanel(width, height int) string {
	repoHeight := height / 3
	if repoHeight < 5 {
		repoHeight = 5
	}
	if repoHeight > height-5 {
		repoHeight = max(1, height/2)
	}
	worktreeHeight := height - repoHeight - 1
	if worktreeHeight < 1 {
		worktreeHeight = 1
	}

	repoLines := strings.Split(m.renderReposPanel(width, repoHeight), "\n")
	worktreeLines := strings.Split(m.renderWorktreesPanel(width, worktreeHeight), "\n")
	lines := append(repoLines, "")
	lines = append(lines, worktreeLines...)
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m model) renderReposPanel(width, height int) string {
	lines := []string{m.workspacePaneTitle(workspacePaneRepos, "Repos"), ""}
	repos := m.visibleRepos()

	maxRows := max(3, height-3)
	start, end := windowForSelection(len(repos)+1, m.selectedRepoIndex, maxRows)
	for idx := start; idx < end; idx++ {
		label := "all repos"
		active := strings.TrimSpace(m.activeRepoID) == ""
		if idx > 0 {
			repo := repos[idx-1]
			name := strings.TrimSpace(repo.RepoKey)
			if name == "" {
				name = strings.TrimSpace(repo.RepoName)
			}
			label = name
			if label == "" {
				label = shortID(repo.RepoID, 12)
			}
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
	if len(repos) == 0 {
		lines = append(lines, dimStyle.Render("  no repos"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderWorktreesPanel(width, height int) string {
	lines := []string{m.workspacePaneTitle(workspacePaneWorktrees, "Worktrees"), ""}
	worktrees := m.visibleWorktrees()

	maxRows := max(3, height-3)
	start, end := windowForSelection(len(worktrees)+1, m.selectedWorktreeIndex, maxRows)
	for idx := start; idx < end; idx++ {
		label := "all worktrees"
		active := strings.TrimSpace(m.activeWorktreeID) == ""
		if idx > 0 {
			wt := worktrees[idx-1]
			label = strings.TrimSpace(wt.WorktreeName)
			if label == "" {
				label = shortID(wt.WorktreeID, 12)
			}
			if strings.TrimSpace(wt.State) == "archived" {
				label += " [archived]"
			}
			if strings.TrimSpace(m.activeRepoID) == "" {
				repoLabel := strings.TrimSpace(wt.RepoName)
				if repoLabel == "" {
					repoLabel = shortID(wt.RepoID, 12)
				}
				if repoLabel != "" {
					label += " / " + repoLabel
				}
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
	if len(worktrees) == 0 {
		lines = append(lines, dimStyle.Render("  no worktrees"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderAgentsPanel(width, height int) string {
	invocations := m.visibleInvocations()
	if len(invocations) == 0 {
		return strings.Join([]string{m.workspacePaneTitle(workspacePaneAgents, "Agents"), "", "  no agents"}, "\n")
	}

	stateWidth := 10
	showWorktree := strings.TrimSpace(m.activeWorktreeID) == "" && width >= 58
	showRepo := strings.TrimSpace(m.activeRepoID) == "" && width >= 72
	showLatest := width >= 52

	worktreeWidth := 0
	repoWidth := 0
	if showWorktree {
		worktreeWidth = max(14, width/5)
	}
	if showRepo {
		repoWidth = max(10, width/8)
	}
	agentWidth := max(16, width/4)
	latestWidth := width - stateWidth - agentWidth - worktreeWidth - repoWidth - 5
	if !showLatest || latestWidth < 12 {
		showLatest = false
		latestWidth = 0
		agentWidth = max(16, width-stateWidth-worktreeWidth-repoWidth-4)
	}

	lines := []string{
		m.workspacePaneTitle(workspacePaneAgents, "Agents"),
		"",
	}
	header := "  " + padRight("STATE", stateWidth) + " " + padRight("AGENT", agentWidth)
	if showWorktree {
		header += " " + padRight("WORKTREE", worktreeWidth)
	}
	if showRepo {
		header += " " + padRight("REPO", repoWidth)
	}
	if showLatest {
		header += " " + padRight("LATEST", latestWidth)
	}
	lines = append(lines, dimStyle.Render(truncateWithEllipsis(header, width)))

	maxRows := max(4, height-5)
	start, end := windowForSelection(len(invocations), m.selectedIndex, maxRows)
	for idx := start; idx < end; idx++ {
		inv := invocations[idx]
		prefix := " "
		if idx == m.selectedIndex {
			prefix = ">"
		}

		agentLabel := strings.TrimSpace(inv.InvocationName)
		if agentLabel == "" {
			agentLabel = shortID(inv.InvocationID, 12)
		}
		row := prefix + " " + padRight(m.invocationState(inv), stateWidth) + " " + padRight(agentLabel, agentWidth)
		if showWorktree {
			worktreeLabel := strings.TrimSpace(inv.WorktreeName)
			if worktreeLabel == "" {
				worktreeLabel = shortID(inv.WorktreeID, 12)
			}
			row += " " + padRight(worktreeLabel, worktreeWidth)
		}
		if showRepo {
			repoLabel := strings.TrimSpace(inv.RepoName)
			if repoLabel == "" {
				repoLabel = shortID(inv.RepoID, 12)
			}
			row += " " + padRight(repoLabel, repoWidth)
		}
		if showLatest {
			row += " " + truncateWithEllipsis(m.latestSummary(inv), latestWidth)
		}
		row = truncateWithEllipsis(row, width)
		if idx == m.selectedIndex && m.workspaceFocus == workspacePaneAgents {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)
	}

	if start > 0 || end < len(invocations) {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(invocations))))
	}

	return strings.Join(lines, "\n")
}

func (m model) renderSelectedPreview(width, height int) string {
	lines := []string{"Selected"}
	selected, ok := m.selectedInvocation()
	if !ok {
		lines = append(lines, "select an agent")
		return truncateLines(lines, width)
	}
	lines = append(lines, m.agentDisplay(selected)+"  "+m.invocationState(selected))
	if latest := m.latestSummary(selected); latest != "" {
		lines = append(lines, latest)
	}
	lines = append(lines, "enter default • x actions • h history • l logs")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return truncateLines(lines, width)
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

	lines = append(lines, "Agent:      "+displayNamed(selected.InvocationName, selected.InvocationID))
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

func (m model) workspaceFilterText() string {
	switch m.workspaceFocus {
	case workspacePaneRepos:
		return strings.TrimSpace(m.repoFilter)
	case workspacePaneWorktrees:
		return strings.TrimSpace(m.worktreeFilter)
	case workspacePaneAgents, "":
		return strings.TrimSpace(m.agentFilter)
	default:
		return ""
	}
}

func (m model) workspaceHelpLine() string {
	if m.workspaceFilterInput {
		return "filter " + string(m.workspaceFocus) + ": " + m.workspaceFilterText() + " • enter apply • esc cancel"
	}
	switch m.workspaceFocus {
	case workspacePaneRepos:
		return "tab focus • j/k move • / filter • enter scope repo • b/esc broaden • s agents:" + m.invocationStateFilter + " • a worktrees:" + m.worktreeStateFilter + " • z layout • r refresh • q quit"
	case workspacePaneWorktrees:
		return "tab focus • j/k move • / filter • enter scope worktree • b/esc broaden • s agents:" + m.invocationStateFilter + " • a worktrees:" + m.worktreeStateFilter + " • z layout • r refresh • q quit"
	default:
		return "tab focus • j/k move • / filter • enter default • d review • x actions • h history • l logs • o open • p pr sync • b/esc broaden • s agents:" + m.invocationStateFilter + " • a worktrees:" + m.worktreeStateFilter + " • z layout • r refresh • q quit"
	}
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
	if strings.TrimSpace(name) == "" && strings.TrimSpace(worktreeID) != "" {
		for _, wt := range m.snapshot.Worktrees {
			if wt.WorktreeID == worktreeID {
				name = wt.WorktreeName
				break
			}
		}
	}
	return displayNamed(name, worktreeID)
}

func (m model) repoDisplay(name, repoID string) string {
	if strings.TrimSpace(name) == "" && strings.TrimSpace(repoID) != "" {
		for _, repo := range m.snapshot.Repos {
			if repo.RepoID == repoID {
				name = repo.RepoKey
				break
			}
		}
	}
	return displayNamed(name, repoID)
}

// displayNamed formats a "name (id)" label for an identified record. Falls
// back to the id alone when the name is empty, the name alone when the id is
// empty, and "-" when both are empty.
func displayNamed(name, id string) string {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if name == "" {
		if id == "" {
			return "-"
		}
		return id
	}
	if id == "" {
		return name
	}
	return name + " (" + id + ")"
}

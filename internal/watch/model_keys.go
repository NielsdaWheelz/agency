package watch

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m model) updateWorkspaceKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.workspaceFilterInput {
		return m.updateWorkspaceFilterKey(msg)
	}

	switch {
	case isQuitKey(msg):
		return m, tea.Quit
	case msg.Code == tea.KeyTab && msg.Mod.Contains(tea.ModShift):
		switch m.workspaceFocus {
		case workspacePaneRepos:
			m.workspaceFocus = workspacePaneAgents
		case workspacePaneWorktrees:
			m.workspaceFocus = workspacePaneRepos
		case workspacePaneAgents:
			m.workspaceFocus = workspacePaneWorktrees
		case "":
			m.workspaceFocus = workspacePaneAgents
		}
		return m, nil
	case msg.Code == tea.KeyTab:
		switch m.workspaceFocus {
		case workspacePaneRepos:
			m.workspaceFocus = workspacePaneWorktrees
		case workspacePaneWorktrees:
			m.workspaceFocus = workspacePaneAgents
		case workspacePaneAgents:
			m.workspaceFocus = workspacePaneRepos
		case "":
			m.workspaceFocus = workspacePaneAgents
		}
		return m, nil
	case isRefreshKey(msg):
		if m.workspaceLoading {
			return m, nil
		}
		m.workspaceLoading = true
		return m, m.loadWorkspaceSnapshotCmd()
	case msg.Text == "/":
		m.workspaceFilterInput = true
		return m, nil
	case msg.Text == "s":
		switch m.invocationStateFilter {
		case "unresolved":
			m.invocationStateFilter = "all"
		case "all":
			m.invocationStateFilter = "finished"
		default:
			m.invocationStateFilter = "unresolved"
		}
		m.resetInvocationSelection()
		return m, m.loadWorkspaceSnapshotCmd()
	case msg.Text == "a":
		switch m.worktreeStateFilter {
		case "present":
			m.worktreeStateFilter = "archived"
		case "archived":
			m.worktreeStateFilter = "all"
		default:
			m.worktreeStateFilter = "present"
			m.activeWorktreeID = ""
		}
		m.resetWorktreeSelection()
		return m, m.loadWorkspaceSnapshotCmd()
	case msg.Text == "z":
		switch m.workspaceLayoutMode {
		case "":
			m.workspaceLayoutMode = "focused"
		case "focused":
			m.workspaceLayoutMode = "detail"
			m.workspaceFocus = workspacePaneAgents
		default:
			m.workspaceLayoutMode = ""
		}
		return m, nil
	case isUpKey(msg):
		m.moveWorkspaceSelection(-1)
		if m.workspaceFocus == workspacePaneAgents {
			return m, m.loadSelectedSessionForSelectionCmd()
		}
		return m, nil
	case isDownKey(msg):
		m.moveWorkspaceSelection(1)
		if m.workspaceFocus == workspacePaneAgents {
			return m, m.loadSelectedSessionForSelectionCmd()
		}
		return m, nil
	case isTopKey(msg):
		m.moveWorkspaceSelectionToTop()
		if m.workspaceFocus == workspacePaneAgents {
			return m, m.loadSelectedSessionForSelectionCmd()
		}
		return m, nil
	case isBottomKey(msg):
		m.moveWorkspaceSelectionToBottom()
		if m.workspaceFocus == workspacePaneAgents {
			return m, m.loadSelectedSessionForSelectionCmd()
		}
		return m, nil
	case msg.Code == tea.KeyEsc || msg.Text == "b":
		if strings.TrimSpace(m.activeWorktreeID) != "" {
			m.activeWorktreeID = ""
			m.workspaceFocus = workspacePaneWorktrees
			m.resetInvocationSelection()
			return m, m.loadWorkspaceSnapshotCmd()
		}
		if strings.TrimSpace(m.activeRepoID) != "" {
			m.activeRepoID = ""
			m.activeWorktreeID = ""
			m.workspaceFocus = workspacePaneRepos
			m.resetWorktreeSelection()
			return m, m.loadWorkspaceSnapshotCmd()
		}
		return m, nil
	case msg.Text == "h":
		if strings.TrimSpace(m.selectedInvocationID) == "" {
			m.setActionError("history unavailable: no invocation selected")
			return m, nil
		}
		m.page = pageHistory
		m.backPage = pageWorkspace
		m.historyLoading = true
		m.historyError = ""
		return m, m.loadHistoryCmd()
	case msg.Text == "l":
		if strings.TrimSpace(m.selectedInvocationID) == "" {
			m.setActionError("logs unavailable: no invocation selected")
			return m, nil
		}
		m.page = pageLogs
		m.backPage = pageWorkspace
		m.logsKind = m.currentLogsKind()
		m.logsContent = ""
		m.logsLoading = true
		m.logsError = ""
		m.logsScroll = 0
		return m, m.loadLogsCmd()
	case msg.Text == "d":
		return m.openReviewPage("", pageWorkspace)
	case msg.Text == "x":
		return m.openActionMenu()
	case isEnterKey(msg):
		switch m.workspaceFocus {
		case workspacePaneRepos:
			if m.selectedRepoIndex == 0 {
				m.activeRepoID = ""
				m.activeWorktreeID = ""
			} else {
				repo, ok := m.selectedRepo()
				if !ok {
					return m, nil
				}
				m.activeRepoID = repo.RepoID
				m.activeWorktreeID = ""
			}
			m.workspaceFocus = workspacePaneWorktrees
			m.resetWorktreeSelection()
			return m, m.loadWorkspaceSnapshotCmd()
		case workspacePaneWorktrees:
			if m.selectedWorktreeIndex == 0 {
				m.activeWorktreeID = ""
			} else {
				wt, ok := m.selectedWorktree()
				if !ok {
					return m, nil
				}
				m.activeRepoID = wt.RepoID
				m.activeWorktreeID = wt.WorktreeID
				for idx, repo := range m.snapshot.Repos {
					if repo.RepoID == wt.RepoID {
						m.selectedRepoIndex = idx + 1
						break
					}
				}
			}
			m.workspaceFocus = workspacePaneAgents
			m.resetInvocationSelection()
			return m, m.loadWorkspaceSnapshotCmd()
		case workspacePaneAgents:
		case "":
			m.workspaceFocus = workspacePaneAgents
		}
		if m.canStartAction(actionAttach) {
			return m.startInvocationAction(actionAttach)
		}
		return m.openActionMenu()
	case msg.Text == "o":
		return m.startInvocationAction(actionOpen)
	case msg.Text == "p":
		return m.startInvocationAction(actionPRSync)
	default:
		return m, nil
	}
}

func (m model) updateWorkspaceFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == tea.KeyEsc {
		m.workspaceFilterInput = false
		return m, nil
	}
	if msg.Code == tea.KeyEnter {
		m.workspaceFilterInput = false
		m.reconcileSelection()
		return m, nil
	}

	var filter *string
	switch m.workspaceFocus {
	case workspacePaneRepos:
		filter = &m.repoFilter
	case workspacePaneWorktrees:
		filter = &m.worktreeFilter
	default:
		filter = &m.agentFilter
	}

	switch msg.String() {
	case "backspace", "ctrl+h":
		*filter = trimLastRune(*filter)
	case "ctrl+u":
		*filter = ""
	default:
		if msg.Text == "" {
			return m, nil
		}
		*filter += msg.Text
	}

	m.reconcileSelection()
	return m, nil
}

func (m model) updateHistoryKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isQuitKey(msg):
		return m, tea.Quit
	case isBackKey(msg):
		if m.backPage == pageWorkspace {
			m.page = pageWorkspace
			if !m.workspaceLoading {
				m.workspaceLoading = true
			}
			return m, tea.Batch(m.loadWorkspaceSnapshotCmd(), tickCmd(m.interval))
		}
		m.page = m.backPage
		return m, nil
	case isRefreshKey(msg):
		if m.historyLoading {
			return m, nil
		}
		m.historyLoading = true
		return m, m.loadHistoryCmd()
	case isUpKey(msg):
		if m.historySelectedIndex > 0 {
			m.historySelectedIndex--
			m.historySelectedEntryID = m.historyTurns[m.historySelectedIndex].EntryID
		}
		return m, nil
	case isDownKey(msg):
		if m.historySelectedIndex < len(m.historyTurns)-1 {
			m.historySelectedIndex++
			m.historySelectedEntryID = m.historyTurns[m.historySelectedIndex].EntryID
		}
		return m, nil
	case isTopKey(msg):
		if len(m.historyTurns) > 0 {
			m.historySelectedIndex = 0
			m.historySelectedEntryID = m.historyTurns[0].EntryID
		}
		return m, nil
	case isBottomKey(msg):
		if len(m.historyTurns) > 0 {
			m.historySelectedIndex = len(m.historyTurns) - 1
			m.historySelectedEntryID = m.historyTurns[m.historySelectedIndex].EntryID
		}
		return m, nil
	case msg.Text == "a":
		return m.startInvocationAction(actionAttach)
	case msg.Text == "t":
		m.page = pageTranscript
		m.backPage = pageHistory
		m.transcriptContent = ""
		m.transcriptLoading = true
		m.transcriptError = ""
		m.transcriptScroll = 0
		return m, m.loadTranscriptCmd()
	case msg.Text == "l":
		m.page = pageLogs
		m.backPage = pageHistory
		m.logsKind = m.currentLogsKind()
		m.logsContent = ""
		m.logsLoading = true
		m.logsError = ""
		m.logsScroll = 0
		return m, m.loadLogsCmd()
	case msg.Text == "d":
		turn, ok := m.selectedTurn()
		if !ok {
			m.setActionError("review unavailable: no turn selected")
			return m, nil
		}
		return m.openReviewPage(turn.EntryID, pageHistory)
	case msg.Text == "x":
		return m.openActionMenu()
	case isEnterKey(msg):
		return m.startRestoreAction()
	default:
		return m, nil
	}
}

func (m model) updateReviewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isQuitKey(msg):
		return m, tea.Quit
	case isBackKey(msg):
		switch m.backPage {
		case pageWorkspace:
			m.page = pageWorkspace
			if !m.workspaceLoading {
				m.workspaceLoading = true
			}
			return m, tea.Batch(m.loadWorkspaceSnapshotCmd(), tickCmd(m.interval))
		case pageHistory, pageTranscript, pageLogs:
			m.page = m.backPage
			return m, nil
		default:
			m.page = pageWorkspace
			if !m.workspaceLoading {
				m.workspaceLoading = true
			}
			return m, tea.Batch(m.loadWorkspaceSnapshotCmd(), tickCmd(m.interval))
		}
	case isRefreshKey(msg):
		if m.reviewLoading {
			return m, nil
		}
		m.reviewLoading = true
		return m, m.loadReviewCmd()
	case msg.Code == tea.KeyTab:
		m.reviewFilesFocus = !m.reviewFilesFocus
		return m, nil
	case isUpKey(msg):
		if m.reviewFilesFocus {
			m.moveReviewSelection(-1)
			return m, nil
		}
		m.reviewScroll = clamp(m.reviewScroll-1, 0, m.maxReviewScroll())
		return m, nil
	case isDownKey(msg):
		if m.reviewFilesFocus {
			m.moveReviewSelection(1)
			return m, nil
		}
		m.reviewScroll = clamp(m.reviewScroll+1, 0, m.maxReviewScroll())
		return m, nil
	case isTopKey(msg):
		if m.reviewFilesFocus {
			m.setReviewSelection(0)
			return m, nil
		}
		m.reviewScroll = 0
		return m, nil
	case isBottomKey(msg):
		if m.reviewFilesFocus {
			m.setReviewSelection(len(m.reviewFiles) - 1)
			return m, nil
		}
		m.reviewScroll = m.maxReviewScroll()
		return m, nil
	case msg.Text == "a":
		return m.startInvocationAction(actionAttach)
	case msg.Text == "x":
		return m.openActionMenu()
	default:
		return m, nil
	}
}

func (m model) updateTranscriptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isQuitKey(msg):
		return m, tea.Quit
	case isBackKey(msg):
		m.page = pageHistory
		return m, nil
	case isRefreshKey(msg):
		if m.transcriptLoading {
			return m, nil
		}
		m.transcriptLoading = true
		return m, m.loadTranscriptCmd()
	case isUpKey(msg):
		m.transcriptScroll = clamp(m.transcriptScroll-1, 0, m.maxTranscriptScroll())
		return m, nil
	case isDownKey(msg):
		m.transcriptScroll = clamp(m.transcriptScroll+1, 0, m.maxTranscriptScroll())
		return m, nil
	case isTopKey(msg):
		m.transcriptScroll = 0
		return m, nil
	case isBottomKey(msg):
		m.transcriptScroll = m.maxTranscriptScroll()
		return m, nil
	case msg.Text == "a":
		return m.startInvocationAction(actionAttach)
	case msg.Text == "l":
		m.page = pageLogs
		m.backPage = pageTranscript
		m.logsKind = m.currentLogsKind()
		m.logsContent = ""
		m.logsLoading = true
		m.logsError = ""
		m.logsScroll = 0
		return m, m.loadLogsCmd()
	case msg.Text == "d":
		return m.openReviewPage("", pageTranscript)
	case msg.Text == "x":
		return m.openActionMenu()
	default:
		return m, nil
	}
}

func (m model) updateLogsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isQuitKey(msg):
		return m, tea.Quit
	case isBackKey(msg):
		if m.backPage == pageWorkspace {
			m.page = pageWorkspace
			if !m.workspaceLoading {
				m.workspaceLoading = true
			}
			return m, tea.Batch(m.loadWorkspaceSnapshotCmd(), tickCmd(m.interval))
		}
		m.page = m.backPage
		return m, nil
	case isRefreshKey(msg):
		if m.logsLoading {
			return m, nil
		}
		m.logsLoading = true
		return m, m.loadLogsCmd()
	case isUpKey(msg):
		m.logsScroll = clamp(m.logsScroll-1, 0, m.maxLogsScroll())
		return m, nil
	case isDownKey(msg):
		m.logsScroll = clamp(m.logsScroll+1, 0, m.maxLogsScroll())
		return m, nil
	case isTopKey(msg):
		m.logsScroll = 0
		return m, nil
	case isBottomKey(msg):
		m.logsScroll = m.maxLogsScroll()
		return m, nil
	case msg.Text == "a":
		return m.startInvocationAction(actionAttach)
	case msg.Text == "d":
		return m.openReviewPage("", pageLogs)
	case msg.Text == "x":
		return m.openActionMenu()
	default:
		return m, nil
	}
}

func isQuitKey(msg tea.KeyPressMsg) bool {
	return msg.String() == "ctrl+c" || msg.Text == "q"
}

func isBackKey(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyEsc || msg.Text == "b"
}

func isRefreshKey(msg tea.KeyPressMsg) bool {
	return msg.Text == "r"
}

func isEnterKey(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyEnter
}

func isUpKey(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyUp || msg.Text == "k"
}

func isDownKey(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyDown || msg.Text == "j"
}

func isTopKey(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyHome || msg.Text == "g"
}

func isBottomKey(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyEnd || msg.Text == "G"
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

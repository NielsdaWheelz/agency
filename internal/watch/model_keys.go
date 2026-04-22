package watch

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m model) updateWorkspaceKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isQuitKey(msg):
		return m, tea.Quit
	case isRefreshKey(msg):
		if m.workspaceLoading {
			return m, nil
		}
		m.workspaceLoading = true
		return m, m.loadWorkspaceSnapshotCmd()
	case isUpKey(msg):
		m.moveSelection(-1)
		return m, m.loadSelectedSessionForSelectionCmd()
	case isDownKey(msg):
		m.moveSelection(1)
		return m, m.loadSelectedSessionForSelectionCmd()
	case isTopKey(msg):
		if len(m.snapshot.Invocations) > 0 {
			m.selectedIndex = 0
			m.selectedInvocationID = m.snapshot.Invocations[0].InvocationID
			m.selectedRepoID = m.snapshot.Invocations[0].RepoID
		}
		return m, m.loadSelectedSessionForSelectionCmd()
	case isBottomKey(msg):
		if len(m.snapshot.Invocations) > 0 {
			m.selectedIndex = len(m.snapshot.Invocations) - 1
			m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
			m.selectedRepoID = m.snapshot.Invocations[m.selectedIndex].RepoID
		}
		return m, m.loadSelectedSessionForSelectionCmd()
	case msg.Text == "h":
		if strings.TrimSpace(m.selectedInvocationID) == "" {
			m.lastActionError = true
			m.lastActionMessage = "history unavailable: no invocation selected"
			return m, nil
		}
		m.page = pageHistory
		m.backPage = pageWorkspace
		m.historyLoading = true
		m.historyError = ""
		return m, m.loadHistoryCmd()
	case msg.Text == "l":
		if strings.TrimSpace(m.selectedInvocationID) == "" {
			m.lastActionError = true
			m.lastActionMessage = "logs unavailable: no invocation selected"
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
			m.lastActionError = true
			m.lastActionMessage = "review unavailable: no turn selected"
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
			m.moveReviewSelectionTo(0)
			return m, nil
		}
		m.reviewScroll = 0
		return m, nil
	case isBottomKey(msg):
		if m.reviewFilesFocus {
			m.moveReviewSelectionTo(len(m.reviewFiles) - 1)
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
	return msg.Code == tea.KeyEsc || msg.String() == "ctrl+c" || msg.Text == "q"
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

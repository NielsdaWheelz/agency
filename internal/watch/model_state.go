package watch

import (
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

func (m model) selectedInvocation() (daemon.InvocationDTO, bool) {
	if len(m.snapshot.Invocations) == 0 {
		return daemon.InvocationDTO{}, false
	}
	idx := clamp(m.selectedIndex, 0, len(m.snapshot.Invocations)-1)
	return m.snapshot.Invocations[idx], true
}

func (m model) selectedTurn() (daemon.Turn, bool) {
	if len(m.historyTurns) == 0 {
		return daemon.Turn{}, false
	}
	idx := clamp(m.historySelectedIndex, 0, len(m.historyTurns)-1)
	return m.historyTurns[idx], true
}

func (m model) selectedReviewFile() (reviewFile, bool) {
	if len(m.reviewFiles) == 0 {
		return reviewFile{}, false
	}
	idx := clamp(m.reviewSelectedIndex, 0, len(m.reviewFiles)-1)
	return m.reviewFiles[idx], true
}

func (m model) selectedRepo() (daemon.RepoDTO, bool) {
	if m.selectedRepoIndex <= 0 || m.selectedRepoIndex > len(m.snapshot.Repos) {
		return daemon.RepoDTO{}, false
	}
	return m.snapshot.Repos[m.selectedRepoIndex-1], true
}

func (m model) selectedWorktree() (daemon.WorktreeDTO, bool) {
	if m.selectedWorktreeIndex <= 0 || m.selectedWorktreeIndex > len(m.snapshot.Worktrees) {
		return daemon.WorktreeDTO{}, false
	}
	return m.snapshot.Worktrees[m.selectedWorktreeIndex-1], true
}

func (m *model) moveWorkspaceSelection(delta int) {
	switch m.workspaceFocus {
	case workspacePaneRepos:
		m.selectedRepoIndex = clamp(m.selectedRepoIndex+delta, 0, len(m.snapshot.Repos))
	case workspacePaneWorktrees:
		m.selectedWorktreeIndex = clamp(m.selectedWorktreeIndex+delta, 0, len(m.snapshot.Worktrees))
	case workspacePaneAgents:
		m.moveSelection(delta)
	case "":
		m.workspaceFocus = workspacePaneAgents
		m.moveSelection(delta)
	}
}

func (m *model) moveWorkspaceSelectionToTop() {
	switch m.workspaceFocus {
	case workspacePaneRepos:
		m.selectedRepoIndex = 0
	case workspacePaneWorktrees:
		m.selectedWorktreeIndex = 0
	case workspacePaneAgents:
		if len(m.snapshot.Invocations) > 0 {
			m.selectedIndex = 0
			m.selectedInvocationID = m.snapshot.Invocations[0].InvocationID
			m.selectedRepoID = m.snapshot.Invocations[0].RepoID
		}
	case "":
		m.workspaceFocus = workspacePaneAgents
		m.moveWorkspaceSelectionToTop()
	}
}

func (m *model) moveWorkspaceSelectionToBottom() {
	switch m.workspaceFocus {
	case workspacePaneRepos:
		m.selectedRepoIndex = len(m.snapshot.Repos)
	case workspacePaneWorktrees:
		m.selectedWorktreeIndex = len(m.snapshot.Worktrees)
	case workspacePaneAgents:
		if len(m.snapshot.Invocations) > 0 {
			m.selectedIndex = len(m.snapshot.Invocations) - 1
			m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
			m.selectedRepoID = m.snapshot.Invocations[m.selectedIndex].RepoID
		}
	case "":
		m.workspaceFocus = workspacePaneAgents
		m.moveWorkspaceSelectionToBottom()
	}
}

func (m *model) moveSelection(delta int) {
	if len(m.snapshot.Invocations) == 0 {
		m.selectedIndex = 0
		m.selectedInvocationID = ""
		m.selectedRepoID = ""
		return
	}
	next := clamp(m.selectedIndex+delta, 0, len(m.snapshot.Invocations)-1)
	m.selectedIndex = next
	m.selectedInvocationID = m.snapshot.Invocations[next].InvocationID
	m.selectedRepoID = m.snapshot.Invocations[next].RepoID
}

func (m *model) reconcileSelection() bool {
	oldRepoID := m.activeRepoID
	oldWorktreeID := m.activeWorktreeID

	if m.activeRepoID != "" {
		found := false
		for _, repo := range m.snapshot.Repos {
			if repo.RepoID == m.activeRepoID {
				found = true
				break
			}
		}
		if !found {
			m.activeRepoID = ""
			m.activeWorktreeID = ""
		}
	}

	if m.activeWorktreeID != "" {
		found := false
		for _, wt := range m.snapshot.Worktrees {
			if wt.WorktreeID == m.activeWorktreeID && (m.activeRepoID == "" || wt.RepoID == m.activeRepoID) {
				found = true
				if m.activeRepoID == "" {
					m.activeRepoID = wt.RepoID
				}
				break
			}
		}
		if !found {
			m.activeWorktreeID = ""
		}
	}

	m.selectedRepoIndex = clamp(m.selectedRepoIndex, 0, len(m.snapshot.Repos))
	m.selectedWorktreeIndex = clamp(m.selectedWorktreeIndex, 0, len(m.snapshot.Worktrees))

	if len(m.snapshot.Invocations) == 0 {
		m.selectedIndex = 0
		m.selectedInvocationID = ""
		m.selectedRepoID = ""
		return oldRepoID != m.activeRepoID || oldWorktreeID != m.activeWorktreeID
	}

	if m.selectedInvocationID != "" {
		for idx, inv := range m.snapshot.Invocations {
			if inv.InvocationID == m.selectedInvocationID {
				m.selectedIndex = idx
				m.selectedRepoID = inv.RepoID
				return oldRepoID != m.activeRepoID || oldWorktreeID != m.activeWorktreeID
			}
		}
		m.selectedIndex = 0
	} else {
		m.selectedIndex = clamp(m.selectedIndex, 0, len(m.snapshot.Invocations)-1)
	}

	m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
	m.selectedRepoID = m.snapshot.Invocations[m.selectedIndex].RepoID
	return oldRepoID != m.activeRepoID || oldWorktreeID != m.activeWorktreeID
}

func (m *model) reconcileHistorySelection() {
	if len(m.historyTurns) == 0 {
		m.historySelectedIndex = 0
		m.historySelectedEntryID = ""
		return
	}

	if m.historySelectedEntryID != "" {
		for idx, turn := range m.historyTurns {
			if turn.EntryID == m.historySelectedEntryID {
				m.historySelectedIndex = idx
				return
			}
		}
	}

	m.historySelectedIndex = len(m.historyTurns) - 1
	m.historySelectedEntryID = m.historyTurns[m.historySelectedIndex].EntryID
}

func (m *model) moveReviewSelection(delta int) {
	if len(m.reviewFiles) == 0 {
		m.reviewSelectedIndex = 0
		m.reviewSelectedKey = ""
		m.reviewScroll = 0
		return
	}
	next := clamp(m.reviewSelectedIndex+delta, 0, len(m.reviewFiles)-1)
	m.reviewSelectedIndex = next
	m.reviewSelectedKey = m.reviewFiles[next].key
	m.reviewScroll = 0
}

func (m *model) moveReviewSelectionTo(index int) {
	if len(m.reviewFiles) == 0 {
		m.reviewSelectedIndex = 0
		m.reviewSelectedKey = ""
		m.reviewScroll = 0
		return
	}
	next := clamp(index, 0, len(m.reviewFiles)-1)
	m.reviewSelectedIndex = next
	m.reviewSelectedKey = m.reviewFiles[next].key
	m.reviewScroll = 0
}

func (m *model) reconcileReviewSelection() {
	if len(m.reviewFiles) == 0 {
		m.reviewSelectedIndex = 0
		m.reviewSelectedKey = ""
		m.reviewScroll = 0
		return
	}

	if strings.TrimSpace(m.reviewSelectedKey) != "" {
		for idx, file := range m.reviewFiles {
			if file.key == m.reviewSelectedKey {
				m.reviewSelectedIndex = idx
				return
			}
		}
	}

	m.reviewSelectedIndex = clamp(m.reviewSelectedIndex, 0, len(m.reviewFiles)-1)
	m.reviewSelectedKey = m.reviewFiles[m.reviewSelectedIndex].key
}

func (m model) maxLogsScroll() int {
	lines := logLines(m.logsContent)
	visible := m.logVisibleLines()
	if len(lines) <= visible {
		return 0
	}
	return len(lines) - visible
}

func (m model) logVisibleLines() int {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

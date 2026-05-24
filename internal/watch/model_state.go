package watch

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

func (m model) selectedInvocation() (daemon.InvocationDTO, bool) {
	invocations := m.visibleInvocations()
	if len(invocations) == 0 {
		return daemon.InvocationDTO{}, false
	}
	idx := clamp(m.selectedIndex, 0, len(invocations)-1)
	return invocations[idx], true
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
	repos := m.visibleRepos()
	if m.selectedRepoIndex <= 0 || m.selectedRepoIndex > len(repos) {
		return daemon.RepoDTO{}, false
	}
	return repos[m.selectedRepoIndex-1], true
}

func (m model) selectedWorktree() (daemon.WorktreeDTO, bool) {
	worktrees := m.visibleWorktrees()
	if m.selectedWorktreeIndex <= 0 || m.selectedWorktreeIndex > len(worktrees) {
		return daemon.WorktreeDTO{}, false
	}
	return worktrees[m.selectedWorktreeIndex-1], true
}

func (m model) visibleRepos() []daemon.RepoDTO {
	filter := strings.ToLower(strings.TrimSpace(m.repoFilter))
	if filter == "" {
		return m.snapshot.Repos
	}
	repos := make([]daemon.RepoDTO, 0, len(m.snapshot.Repos))
	for _, repo := range m.snapshot.Repos {
		name := firstNonEmpty(strings.TrimSpace(repo.RepoKey), strings.TrimSpace(repo.RepoName))
		text := strings.ToLower(name + " " + repo.RepoID)
		if strings.Contains(text, filter) {
			repos = append(repos, repo)
		}
	}
	return repos
}

func (m model) visibleWorktrees() []daemon.WorktreeDTO {
	filter := strings.ToLower(strings.TrimSpace(m.worktreeFilter))
	if filter == "" {
		return m.snapshot.Worktrees
	}
	worktrees := make([]daemon.WorktreeDTO, 0, len(m.snapshot.Worktrees))
	for _, wt := range m.snapshot.Worktrees {
		text := strings.ToLower(wt.WorktreeName + " " + wt.WorktreeID + " " + wt.RepoName + " " + wt.RepoID + " " + wt.State)
		if strings.Contains(text, filter) {
			worktrees = append(worktrees, wt)
		}
	}
	return worktrees
}

func (m model) visibleInvocations() []daemon.InvocationDTO {
	filter := strings.ToLower(strings.TrimSpace(m.agentFilter))
	if filter == "" {
		return m.snapshot.Invocations
	}
	invocations := make([]daemon.InvocationDTO, 0, len(m.snapshot.Invocations))
	for _, inv := range m.snapshot.Invocations {
		text := strings.ToLower(strings.Join([]string{
			m.invocationState(inv),
			m.agentDisplay(inv),
			m.worktreeDisplay(inv.WorktreeName, inv.WorktreeID),
			m.repoDisplay(inv.RepoName, inv.RepoID),
			m.latestSummary(inv),
		}, " "))
		if strings.Contains(text, filter) {
			invocations = append(invocations, inv)
		}
	}
	return invocations
}

func (m *model) moveWorkspaceSelection(delta int) {
	switch m.workspaceFocus {
	case workspacePaneRepos:
		m.selectedRepoIndex = clamp(m.selectedRepoIndex+delta, 0, len(m.visibleRepos()))
	case workspacePaneWorktrees:
		m.selectedWorktreeIndex = clamp(m.selectedWorktreeIndex+delta, 0, len(m.visibleWorktrees()))
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
		invocations := m.visibleInvocations()
		if len(invocations) > 0 {
			m.applyInvocationSelection(0, invocations)
		}
	case "":
		m.workspaceFocus = workspacePaneAgents
		m.moveWorkspaceSelectionToTop()
	}
}

func (m *model) moveWorkspaceSelectionToBottom() {
	switch m.workspaceFocus {
	case workspacePaneRepos:
		m.selectedRepoIndex = len(m.visibleRepos())
	case workspacePaneWorktrees:
		m.selectedWorktreeIndex = len(m.visibleWorktrees())
	case workspacePaneAgents:
		invocations := m.visibleInvocations()
		if len(invocations) > 0 {
			m.applyInvocationSelection(len(invocations)-1, invocations)
		}
	case "":
		m.workspaceFocus = workspacePaneAgents
		m.moveWorkspaceSelectionToBottom()
	}
}

func (m *model) moveSelection(delta int) {
	m.applyInvocationSelection(m.selectedIndex+delta, m.visibleInvocations())
}

func (m *model) clearInvocationSelection() {
	m.selectedIndex = 0
	m.selectedInvocationID = ""
	m.selectedRepoID = ""
}

// applyInvocationSelection sets the cursor to invocations[idx], clamped, or
// clears the cursor when invocations is empty.
func (m *model) applyInvocationSelection(idx int, invocations []daemon.InvocationDTO) {
	if len(invocations) == 0 {
		m.clearInvocationSelection()
		return
	}
	idx = clamp(idx, 0, len(invocations)-1)
	m.selectedIndex = idx
	m.selectedInvocationID = invocations[idx].InvocationID
	m.selectedRepoID = invocations[idx].RepoID
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

	repos := m.visibleRepos()
	worktrees := m.visibleWorktrees()
	invocations := m.visibleInvocations()

	m.selectedRepoIndex = clamp(m.selectedRepoIndex, 0, len(repos))
	m.selectedWorktreeIndex = clamp(m.selectedWorktreeIndex, 0, len(worktrees))

	if len(invocations) == 0 {
		m.clearInvocationSelection()
		return oldRepoID != m.activeRepoID || oldWorktreeID != m.activeWorktreeID
	}

	if m.selectedInvocationID != "" {
		for idx, inv := range invocations {
			if inv.InvocationID == m.selectedInvocationID {
				m.selectedIndex = idx
				m.selectedRepoID = inv.RepoID
				return oldRepoID != m.activeRepoID || oldWorktreeID != m.activeWorktreeID
			}
		}
		m.selectedIndex = 0
	} else {
		m.selectedIndex = clamp(m.selectedIndex, 0, len(invocations)-1)
	}

	m.selectedInvocationID = invocations[m.selectedIndex].InvocationID
	m.selectedRepoID = invocations[m.selectedIndex].RepoID
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

func (m *model) clearReviewSelection() {
	m.reviewSelectedIndex = 0
	m.reviewSelectedKey = ""
	m.reviewScroll = 0
}

func (m *model) setReviewSelection(index int) {
	if len(m.reviewFiles) == 0 {
		m.clearReviewSelection()
		return
	}
	next := clamp(index, 0, len(m.reviewFiles)-1)
	m.reviewSelectedIndex = next
	m.reviewSelectedKey = m.reviewFiles[next].key
	m.reviewScroll = 0
}

func (m *model) moveReviewSelection(delta int) {
	m.setReviewSelection(m.reviewSelectedIndex + delta)
}

func (m *model) reconcileReviewSelection() {
	if len(m.reviewFiles) == 0 {
		m.clearReviewSelection()
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

// scrollWindow returns the (start, end) slice indices for a scrollable list of
// `total` items given a scroll offset and visible viewport size. When total
// fits in the viewport, the window is the whole slice.
func scrollWindow(total, scroll, visible int) (start, end int) {
	if total <= visible {
		return 0, total
	}
	start = clamp(scroll, 0, max(0, total-visible))
	end = clamp(start+visible, 0, total)
	return start, end
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
	if runewidth.StringWidth(value) <= maxWidth {
		return value
	}
	if maxWidth <= 3 {
		return runewidth.Truncate(value, maxWidth, "")
	}
	return runewidth.Truncate(value, maxWidth, "...")
}

func padRight(value string, width int) string {
	value = truncateWithEllipsis(value, width)
	if padding := width - runewidth.StringWidth(value); padding > 0 {
		return value + strings.Repeat(" ", padding)
	}
	return value
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// setActionError flags the next render as showing an error message in the
// action status line.
func (m *model) setActionError(msg string) {
	m.lastActionError = true
	m.lastActionMessage = msg
}

// setActionMessage flags the next render as showing a non-error message.
func (m *model) setActionMessage(msg string) {
	m.lastActionError = false
	m.lastActionMessage = msg
}

// resetInvocationSelection clears the cached invocations list and selection,
// marking workspace as loading so the next tick triggers a fetch.
func (m *model) resetInvocationSelection() {
	m.snapshot.Invocations = nil
	m.clearInvocationSelection()
	m.workspaceLoading = true
}

// resetWorktreeSelection clears the cached worktree+invocation lists and
// selection (extends resetInvocationSelection with worktree-pane state).
func (m *model) resetWorktreeSelection() {
	m.snapshot.Worktrees = nil
	m.selectedWorktreeIndex = 0
	m.resetInvocationSelection()
}

package watch

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

const defaultRefreshInterval = 2 * time.Second

const minPanelWidth = 40

type InitialPage string

const (
	InitialPageWorkspace InitialPage = "workspace"
	InitialPageHistory   InitialPage = "history"
)

type watchPage string

const (
	pageWorkspace watchPage = "workspace"
	pageHistory   watchPage = "history"
	pageLogs      watchPage = "logs"
)

type actionKind string

const (
	actionAttach  actionKind = "attach"
	actionOpen    actionKind = "open"
	actionPRSync  actionKind = "pr sync"
	actionRestore actionKind = "restore"
)

func (k actionKind) String() string {
	return string(k)
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	panelStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)
	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("229")).
				Background(lipgloss.Color("57"))
	readyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	blockedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	actionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
)

type refreshTickMsg time.Time

type snapshotLoadedMsg struct {
	snapshot Snapshot
	err      error
}

type historyLoadedMsg struct {
	turns []daemon.Turn
	err   error
}

type logsLoadedMsg struct {
	content string
	err     error
}

type actionResultMsg struct {
	kind         actionKind
	invocationID string
	worktreeID   string
	turnID       string
	output       string
	err          error
}

type model struct {
	ctx      context.Context
	client   *daemonclient.Client
	interval time.Duration
	page     watchPage
	backPage watchPage

	width  int
	height int

	snapshot             Snapshot
	selectedIndex        int
	selectedInvocationID string
	selectedRepoID       string

	workspaceLoading bool
	workspaceError   string

	historyTurns           []daemon.Turn
	historySelectedIndex   int
	historySelectedEntryID string
	historyLoading         bool
	historyError           string

	logsContent string
	logsScroll  int
	logsLoading bool
	logsError   string

	actionRunning     bool
	lastActionMessage string
	lastActionError   bool

	attach  func(context.Context, string, string) (string, error)
	open    func(context.Context, string, string) (string, error)
	prSync  func(context.Context, string, string) (string, error)
	restore func(context.Context, string, string, string) (string, error)
}

func newModel(ctx context.Context, client *daemonclient.Client, opts RunOptions) model {
	if ctx == nil {
		ctx = context.Background()
	}

	interval := opts.Interval
	if interval <= 0 {
		interval = defaultRefreshInterval
	}

	var page watchPage
	switch opts.InitialPage {
	case "", InitialPageWorkspace:
		page = pageWorkspace
	case InitialPageHistory:
		page = pageHistory
	default:
		page = watchPage(opts.InitialPage)
	}

	return model{
		ctx:                  ctx,
		client:               client,
		interval:             interval,
		page:                 page,
		backPage:             pageWorkspace,
		selectedInvocationID: strings.TrimSpace(opts.InvocationID),
		selectedRepoID:       strings.TrimSpace(opts.RepoID),
		attach:               opts.Attach,
		open:                 opts.Open,
		prSync:               opts.PRSync,
		restore:              opts.Restore,
	}
}

func (m model) Init() tea.Cmd {
	switch m.page {
	case pageWorkspace:
		m.workspaceLoading = true
		return tea.Batch(m.loadWorkspaceSnapshotCmd(), tickCmd(m.interval))
	case pageHistory:
		if len(m.historyTurns) > 0 {
			return nil
		}
		m.historyLoading = true
		return m.loadHistoryCmd()
	case pageLogs:
		m.logsLoading = true
		return m.loadLogsCmd()
	default:
		return nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case refreshTickMsg:
		if m.page != pageWorkspace {
			return m, nil
		}
		if m.workspaceLoading {
			return m, tickCmd(m.interval)
		}
		m.workspaceLoading = true
		return m, tea.Batch(m.loadWorkspaceSnapshotCmd(), tickCmd(m.interval))

	case snapshotLoadedMsg:
		m.workspaceLoading = false
		if msg.err != nil {
			m.workspaceError = msg.err.Error()
			return m, nil
		}
		m.snapshot = msg.snapshot
		m.workspaceError = ""
		m.reconcileSelection()
		return m, nil

	case historyLoadedMsg:
		m.historyLoading = false
		if msg.err != nil {
			m.historyError = msg.err.Error()
			return m, nil
		}
		m.historyTurns = msg.turns
		m.historyError = ""
		m.reconcileHistorySelection()
		return m, nil

	case logsLoadedMsg:
		m.logsLoading = false
		if msg.err != nil {
			m.logsError = msg.err.Error()
			return m, nil
		}
		m.logsContent = msg.content
		m.logsError = ""
		m.logsScroll = clamp(m.logsScroll, 0, m.maxLogsScroll())
		return m, nil

	case actionResultMsg:
		m.actionRunning = false
		m.lastActionError = msg.err != nil
		if msg.err != nil {
			m.lastActionMessage = formatActionError(msg.kind, msg.err, msg.invocationID, msg.worktreeID, msg.turnID)
			if output := strings.TrimSpace(msg.output); output != "" {
				m.lastActionMessage += " | " + output
			}
		} else if output := strings.TrimSpace(msg.output); output != "" {
			m.lastActionMessage = output
		} else {
			m.lastActionMessage = fmt.Sprintf("%s complete for %s", msg.kind, actionTarget(msg.kind, msg.invocationID, msg.worktreeID, msg.turnID))
		}

		switch msg.kind {
		case actionAttach, actionOpen, actionPRSync:
			if m.page == pageWorkspace && !m.workspaceLoading {
				m.workspaceLoading = true
				return m, m.loadWorkspaceSnapshotCmd()
			}
		case actionRestore:
			if m.page == pageHistory && !m.historyLoading {
				m.historyLoading = true
				return m, m.loadHistoryCmd()
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.page {
		case pageWorkspace:
			return m.updateWorkspaceKey(msg)
		case pageHistory:
			return m.updateHistoryKey(msg)
		case pageLogs:
			return m.updateLogsKey(msg)
		default:
			return m, nil
		}
	}

	return m, nil
}

func (m model) View() tea.View {
	var content string
	switch m.page {
	case pageHistory:
		content = m.renderHistory()
	case pageLogs:
		content = m.renderLogs()
	default:
		content = m.renderWorkspace()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.Cursor = nil
	return v
}

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
		return m, nil
	case isDownKey(msg):
		m.moveSelection(1)
		return m, nil
	case isTopKey(msg):
		if len(m.snapshot.Invocations) > 0 {
			m.selectedIndex = 0
			m.selectedInvocationID = m.snapshot.Invocations[0].InvocationID
			m.selectedRepoID = m.snapshot.Invocations[0].RepoID
		}
		return m, nil
	case isBottomKey(msg):
		if len(m.snapshot.Invocations) > 0 {
			m.selectedIndex = len(m.snapshot.Invocations) - 1
			m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
			m.selectedRepoID = m.snapshot.Invocations[m.selectedIndex].RepoID
		}
		return m, nil
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
		m.logsLoading = true
		m.logsError = ""
		m.logsScroll = 0
		return m, m.loadLogsCmd()
	case isEnterKey(msg):
		return m.startWorkspaceAction(actionAttach)
	case msg.Text == "o":
		return m.startWorkspaceAction(actionOpen)
	case msg.Text == "p":
		return m.startWorkspaceAction(actionPRSync)
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
	case msg.Text == "l":
		m.page = pageLogs
		m.backPage = pageHistory
		m.logsLoading = true
		m.logsError = ""
		m.logsScroll = 0
		return m, m.loadLogsCmd()
	case isEnterKey(msg):
		return m.startRestoreAction()
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
	default:
		return m, nil
	}
}

func (m *model) loadWorkspaceSnapshotCmd() tea.Cmd {
	ctx := m.ctx
	client := m.client
	return func() tea.Msg {
		snapshot, err := loadWorkspaceSnapshot(ctx, client)
		return snapshotLoadedMsg{snapshot: snapshot, err: err}
	}
}

func (m *model) loadHistoryCmd() tea.Cmd {
	ctx := m.ctx
	client := m.client
	invocationID := m.selectedInvocationID
	repoID := m.selectedRepoID
	return func() tea.Msg {
		turns, err := loadHistoryTurns(ctx, client, invocationID, repoID)
		return historyLoadedMsg{turns: turns, err: err}
	}
}

func (m *model) loadLogsCmd() tea.Cmd {
	ctx := m.ctx
	client := m.client
	invocationID := m.selectedInvocationID
	repoID := m.selectedRepoID
	return func() tea.Msg {
		content, err := loadInvocationLogs(ctx, client, invocationID, repoID)
		return logsLoadedMsg{content: content, err: err}
	}
}

func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return refreshTickMsg(t)
	})
}

func (m model) startWorkspaceAction(kind actionKind) (tea.Model, tea.Cmd) {
	if m.actionRunning {
		return m, nil
	}

	selected, ok := m.selectedInvocation()
	if !ok {
		m.lastActionError = true
		m.lastActionMessage = fmt.Sprintf("%s unavailable: no invocation selected", kind)
		return m, nil
	}

	var run func() (string, error)
	switch kind {
	case actionAttach:
		if m.attach == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.attach(m.ctx, selected.InvocationID, selected.RepoID)
		}
	case actionOpen:
		if m.open == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.open(m.ctx, selected.InvocationID, selected.RepoID)
		}
	case actionPRSync:
		if m.prSync == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.prSync(m.ctx, selected.WorktreeID, selected.RepoID)
		}
	default:
		m.lastActionError = true
		m.lastActionMessage = fmt.Sprintf("%s unavailable: unsupported workspace action", kind)
		return m, nil
	}
	if kind == actionPRSync && strings.TrimSpace(selected.WorktreeID) == "" {
		m.lastActionError = true
		m.lastActionMessage = formatActionError(
			kind,
			agencyerrors.NewWithDetails(
				agencyerrors.EInvalidArgument,
				"selected invocation is not associated with an integration worktree",
				map[string]string{
					"invocation_id": selected.InvocationID,
					"hint":          "refresh and retry; if this persists, inspect invocation metadata",
				},
			),
			selected.InvocationID,
			selected.WorktreeID,
			"",
		)
		return m, nil
	}

	m.actionRunning = true
	m.lastActionError = false
	m.lastActionMessage = fmt.Sprintf("%s in progress for %s", kind, actionTarget(kind, selected.InvocationID, selected.WorktreeID, ""))

	invocationID := selected.InvocationID
	worktreeID := selected.WorktreeID
	return m, func() tea.Msg {
		output, err := run()
		return actionResultMsg{
			kind:         kind,
			invocationID: invocationID,
			worktreeID:   worktreeID,
			output:       output,
			err:          err,
		}
	}
}

func (m model) startRestoreAction() (tea.Model, tea.Cmd) {
	if m.actionRunning {
		return m, nil
	}
	if m.restore == nil {
		m.lastActionError = true
		m.lastActionMessage = "restore unavailable: action is not configured"
		return m, nil
	}
	turn, ok := m.selectedTurn()
	if !ok {
		m.lastActionError = true
		m.lastActionMessage = "restore unavailable: no turn selected"
		return m, nil
	}
	if !turn.Restorable || turn.CheckpointID <= 0 {
		m.lastActionError = true
		m.lastActionMessage = "restore unavailable: selected turn does not have a restorable checkpoint"
		return m, nil
	}

	m.actionRunning = true
	m.lastActionError = false
	m.lastActionMessage = fmt.Sprintf("%s in progress for %s", actionRestore, actionTarget(actionRestore, m.selectedInvocationID, "", turn.EntryID))

	ctx := m.ctx
	invocationID := m.selectedInvocationID
	repoID := m.selectedRepoID
	turnID := turn.EntryID
	return m, func() tea.Msg {
		output, err := m.restore(ctx, invocationID, repoID, turnID)
		return actionResultMsg{
			kind:         actionRestore,
			invocationID: invocationID,
			turnID:       turnID,
			output:       output,
			err:          err,
		}
	}
}

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

func (m *model) reconcileSelection() {
	if len(m.snapshot.Invocations) == 0 {
		m.selectedIndex = 0
		m.selectedInvocationID = ""
		m.selectedRepoID = ""
		return
	}

	if m.selectedInvocationID != "" {
		for idx, inv := range m.snapshot.Invocations {
			if inv.InvocationID == m.selectedInvocationID {
				m.selectedIndex = idx
				m.selectedRepoID = inv.RepoID
				return
			}
		}
	}

	m.selectedIndex = clamp(m.selectedIndex, 0, len(m.snapshot.Invocations)-1)
	m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
	m.selectedRepoID = m.snapshot.Invocations[m.selectedIndex].RepoID
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
	visible := height - 5
	if visible < 5 {
		visible = 5
	}
	return visible
}

func formatActionError(kind actionKind, err error, invocationID, worktreeID, turnID string) string {
	target := actionTarget(kind, invocationID, worktreeID, turnID)
	code := agencyerrors.GetCode(err)
	if code == agencyerrors.ESessionEnded {
		hint := "session ended; use 'agency agent history logs' or 'agency agent open' to view"
		if ae, ok := agencyerrors.AsAgencyError(err); ok {
			if resolvedHint := strings.TrimSpace(ae.Details["hint"]); resolvedHint != "" {
				hint = resolvedHint
			}
		}
		return fmt.Sprintf("%s failed (%s) for %s: %s", kind, code, target, hint)
	}
	if code != "" {
		return fmt.Sprintf("%s failed (%s) for %s: %s", kind, code, target, err.Error())
	}
	return fmt.Sprintf("%s failed for %s: %s", kind, target, err.Error())
}

func actionTarget(kind actionKind, invocationID, worktreeID, turnID string) string {
	switch kind {
	case actionPRSync:
		if strings.TrimSpace(worktreeID) == "" {
			return fmt.Sprintf("invocation %s (worktree missing)", shortID(invocationID, 10))
		}
		return fmt.Sprintf("worktree %s (invocation %s)", worktreeID, shortID(invocationID, 10))
	case actionRestore:
		if strings.TrimSpace(turnID) != "" {
			return fmt.Sprintf("%s @ %s", shortID(invocationID, 10), turnID)
		}
	}

	shortInvocationID := shortID(invocationID, 10)
	if shortInvocationID == "" {
		return "selected invocation"
	}
	return shortInvocationID
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

func readinessCounts(invocations []daemon.InvocationDTO, checks map[string]daemon.InvocationCheckData) (ready int, blocked int, unknown int) {
	for _, inv := range invocations {
		check, ok := checks[inv.InvocationID]
		if !ok {
			unknown++
			continue
		}
		if check.Ready || check.Readiness == "ready" {
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

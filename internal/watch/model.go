package watch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

const defaultRefreshInterval = 2 * time.Second

const minPanelWidth = 40

type loader interface {
	Load(ctx context.Context) (Snapshot, error)
}

// ActionDispatcher executes delegated watch actions for a selected invocation.
// Implementations should call canonical command contracts rather than
// reimplementing policy in the watch runtime.
type ActionDispatcher interface {
	Enter(ctx context.Context, invocationID, repoID string) error
	Open(ctx context.Context, invocationID, repoID string) error
	PRSync(ctx context.Context, worktreeID, repoID string) error
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Top     key.Binding
	Bottom  key.Binding
	Enter   key.Binding
	Open    key.Binding
	PRSync  key.Binding
	Refresh key.Binding
	Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Open, k.PRSync, k.Refresh, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Enter, k.Open, k.PRSync},
		{k.Refresh, k.Quit},
	}
}

var defaultKeyMap = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("up/k", "move selection up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("down/j", "move selection down"),
	),
	Top: key.NewBinding(
		key.WithKeys("home", "g"),
		key.WithHelp("home/g", "jump to top"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("end", "G"),
		key.WithHelp("end/G", "jump to bottom"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "enter invocation"),
	),
	Open: key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "open sandbox"),
	),
	PRSync: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "pr sync (worktree)"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh now"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "esc", "ctrl+c"),
		key.WithHelp("q/esc", "quit"),
	),
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

type refreshRequestMsg struct{}

type snapshotLoadedMsg struct {
	snapshot Snapshot
	err      error
}

type actionKind string

const (
	actionEnter  actionKind = "enter"
	actionOpen   actionKind = "open"
	actionPRSync actionKind = "pr sync"
)

func (k actionKind) String() string {
	return string(k)
}

type actionResultMsg struct {
	kind         actionKind
	invocationID string
	worktreeID   string
	err          error
}

type model struct {
	ctx      context.Context
	loader   loader
	actions  ActionDispatcher
	interval time.Duration
	keys     keyMap
	help     help.Model

	width  int
	height int

	snapshot             Snapshot
	selectedIndex        int
	selectedInvocationID string
	refreshing           bool
	lastError            string
	actionRunning        bool
	lastActionMessage    string
	lastActionError      bool
}

func newModel(ctx context.Context, snapshotLoader loader, interval time.Duration, actions ActionDispatcher) model {
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = defaultRefreshInterval
	}

	h := help.New()
	h.ShortSeparator = " • "

	return model{
		ctx:      ctx,
		loader:   snapshotLoader,
		actions:  actions,
		interval: interval,
		keys:     defaultKeyMap,
		help:     h,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(scheduleRefreshCmd(), tickCmd(m.interval))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		return m, nil

	case refreshTickMsg:
		return m, tea.Batch(tickCmd(m.interval), scheduleRefreshCmd())

	case refreshRequestMsg:
		if m.refreshing {
			return m, nil
		}
		m.refreshing = true
		return m, m.loadSnapshotCmd()

	case snapshotLoadedMsg:
		m.refreshing = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			return m, nil
		}
		m.snapshot = msg.snapshot
		m.lastError = ""
		m.reconcileSelection()
		return m, nil

	case actionResultMsg:
		m.actionRunning = false
		m.lastActionError = msg.err != nil
		if msg.err != nil {
			m.lastActionMessage = formatActionError(msg.kind, msg.err, msg.invocationID, msg.worktreeID)
		} else {
			m.lastActionMessage = fmt.Sprintf("%s complete for %s", msg.kind, actionTarget(msg.kind, msg.invocationID, msg.worktreeID))
		}
		// Refresh after each action outcome so readiness/details remain actionable.
		return m, scheduleRefreshCmd()

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Refresh):
			return m, scheduleRefreshCmd()
		case key.Matches(msg, m.keys.Up):
			m.moveSelection(-1)
			return m, nil
		case key.Matches(msg, m.keys.Down):
			m.moveSelection(1)
			return m, nil
		case key.Matches(msg, m.keys.Top):
			if len(m.snapshot.Invocations) > 0 {
				m.selectedIndex = 0
				m.selectedInvocationID = m.snapshot.Invocations[0].InvocationID
			}
			return m, nil
		case key.Matches(msg, m.keys.Bottom):
			if len(m.snapshot.Invocations) > 0 {
				m.selectedIndex = len(m.snapshot.Invocations) - 1
				m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
			}
			return m, nil
		case key.Matches(msg, m.keys.Enter):
			return m.triggerAction(actionEnter)
		case key.Matches(msg, m.keys.Open):
			return m.triggerAction(actionOpen)
		case key.Matches(msg, m.keys.PRSync):
			return m.triggerAction(actionPRSync)
		default:
			return m, nil
		}
	}

	return m, nil
}

func (m model) View() tea.View {
	content := m.renderWorkspace()
	v := tea.NewView(content)
	v.AltScreen = true
	v.Cursor = nil
	return v
}

func (m *model) loadSnapshotCmd() tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.loader.Load(m.ctx)
		return snapshotLoadedMsg{snapshot: snapshot, err: err}
	}
}

func scheduleRefreshCmd() tea.Cmd {
	return func() tea.Msg {
		return refreshRequestMsg{}
	}
}

func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return refreshTickMsg(t)
	})
}

func (m model) triggerAction(kind actionKind) (tea.Model, tea.Cmd) {
	if m.actionRunning {
		return m, nil
	}
	if m.actions == nil {
		m.lastActionError = true
		m.lastActionMessage = fmt.Sprintf("%s unavailable: watch actions are not configured", kind)
		return m, nil
	}

	selected, ok := m.selectedInvocation()
	if !ok {
		m.lastActionError = true
		m.lastActionMessage = fmt.Sprintf("%s unavailable: no invocation selected", kind)
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
		)
		return m, nil
	}

	m.actionRunning = true
	m.lastActionError = false
	m.lastActionMessage = fmt.Sprintf("%s in progress for %s", kind, actionTarget(kind, selected.InvocationID, selected.WorktreeID))
	return m, m.runActionCmd(kind, selected)
}

func (m model) runActionCmd(kind actionKind, selected daemon.InvocationDTO) tea.Cmd {
	dispatcher := m.actions
	ctx := m.ctx

	return func() tea.Msg {
		var err error
		switch kind {
		case actionEnter:
			err = dispatcher.Enter(ctx, selected.InvocationID, selected.RepoID)
		case actionOpen:
			err = dispatcher.Open(ctx, selected.InvocationID, selected.RepoID)
		case actionPRSync:
			err = dispatcher.PRSync(ctx, selected.WorktreeID, selected.RepoID)
		default:
			err = agencyerrors.New(agencyerrors.EInternal, "unknown watch action")
		}
		return actionResultMsg{
			kind:         kind,
			invocationID: selected.InvocationID,
			worktreeID:   selected.WorktreeID,
			err:          err,
		}
	}
}

func formatActionError(kind actionKind, err error, invocationID, worktreeID string) string {
	target := actionTarget(kind, invocationID, worktreeID)
	code := agencyerrors.GetCode(err)
	if code == agencyerrors.ESessionEnded {
		hint := "session ended; use 'agency agent logs' or 'agency agent open' to view"
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

func actionTarget(kind actionKind, invocationID, worktreeID string) string {
	if kind == actionPRSync {
		trimmedWorktreeID := strings.TrimSpace(worktreeID)
		shortInvocationID := shortID(invocationID, 10)
		if trimmedWorktreeID == "" {
			if shortInvocationID != "" {
				return fmt.Sprintf("invocation %s (worktree missing)", shortInvocationID)
			}
			return "selected invocation (worktree missing)"
		}
		if shortInvocationID != "" {
			return fmt.Sprintf("worktree %s (invocation %s)", trimmedWorktreeID, shortInvocationID)
		}
		return fmt.Sprintf("worktree %s", trimmedWorktreeID)
	}

	shortInvocationID := shortID(invocationID, 10)
	if shortInvocationID == "" {
		return "selected invocation"
	}
	return shortInvocationID
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

		row := fmt.Sprintf(
			"%s %-8s %-10s %-9s %-14s %s",
			indicator,
			readiness,
			shortID(inv.InvocationID, 10),
			inv.Runner,
			truncateWithEllipsis(displayStatus, 14),
			truncateWithEllipsis(worktreeName, max(1, width-55)),
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
	if len(value) <= maxWidth {
		return value
	}
	if maxWidth <= 3 {
		return value[:maxWidth]
	}
	return value[:maxWidth-3] + "..."
}

func shortID(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
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

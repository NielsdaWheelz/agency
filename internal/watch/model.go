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

type watchMode string

const (
	modeWorkspace watchMode = "workspace"
	modeHistory   watchMode = "history"
)

type loader interface {
	Load(ctx context.Context) (Snapshot, error)
}

// ActionDispatcher executes delegated watch actions for a selected invocation.
// Implementations should call canonical command contracts rather than
// reimplementing policy in the watch runtime.
type ActionDispatcher interface {
	Attach(ctx context.Context, invocationID, repoID string) (string, error)
	Open(ctx context.Context, invocationID, repoID string) (string, error)
	PRSync(ctx context.Context, worktreeID, repoID string) (string, error)
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Top     key.Binding
	Bottom  key.Binding
	Attach  key.Binding
	Open    key.Binding
	PRSync  key.Binding
	Refresh key.Binding
	Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Attach, k.Open, k.PRSync, k.Refresh, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Attach, k.Open, k.PRSync},
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
	Attach: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "attach invocation"),
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
	actionAttach actionKind = "attach"
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
	output       string
	err          error
}

type model struct {
	mode     watchMode
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
	historyTurns         []daemon.Turn
	historySelectedIndex int
	historyNoColor       bool
	historyKeys          historyKeyMap
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
		mode:     modeWorkspace,
		ctx:      ctx,
		loader:   snapshotLoader,
		actions:  actions,
		interval: interval,
		keys:     defaultKeyMap,
		help:     h,
	}
}

func (m model) Init() tea.Cmd {
	if m.mode == modeHistory {
		return nil
	}
	return tea.Batch(scheduleRefreshCmd(), tickCmd(m.interval))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.mode == modeWorkspace {
			m.help.SetWidth(msg.Width)
		}
		return m, nil

	case refreshTickMsg:
		if m.mode == modeHistory {
			return m, nil
		}
		return m, tea.Batch(tickCmd(m.interval), scheduleRefreshCmd())

	case refreshRequestMsg:
		if m.mode == modeHistory {
			return m, nil
		}
		if m.refreshing {
			return m, nil
		}
		m.refreshing = true
		return m, m.loadSnapshotCmd()

	case snapshotLoadedMsg:
		if m.mode == modeHistory {
			return m, nil
		}
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
		if m.mode == modeHistory {
			return m, nil
		}
		m.actionRunning = false
		m.lastActionError = msg.err != nil
		if msg.err != nil {
			m.lastActionMessage = formatActionError(msg.kind, msg.err, msg.invocationID, msg.worktreeID)
			if output := strings.TrimSpace(msg.output); output != "" {
				m.lastActionMessage += " | " + output
			}
		} else {
			if output := strings.TrimSpace(msg.output); output != "" {
				m.lastActionMessage = output
			} else {
				m.lastActionMessage = fmt.Sprintf("%s complete for %s", msg.kind, actionTarget(msg.kind, msg.invocationID, msg.worktreeID))
			}
		}
		// Refresh after each action outcome so readiness/details remain actionable.
		return m, scheduleRefreshCmd()

	case tea.KeyPressMsg:
		if m.mode == modeHistory {
			return m.updateHistory(msg)
		}
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
		case key.Matches(msg, m.keys.Attach):
			return m.triggerAction(actionAttach)
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
	if m.mode == modeHistory {
		content = m.renderHistory()
	}
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
		var output string
		var err error
		switch kind {
		case actionAttach:
			output, err = dispatcher.Attach(ctx, selected.InvocationID, selected.RepoID)
		case actionOpen:
			output, err = dispatcher.Open(ctx, selected.InvocationID, selected.RepoID)
		case actionPRSync:
			output, err = dispatcher.PRSync(ctx, selected.WorktreeID, selected.RepoID)
		default:
			err = agencyerrors.New(agencyerrors.EInternal, "unknown watch action")
		}
		return actionResultMsg{
			kind:         kind,
			invocationID: selected.InvocationID,
			worktreeID:   selected.WorktreeID,
			output:       output,
			err:          err,
		}
	}
}

func formatActionError(kind actionKind, err error, invocationID, worktreeID string) string {
	target := actionTarget(kind, invocationID, worktreeID)
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

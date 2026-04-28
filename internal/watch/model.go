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
	pageWorkspace  watchPage = "workspace"
	pageHistory    watchPage = "history"
	pageReview     watchPage = "review"
	pageTranscript watchPage = "transcript"
	pageLogs       watchPage = "logs"
)

type workspacePane string

const (
	workspacePaneRepos     workspacePane = "repos"
	workspacePaneWorktrees workspacePane = "worktrees"
	workspacePaneAgents    workspacePane = "agents"
)

type actionKind string

const (
	actionAttach   actionKind = "attach"
	actionOpen     actionKind = "open"
	actionStop     actionKind = "stop"
	actionKill     actionKind = "kill"
	actionLand     actionKind = "land"
	actionDiscard  actionKind = "discard"
	actionFollowup actionKind = "followup"
	actionRecreate actionKind = "recreate"
	actionPRSync   actionKind = "pr sync"
	actionPRMerge  actionKind = "pr merge"
	actionRebase   actionKind = "rebase"
	actionRestore  actionKind = "restore"
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
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	actionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	dimStyle     = lipgloss.NewStyle().Faint(true)
)

type refreshTickMsg time.Time

type snapshotLoadedMsg struct {
	repoID          string
	worktreeID      string
	worktreeState   string
	invocationState string
	snapshot        Snapshot
	err             error
}

type historyLoadedMsg struct {
	turns []daemon.Turn
	err   error
}

type reviewLoadedMsg struct {
	invocationID string
	repoID       string
	turnID       string
	diff         daemon.InvocationDiffData
	check        daemon.InvocationCheckData
	files        []reviewFile
	err          error
}

type logsLoadedMsg struct {
	kind    string
	content string
	err     error
}

type transcriptLoadedMsg struct {
	content string
	err     error
}

type sessionLoadedMsg struct {
	invocationID string
	repoID       string
	session      daemon.InvocationSessionData
	err          error
}

type actionResultMsg struct {
	kind         actionKind
	invocationID string
	worktreeID   string
	turnID       string
	prompt       string
	output       string
	err          error
}

type model struct {
	ctx           context.Context
	client        *daemonclient.Client
	interval      time.Duration
	sessionLoader InvocationSessionLoader
	page          watchPage
	backPage      watchPage

	width  int
	height int

	snapshot              Snapshot
	workspaceFocus        workspacePane
	activeRepoID          string
	activeWorktreeID      string
	worktreeStateFilter   string
	invocationStateFilter string
	workspaceLayoutMode   string
	workspaceFilterInput  bool
	repoFilter            string
	worktreeFilter        string
	agentFilter           string
	selectedRepoIndex     int
	selectedWorktreeIndex int
	selectedIndex         int
	selectedInvocationID  string
	selectedRepoID        string

	workspaceLoading bool
	workspaceError   string

	historyTurns           []daemon.Turn
	historySelectedIndex   int
	historySelectedEntryID string
	historyLoading         bool
	historyError           string

	reviewTurnID        string
	reviewDiff          daemon.InvocationDiffData
	reviewCheck         daemon.InvocationCheckData
	reviewFiles         []reviewFile
	reviewSelectedIndex int
	reviewSelectedKey   string
	reviewScroll        int
	reviewLoading       bool
	reviewError         string
	reviewFilesFocus    bool

	transcriptContent string
	transcriptScroll  int
	transcriptLoading bool
	transcriptError   string

	logsKind    string
	logsContent string
	logsScroll  int
	logsLoading bool
	logsError   string

	selectedSession           daemon.InvocationSessionData
	selectedSessionLoading    bool
	selectedSessionError      string
	selectedSessionInvocation string
	selectedSessionRepo       string

	actionRunning     bool
	lastActionMessage string
	lastActionError   bool
	actionMenuOpen    bool
	confirmAction     actionKind
	followupInput     bool
	followupText      string

	attachInvocationID  string
	attachRequestedRepo string

	open     func(context.Context, string, string) (string, error)
	stop     func(context.Context, string, string) (string, error)
	kill     func(context.Context, string, string) (string, error)
	land     func(context.Context, string, string) (string, error)
	discard  func(context.Context, string, string) (string, error)
	recreate func(context.Context, string, string) (string, error)
	followup func(context.Context, string, string, string) (string, error)
	prSync   func(context.Context, string, string) (string, error)
	prMerge  func(context.Context, string, string) (string, error)
	rebase   func(context.Context, string, string) (string, error)
	restore  func(context.Context, string, string, string) (string, error)
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

	sessionLoader := opts.SessionLoader
	if sessionLoader == nil && client != nil {
		sessionLoader = func(ctx context.Context, invocationID, repoID string) (daemon.InvocationSessionData, error) {
			result, err := client.GetInvocationSession(ctx, invocationID, repoID)
			if err != nil {
				return daemon.InvocationSessionData{}, err
			}
			return result.Data, nil
		}
	}

	return model{
		ctx:                   ctx,
		client:                client,
		interval:              interval,
		sessionLoader:         sessionLoader,
		page:                  page,
		backPage:              pageWorkspace,
		workspaceFocus:        workspacePaneAgents,
		worktreeStateFilter:   "present",
		invocationStateFilter: "unresolved",
		selectedInvocationID:  strings.TrimSpace(opts.InvocationID),
		selectedRepoID:        strings.TrimSpace(opts.RepoID),
		open:                  opts.Open,
		stop:                  opts.Stop,
		kill:                  opts.Kill,
		land:                  opts.Land,
		discard:               opts.Discard,
		recreate:              opts.Recreate,
		followup:              opts.Followup,
		prSync:                opts.PRSync,
		prMerge:               opts.PRMerge,
		rebase:                opts.Rebase,
		restore:               opts.Restore,
	}
}

func (m model) Init() tea.Cmd {
	switch m.page {
	case pageWorkspace:
		m.workspaceLoading = true
		return tea.Batch(m.loadWorkspaceSnapshotCmd(), tickCmd(m.interval))
	case pageHistory:
		if len(m.historyTurns) > 0 {
			return m.loadSelectedSessionForSelectionCmd()
		}
		m.historyLoading = true
		return batchCmds(m.loadHistoryCmd(), m.loadSelectedSessionForSelectionCmd())
	case pageTranscript:
		if strings.TrimSpace(m.transcriptContent) != "" {
			return nil
		}
		m.transcriptLoading = true
		return m.loadTranscriptCmd()
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
		msgWorktreeState := strings.TrimSpace(msg.worktreeState)
		if msgWorktreeState == "" {
			msgWorktreeState = strings.TrimSpace(m.worktreeStateFilter)
		}
		msgInvocationState := strings.TrimSpace(msg.invocationState)
		if msgInvocationState == "" {
			msgInvocationState = strings.TrimSpace(m.invocationStateFilter)
		}
		if strings.TrimSpace(msg.repoID) != strings.TrimSpace(m.activeRepoID) ||
			strings.TrimSpace(msg.worktreeID) != strings.TrimSpace(m.activeWorktreeID) ||
			msgWorktreeState != strings.TrimSpace(m.worktreeStateFilter) ||
			msgInvocationState != strings.TrimSpace(m.invocationStateFilter) {
			return m, nil
		}
		m.workspaceLoading = false
		if msg.err != nil {
			m.workspaceError = msg.err.Error()
			return m, nil
		}
		m.snapshot = msg.snapshot
		m.workspaceError = ""
		if m.reconcileSelection() {
			m.workspaceLoading = true
			return m, m.loadWorkspaceSnapshotCmd()
		}
		return m, m.refreshSelectedSessionCmd()

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

	case reviewLoadedMsg:
		if strings.TrimSpace(m.selectedInvocationID) != strings.TrimSpace(msg.invocationID) ||
			strings.TrimSpace(m.selectedRepoID) != strings.TrimSpace(msg.repoID) ||
			strings.TrimSpace(m.reviewTurnID) != strings.TrimSpace(msg.turnID) {
			return m, nil
		}
		m.reviewLoading = false
		if msg.err != nil {
			m.reviewError = msg.err.Error()
			return m, nil
		}
		m.reviewDiff = msg.diff
		m.reviewCheck = msg.check
		m.reviewFiles = msg.files
		m.reviewError = ""
		m.reconcileReviewSelection()
		m.reviewScroll = clamp(m.reviewScroll, 0, m.maxReviewScroll())
		return m, nil

	case transcriptLoadedMsg:
		m.transcriptLoading = false
		if msg.err != nil {
			m.transcriptError = msg.err.Error()
			return m, nil
		}
		m.transcriptContent = msg.content
		m.transcriptError = ""
		m.transcriptScroll = clamp(m.transcriptScroll, 0, m.maxTranscriptScroll())
		return m, nil

	case logsLoadedMsg:
		m.logsLoading = false
		if msg.err != nil {
			m.logsError = msg.err.Error()
			return m, nil
		}
		m.logsKind = msg.kind
		m.logsContent = msg.content
		m.logsError = ""
		m.logsScroll = clamp(m.logsScroll, 0, m.maxLogsScroll())
		return m, nil

	case sessionLoadedMsg:
		if strings.TrimSpace(m.selectedInvocationID) != strings.TrimSpace(msg.invocationID) ||
			strings.TrimSpace(m.selectedRepoID) != strings.TrimSpace(msg.repoID) {
			return m, nil
		}
		m.selectedSessionLoading = false
		m.selectedSessionInvocation = msg.invocationID
		m.selectedSessionRepo = msg.repoID
		if msg.err != nil {
			m.selectedSession = daemon.InvocationSessionData{}
			m.selectedSessionError = msg.err.Error()
			return m, nil
		}
		m.selectedSession = msg.session
		m.selectedSessionError = ""
		return m, nil

	case actionResultMsg:
		m.actionRunning = false
		m.actionMenuOpen = false
		m.confirmAction = ""
		m.followupInput = false
		m.followupText = ""
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

		cmds := make([]tea.Cmd, 0, 2)
		if msg.kind == actionRestore && m.page == pageHistory && !m.historyLoading {
			m.historyLoading = true
			cmds = append(cmds, m.loadHistoryCmd())
		}
		if m.page == pageReview && !m.reviewLoading {
			m.reviewLoading = true
			cmds = append(cmds, m.loadReviewCmd())
		}
		if !m.workspaceLoading {
			m.workspaceLoading = true
			cmds = append(cmds, m.loadWorkspaceSnapshotCmd())
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.followupInput {
			return m.updateFollowupKey(msg)
		}
		if m.confirmAction != "" {
			return m.updateConfirmKey(msg)
		}
		if m.actionMenuOpen {
			return m.updateActionMenuKey(msg)
		}
		switch m.page {
		case pageWorkspace:
			return m.updateWorkspaceKey(msg)
		case pageHistory:
			return m.updateHistoryKey(msg)
		case pageReview:
			return m.updateReviewKey(msg)
		case pageTranscript:
			return m.updateTranscriptKey(msg)
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
	case pageReview:
		content = m.renderReview()
	case pageTranscript:
		content = m.renderTranscript()
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

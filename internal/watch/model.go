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
	pageWorkspace  watchPage = "workspace"
	pageHistory    watchPage = "history"
	pageReview     watchPage = "review"
	pageTranscript watchPage = "transcript"
	pageLogs       watchPage = "logs"
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
	snapshot Snapshot
	err      error
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
	session      InvocationSession
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

	snapshot             Snapshot
	selectedIndex        int
	selectedInvocationID string
	selectedRepoID       string
	selectedMode         string

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
	reviewReviewed      map[string]bool

	transcriptContent string
	transcriptScroll  int
	transcriptLoading bool
	transcriptError   string

	logsKind    string
	logsContent string
	logsScroll  int
	logsLoading bool
	logsError   string

	selectedSession           InvocationSession
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

	attachRequested     bool
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
		sessionLoader = func(ctx context.Context, invocationID, repoID string) (InvocationSession, error) {
			result, err := client.GetInvocationSession(ctx, invocationID, repoID)
			if err != nil {
				return InvocationSession{}, err
			}
			return invocationSessionFromDTO(result.Data), nil
		}
	}

	return model{
		ctx:                  ctx,
		client:               client,
		interval:             interval,
		sessionLoader:        sessionLoader,
		page:                 page,
		backPage:             pageWorkspace,
		selectedInvocationID: strings.TrimSpace(opts.InvocationID),
		selectedRepoID:       strings.TrimSpace(opts.RepoID),
		open:                 opts.Open,
		stop:                 opts.Stop,
		kill:                 opts.Kill,
		land:                 opts.Land,
		discard:              opts.Discard,
		recreate:             opts.Recreate,
		followup:             opts.Followup,
		prSync:               opts.PRSync,
		prMerge:              opts.PRMerge,
		rebase:               opts.Rebase,
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
		m.workspaceLoading = false
		if msg.err != nil {
			m.workspaceError = msg.err.Error()
			return m, nil
		}
		m.snapshot = msg.snapshot
		m.workspaceError = ""
		m.reconcileSelection()
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
		if m.reviewReviewed == nil {
			m.reviewReviewed = make(map[string]bool)
		}
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
			m.selectedSession = InvocationSession{}
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
			m.selectedMode = m.snapshot.Invocations[0].Mode
		}
		return m, m.loadSelectedSessionForSelectionCmd()
	case isBottomKey(msg):
		if len(m.snapshot.Invocations) > 0 {
			m.selectedIndex = len(m.snapshot.Invocations) - 1
			m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
			m.selectedRepoID = m.snapshot.Invocations[m.selectedIndex].RepoID
			m.selectedMode = m.snapshot.Invocations[m.selectedIndex].Mode
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
	case msg.Text == " ":
		m.toggleReviewed()
		return m, nil
	case msg.Text == "n":
		m.moveReviewSelectionToNextUnreviewed(1)
		return m, nil
	case msg.Text == "N":
		m.moveReviewSelectionToNextUnreviewed(-1)
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

func (m *model) loadReviewCmd() tea.Cmd {
	ctx := m.ctx
	client := m.client
	invocationID := m.selectedInvocationID
	repoID := m.selectedRepoID
	turnID := m.reviewTurnID
	return func() tea.Msg {
		diffResult, err := client.GetInvocationDiff(ctx, invocationID, repoID, daemonclient.GetInvocationDiffOpts{
			IncludePatch:       true,
			MaxPatchBytes:      5 * 1024 * 1024,
			IncludeUncommitted: true,
			TurnID:             turnID,
		})
		if err != nil {
			return reviewLoadedMsg{
				invocationID: invocationID,
				repoID:       repoID,
				turnID:       turnID,
				err:          err,
			}
		}
		checkResult, err := client.GetInvocationCheck(ctx, invocationID, repoID)
		if err != nil {
			return reviewLoadedMsg{
				invocationID: invocationID,
				repoID:       repoID,
				turnID:       turnID,
				err:          err,
			}
		}
		return reviewLoadedMsg{
			invocationID: invocationID,
			repoID:       repoID,
			turnID:       turnID,
			diff:         diffResult.Data,
			check:        checkResult.Data,
			files:        reviewFilesFromDiff(diffResult.Data),
		}
	}
}

func (m *model) loadTranscriptCmd() tea.Cmd {
	ctx := m.ctx
	client := m.client
	invocationID := m.selectedInvocationID
	repoID := m.selectedRepoID
	return func() tea.Msg {
		content, err := loadInvocationTranscript(ctx, client, invocationID, repoID)
		return transcriptLoadedMsg{content: content, err: err}
	}
}

func (m *model) loadLogsCmd() tea.Cmd {
	ctx := m.ctx
	client := m.client
	invocationID := m.selectedInvocationID
	repoID := m.selectedRepoID
	kind := m.currentLogsKind()
	return func() tea.Msg {
		content, err := loadInvocationLogs(ctx, client, invocationID, repoID, kind)
		return logsLoadedMsg{kind: kind, content: content, err: err}
	}
}

func (m *model) loadSelectedSessionCmd(invocationID, repoID string) tea.Cmd {
	ctx := m.ctx
	loader := m.sessionLoader
	if loader == nil || strings.TrimSpace(invocationID) == "" || strings.TrimSpace(repoID) == "" {
		return nil
	}
	return func() tea.Msg {
		session, err := loader(ctx, invocationID, repoID)
		return sessionLoadedMsg{
			invocationID: invocationID,
			repoID:       repoID,
			session:      session,
			err:          err,
		}
	}
}

func (m *model) clearSelectedSession() {
	m.selectedSession = InvocationSession{}
	m.selectedSessionLoading = false
	m.selectedSessionError = ""
	m.selectedSessionInvocation = ""
	m.selectedSessionRepo = ""
}

func (m *model) beginSelectedSessionLoad(invocationID, repoID string) tea.Cmd {
	m.selectedSession = InvocationSession{}
	m.selectedSessionError = ""
	m.selectedSessionLoading = true
	m.selectedSessionInvocation = invocationID
	m.selectedSessionRepo = repoID
	return m.loadSelectedSessionCmd(invocationID, repoID)
}

func (m *model) loadSelectedSessionForSelectionCmd() tea.Cmd {
	selected, ok := m.selectedInvocation()
	if !ok || m.sessionLoader == nil || strings.TrimSpace(selected.Mode) != "headed" {
		m.clearSelectedSession()
		return nil
	}
	if m.selectedSessionInvocation == selected.InvocationID &&
		m.selectedSessionRepo == selected.RepoID &&
		(m.selectedSessionLoading || m.selectedSession.Status != "" || m.selectedSessionError != "") {
		return nil
	}
	return m.beginSelectedSessionLoad(selected.InvocationID, selected.RepoID)
}

func (m *model) refreshSelectedSessionCmd() tea.Cmd {
	selected, ok := m.selectedInvocation()
	if !ok || strings.TrimSpace(selected.Mode) != "headed" {
		m.clearSelectedSession()
		return nil
	}
	return m.beginSelectedSessionLoad(selected.InvocationID, selected.RepoID)
}

func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return refreshTickMsg(t)
	})
}

func batchCmds(cmds ...tea.Cmd) tea.Cmd {
	filtered := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd != nil {
			filtered = append(filtered, cmd)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return tea.Batch(filtered...)
}

func (m model) openActionMenu() (tea.Model, tea.Cmd) {
	if _, ok := m.selectedInvocation(); !ok {
		m.lastActionError = true
		m.lastActionMessage = "actions unavailable: no invocation selected"
		return m, nil
	}
	m.actionMenuOpen = true
	m.confirmAction = ""
	m.followupInput = false
	m.followupText = ""
	return m, nil
}

func (m model) updateActionMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEsc || msg.Text == "x":
		m.actionMenuOpen = false
		return m, nil
	case msg.Text == "q":
		return m, tea.Quit
	case msg.Text == "a":
		return m.startInvocationAction(actionAttach)
	case msg.Text == "o":
		return m.startInvocationAction(actionOpen)
	case msg.Text == "s":
		return m.startInvocationAction(actionStop)
	case msg.Text == "k":
		return m.startInvocationAction(actionKill)
	case msg.Text == "n":
		return m.startInvocationAction(actionLand)
	case msg.Text == "d":
		return m.startInvocationAction(actionDiscard)
	case msg.Text == "f":
		return m.startInvocationAction(actionFollowup)
	case msg.Text == "c":
		return m.startInvocationAction(actionRecreate)
	case msg.Text == "p":
		return m.startInvocationAction(actionPRSync)
	case msg.Text == "m":
		return m.startInvocationAction(actionPRMerge)
	case msg.Text == "b":
		return m.startInvocationAction(actionRebase)
	default:
		return m, nil
	}
}

func (m model) updateConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEsc:
		m.confirmAction = ""
		return m, nil
	case msg.Text == "y":
		kind := m.confirmAction
		m.confirmAction = ""
		return m.executeInvocationAction(kind, "")
	default:
		return m, nil
	}
}

func (m model) updateFollowupKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEsc:
		m.followupInput = false
		m.followupText = ""
		return m, nil
	case isEnterKey(msg):
		prompt := strings.TrimSpace(m.followupText)
		if prompt == "" {
			m.lastActionError = true
			m.lastActionMessage = "followup unavailable: prompt is empty"
			return m, nil
		}
		return m.executeInvocationAction(actionFollowup, prompt)
	case msg.Code == tea.KeyBackspace || msg.Code == tea.KeyDelete:
		runes := []rune(m.followupText)
		if len(runes) > 0 {
			m.followupText = string(runes[:len(runes)-1])
		}
		return m, nil
	case msg.Text != "":
		m.followupText += msg.Text
		return m, nil
	default:
		return m, nil
	}
}

func (m model) startInvocationAction(kind actionKind) (tea.Model, tea.Cmd) {
	if kind == actionFollowup {
		if !m.canStartAction(kind) {
			m.lastActionError = true
			m.lastActionMessage = "followup unavailable for the selected invocation"
			m.actionMenuOpen = false
			return m, nil
		}
		m.actionMenuOpen = false
		m.confirmAction = ""
		m.followupInput = true
		m.followupText = ""
		return m, nil
	}
	if actionNeedsConfirm(kind) {
		m.actionMenuOpen = false
		m.confirmAction = kind
		return m, nil
	}
	return m.executeInvocationAction(kind, "")
}

func (m model) executeInvocationAction(kind actionKind, prompt string) (tea.Model, tea.Cmd) {
	if m.actionRunning {
		return m, nil
	}

	selected, ok := m.selectedInvocation()
	if !ok && kind != actionAttach {
		m.lastActionError = true
		m.lastActionMessage = fmt.Sprintf("%s unavailable: no invocation selected", kind)
		return m, nil
	}

	var run func() (string, error)
	switch kind {
	case actionAttach:
		invocationID := strings.TrimSpace(m.selectedInvocationID)
		repoID := strings.TrimSpace(m.selectedRepoID)
		mode := strings.TrimSpace(m.selectedMode)
		if ok {
			invocationID = selected.InvocationID
			repoID = selected.RepoID
			mode = firstNonEmpty(selected.Mode, mode)
		}
		if invocationID == "" || repoID == "" {
			m.lastActionError = true
			m.lastActionMessage = "attach unavailable: no invocation selected"
			return m, nil
		}
		if mode != "headed" {
			m.lastActionError = true
			m.lastActionMessage = formatActionError(
				kind,
				agencyerrors.NewWithDetails(
					agencyerrors.EInvocationInvalidMode,
					"invocation is headless; attach is only supported for headed invocations",
					map[string]string{
						"invocation_id": invocationID,
						"mode":          mode,
						"hint":          "use history, transcript, or logs to inspect headless invocations",
					},
				),
				invocationID,
				"",
				"",
			)
			return m, nil
		}
		if m.selectedSessionLoading {
			m.lastActionError = true
			m.lastActionMessage = "attach unavailable: session facts are still loading"
			return m, nil
		}
		if strings.TrimSpace(m.selectedSessionError) != "" {
			m.lastActionError = true
			m.lastActionMessage = "attach unavailable: " + m.selectedSessionError
			return m, nil
		}
		if !m.selectedSession.IsLive() {
			m.lastActionError = true
			m.lastActionMessage = formatActionError(
				kind,
				agencyerrors.NewWithDetails(
					agencyerrors.ESessionEnded,
					"tmux session not found",
					map[string]string{
						"invocation_id": invocationID,
						"session_name":  strings.TrimSpace(m.selectedSession.TmuxSession),
						"hint":          "session ended; use recreate, history, transcript, logs, or open to inspect the invocation",
					},
				),
				invocationID,
				"",
				"",
			)
			return m, nil
		}
		m.attachRequested = true
		m.attachInvocationID = invocationID
		m.attachRequestedRepo = repoID
		m.actionMenuOpen = false
		m.confirmAction = ""
		m.followupInput = false
		m.followupText = ""
		return m, tea.Quit
	case actionOpen:
		if m.open == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.open(m.ctx, selected.InvocationID, selected.RepoID)
		}
	case actionStop:
		if m.stop == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.stop(m.ctx, selected.InvocationID, selected.RepoID)
		}
	case actionKill:
		if m.kill == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.kill(m.ctx, selected.InvocationID, selected.RepoID)
		}
	case actionLand:
		if m.land == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.land(m.ctx, selected.InvocationID, selected.RepoID)
		}
	case actionDiscard:
		if m.discard == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.discard(m.ctx, selected.InvocationID, selected.RepoID)
		}
	case actionFollowup:
		if m.followup == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		if strings.TrimSpace(prompt) == "" {
			m.lastActionError = true
			m.lastActionMessage = "followup unavailable: prompt is empty"
			return m, nil
		}
		run = func() (string, error) {
			return m.followup(m.ctx, selected.InvocationID, selected.RepoID, prompt)
		}
	case actionRecreate:
		if m.recreate == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.recreate(m.ctx, selected.InvocationID, selected.RepoID)
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
	case actionPRMerge:
		if m.prMerge == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.prMerge(m.ctx, selected.WorktreeID, selected.RepoID)
		}
	case actionRebase:
		if m.rebase == nil {
			m.lastActionError = true
			m.lastActionMessage = fmt.Sprintf("%s unavailable: action is not configured", kind)
			return m, nil
		}
		run = func() (string, error) {
			return m.rebase(m.ctx, selected.WorktreeID, selected.RepoID)
		}
	default:
		m.lastActionError = true
		m.lastActionMessage = fmt.Sprintf("%s unavailable: unsupported action", kind)
		return m, nil
	}
	if (kind == actionPRSync || kind == actionPRMerge || kind == actionRebase) && strings.TrimSpace(selected.WorktreeID) == "" {
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
	m.actionMenuOpen = false
	m.confirmAction = ""
	m.followupInput = false
	m.followupText = ""
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
			prompt:       prompt,
			output:       output,
			err:          err,
		}
	}
}

func (m model) requestedAttach() (string, string, bool) {
	if !m.attachRequested {
		return "", "", false
	}
	invocationID := strings.TrimSpace(m.attachInvocationID)
	repoID := strings.TrimSpace(m.attachRequestedRepo)
	if invocationID == "" || repoID == "" {
		return "", "", false
	}
	return invocationID, repoID, true
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

func (m model) openReviewPage(turnID string, backPage watchPage) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.selectedInvocationID) == "" || strings.TrimSpace(m.selectedRepoID) == "" {
		m.lastActionError = true
		m.lastActionMessage = "review unavailable: no invocation selected"
		return m, nil
	}
	m.page = pageReview
	m.backPage = backPage
	m.reviewTurnID = strings.TrimSpace(turnID)
	m.reviewDiff = daemon.InvocationDiffData{}
	m.reviewCheck = daemon.InvocationCheckData{}
	m.reviewFiles = nil
	m.reviewSelectedIndex = 0
	m.reviewSelectedKey = ""
	m.reviewScroll = 0
	m.reviewLoading = true
	m.reviewError = ""
	m.reviewFilesFocus = true
	m.reviewReviewed = make(map[string]bool)
	return m, m.loadReviewCmd()
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

func (m model) selectedReviewFile() (reviewFile, bool) {
	if len(m.reviewFiles) == 0 {
		return reviewFile{}, false
	}
	idx := clamp(m.reviewSelectedIndex, 0, len(m.reviewFiles)-1)
	return m.reviewFiles[idx], true
}

func (m model) selectedSessionCanRecreate() bool {
	if m.selectedSessionLoading || strings.TrimSpace(m.selectedSessionError) != "" {
		return false
	}
	return m.selectedSession.RecreateAvailable
}

func (m model) canStartAction(kind actionKind) bool {
	selected, ok := m.selectedInvocation()
	if !ok && kind != actionAttach {
		return false
	}

	switch kind {
	case actionAttach:
		mode := strings.TrimSpace(m.selectedMode)
		if ok {
			mode = firstNonEmpty(selected.Mode, mode)
		}
		return mode == "headed" &&
			strings.TrimSpace(m.selectedInvocationID) != "" &&
			strings.TrimSpace(m.selectedRepoID) != "" &&
			m.selectedSession.IsLive() &&
			!m.selectedSessionLoading &&
			strings.TrimSpace(m.selectedSessionError) == ""
	case actionOpen:
		return m.open != nil
	case actionStop:
		return m.stop != nil && selected.FinishedAt == ""
	case actionKill:
		return m.kill != nil && selected.FinishedAt == ""
	case actionLand:
		return m.land != nil && selected.LandingStatus != "landed" && selected.LandingStatus != "discarded"
	case actionDiscard:
		return m.discard != nil && selected.LandingStatus != "landed" && selected.LandingStatus != "discarded"
	case actionFollowup:
		return m.followup != nil &&
			selected.Mode == "headless" &&
			selected.FinishedAt == "" &&
			(selected.State == "running" || selected.State == "waiting")
	case actionRecreate:
		return m.recreate != nil && selected.Mode == "headed" && m.selectedSessionCanRecreate()
	case actionPRSync:
		if m.prSync == nil || strings.TrimSpace(selected.WorktreeID) == "" {
			return false
		}
		return selected.PRSyncEligible
	case actionPRMerge:
		return m.prMerge != nil && strings.TrimSpace(selected.WorktreeID) != ""
	case actionRebase:
		return m.rebase != nil && strings.TrimSpace(selected.WorktreeID) != ""
	default:
		return false
	}
}

func actionNeedsConfirm(kind actionKind) bool {
	switch kind {
	case actionKill, actionLand, actionDiscard, actionPRMerge, actionRebase:
		return true
	default:
		return false
	}
}

func (m *model) moveSelection(delta int) {
	if len(m.snapshot.Invocations) == 0 {
		m.selectedIndex = 0
		m.selectedInvocationID = ""
		m.selectedRepoID = ""
		m.selectedMode = ""
		return
	}
	next := clamp(m.selectedIndex+delta, 0, len(m.snapshot.Invocations)-1)
	m.selectedIndex = next
	m.selectedInvocationID = m.snapshot.Invocations[next].InvocationID
	m.selectedRepoID = m.snapshot.Invocations[next].RepoID
	m.selectedMode = m.snapshot.Invocations[next].Mode
}

func (m *model) reconcileSelection() {
	if len(m.snapshot.Invocations) == 0 {
		m.selectedIndex = 0
		m.selectedInvocationID = ""
		m.selectedRepoID = ""
		m.selectedMode = ""
		return
	}

	if m.selectedInvocationID != "" {
		for idx, inv := range m.snapshot.Invocations {
			if inv.InvocationID == m.selectedInvocationID {
				m.selectedIndex = idx
				m.selectedRepoID = inv.RepoID
				m.selectedMode = inv.Mode
				return
			}
		}
	}

	m.selectedIndex = clamp(m.selectedIndex, 0, len(m.snapshot.Invocations)-1)
	m.selectedInvocationID = m.snapshot.Invocations[m.selectedIndex].InvocationID
	m.selectedRepoID = m.snapshot.Invocations[m.selectedIndex].RepoID
	m.selectedMode = m.snapshot.Invocations[m.selectedIndex].Mode
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

func (m *model) moveReviewSelectionToNextUnreviewed(delta int) {
	if len(m.reviewFiles) == 0 || delta == 0 {
		return
	}
	start := clamp(m.reviewSelectedIndex, 0, len(m.reviewFiles)-1)
	for step := 1; step <= len(m.reviewFiles); step++ {
		idx := (start + step*delta + len(m.reviewFiles)) % len(m.reviewFiles)
		if !m.reviewReviewed[m.reviewFiles[idx].key] {
			m.reviewSelectedIndex = idx
			m.reviewSelectedKey = m.reviewFiles[idx].key
			m.reviewScroll = 0
			return
		}
	}
}

func (m *model) toggleReviewed() {
	selected, ok := m.selectedReviewFile()
	if !ok {
		return
	}
	if m.reviewReviewed == nil {
		m.reviewReviewed = make(map[string]bool)
	}
	if m.reviewReviewed[selected.key] {
		delete(m.reviewReviewed, selected.key)
		return
	}
	m.reviewReviewed[selected.key] = true
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

func formatActionError(kind actionKind, err error, invocationID, worktreeID, turnID string) string {
	target := actionTarget(kind, invocationID, worktreeID, turnID)
	code := agencyerrors.GetCode(err)
	if code == agencyerrors.ESessionEnded {
		hint := "session ended; use recreate, history, transcript, logs, or open to inspect the invocation"
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

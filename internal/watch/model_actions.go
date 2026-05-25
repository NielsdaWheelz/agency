package watch

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func (m model) openActionMenu() (tea.Model, tea.Cmd) {
	if _, ok := m.selectedInvocation(); !ok {
		m.setActionError("actions unavailable: no invocation selected")
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
	}
	for _, entry := range actionMenuEntries {
		if msg.Text == entry.key {
			return m.startInvocationAction(entry.kind)
		}
	}
	return m, nil
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
			m.setActionError("followup unavailable: prompt is empty")
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

// closeActionState clears all action-related transient state.
func (m *model) closeActionState() {
	m.actionMenuOpen = false
	m.confirmAction = ""
	m.followupInput = false
	m.followupText = ""
}

func (m model) startInvocationAction(kind actionKind) (tea.Model, tea.Cmd) {
	if kind == actionFollowup {
		if !m.canStartAction(kind) {
			m.setActionError("followup unavailable for the selected invocation")
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
		m.setActionError(fmt.Sprintf("%s unavailable: no invocation selected", kind))
		return m, nil
	}

	var run func() (string, error)
	switch kind {
	case actionAttach:
		invocationID := strings.TrimSpace(m.selectedInvocationID)
		repoID := strings.TrimSpace(m.selectedRepoID)
		mode := ""
		if ok {
			invocationID = strings.TrimSpace(selected.InvocationID)
			repoID = strings.TrimSpace(selected.RepoID)
			mode = strings.TrimSpace(selected.Mode)
		}
		if invocationID == "" || repoID == "" {
			m.setActionError("attach unavailable: no invocation selected")
			return m, nil
		}
		if ok && mode != "headed" {
			m.setActionError(formatActionError(
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
			))
			return m, nil
		}
		if m.selectedSessionLoading {
			m.setActionError("attach unavailable: session facts are still loading")
			return m, nil
		}
		if strings.TrimSpace(m.selectedSessionError) != "" {
			m.setActionError("attach unavailable: " + m.selectedSessionError)
			return m, nil
		}
		if !sessionIsLive(m.selectedSession) {
			m.setActionError(formatActionError(
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
			))
			return m, nil
		}
		m.attachInvocationID = invocationID
		m.attachRequestedRepo = repoID
		m.closeActionState()
		return m, tea.Quit
	case actionFollowup:
		if m.followup == nil {
			m.setActionError(fmt.Sprintf("%s unavailable: action is not configured", kind))
			return m, nil
		}
		if strings.TrimSpace(prompt) == "" {
			m.setActionError("followup unavailable: prompt is empty")
			return m, nil
		}
		run = func() (string, error) {
			return m.followup(m.ctx, selected.InvocationID, selected.RepoID, prompt)
		}
	default:
		fn, targetID, supported := m.resolveSimpleAction(kind, selected)
		if !supported {
			m.setActionError(fmt.Sprintf("%s unavailable: unsupported action", kind))
			return m, nil
		}
		if fn == nil {
			m.setActionError(fmt.Sprintf("%s unavailable: action is not configured", kind))
			return m, nil
		}
		run = func() (string, error) {
			return fn(m.ctx, targetID, selected.RepoID)
		}
	}
	if (kind == actionPRSync || kind == actionPRMerge || kind == actionRebase) && strings.TrimSpace(selected.WorktreeID) == "" {
		m.setActionError(formatActionError(
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
		))
		return m, nil
	}

	m.actionRunning = true
	m.closeActionState()
	m.setActionMessage(fmt.Sprintf("%s in progress for %s", kind, actionTarget(kind, selected.InvocationID, selected.WorktreeID, "")))

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

// resolveSimpleAction returns the run function and target id for the simple
// invocation/worktree actions whose dispatch is structurally identical. The
// supported flag is false for actionFollowup, actionAttach, or any action kind
// not in the simple set.
func (m model) resolveSimpleAction(kind actionKind, selected daemon.InvocationDTO) (fn func(context.Context, string, string) (string, error), targetID string, supported bool) {
	supported = true
	switch kind {
	case actionOpen:
		fn, targetID = m.open, selected.InvocationID
	case actionStop:
		fn, targetID = m.stop, selected.InvocationID
	case actionKill:
		fn, targetID = m.kill, selected.InvocationID
	case actionLand:
		fn, targetID = m.land, selected.InvocationID
	case actionDiscard:
		fn, targetID = m.discard, selected.InvocationID
	case actionRecreate:
		fn, targetID = m.recreate, selected.InvocationID
	case actionPRSync:
		fn, targetID = m.prSync, selected.WorktreeID
	case actionPRMerge:
		fn, targetID = m.prMerge, selected.WorktreeID
	case actionRebase:
		fn, targetID = m.rebase, selected.WorktreeID
	default:
		supported = false
	}
	return
}

func (m model) requestedAttach() (string, string, bool) {
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
		m.setActionError("restore unavailable: action is not configured")
		return m, nil
	}
	turn, ok := m.selectedTurn()
	if !ok {
		m.setActionError("restore unavailable: no turn selected")
		return m, nil
	}
	if !turn.Restorable || turn.CheckpointID <= 0 {
		m.setActionError("restore unavailable: selected turn does not have a restorable checkpoint")
		return m, nil
	}

	m.actionRunning = true
	m.setActionMessage(fmt.Sprintf("%s in progress for %s", actionRestore, actionTarget(actionRestore, m.selectedInvocationID, "", turn.EntryID)))

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
		m.setActionError("review unavailable: no invocation selected")
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
	return m, m.loadReviewCmd()
}

func (m model) openHistoryPage(backPage watchPage) (tea.Model, tea.Cmd) {
	m.page = pageHistory
	m.backPage = backPage
	m.historyLoading = true
	m.historyError = ""
	return m, m.loadHistoryCmd()
}

func (m model) openTranscriptPage(backPage watchPage) (tea.Model, tea.Cmd) {
	m.page = pageTranscript
	m.backPage = backPage
	m.transcriptContent = ""
	m.transcriptLoading = true
	m.transcriptError = ""
	m.transcriptScroll = 0
	return m, m.loadTranscriptCmd()
}

func (m model) openLogsPage(backPage watchPage) (tea.Model, tea.Cmd) {
	m.page = pageLogs
	m.backPage = backPage
	m.logsKind = m.currentLogsKind()
	m.logsContent = ""
	m.logsLoading = true
	m.logsError = ""
	m.logsScroll = 0
	return m, m.loadLogsCmd()
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
		invocationID := m.selectedInvocationID
		repoID := m.selectedRepoID
		if ok {
			invocationID = selected.InvocationID
			repoID = selected.RepoID
			if strings.TrimSpace(selected.Mode) != "headed" {
				return false
			}
		}
		return strings.TrimSpace(invocationID) != "" &&
			strings.TrimSpace(repoID) != "" &&
			sessionIsLive(m.selectedSession) &&
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
		return m.prSync != nil && strings.TrimSpace(selected.WorktreeID) != "" && selected.PRSyncEligible
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

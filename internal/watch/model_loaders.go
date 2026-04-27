package watch

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
)

func (m *model) loadWorkspaceSnapshotCmd() tea.Cmd {
	ctx := m.ctx
	client := m.client
	repoID := strings.TrimSpace(m.activeRepoID)
	worktreeID := strings.TrimSpace(m.activeWorktreeID)
	return func() tea.Msg {
		snapshot, err := loadWorkspaceSnapshot(ctx, client, repoID, worktreeID)
		return snapshotLoadedMsg{repoID: repoID, worktreeID: worktreeID, snapshot: snapshot, err: err}
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
	m.selectedSession = daemon.InvocationSessionData{}
	m.selectedSessionLoading = false
	m.selectedSessionError = ""
	m.selectedSessionInvocation = ""
	m.selectedSessionRepo = ""
}

func (m *model) beginSelectedSessionLoad(invocationID, repoID string) tea.Cmd {
	m.selectedSession = daemon.InvocationSessionData{}
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
		(m.selectedSessionLoading || strings.TrimSpace(m.selectedSession.SessionStatus) != "" || m.selectedSessionError != "") {
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

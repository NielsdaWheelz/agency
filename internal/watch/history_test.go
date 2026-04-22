package watch

import (
	"context"
	"net/http"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func historyTestTurns() []daemon.Turn {
	return []daemon.Turn{
		{EntryID: "e-1", Kind: daemon.TurnPrompt, Summary: "Fix the bug", ShortTimestamp: "11:50:00", Restorable: false},
		{
			EntryID:        "e-2",
			Kind:           daemon.TurnAssistant,
			Summary:        "I'll examine the code",
			ShortTimestamp: "11:50:10",
			CheckpointID:   1,
			Restorable:     true,
			ToolCalls: []daemon.ToolCall{
				{Name: "Read", Command: "/src/auth.go", ExitCode: 0, HasExit: true},
			},
		},
		{
			EntryID:        "e-3",
			Kind:           daemon.TurnAssistant,
			Summary:        "The fix is applied",
			ShortTimestamp: "11:50:30",
			CheckpointID:   2,
			Restorable:     true,
			ToolCalls: []daemon.ToolCall{
				{Name: "Write", Command: "/src/auth.go"},
				{Name: "Bash", Command: "make test", ExitCode: 0, HasExit: true},
			},
		},
		{EntryID: "e-4", Kind: daemon.TurnFollowup, Summary: "Now fix the tests", ShortTimestamp: "11:50:45", CheckpointID: 2, Restorable: true},
		{EntryID: "e-5", Kind: daemon.TurnAssistant, Summary: "Tests are fixed", ShortTimestamp: "11:51:00", CheckpointID: 3, Restorable: true},
	}
}

func applyHistoryKey(m model, msg tea.KeyPressMsg) (model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func TestHistoryPageNavigation_TracksSelectedTurnEntryID(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{InitialPage: InitialPageHistory, InvocationID: "inv-1", RepoID: "repo-1"})
	m.page = pageHistory
	m.historyTurns = historyTestTurns()
	m.historySelectedIndex = len(m.historyTurns) - 1
	m.historySelectedEntryID = m.historyTurns[m.historySelectedIndex].EntryID
	m.reconcileHistorySelection()
	assert.Equal(t, len(m.historyTurns)-1, m.historySelectedIndex)

	m, _ = applyHistoryKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	assert.Equal(t, len(m.historyTurns)-2, m.historySelectedIndex)
	assert.Equal(t, "e-4", m.historySelectedEntryID)

	m, _ = applyHistoryKey(m, tea.KeyPressMsg{Code: tea.KeyHome, Text: "g"})
	assert.Equal(t, 0, m.historySelectedIndex)
	assert.Equal(t, "e-1", m.historySelectedEntryID)

	m, _ = applyHistoryKey(m, tea.KeyPressMsg{Code: tea.KeyEnd, Text: "G"})
	assert.Equal(t, len(m.historyTurns)-1, m.historySelectedIndex)
	assert.Equal(t, "e-5", m.historySelectedEntryID)
}

func TestHistoryPage_ViewContainsTurnDataAndHelp(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{InitialPage: InitialPageHistory, InvocationID: "inv-1", RepoID: "repo-1"})
	m.page = pageHistory
	m.snapshot = Snapshot{
		Repos: []daemon.RepoDTO{
			{RepoID: "repo-1", RepoKey: "github.com/acme/one"},
		},
		Worktrees: []daemon.WorktreeDTO{
			{WorktreeID: "wt-1", RepoID: "repo-1", WorktreeName: "feature-auth"},
		},
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1", WorktreeID: "wt-1", Runner: "codex", Mode: "headed"},
		},
	}
	m.selectedIndex = 0
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"
	m.historyTurns = historyTestTurns()
	m.historySelectedIndex = len(m.historyTurns) - 1
	m.historySelectedEntryID = m.historyTurns[m.historySelectedIndex].EntryID
	m.reconcileHistorySelection()
	m.width = 120
	m.height = 40

	view := m.View()
	content := view.Content

	assert.Contains(t, content, "codex/headed / feature-auth / github.com/acme/one")
	assert.Contains(t, content, "history / invocation inv-1")
	assert.NotContains(t, content, "invocation history  inv-1")
	assert.Contains(t, content, "Fix the bug")
	assert.Contains(t, content, "I'll examine the code")
	assert.Contains(t, content, "The fix is applied")
	assert.Contains(t, content, "Now fix the tests")
	assert.Contains(t, content, "Tests are fixed")
	assert.Contains(t, content, "[prompt]")
	assert.Contains(t, content, "[assistant]")
	assert.Contains(t, content, "[follow-up]")
	assert.Contains(t, content, "Read")
	assert.Contains(t, content, "Write")
	assert.Contains(t, content, "Bash")
	assert.Contains(t, content, "cp:1")
	assert.Contains(t, content, "cp:2")
	assert.Contains(t, content, "cp:3")
	assert.Contains(t, content, "restore")
	assert.Contains(t, content, "quit")
}

func TestHistoryPageRestoreAction_UsesSelectedTurnAndQueuesReload(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{
		Restore: func(_ context.Context, invocationID, repoID, turnID string) (string, error) {
			assert.Equal(t, "inv-1", invocationID)
			assert.Equal(t, "repo-1", repoID)
			assert.Equal(t, "e-3", turnID)
			return "restored checkpoint", nil
		},
	})
	m.page = pageHistory
	m.backPage = pageWorkspace
	m.historyTurns = historyTestTurns()
	m.historySelectedIndex = 2
	m.historySelectedEntryID = "e-3"
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)

	msg := cmd()
	next, followCmd := next.(model).Update(msg)
	nextModel := next.(model)

	assert.Equal(t, "restored checkpoint", nextModel.lastActionMessage)
	assert.False(t, nextModel.lastActionError)
	assert.True(t, nextModel.historyLoading)
	require.NotNil(t, followCmd)
}

func TestHistoryPageAttach_QuitsAndDefersAttach(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{InitialPage: InitialPageHistory, InvocationID: "inv-1", RepoID: "repo-1"})
	m.page = pageHistory
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"
	m.selectedMode = "headed"
	m.selectedSession = daemon.InvocationSessionData{
		InvocationID:  "inv-1",
		RepoID:        "repo-1",
		SessionStatus: "live",
		TmuxSession:   "agency_inv-1",
	}
	m.selectedSessionInvocation = "inv-1"
	m.selectedSessionRepo = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Text: "a"})
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())

	nextModel := next.(model)
	invocationID, repoID, ok := nextModel.requestedAttach()
	require.True(t, ok)
	assert.Equal(t, "inv-1", invocationID)
	assert.Equal(t, "repo-1", repoID)
}

func TestLoadHistoryTurns_ValidatesScopeBeforeLoading(t *testing.T) {
	t.Parallel()

	_, err := loadHistoryTurns(context.Background(), nil, "inv-1", "repo-1")
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EInternal, agencyerrors.GetCode(err))

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("history helper should reject blank scope before issuing requests")
	})))

	_, err = loadHistoryTurns(context.Background(), client, "", "repo-1")
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EInvalidArgument, agencyerrors.GetCode(err))

	_, err = loadHistoryTurns(context.Background(), client, "inv-1", "")
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EInvalidArgument, agencyerrors.GetCode(err))
}

func TestLoadHistoryTurns_LoadsUnifiedTimeline(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invocations/inv-1/timeline":
			assert.Equal(t, "500", r.URL.Query().Get("limit"))
			writeDaemonOK(t, w, daemon.InvocationTimelineData{
				Entries: []daemon.TimelineEntryDTO{
					{EntryID: "e-1", Kind: "prompt_seed", Timestamp: "2026-04-17T17:50:00Z", Data: map[string]interface{}{"text": "Fix the bug"}},
					{EntryID: "e-2", Kind: "message", Timestamp: "2026-04-17T17:50:10Z", Data: map[string]interface{}{"role": "assistant", "text": "I'll examine the code"}},
					{EntryID: "e-3", Kind: "checkpoint_event", Timestamp: "2026-04-17T17:50:20Z", Data: map[string]interface{}{"checkpoint_id": 1}},
					{EntryID: "e-4", Kind: "followup_prompt", Timestamp: "2026-04-17T17:50:30Z", Data: map[string]interface{}{"text": "Now fix the tests"}},
					{EntryID: "e-5", Kind: "message", Timestamp: "2026-04-17T17:50:40Z", Data: map[string]interface{}{"role": "assistant", "text": "Tests are fixed"}},
				},
			})
		case "/invocations/inv-1/checkpoints":
			writeDaemonOK(t, w, daemon.ListCheckpointsData{
				Checkpoints: []daemon.CheckpointDTO{
					{
						ID:                   1,
						Description:          "checkpoint after edits",
						Diffstat:             "2 files changed",
						ChangedPaths:         []string{"README.md", "docs/note.txt"},
						ChangedPathCount:     2,
						ChangedPathTruncated: false,
					},
				},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	})))

	turns, err := loadHistoryTurns(context.Background(), client, "inv-1", "repo-1")
	require.NoError(t, err)
	require.NotEmpty(t, turns)

	var assistant *daemon.Turn
	for i := range turns {
		if turns[i].Summary == "I'll examine the code" {
			assistant = &turns[i]
			break
		}
	}
	require.NotNil(t, assistant)
	assert.True(t, assistant.Restorable)
	assert.Equal(t, 1, assistant.CheckpointID)
	assert.Contains(t, assistant.CheckpointChangedPaths, "README.md")
	assert.Contains(t, assistant.CheckpointChangedPaths, "docs/note.txt")
}

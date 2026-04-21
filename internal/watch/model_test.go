package watch

import (
	"context"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

func TestModel_WorkspaceSelection_ReconcilesByInvocationID(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{Interval: 2 * time.Second})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1"},
			{InvocationID: "inv-2", RepoID: "repo-2"},
			{InvocationID: "inv-3", RepoID: "repo-3"},
		},
	}
	m.selectedInvocationID = "inv-2"
	m.selectedIndex = 1
	m.selectedRepoID = "repo-old"
	m.workspaceLoading = true

	next, _ := m.Update(snapshotLoadedMsg{
		snapshot: Snapshot{
			Invocations: []daemon.InvocationDTO{
				{InvocationID: "inv-3", RepoID: "repo-3"},
				{InvocationID: "inv-2", RepoID: "repo-2"},
				{InvocationID: "inv-1", RepoID: "repo-1"},
			},
		},
	})
	nextModel := next.(model)

	assert.False(t, nextModel.workspaceLoading)
	assert.Equal(t, "inv-2", nextModel.selectedInvocationID)
	assert.Equal(t, 1, nextModel.selectedIndex)
	assert.Equal(t, "repo-2", nextModel.selectedRepoID)
}

func TestModel_WorkspaceNavigation_TracksSelectedInvocationIdentity(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{Interval: 2 * time.Second})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1"},
			{InvocationID: "inv-2", RepoID: "repo-2"},
			{InvocationID: "inv-3", RepoID: "repo-3"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	nextModel := next.(model)

	assert.Equal(t, 1, nextModel.selectedIndex)
	assert.Equal(t, "inv-2", nextModel.selectedInvocationID)
	assert.Equal(t, "repo-2", nextModel.selectedRepoID)
}

func TestModel_PageSwitchingBetweenWorkspaceHistoryAndLogs(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{Interval: 2 * time.Second})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1", WorktreeID: "wt-1"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Text: "h"})
	require.NotNil(t, cmd)
	historyModel := next.(model)
	assert.Equal(t, pageHistory, historyModel.page)
	assert.Equal(t, pageWorkspace, historyModel.backPage)
	assert.True(t, historyModel.historyLoading)

	next, cmd = historyModel.Update(tea.KeyPressMsg{Text: "l"})
	require.NotNil(t, cmd)
	logsModel := next.(model)
	assert.Equal(t, pageLogs, logsModel.page)
	assert.Equal(t, pageHistory, logsModel.backPage)
	assert.True(t, logsModel.logsLoading)
}

func TestModel_ActionAttach_QuitsAndDefersAttach(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1", Mode: "headed", State: string(runnerstatus.StateRunning)},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"
	m.selectedSession = InvocationSession{
		InvocationID: "inv-1",
		RepoID:       "repo-1",
		Status:       "live",
		TmuxSession:  "agency_inv-1",
	}
	m.selectedSessionInvocation = "inv-1"
	m.selectedSessionRepo = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())

	nextModel := next.(model)
	invocationID, repoID, ok := nextModel.requestedAttach()
	require.True(t, ok)
	assert.Equal(t, "inv-1", invocationID)
	assert.Equal(t, "repo-1", repoID)
	assert.False(t, nextModel.actionRunning)
	assert.Empty(t, nextModel.lastActionMessage)
}

func TestModel_ActionAttach_MissingSessionOpensActionsAndAllowsRecreate(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{
		Recreate: func(context.Context, string, string) (string, error) {
			return "", nil
		},
	})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1", Mode: "headed", State: string(runnerstatus.StateRunning)},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"
	m.selectedSession = InvocationSession{
		InvocationID:      "inv-1",
		RepoID:            "repo-1",
		Status:            "missing",
		TmuxSession:       "agency_inv-1",
		RecreateAvailable: true,
	}
	m.selectedSessionInvocation = "inv-1"
	m.selectedSessionRepo = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd)
	nextModel := next.(model)

	assert.True(t, nextModel.actionMenuOpen)
	assert.False(t, nextModel.canStartAction(actionAttach))
	assert.True(t, nextModel.canStartAction(actionRecreate))
}

func TestModel_ActionAttach_HeadlessInvocationStaysInTUI(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1", Mode: "headless", State: string(runnerstatus.StateRunning)},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd)
	nextModel := next.(model)

	_, _, ok := nextModel.requestedAttach()
	assert.False(t, ok)
	assert.True(t, nextModel.actionMenuOpen)
	assert.False(t, nextModel.lastActionError)
	assert.Empty(t, nextModel.lastActionMessage)
}

func TestModel_ActionAttach_NonRunningInvocationStaysInTUI(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{
				InvocationID: "inv-1",
				RepoID:       "repo-1",
				Mode:         "headed",
				State:        string(runnerstatus.StateSucceeded),
				FinishedAt:   "2026-02-05T12:00:00Z",
			},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd)
	nextModel := next.(model)

	_, _, ok := nextModel.requestedAttach()
	assert.False(t, ok)
	assert.True(t, nextModel.actionMenuOpen)
	assert.False(t, nextModel.lastActionError)
	assert.Empty(t, nextModel.lastActionMessage)
}

func TestModel_ActionPRSync_MissingWorktreeIDIsRecoverable(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{
		PRSync: func(context.Context, string, string) (string, error) {
			t.Fatal("pr sync callback should not be called for a missing worktree")
			return "", nil
		},
	})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1"},
			{InvocationID: "inv-2", RepoID: "repo-2", WorktreeID: "wt-2"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	require.Nil(t, cmd)
	nextModel := next.(model)
	assert.True(t, nextModel.lastActionError)
	assert.Contains(t, nextModel.lastActionMessage, string(errors.EInvalidArgument))
	assert.Contains(t, nextModel.lastActionMessage, "worktree")
	assert.Contains(t, nextModel.lastActionMessage, "inv-1")
}

func TestModel_ActionOpenSuccessUsesConfiguredOutput(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{
		Open: func(context.Context, string, string) (string, error) {
			return "opened sandbox", nil
		},
	})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	require.NotNil(t, cmd)
	msg := cmd()
	next, _ = next.(model).Update(msg)
	nextModel := next.(model)

	assert.Equal(t, "opened sandbox", nextModel.lastActionMessage)
	assert.False(t, nextModel.lastActionError)
}

func TestModel_WorkspaceView_ShowsUnifiedActionsAndActivityProjection(t *testing.T) {
	t.Parallel()

	const invocationID = "invocation-selected-pane-1234567890abcdef"
	const worktreeID = "20260421103000-c3d4"
	const repoID = "769749d77af0806f"

	m := newModel(context.Background(), nil, RunOptions{
		Open:     func(context.Context, string, string) (string, error) { return "", nil },
		Stop:     func(context.Context, string, string) (string, error) { return "", nil },
		Kill:     func(context.Context, string, string) (string, error) { return "", nil },
		Followup: func(context.Context, string, string, string) (string, error) { return "", nil },
		PRSync:   func(context.Context, string, string) (string, error) { return "", nil },
		PRMerge:  func(context.Context, string, string) (string, error) { return "", nil },
		Rebase:   func(context.Context, string, string) (string, error) { return "", nil },
	})
	m.width = 240
	m.height = 28
	m.snapshot = Snapshot{
		Repos:     []daemon.RepoDTO{{RepoID: repoID, RepoKey: "github.com/acme/one"}},
		Worktrees: []daemon.WorktreeDTO{{WorktreeID: worktreeID, WorktreeName: "feature-auth"}},
		Invocations: []daemon.InvocationDTO{
			{
				InvocationID:   invocationID,
				InvocationName: "agent auth",
				RepoID:         repoID,
				WorktreeID:     worktreeID,
				Runner:         "claude-code",
				Mode:           "headless",
				State:          string(runnerstatus.StateWaiting),
				LandingStatus:  "landed",
				PRSyncEligible: true,
				StatusSummary:  "waiting on api contract",
				LatestActivity: &daemon.InvocationLatestActivity{
					TurnID:        "stream:1",
					Kind:          "assistant",
					Summary:       "latest activity summary",
					ToolCallCount: 1,
					ToolCalls: []daemon.InvocationActivityToolCall{
						{Name: "Bash", Command: "go test ./...", HasExit: true, ExitCode: 1},
					},
					CheckpointID:           3,
					Restorable:             true,
					CheckpointDescription:  "checkpoint after edits",
					CheckpointDiffstat:     "2 files changed, 12 insertions(+), 3 deletions(-)",
					CheckpointChangedPaths: []string{"internal/watch/model.go", "internal/watch/model_test.go"},
					CheckpointChangedCount: 2,
				},
				Navigation: &daemon.InvocationActivityNavigation{
					HistoryCommand: "agency agent " + invocationID + " history --repo " + repoID,
					DiffCommand:    "agency agent " + invocationID + " diff --repo " + repoID + " --turn stream:1",
					LatestTurnID:   "stream:1",
				},
			},
		},
	}
	m.selectedInvocationID = invocationID
	m.selectedIndex = 0
	m.actionMenuOpen = true

	view := m.View()
	assert.Contains(t, view.Content, "agents")
	assert.Contains(t, view.Content, "selected")
	assert.Contains(t, view.Content, "STATE")
	assert.Contains(t, view.Content, "AGENT")
	assert.Contains(t, view.Content, "Agent:      agent auth ("+invocationID+")")
	assert.Contains(t, view.Content, "Worktree:   feature-auth ("+worktreeID+")")
	assert.Contains(t, view.Content, "Repo:       github.com/acme/one ("+repoID+")")
	assert.Contains(t, view.Content, "State:      waiting")
	assert.Contains(t, view.Content, "Latest:     [assistant] latest activity summary (tools=1, checkpoint=3)")
	assert.NotContains(t, view.Content, "Next:")
	assert.Contains(t, view.Content, "Actions:")
	assert.Contains(t, view.Content, "open sandbox")
	assert.Contains(t, view.Content, "send follow-up")
	assert.Contains(t, view.Content, "sync PR")
	assert.Contains(t, view.Content, "merge PR")
	assert.Contains(t, view.Content, "rebase worktree")
	assert.Contains(t, view.Content, "stop invocation")
	assert.Contains(t, view.Content, "kill invocation")
	assert.Contains(t, view.Content, "open")
	assert.NotContains(t, view.Content, "IDs:")
}

func TestModel_WorkspaceView_ShowsHeadedSessionFacts(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{
		Recreate: func(context.Context, string, string) (string, error) { return "", nil },
		SessionLoader: func(context.Context, string, string) (InvocationSession, error) {
			return InvocationSession{}, nil
		},
	})
	m.width = 180
	m.height = 28
	m.snapshot = Snapshot{
		Repos: []daemon.RepoDTO{{RepoID: "repo-1", RepoKey: "github.com/acme/one"}},
		Invocations: []daemon.InvocationDTO{
			{
				InvocationID:   "inv-1",
				InvocationName: "headed auth",
				RepoID:         "repo-1",
				Runner:         "codex",
				Mode:           "headed",
				State:          string(runnerstatus.StateRunning),
			},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"
	m.selectedSession = InvocationSession{
		InvocationID:      "inv-1",
		RepoID:            "repo-1",
		Status:            "missing",
		TmuxSession:       "agency_inv-1",
		ClientCount:       2,
		AttachCommand:     "agency agent inv-1 attach --repo repo-1",
		RecreateAvailable: true,
		Hint:              "use recreate to start a new headed session in the same sandbox",
		Clients: []InvocationSessionClient{
			{Name: "tty1"},
			{Name: "tty2", ReadOnly: true},
		},
	}
	m.selectedSessionInvocation = "inv-1"
	m.selectedSessionRepo = "repo-1"

	content := m.View().Content
	assert.Contains(t, content, "Session:     missing")
	assert.Contains(t, content, "Tmux:        agency_inv-1")
	assert.Contains(t, content, "Clients:     2")
	assert.Contains(t, content, "Attach:      agency agent inv-1 attach --repo repo-1")
	assert.Contains(t, content, "Recreate:    yes")
	assert.Contains(t, content, "tty2 (read-only)")
}

func TestTruncateWithEllipsis_UTF8Safe(t *testing.T) {
	t.Parallel()

	value := "café résumé"
	truncated := truncateWithEllipsis(value, 7)
	assert.Equal(t, "café...", truncated)
	assert.True(t, utf8.ValidString(truncated))

	tiny := truncateWithEllipsis("🙂🙂🙂🙂", 3)
	assert.Equal(t, "🙂🙂🙂", tiny)
	assert.True(t, utf8.ValidString(tiny))
}

func TestShortID_UTF8Safe(t *testing.T) {
	t.Parallel()

	short := shortID("résumé-123", 4)
	assert.Equal(t, "résu", short)
	assert.True(t, utf8.ValidString(short))
}

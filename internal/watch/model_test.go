package watch

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

type noopLoader struct{}

func (noopLoader) Load(_ context.Context) (Snapshot, error) {
	return Snapshot{}, nil
}

type fakeActionDispatcher struct {
	enterErr  error
	openErr   error
	prSyncErr error

	enterCalls  []string
	openCalls   []string
	prSyncCalls []string
}

func (f *fakeActionDispatcher) Enter(_ context.Context, invocationID, repoID string) error {
	f.enterCalls = append(f.enterCalls, invocationID+"@"+repoID)
	return f.enterErr
}

func (f *fakeActionDispatcher) Open(_ context.Context, invocationID, repoID string) error {
	f.openCalls = append(f.openCalls, invocationID+"@"+repoID)
	return f.openErr
}

func (f *fakeActionDispatcher) PRSync(_ context.Context, worktreeID, repoID string) error {
	f.prSyncCalls = append(f.prSyncCalls, worktreeID+"@"+repoID)
	return f.prSyncErr
}

func TestModel_SnapshotRefresh_KeepsSelectionByInvocationID(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, nil)
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1"},
			{InvocationID: "inv-2"},
			{InvocationID: "inv-3"},
		},
	}
	m.selectedInvocationID = "inv-2"
	m.selectedIndex = 1
	m.refreshing = true

	newSnapshot := Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-3"},
			{InvocationID: "inv-2"},
			{InvocationID: "inv-1"},
		},
	}

	next, _ := m.Update(snapshotLoadedMsg{snapshot: newSnapshot})
	nextModel, ok := next.(model)
	require.True(t, ok)

	assert.False(t, nextModel.refreshing)
	assert.Equal(t, "inv-2", nextModel.selectedInvocationID)
	assert.Equal(t, 1, nextModel.selectedIndex)
}

func TestModel_SnapshotRefresh_ErrorKeepsPriorWorkspace(t *testing.T) {
	t.Parallel()

	oldSnapshot := Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1"},
		},
		Warnings: []string{"old warning"},
	}

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, nil)
	m.snapshot = oldSnapshot
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0
	m.refreshing = true

	next, _ := m.Update(snapshotLoadedMsg{err: fmt.Errorf("daemon temporarily unavailable")})
	nextModel, ok := next.(model)
	require.True(t, ok)

	assert.False(t, nextModel.refreshing)
	assert.Equal(t, oldSnapshot, nextModel.snapshot)
	assert.Equal(t, "inv-1", nextModel.selectedInvocationID)
	assert.Equal(t, 0, nextModel.selectedIndex)
	assert.Contains(t, nextModel.lastError, "daemon temporarily unavailable")
}

func TestModel_KeyNavigation_TracksSelectedInvocationIdentity(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, nil)
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1"},
			{InvocationID: "inv-2"},
			{InvocationID: "inv-3"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	nextModel := next.(model)
	assert.Equal(t, 1, nextModel.selectedIndex)
	assert.Equal(t, "inv-2", nextModel.selectedInvocationID)
}

func TestModel_View_NarrowTerminalRendersWithoutBreakingPanels(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, nil)
	m.width = 48
	m.height = 18
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{
				InvocationID: "inv-1",
				RepoID:       "repo-1",
				WorktreeID:   "wt-1",
				Runner:       "claude",
				Mode:         "headless",
				Status:       "running",
			},
		},
		Worktrees: []daemon.WorktreeDTO{
			{
				WorktreeID: "wt-1",
				Name:       "feature-auth",
			},
		},
		Reviews: map[string]daemon.InvocationReviewData{
			"inv-1": {
				InvocationID:   "inv-1",
				Readiness:      "ready",
				Ready:          true,
				PRSyncEligible: true,
			},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0

	view := m.View()
	assert.NotEmpty(t, view.Content)
	assert.Contains(t, view.Content, "invocations")
	assert.Contains(t, view.Content, "invocation details")
}

func TestModel_View_ShowsWatchActionSet(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, nil)
	m.width = 96
	m.height = 22

	view := m.View()
	assert.Contains(t, view.Content, "enter")
	assert.Contains(t, view.Content, "open")
	assert.Contains(t, view.Content, "pr sync")
}

func TestModel_View_IncludesSharedActivityProjectionFields(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, nil)
	m.width = 140
	m.height = 28
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{
				InvocationID:   "inv-1",
				RepoID:         "repo-1",
				WorktreeID:     "wt-1",
				Runner:         "claude",
				Mode:           "headless",
				Status:         "running",
				DisplayStatus:  "working",
				StatusSummary:  "waiting on api contract",
				LatestActivity: &daemon.InvocationLatestActivity{TurnID: "stream:1", Summary: "latest activity summary"},
				Navigation: &daemon.InvocationActivityNavigation{
					LatestTurnID:   "stream:1",
					HistoryCommand: "agency agent history inv-1 --repo repo-1",
					DiffCommand:    "agency agent diff inv-1 --repo repo-1 --turn stream:1",
				},
			},
		},
		Worktrees: []daemon.WorktreeDTO{
			{WorktreeID: "wt-1", Name: "feature-auth"},
		},
		Reviews: map[string]daemon.InvocationReviewData{
			"inv-1": {
				InvocationID:   "inv-1",
				Readiness:      "blocked",
				Ready:          false,
				PRSyncEligible: false,
				Navigation: daemon.InvocationReviewNavigation{
					HistoryCommand: "agency agent history inv-1 --repo repo-1",
					DiffCommand:    "agency agent diff inv-1 --repo repo-1 --turn stream:1",
					LatestTurnID:   "stream:1",
				},
			},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0

	view := m.View()
	assert.Contains(t, view.Content, "status_summary: waiting on api contract")
	assert.Contains(t, view.Content, "latest_activity: [stream:1] latest activity summary")
	assert.Contains(t, view.Content, "turn:    stream:1")
}

func TestModel_ActionEnter_SessionEndedIsRecoverable(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeActionDispatcher{
		enterErr: errors.NewWithDetails(
			errors.ESessionEnded,
			"tmux session not found",
			map[string]string{
				"hint": "session ended; use 'agency agent logs' or 'agency agent open' to view",
			},
		),
	}

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, dispatcher)
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)

	msg := cmd()
	next, _ = next.(model).Update(msg)
	nextModel := next.(model)

	assert.Equal(t, "inv-1", nextModel.selectedInvocationID)
	assert.Equal(t, 0, nextModel.selectedIndex)
	assert.Contains(t, nextModel.lastActionMessage, string(errors.ESessionEnded))
	assert.Contains(t, nextModel.lastActionMessage, "session ended")
	require.Len(t, dispatcher.enterCalls, 1)
	assert.Equal(t, "inv-1@repo-1", dispatcher.enterCalls[0])
}

func TestModel_ActionFailure_RemainsInteractive(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeActionDispatcher{
		prSyncErr: errors.NewWithDetails(
			errors.EInvalidArgument,
			"pr sync blocked by contract validation",
			map[string]string{"hint": "address blocking reasons and retry"},
		),
	}

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, dispatcher)
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", WorktreeID: "wt-1", RepoID: "repo-1"},
			{InvocationID: "inv-2", WorktreeID: "wt-2", RepoID: "repo-1"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	require.NotNil(t, cmd)
	msg := cmd()
	next, _ = next.(model).Update(msg)
	nextModel := next.(model)

	assert.Contains(t, nextModel.lastActionMessage, string(errors.EInvalidArgument))

	moved, _ := nextModel.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	movedModel := moved.(model)
	assert.Equal(t, 1, movedModel.selectedIndex, "watch should continue handling navigation after recoverable action failure")
	require.Len(t, dispatcher.prSyncCalls, 1)
	assert.Equal(t, "wt-1@repo-1", dispatcher.prSyncCalls[0])
}

func TestModel_ActionPRSync_MessagesIncludeWorktreeTarget(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeActionDispatcher{}

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, dispatcher)
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-abcdefghij", WorktreeID: "wt-1", RepoID: "repo-1"},
		},
	}
	m.selectedInvocationID = "inv-abcdefghij"
	m.selectedIndex = 0

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	nextModel := next.(model)
	require.NotNil(t, cmd)
	assert.Contains(t, nextModel.lastActionMessage, "worktree wt-1")
	assert.Contains(t, nextModel.lastActionMessage, "invocation")

	msg := cmd()
	next, _ = nextModel.Update(msg)
	nextModel = next.(model)
	assert.Contains(t, nextModel.lastActionMessage, "worktree wt-1")
	assert.Contains(t, nextModel.lastActionMessage, "complete")
	require.Len(t, dispatcher.prSyncCalls, 1)
	assert.Equal(t, "wt-1@repo-1", dispatcher.prSyncCalls[0])
}

func TestModel_ActionPRSync_MissingWorktreeIDIsRecoverable(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeActionDispatcher{}

	m := newModel(context.Background(), noopLoader{}, 2*time.Second, dispatcher)
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1"},
			{InvocationID: "inv-2", WorktreeID: "wt-2", RepoID: "repo-1"},
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
	require.Empty(t, dispatcher.prSyncCalls)

	moved, _ := nextModel.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	movedModel := moved.(model)
	assert.Equal(t, 1, movedModel.selectedIndex)
}

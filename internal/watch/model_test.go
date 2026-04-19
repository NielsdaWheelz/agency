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

func TestModel_ActionAttach_SessionEndedIsRecoverable(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{
		Attach: func(context.Context, string, string) (string, error) {
			return "", errors.NewWithDetails(
				errors.ESessionEnded,
				"tmux session not found",
				map[string]string{
					"hint": "session ended; use 'agency agent <invocation-ref> history logs' or 'agency agent <invocation-ref> open' to view",
				},
			)
		},
	})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)

	msg := cmd()
	next, _ = next.(model).Update(msg)
	nextModel := next.(model)

	assert.Equal(t, "inv-1", nextModel.selectedInvocationID)
	assert.Equal(t, 0, nextModel.selectedIndex)
	assert.True(t, nextModel.lastActionError)
	assert.Contains(t, nextModel.lastActionMessage, string(errors.ESessionEnded))
	assert.Contains(t, nextModel.lastActionMessage, "session ended")
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

func TestModel_ActionSuccessUsesConfiguredOutput(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{
		Attach: func(context.Context, string, string) (string, error) {
			return "attached to tmux session", nil
		},
	})
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1"},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	next, _ = next.(model).Update(msg)
	nextModel := next.(model)

	assert.Equal(t, "attached to tmux session", nextModel.lastActionMessage)
	assert.False(t, nextModel.lastActionError)
}

func TestModel_WorkspaceView_ShowsUnifiedActionsAndActivityProjection(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{})
	m.width = 240
	m.height = 28
	m.snapshot = Snapshot{
		Repos: []daemon.RepoDTO{{RepoID: "repo-1", RepoKey: "github.com/acme/one"}},
		Invocations: []daemon.InvocationDTO{
			{
				InvocationID:  "inv-1",
				RepoID:        "repo-1",
				WorktreeID:    "wt-1",
				Runner:        "claude-code",
				Mode:          "headless",
				Status:        "running",
				DisplayStatus: "working",
			},
		},
		Worktrees: []daemon.WorktreeDTO{{WorktreeID: "wt-1", Name: "feature-auth"}},
		Checks: map[string]daemon.InvocationCheckData{
			"inv-1": {
				InvocationID:   "inv-1",
				Readiness:      "blocked",
				Ready:          false,
				PRSyncEligible: false,
				DisplayStatus:  "working",
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
				Navigation: daemon.InvocationCheckNavigation{
					HistoryCommand: "agency agent inv-1 history --repo repo-1",
					DiffCommand:    "agency agent inv-1 diff --repo repo-1 --turn stream:1",
					LatestTurnID:   "stream:1",
				},
			},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedIndex = 0

	view := m.View()
	assert.Contains(t, view.Content, "invocations")
	assert.Contains(t, view.Content, "selected invocation")
	assert.Contains(t, view.Content, "attach")
	assert.Contains(t, view.Content, "open")
	assert.Contains(t, view.Content, "pr sync")
	assert.Contains(t, view.Content, "summary:   waiting on api contract")
	assert.Contains(t, view.Content, "latest:    [stream:1] [assistant] latest activity summary (tools=1, checkpoint=3)")
	assert.Contains(t, view.Content, "tool:      ▶ Bash go test ./... (exit=1)")
	assert.Contains(t, view.Content, "files:     internal/watch/model.go, internal/watch/model_test.go")
	assert.Contains(t, view.Content, "turn:    stream:1")
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

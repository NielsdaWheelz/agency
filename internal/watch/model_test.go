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
)

type noopLoader struct{}

func (noopLoader) Load(_ context.Context) (Snapshot, error) {
	return Snapshot{}, nil
}

func TestModel_SnapshotRefresh_KeepsSelectionByInvocationID(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), noopLoader{}, 2*time.Second)
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

	m := newModel(context.Background(), noopLoader{}, 2*time.Second)
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

	m := newModel(context.Background(), noopLoader{}, 2*time.Second)
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

	m := newModel(context.Background(), noopLoader{}, 2*time.Second)
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

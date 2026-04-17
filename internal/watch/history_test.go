package watch

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
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

func TestNewHistoryModel_InitialSelectionIsLastTurn(t *testing.T) {
	t.Parallel()

	m := newHistoryModel(historyTestTurns(), true)
	assert.Equal(t, modeHistory, m.mode)
	assert.Equal(t, len(m.historyTurns)-1, m.historySelectedIndex)
}

func TestHistoryMode_UpdateHandlesNavigation(t *testing.T) {
	t.Parallel()

	m := newHistoryModel(historyTestTurns(), true)
	initialIndex := m.historySelectedIndex

	m, _ = applyHistoryKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	assert.Equal(t, initialIndex-1, m.historySelectedIndex)

	m, _ = applyHistoryKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	assert.Equal(t, initialIndex, m.historySelectedIndex)

	m, _ = applyHistoryKey(m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	assert.Equal(t, 0, m.historySelectedIndex)

	m, _ = applyHistoryKey(m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	assert.Equal(t, len(m.historyTurns)-1, m.historySelectedIndex)

	m, cmd := applyHistoryKey(m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	require.NotNil(t, cmd)
}

func TestHistoryMode_ViewContainsTurnData(t *testing.T) {
	t.Parallel()

	m := newHistoryModel(historyTestTurns(), true)
	m.width = 120
	m.height = 40

	view := m.View()
	content := view.Content

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
	assert.Contains(t, content, "quit")
}

func TestHistoryMode_ViewShowsCheckpointChangedPaths(t *testing.T) {
	t.Parallel()

	m := newHistoryModel([]daemon.Turn{
		{
			EntryID:                "e-1",
			Kind:                   daemon.TurnAssistant,
			Summary:                "Applied fix",
			ShortTimestamp:         "11:50:10",
			CheckpointID:           7,
			Restorable:             true,
			CheckpointChangedPaths: []string{"README.md", "docs/note.txt"},
			CheckpointChangedCount: 2,
		},
	}, true)
	m.width = 120
	m.height = 40

	view := m.View()
	assert.Contains(t, view.Content, "files: README.md, docs/note.txt")
}

func TestRunHistory_EmptyTurnsReturnsCheckpointNotFound(t *testing.T) {
	t.Parallel()

	err := RunHistory(nil, HistoryRunOptions{})
	require.Error(t, err)
	assert.Equal(t, errors.ECheckpointNotFound, errors.GetCode(err))
}

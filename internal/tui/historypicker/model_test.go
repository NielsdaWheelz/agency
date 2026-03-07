package historypicker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTurns() []Turn {
	return []Turn{
		{EntryID: "e-1", Kind: TurnPrompt, Summary: "Fix the bug", ShortTimestamp: "11:50:00", Restorable: false},
		{EntryID: "e-2", Kind: TurnAssistant, Summary: "I'll examine the code", ShortTimestamp: "11:50:10", CheckpointID: 1, Restorable: true,
			ToolCalls: []ToolCall{
				{Name: "Read", Command: "/src/auth.go", ExitCode: 0, HasExit: true},
			}},
		{EntryID: "e-3", Kind: TurnAssistant, Summary: "The fix is applied", ShortTimestamp: "11:50:30", CheckpointID: 2, Restorable: true,
			ToolCalls: []ToolCall{
				{Name: "Write", Command: "/src/auth.go"},
				{Name: "Bash", Command: "make test", ExitCode: 0, HasExit: true},
			}},
		{EntryID: "e-4", Kind: TurnFollowup, Summary: "Now fix the tests", ShortTimestamp: "11:50:45", CheckpointID: 2, Restorable: true},
		{EntryID: "e-5", Kind: TurnAssistant, Summary: "Tests are fixed", ShortTimestamp: "11:51:00", CheckpointID: 3, Restorable: true},
	}
}

func TestModel_InitialSelectionIsLastTurn(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	assert.Equal(t, len(m.turns)-1, m.selectedIndex)
}

func TestModel_UpDownNavigation(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	initial := m.selectedIndex

	// Move up
	m, _ = applyKey(m, tea.KeyUp)
	assert.Equal(t, initial-1, m.selectedIndex)

	// Move down
	m, _ = applyKey(m, tea.KeyDown)
	assert.Equal(t, initial, m.selectedIndex)

	// Can't go past bottom
	for i := 0; i < 10; i++ {
		m, _ = applyKey(m, tea.KeyDown)
	}
	assert.Equal(t, len(m.turns)-1, m.selectedIndex)

	// Can't go past top
	for i := 0; i < 20; i++ {
		m, _ = applyKey(m, tea.KeyUp)
	}
	assert.Equal(t, 0, m.selectedIndex)
}

func TestModel_EnterConfirmsSelection(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	// Move to second item
	m, _ = applyKey(m, tea.KeyUp)
	m, _ = applyKey(m, tea.KeyUp)
	m, _ = applyKey(m, tea.KeyUp)

	var cmd tea.Cmd
	m, cmd = applyKey(m, tea.KeyEnter)
	assert.True(t, m.confirmed)
	assert.NotNil(t, cmd) // should return tea.Quit
}

func TestModel_QuitCancels(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})

	var cmd tea.Cmd
	m, cmd = applyKeyRune(m, 'q')
	assert.True(t, m.canceled)
	assert.NotNil(t, cmd) // should return tea.Quit
}

func TestModel_EscCancels(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})

	m, _ = applyKey(m, tea.KeyEscape)
	assert.True(t, m.canceled)
}

func TestModel_ViewContainsTurnSummaries(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	assert.Contains(t, content, "Fix the bug")
	assert.Contains(t, content, "I'll examine the code")
	assert.Contains(t, content, "The fix is applied")
	assert.Contains(t, content, "Now fix the tests")
	assert.Contains(t, content, "Tests are fixed")
}

func TestModel_ViewContainsToolCalls(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	assert.Contains(t, content, "Read")
	assert.Contains(t, content, "Write")
	assert.Contains(t, content, "Bash")
}

func TestModel_ViewContainsShortTimestamps(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	assert.Contains(t, content, "11:50:00")
	assert.Contains(t, content, "11:50:10")
}

func TestModel_ViewShowsCheckpointBadge(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	assert.Contains(t, content, "cp:1")
	assert.Contains(t, content, "cp:2")
	assert.Contains(t, content, "cp:3")
}

func TestModel_ViewShowsCheckpointChangedPaths(t *testing.T) {
	t.Parallel()

	turns := []Turn{
		{
			EntryID:                "e-1",
			Kind:                   TurnAssistant,
			Summary:                "Applied fix",
			ShortTimestamp:         "11:50:10",
			CheckpointID:           7,
			Restorable:             true,
			CheckpointChangedPaths: []string{"README.md", "docs/note.txt"},
			CheckpointChangedCount: 2,
		},
	}
	m := newModel(turns, Options{NoColor: true})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	assert.Contains(t, content, "files: README.md, docs/note.txt")
}

func TestModel_ViewShowsSelectionMarker(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	lines := strings.Split(content, "\n")
	markerCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "> ") {
			markerCount++
		}
	}
	assert.Equal(t, 1, markerCount, "expected exactly one selection marker")
}

func TestModel_ViewShowsHelpText(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	assert.Contains(t, content, "enter")
	assert.Contains(t, content, "quit")
}

func TestModel_NonRestorableTurnsDimmedInView(t *testing.T) {
	t.Parallel()
	turns := []Turn{
		{EntryID: "e-1", Kind: TurnPrompt, Summary: "Fix it", ShortTimestamp: "11:50:00", Restorable: false},
		{EntryID: "e-2", Kind: TurnAssistant, Summary: "Done", ShortTimestamp: "11:50:10", CheckpointID: 1, Restorable: true},
	}
	m := newModel(turns, Options{NoColor: false})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	// Both turns should be present in the output
	assert.Contains(t, content, "Fix it")
	assert.Contains(t, content, "Done")
}

func TestModel_WindowResize(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	um := updated.(model)
	assert.Equal(t, 80, um.width)
	assert.Equal(t, 24, um.height)
}

func TestModel_EmptyTurns(t *testing.T) {
	t.Parallel()
	m := newModel(nil, Options{NoColor: true})
	m.width = 120
	m.height = 40
	view := m.View()
	content := view.Content

	assert.Contains(t, content, "no history")
}

func TestModel_VimKeys(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	initial := m.selectedIndex

	// k = up
	m, _ = applyKeyRune(m, 'k')
	assert.Equal(t, initial-1, m.selectedIndex)

	// j = down
	m, _ = applyKeyRune(m, 'j')
	assert.Equal(t, initial, m.selectedIndex)
}

func TestModel_HomeJumpsToTop(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})

	m, _ = applyKey(m, tea.KeyHome)
	assert.Equal(t, 0, m.selectedIndex)
}

func TestModel_EndJumpsToBottom(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	m.selectedIndex = 0

	m, _ = applyKey(m, tea.KeyEnd)
	assert.Equal(t, len(m.turns)-1, m.selectedIndex)
}

func TestModel_SelectedTurnAfterConfirm(t *testing.T) {
	t.Parallel()
	m := newModel(testTurns(), Options{NoColor: true})
	m, _ = applyKey(m, tea.KeyUp) // move to second-to-last
	m, _ = applyKey(m, tea.KeyEnter)

	require.True(t, m.confirmed)
	selected := m.turns[m.selectedIndex]
	assert.Equal(t, "e-4", selected.EntryID)
}

func TestTruncate_UTF8Safe(t *testing.T) {
	t.Parallel()
	input := "fix café résumé"
	got := truncate(input, 8)
	assert.Equal(t, "fix c...", got)
	assert.NotContains(t, got, "\uFFFD", "truncate should not split utf-8 runes")
}

func TestFormatToolCall_EmptyNameUsesFallback(t *testing.T) {
	t.Parallel()
	got := formatToolCall(ToolCall{Name: "", Command: "echo hi"})
	assert.Equal(t, "▶ tool echo hi", got)
}

func TestFormatCheckpointChangedPaths_InconsistentCounts(t *testing.T) {
	t.Parallel()
	turn := Turn{
		CheckpointChangedPaths: []string{"a.go", "b.go"},
		CheckpointChangedCount: 1,
		CheckpointPathsTrimmed: true,
	}
	got := formatCheckpointChangedPaths(turn)
	assert.Equal(t, "a.go, b.go", got)
}

// applyKey sends a named key press to the model.
func applyKey(m model, code rune) (model, tea.Cmd) {
	msg := tea.KeyPressMsg{Code: code}
	updated, cmd := m.Update(msg)
	return updated.(model), cmd
}

// applyKeyRune sends a character key press to the model.
func applyKeyRune(m model, r rune) (model, tea.Cmd) {
	msg := tea.KeyPressMsg{Code: r, Text: string(r)}
	updated, cmd := m.Update(msg)
	return updated.(model), cmd
}

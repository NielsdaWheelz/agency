package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimelineTurnGrouping_EmptyInput(t *testing.T) {
	t.Parallel()

	turns := groupTimelineIntoTurns(nil, nil)
	assert.Empty(t, turns)
}

func TestTimelineTurnGrouping_SinglePrompt(t *testing.T) {
	t.Parallel()

	entries := []timelineTurnEntry{
		{EntryID: "e-1", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{"text": "Fix the bug"}},
	}

	turns := groupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	assert.Equal(t, TurnPrompt, turns[0].Kind)
	assert.Equal(t, "Fix the bug", turns[0].Summary)
	assert.Equal(t, "e-1", turns[0].EntryID)
	assert.False(t, turns[0].Restorable)
}

func TestTimelineTurnGrouping_AssistantWithToolCalls(t *testing.T) {
	t.Parallel()

	entries := []timelineTurnEntry{
		{EntryID: "e-1", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{"text": "Fix the bug"}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{"role": "assistant", "text": "Let me check the file"}},
		{EntryID: "e-3", Kind: "tool_use", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{"name": "Read", "command": "/src/auth.go", "exit_code": float64(0)}},
		{EntryID: "e-4", Kind: "message", Timestamp: "2026-02-05T11:50:12Z", Data: map[string]interface{}{"role": "user", "text": "package auth\nimport \"fmt\""}},
	}

	turns := groupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 2)

	assistant := turns[1]
	assert.Equal(t, TurnAssistant, assistant.Kind)
	assert.Equal(t, "Let me check the file", assistant.Summary)
	require.Len(t, assistant.ToolCalls, 1)
	assert.Equal(t, "Read", assistant.ToolCalls[0].Name)
	assert.Equal(t, "/src/auth.go", assistant.ToolCalls[0].Command)
	assert.Equal(t, 0, assistant.ToolCalls[0].ExitCode)
	assert.True(t, assistant.ToolCalls[0].HasExit)
}

func TestTimelineTurnGrouping_UserPromptMessageFamilyBecomesFollowupTurn(t *testing.T) {
	t.Parallel()

	entries := []timelineTurnEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{"role": "assistant", "text": "Done fixing"}},
		{EntryID: "cp-1", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(1)}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:12Z", Data: map[string]interface{}{"role": "user", "message_family": "prompt", "text": "continue from checkpoint one"}},
	}
	checkpoints := []turnCheckpointRef{{ID: 1}}

	turns := groupTimelineIntoTurns(entries, checkpoints)
	require.Len(t, turns, 2)
	assert.Equal(t, TurnAssistant, turns[0].Kind)
	assert.Equal(t, TurnFollowup, turns[1].Kind)
	assert.Equal(t, "continue from checkpoint one", turns[1].Summary)
	assert.Equal(t, "e-2", turns[1].EntryID)
	assert.Equal(t, 1, turns[1].CheckpointID)
	assert.True(t, turns[1].Restorable)
}

func TestTimelineTurnGrouping_NonRestorableTurnsRemainMarked(t *testing.T) {
	t.Parallel()

	entries := []timelineTurnEntry{
		{EntryID: "e-0", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{"text": "Fix it"}},
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:05Z", Data: map[string]interface{}{"role": "assistant", "text": "Before checkpoint"}},
		{EntryID: "cp-1", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(1)}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:20Z", Data: map[string]interface{}{"role": "assistant", "text": "After checkpoint"}},
	}
	checkpoints := []turnCheckpointRef{{ID: 1}}

	turns := groupTimelineIntoTurns(entries, checkpoints)
	require.Len(t, turns, 3)
	assert.False(t, turns[0].Restorable)
	assert.Equal(t, 0, turns[0].CheckpointID)
	assert.True(t, turns[1].Restorable)
	assert.Equal(t, 1, turns[1].CheckpointID)
	assert.True(t, turns[2].Restorable)
	assert.Equal(t, 1, turns[2].CheckpointID)
}

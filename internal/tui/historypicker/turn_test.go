package historypicker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupTimelineIntoTurns_EmptyInput(t *testing.T) {
	t.Parallel()
	turns := GroupTimelineIntoTurns(nil, nil)
	assert.Empty(t, turns)
}

func TestGroupTimelineIntoTurns_SinglePrompt(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{"text": "Fix the bug"}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	assert.Equal(t, TurnPrompt, turns[0].Kind)
	assert.Equal(t, "Fix the bug", turns[0].Summary)
	assert.Equal(t, "e-1", turns[0].EntryID)
	assert.False(t, turns[0].Restorable)
}

func TestGroupTimelineIntoTurns_AssistantWithToolCalls(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{"text": "Fix the bug"}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant",
			"text": "Let me check the file",
		}},
		{EntryID: "e-3", Kind: "tool_use", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"name": "Read", "command": "/src/auth.go", "exit_code": float64(0),
		}},
		{EntryID: "e-4", Kind: "message", Timestamp: "2026-02-05T11:50:12Z", Data: map[string]interface{}{
			"role": "user",
			"text": "package auth\nimport \"fmt\"",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 2) // prompt + assistant turn

	assistant := turns[1]
	assert.Equal(t, TurnAssistant, assistant.Kind)
	assert.Equal(t, "Let me check the file", assistant.Summary)
	require.Len(t, assistant.ToolCalls, 1)
	assert.Equal(t, "Read", assistant.ToolCalls[0].Name)
	assert.Equal(t, "/src/auth.go", assistant.ToolCalls[0].Command)
	assert.Equal(t, 0, assistant.ToolCalls[0].ExitCode)
	assert.True(t, assistant.ToolCalls[0].HasExit)
}

func TestGroupTimelineIntoTurns_CheckpointAbsorbedIntoTurn(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Done",
		}},
		{EntryID: "cp-1", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(1),
		}},
	}
	checkpoints := []CheckpointRef{{ID: 1}}
	turns := GroupTimelineIntoTurns(entries, checkpoints)

	// checkpoint_event should not produce its own turn
	require.Len(t, turns, 1)
	assert.Equal(t, TurnAssistant, turns[0].Kind)
	assert.Equal(t, 1, turns[0].CheckpointID)
	assert.True(t, turns[0].Restorable)
}

func TestGroupTimelineIntoTurns_FollowupIsOwnTurn(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Done fixing",
		}},
		{EntryID: "cp-1", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(1),
		}},
		{EntryID: "e-2", Kind: "followup_prompt", Timestamp: "2026-02-05T11:50:20Z", Data: map[string]interface{}{
			"text": "Now fix the tests too",
		}},
	}
	checkpoints := []CheckpointRef{{ID: 1}}
	turns := GroupTimelineIntoTurns(entries, checkpoints)
	require.Len(t, turns, 2)
	assert.Equal(t, TurnAssistant, turns[0].Kind)
	assert.Equal(t, TurnFollowup, turns[1].Kind)
	assert.Equal(t, "Now fix the tests too", turns[1].Summary)
	// Followup should inherit the checkpoint from the preceding turn
	assert.Equal(t, 1, turns[1].CheckpointID)
	assert.True(t, turns[1].Restorable)
}

func TestGroupTimelineIntoTurns_UserPromptMessageFamilyBecomesFollowupTurn(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Done fixing",
		}},
		{EntryID: "cp-1", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(1),
		}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:12Z", Data: map[string]interface{}{
			"role":           "user",
			"message_family": "prompt",
			"text":           "continue from checkpoint one",
		}},
	}
	checkpoints := []CheckpointRef{{ID: 1}}
	turns := GroupTimelineIntoTurns(entries, checkpoints)

	require.Len(t, turns, 2)
	assert.Equal(t, TurnAssistant, turns[0].Kind)
	assert.Equal(t, TurnFollowup, turns[1].Kind)
	assert.Equal(t, "continue from checkpoint one", turns[1].Summary)
	assert.Equal(t, "e-2", turns[1].EntryID)
	assert.Equal(t, 1, turns[1].CheckpointID)
	assert.True(t, turns[1].Restorable)
}

func TestGroupTimelineIntoTurns_UserPromptMessageFamilyWithoutTextUsesFallbackSummary(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Done fixing",
		}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:12Z", Data: map[string]interface{}{
			"role":           "user",
			"message_family": "prompt",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)

	require.Len(t, turns, 2)
	assert.Equal(t, TurnFollowup, turns[1].Kind)
	assert.Equal(t, "e-2", turns[1].EntryID)
	assert.Equal(t, "follow-up prompt", turns[1].Summary)
}

func TestGroupTimelineIntoTurns_NonRestorableTurnsMarked(t *testing.T) {
	t.Parallel()
	// The prompt has no checkpoint before it; the assistant turn gets
	// a checkpoint absorbed from the checkpoint_event that follows it.
	entries := []TimelineEntry{
		{EntryID: "e-0", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{
			"text": "Fix it",
		}},
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:05Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Before checkpoint",
		}},
		{EntryID: "cp-1", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(1),
		}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:20Z", Data: map[string]interface{}{
			"role": "assistant", "text": "After checkpoint",
		}},
	}
	checkpoints := []CheckpointRef{{ID: 1}}
	turns := GroupTimelineIntoTurns(entries, checkpoints)
	require.Len(t, turns, 3)

	// Prompt turn has no checkpoint
	assert.False(t, turns[0].Restorable)
	assert.Equal(t, 0, turns[0].CheckpointID)

	// First assistant turn gets checkpoint absorbed from the event after it
	assert.True(t, turns[1].Restorable)
	assert.Equal(t, 1, turns[1].CheckpointID)

	// Second assistant turn inherits the latest checkpoint
	assert.True(t, turns[2].Restorable)
	assert.Equal(t, 1, turns[2].CheckpointID)
}

func TestGroupTimelineIntoTurns_SystemEventsFiltered(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-0", Kind: "session_start", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{
			"model": "claude-3-opus", "cwd": "/sandbox",
		}},
		{EntryID: "e-1", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:01Z", Data: map[string]interface{}{
			"text": "Fix the bug",
		}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Done",
		}},
		{EntryID: "e-3", Kind: "final", Timestamp: "2026-02-05T11:51:00Z", Data: map[string]interface{}{
			"duration_ms": float64(60000), "success": true,
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)

	// session_start, final, raw_log_coverage should not appear as turns
	kinds := make([]TurnKind, len(turns))
	for i, turn := range turns {
		kinds[i] = turn.Kind
	}
	assert.NotContains(t, kinds, TurnKind("session_start"))
	assert.NotContains(t, kinds, TurnKind("final"))
	assert.Contains(t, kinds, TurnPrompt)
	assert.Contains(t, kinds, TurnAssistant)
}

func TestGroupTimelineIntoTurns_InvocationEventAbsorbed(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Working on it",
		}},
		{EntryID: "ie-1", Kind: "invocation_event", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"event_kind": "agency.some_event",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	assert.Equal(t, TurnAssistant, turns[0].Kind)
}

func TestGroupTimelineIntoTurns_RawLogCoverageAbsorbed(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Working",
		}},
		{EntryID: "r-1", Kind: "raw_log_coverage", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"bytes": float64(1024),
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
}

func TestGroupTimelineIntoTurns_MultipleAssistantTurns(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{"text": "Fix it"}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "First response",
		}},
		{EntryID: "e-3", Kind: "tool_use", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"name": "Read", "command": "main.go",
		}},
		{EntryID: "e-4", Kind: "message", Timestamp: "2026-02-05T11:50:12Z", Data: map[string]interface{}{
			"role": "user", "text": "file contents",
		}},
		{EntryID: "cp-1", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:13Z", Data: map[string]interface{}{
			"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(1),
		}},
		{EntryID: "e-5", Kind: "message", Timestamp: "2026-02-05T11:50:20Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Second response",
		}},
		{EntryID: "e-6", Kind: "tool_use", Timestamp: "2026-02-05T11:50:21Z", Data: map[string]interface{}{
			"name": "Write", "command": "main.go",
		}},
		{EntryID: "e-7", Kind: "message", Timestamp: "2026-02-05T11:50:22Z", Data: map[string]interface{}{
			"role": "user", "text": "written",
		}},
		{EntryID: "cp-2", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:23Z", Data: map[string]interface{}{
			"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(2),
		}},
	}
	checkpoints := []CheckpointRef{{ID: 1}, {ID: 2}}
	turns := GroupTimelineIntoTurns(entries, checkpoints)

	require.Len(t, turns, 3) // prompt + 2 assistant turns
	assert.Equal(t, TurnPrompt, turns[0].Kind)

	assert.Equal(t, "First response", turns[1].Summary)
	require.Len(t, turns[1].ToolCalls, 1)
	assert.Equal(t, "Read", turns[1].ToolCalls[0].Name)

	assert.Equal(t, "Second response", turns[2].Summary)
	require.Len(t, turns[2].ToolCalls, 1)
	assert.Equal(t, "Write", turns[2].ToolCalls[0].Name)
	assert.Equal(t, 2, turns[2].CheckpointID)
}

func TestGroupTimelineIntoTurns_ShortTimestamp(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "prompt_seed", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{"text": "Fix it"}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:45Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Done",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 2)

	// ShortTimestamp should show HH:MM:SS, not the full RFC3339
	assert.Equal(t, "11:50:00", turns[0].ShortTimestamp)
	assert.Equal(t, "11:50:45", turns[1].ShortTimestamp)
}

func TestGroupTimelineIntoTurns_ErrorEntryFiltered(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Working",
		}},
		{EntryID: "e-2", Kind: "error", Timestamp: "2026-02-05T11:50:20Z", Data: map[string]interface{}{
			"message": "rate limit exceeded",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	// error events should not produce their own turn
	require.Len(t, turns, 1)
	assert.Equal(t, TurnAssistant, turns[0].Kind)
}

func TestGroupTimelineIntoTurns_ToolUseWithoutExitCode(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Let me check",
		}},
		{EntryID: "e-2", Kind: "tool_use", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"name": "Read", "command": "main.go",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	require.Len(t, turns[0].ToolCalls, 1)
	assert.False(t, turns[0].ToolCalls[0].HasExit)
}

func TestGroupTimelineIntoTurns_ToolStartAndEndMergeIntoSingleToolCall(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Running command",
		}},
		{EntryID: "e-2", Kind: "tool_use", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"name": "Bash", "command": "echo hi", "in_progress": true,
		}},
		{EntryID: "e-3", Kind: "tool_use", Timestamp: "2026-02-05T11:50:12Z", Data: map[string]interface{}{
			"name": "Bash", "command": "echo hi", "exit_code": float64(0),
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	require.Len(t, turns[0].ToolCalls, 1, "tool start + tool end should merge")
	assert.Equal(t, "echo hi", turns[0].ToolCalls[0].Command)
	assert.True(t, turns[0].ToolCalls[0].HasExit)
	assert.Equal(t, 0, turns[0].ToolCalls[0].ExitCode)
}

func TestGroupTimelineIntoTurns_RepeatedInProgressToolUpdatesCollapseByID(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Running command",
		}},
		{EntryID: "e-2", Kind: "tool_use", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"tool_id": "item_1", "name": "Bash", "command": "sh -lc probe", "in_progress": true,
		}},
		{EntryID: "e-3", Kind: "tool_use", Timestamp: "2026-02-05T11:50:12Z", Data: map[string]interface{}{
			"tool_id": "item_1", "name": "Bash", "command": "sh -lc probe", "in_progress": true,
		}},
		{EntryID: "e-4", Kind: "tool_use", Timestamp: "2026-02-05T11:50:13Z", Data: map[string]interface{}{
			"tool_id": "item_1", "name": "Bash", "command": "sh -lc probe", "exit_code": float64(0),
		}},
	}

	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	require.Len(t, turns[0].ToolCalls, 1, "repeated Codex updates should keep one tool row")
	assert.Equal(t, "item_1", turns[0].ToolCalls[0].ID)
	assert.Equal(t, "sh -lc probe", turns[0].ToolCalls[0].Command)
	assert.True(t, turns[0].ToolCalls[0].HasExit)
	assert.Equal(t, 0, turns[0].ToolCalls[0].ExitCode)
}

func TestGroupTimelineIntoTurns_ParseErrorIncludedAsDiagnosticTurn(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Working",
		}},
		{EntryID: "e-2", Kind: "parse_error", Timestamp: "2026-02-05T11:50:20Z", Data: map[string]interface{}{
			"reason": "line_too_large",
			"line":   float64(3),
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 2)
	assert.Equal(t, TurnAssistant, turns[1].Kind)
	assert.Contains(t, turns[1].Summary, "parse error")
	assert.Contains(t, turns[1].Summary, "line too large")
}

func TestGroupTimelineIntoTurns_UnknownEventIncludedAsDiagnosticTurn(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "working",
		}},
		{EntryID: "e-2", Kind: "unknown", Timestamp: "2026-02-05T11:50:20Z", Data: map[string]interface{}{
			"runner_event_type": "cursor.weird_event",
			"reason":            "unrecognized_event_shape",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 2)
	assert.Equal(t, TurnAssistant, turns[1].Kind)
	assert.Contains(t, turns[1].Summary, "unknown runner event")
	assert.Contains(t, turns[1].Summary, "cursor.weird_event")
}

func TestGroupTimelineIntoTurns_UnrecognizedKindIncludedAsDiagnosticTurn(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "working",
		}},
		{EntryID: "e-2", Kind: "runner_notice", Timestamp: "2026-02-05T11:50:20Z", Data: map[string]interface{}{
			"text": "unexpected status payload",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 2)
	assert.Equal(t, TurnAssistant, turns[1].Kind)
	assert.Contains(t, turns[1].Summary, "unrecognized timeline event")
	assert.Contains(t, turns[1].Summary, "runner notice")
}

func TestGroupTimelineIntoTurns_ToolUseBeforeAssistantDropped(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "tool_use", Timestamp: "2026-02-05T11:50:00Z", Data: map[string]interface{}{
			"name": "Read", "command": "main.go",
		}},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Done",
		}},
	}
	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	assert.Equal(t, TurnAssistant, turns[0].Kind)
	assert.Empty(t, turns[0].ToolCalls, "tool_use before any assistant turn should be dropped")
}

func TestGroupTimelineIntoTurns_CheckpointNotInListIgnored(t *testing.T) {
	t.Parallel()
	entries := []TimelineEntry{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z", Data: map[string]interface{}{
			"role": "assistant", "text": "Done",
		}},
		{EntryID: "cp-1", Kind: "checkpoint_event", Timestamp: "2026-02-05T11:50:11Z", Data: map[string]interface{}{
			"event_kind": "agency.checkpoint_created", "checkpoint_id": float64(99),
		}},
	}
	// Checkpoint 99 is not in the valid list — should not make turn restorable.
	turns := GroupTimelineIntoTurns(entries, []CheckpointRef{{ID: 1}})
	require.Len(t, turns, 1)
	assert.False(t, turns[0].Restorable)
	assert.Equal(t, 0, turns[0].CheckpointID)
}

func TestGroupTimelineIntoTurns_AssistantSummarySynthesizedFromContentBlocks(t *testing.T) {
	t.Parallel()

	entries := []TimelineEntry{
		{
			EntryID:   "e-1",
			Kind:      "message",
			Timestamp: "2026-02-05T11:50:10Z",
			Data:      map[string]interface{}{"role": "assistant", "text": "", "content_blocks": []interface{}{map[string]interface{}{"type": "text", "text": "Updated auth middleware"}}},
		},
	}

	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	assert.Equal(t, "Updated auth middleware", turns[0].Summary)
}

func TestGroupTimelineIntoTurns_SparseSummaryIncludesToolHintsAndCheckpointMetadata(t *testing.T) {
	t.Parallel()

	entries := []TimelineEntry{
		{
			EntryID:   "e-1",
			Kind:      "message",
			Timestamp: "2026-02-05T11:50:10Z",
			Data: map[string]interface{}{
				"role":       "assistant",
				"text":       "Done.",
				"tool_names": []interface{}{"Edit", "Write"},
				"content_blocks": []interface{}{
					map[string]interface{}{"type": "tool_use", "name": "Edit"},
					map[string]interface{}{"type": "tool_use", "name": "Write"},
				},
			},
		},
		{
			EntryID:   "cp-1",
			Kind:      "checkpoint_event",
			Timestamp: "2026-02-05T11:50:11Z",
			Data: map[string]interface{}{
				"event_kind":    "agency.checkpoint_created",
				"checkpoint_id": float64(1),
			},
		},
	}
	checkpoints := []CheckpointRef{
		{
			ID:                   1,
			Description:          "Applied auth file edits",
			Diffstat:             "2 files changed, 12 insertions(+), 3 deletions(-)",
			ChangedPaths:         []string{"internal/auth/middleware.go", "internal/auth/middleware_test.go"},
			ChangedPathCount:     2,
			ChangedPathTruncated: false,
		},
	}

	turns := GroupTimelineIntoTurns(entries, checkpoints)
	require.Len(t, turns, 1)
	assert.Contains(t, turns[0].Summary, "Done.")
	assert.Contains(t, turns[0].Summary, "tools: Edit, Write")
	assert.Contains(t, turns[0].Summary, "Applied auth file edits")
	assert.Contains(t, turns[0].Summary, "2 files changed")
	assert.Equal(t, []string{"internal/auth/middleware.go", "internal/auth/middleware_test.go"}, turns[0].CheckpointChangedPaths)
	assert.Equal(t, 2, turns[0].CheckpointChangedCount)
	assert.False(t, turns[0].CheckpointPathsTrimmed)
}

func TestGroupTimelineIntoTurns_EmptyAssistantSummaryFallsBackToTools(t *testing.T) {
	t.Parallel()

	entries := []TimelineEntry{
		{
			EntryID:   "e-1",
			Kind:      "message",
			Timestamp: "2026-02-05T11:50:10Z",
			Data: map[string]interface{}{
				"role":       "assistant",
				"text":       "",
				"tool_names": []interface{}{"Write", "Read"},
			},
		},
	}

	turns := GroupTimelineIntoTurns(entries, nil)
	require.Len(t, turns, 1)
	assert.Equal(t, "tools: Write, Read", turns[0].Summary)
}

func TestGroupTimelineIntoTurns_SystemMessageIgnored(t *testing.T) {
	t.Parallel()

	entries := []TimelineEntry{
		{
			EntryID:   "e-1",
			Kind:      "message",
			Timestamp: "2026-02-05T11:50:10Z",
			Data: map[string]interface{}{
				"role": "system",
				"text": "internal system note",
			},
		},
	}

	turns := GroupTimelineIntoTurns(entries, nil)
	assert.Empty(t, turns)
}

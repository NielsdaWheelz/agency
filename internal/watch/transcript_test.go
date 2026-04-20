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
)

func loadTranscriptModel(t *testing.T) model {
	t.Helper()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invocations/inv-1/timeline":
			writeDaemonOK(t, w, daemon.InvocationTimelineData{
				Entries: []daemon.TimelineEntryDTO{
					{
						EntryID:   "prompt:1",
						Kind:      "prompt_seed",
						Timestamp: "2026-04-19T17:50:00Z",
						Data: map[string]interface{}{
							"text": "Fix the watch transcript view",
						},
					},
					{
						EntryID:   "stream:2",
						Kind:      "message",
						Timestamp: "2026-04-19T17:50:10Z",
						Data: map[string]interface{}{
							"role": "assistant",
							"text": "I will inspect the watch package",
						},
					},
					{
						EntryID:   "tool:3",
						Kind:      "tool_use",
						Timestamp: "2026-04-19T17:50:12Z",
						Data: map[string]interface{}{
							"tool_id": "tool-1",
							"name":    "Bash",
							"command": "go test ./internal/watch",
						},
					},
					{
						EntryID:   "tool:4",
						Kind:      "tool_use",
						Timestamp: "2026-04-19T17:50:13Z",
						Data: map[string]interface{}{
							"tool_id":   "tool-1",
							"name":      "Bash",
							"command":   "go test ./internal/watch",
							"exit_code": float64(0),
						},
					},
					{
						EntryID:   "cp:5",
						Kind:      "checkpoint_event",
						Timestamp: "2026-04-19T17:50:14Z",
						Data: map[string]interface{}{
							"checkpoint_id": float64(1),
						},
					},
					{
						EntryID:   "followup:6",
						Kind:      "followup_prompt",
						Timestamp: "2026-04-19T17:50:20Z",
						Data: map[string]interface{}{
							"text": "Add focused transcript tests",
						},
					},
					{
						EntryID:   "stream:7",
						Kind:      "message",
						Timestamp: "2026-04-19T17:50:30Z",
						Data: map[string]interface{}{
							"role": "assistant",
							"text": "Added transcript coverage for the history page",
						},
					},
				},
			})
		case "/invocations/inv-1/checkpoints":
			writeDaemonOK(t, w, daemon.ListCheckpointsData{
				Checkpoints: []daemon.CheckpointDTO{
					{
						ID:                   1,
						Description:          "checkpoint after transcript edits",
						Diffstat:             "1 file changed",
						ChangedPaths:         []string{"internal/watch/transcript_test.go"},
						ChangedPathCount:     1,
						ChangedPathTruncated: false,
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	m := newModel(context.Background(), client, RunOptions{InitialPage: InitialPageHistory, InvocationID: "inv-1", RepoID: "repo-1"})
	m.page = pageHistory
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	cmd := m.loadHistoryCmd()
	require.NotNil(t, cmd)

	msg := cmd()
	next, _ := m.Update(msg)
	nextModel := next.(model)
	nextModel.width = 140
	nextModel.height = 40
	return nextModel
}

func TestTranscriptPage_RenderedFromLoadedTimelineEntries(t *testing.T) {
	t.Parallel()

	m := loadTranscriptModel(t)
	next, cmd := m.Update(tea.KeyPressMsg{Text: "t"})
	require.NotNil(t, cmd)

	msg := cmd()
	next, _ = next.(model).Update(msg)
	nextModel := next.(model)
	nextModel.width = 140
	nextModel.height = 40

	view := nextModel.View()

	assert.Contains(t, view.Content, "Fix the watch transcript view")
	assert.Contains(t, view.Content, "I will inspect the watch package")
	assert.Contains(t, view.Content, "Add focused transcript tests")
	assert.Contains(t, view.Content, "Added transcript coverage for the history page")
	assert.Contains(t, view.Content, "Prompt")
	assert.Contains(t, view.Content, "Assistant")
	assert.Contains(t, view.Content, "User")
	assert.Contains(t, view.Content, "Bash")
	assert.Contains(t, view.Content, "go test ./internal/watch")
	assert.Contains(t, view.Content, "[checkpoint_event]")
	assert.Contains(t, view.Content, "attach")
	assert.Contains(t, view.Content, "quit")
	assert.Equal(t, pageTranscript, nextModel.page)
	assert.False(t, nextModel.transcriptLoading)
	assert.Empty(t, nextModel.transcriptError)
}

func TestTranscriptPage_LoadedTimelineEntriesPreserveSelectedEntryID(t *testing.T) {
	t.Parallel()

	m := loadTranscriptModel(t)

	require.Len(t, m.historyTurns, 4)
	assert.Equal(t, "prompt:1", m.historyTurns[0].EntryID)
	assert.Equal(t, "stream:2", m.historyTurns[1].EntryID)
	assert.Equal(t, "followup:6", m.historyTurns[2].EntryID)
	assert.Equal(t, "stream:7", m.historyTurns[3].EntryID)
	assert.Equal(t, 3, m.historySelectedIndex)
	assert.Equal(t, "stream:7", m.historySelectedEntryID)
}

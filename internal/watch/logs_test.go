package watch

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
)

func loadLogsPageForMode(t *testing.T, mode string) (string, model) {
	t.Helper()

	requestKinds := make(chan string, 1)
	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invocations/inv-1/logs":
			requestKinds <- r.URL.Query().Get("kind")
			writeDaemonOK(t, w, daemon.InvocationLogsOffsetData{
				Kind:       r.URL.Query().Get("kind"),
				DataB64:    base64.StdEncoding.EncodeToString([]byte(mode + " logs")),
				NextOffset: int64(len(mode) + len(" logs")),
				TotalBytes: int64(len(mode) + len(" logs")),
			})
		default:
			http.NotFound(w, r)
		}
	})))

	m := newModel(context.Background(), client, RunOptions{})
	m.snapshot = snapshot{
		Invocations: []daemon.InvocationDTO{
			{InvocationID: "inv-1", RepoID: "repo-1", Mode: mode},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"

	next, cmd := m.Update(tea.KeyPressMsg{Text: "l"})
	require.NotNil(t, cmd)

	msg := cmd()
	next, _ = next.(model).Update(msg)
	nextModel := next.(model)

	select {
	case kind := <-requestKinds:
		return kind, nextModel
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for logs request")
		return "", model{}
	}
}

func TestLogsPage_HeadedInvocationUsesTerminalLogsByDefault(t *testing.T) {
	t.Parallel()

	kind, nextModel := loadLogsPageForMode(t, "headed")

	assert.Equal(t, daemon.InvocationLogKindTerminal, kind)
	assert.Equal(t, pageLogs, nextModel.page)
	assert.Equal(t, "headed logs", nextModel.logsContent)
	assert.False(t, nextModel.logsLoading)
	assert.Empty(t, nextModel.logsError)
}

func TestLogsPage_HeadlessInvocationUsesRawLogsByDefault(t *testing.T) {
	t.Parallel()

	kind, nextModel := loadLogsPageForMode(t, "headless")

	assert.Equal(t, daemon.InvocationLogKindRaw, kind)
	assert.Equal(t, pageLogs, nextModel.page)
	assert.Equal(t, "headless logs", nextModel.logsContent)
	assert.False(t, nextModel.logsLoading)
	assert.Empty(t, nextModel.logsError)
}

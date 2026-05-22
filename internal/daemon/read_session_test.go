package daemon

import (
	stderrors "errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func TestHandleGetInvocationSession_LiveHeaded(t *testing.T) {
	t.Parallel()
	fakeTmux := testutil.NewFakeTmuxClient()
	env := setupReadTestEnv(t, fakeTmux)

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-2", func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.FinishedAt = ""
		meta.LandingStatus = ""
	}))

	sessionName := tmux.SessionName("inv-2")
	fakeTmux.Sessions[sessionName] = testutil.FakeTmuxSession{
		Name: sessionName,
		Clients: []tmux.AttachedClient{
			{Name: "client-1", TTY: "/dev/ttys001", PID: 101, ReadOnly: false},
			{Name: "client-2", TTY: "/dev/ttys002", PID: 202, ReadOnly: true},
		},
	}

	resp := decodeAPIResponse(t, env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-2/session?repo_id="+env.RepoID))
	require.True(t, resp.OK)

	var data InvocationSessionData
	decodeData(t, resp, &data)

	assert.Equal(t, "inv-2", data.InvocationID)
	assert.Equal(t, env.RepoID, data.RepoID)
	assert.Equal(t, "live", data.SessionStatus)
	assert.Equal(t, sessionName, data.TmuxSession)
	assert.Equal(t, 2, data.ClientCount)
	assert.Equal(t, []InvocationSessionClient{
		{Name: "client-1", TTY: "/dev/ttys001", PID: 101, ReadOnly: false},
		{Name: "client-2", TTY: "/dev/ttys002", PID: 202, ReadOnly: true},
	}, data.ConnectedClients)
	assert.Equal(t, "agency agent inv-2 attach --repo "+env.RepoID, data.AttachCommand)
	assert.False(t, data.RecreateAvailable)
}

func TestHandleGetInvocationSession_MissingHeaded_RecreateAvailable(t *testing.T) {
	t.Parallel()
	fakeTmux := testutil.NewFakeTmuxClient()
	env := setupReadTestEnv(t, fakeTmux)

	sandboxDir := t.TempDir()
	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-2", func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.FinishedAt = ""
		meta.LandingStatus = ""
		meta.SandboxPath = sandboxDir
	}))

	resp := decodeAPIResponse(t, env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-2/session?repo_id="+env.RepoID))
	require.True(t, resp.OK)

	var data InvocationSessionData
	decodeData(t, resp, &data)

	assert.Equal(t, "missing", data.SessionStatus)
	assert.Equal(t, tmux.SessionName("inv-2"), data.TmuxSession)
	assert.Equal(t, 0, data.ClientCount)
	assert.Empty(t, data.ConnectedClients)
	assert.Empty(t, data.AttachCommand)
	assert.True(t, data.RecreateAvailable)
}

func TestHandleGetInvocationSession_MissingHeaded_RecreateUnavailableWhenLanded(t *testing.T) {
	t.Parallel()
	fakeTmux := testutil.NewFakeTmuxClient()
	env := setupReadTestEnv(t, fakeTmux)

	resp := decodeAPIResponse(t, env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-2/session?repo_id="+env.RepoID))
	require.True(t, resp.OK)

	var data InvocationSessionData
	decodeData(t, resp, &data)

	assert.Equal(t, "missing", data.SessionStatus)
	assert.False(t, data.RecreateAvailable)
	assert.Empty(t, data.AttachCommand)
}

func TestHandleGetInvocationSession_HeadlessRejected(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t)

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-1/session?repo_id="+env.RepoID)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvocationInvalidMode), resp.ErrorCode)
	assert.Contains(t, resp.Message, "headed invocations")
}

func TestHandleGetInvocationSession_TmuxFailure(t *testing.T) {
	t.Parallel()
	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.ListClientsErr = stderrors.New("connection refused")
	env := setupReadTestEnv(t, fakeTmux)

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-2", func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.FinishedAt = ""
		meta.LandingStatus = ""
	}))

	sessionName := tmux.SessionName("inv-2")
	fakeTmux.Sessions[sessionName] = testutil.FakeTmuxSession{Name: sessionName}

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-2/session?repo_id="+env.RepoID)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.ETmuxFailed), resp.ErrorCode)
	assert.Contains(t, resp.Message, "failed to inspect tmux clients")
}

func TestHandleGetInvocationSession_MissingPersistedSessionRejected(t *testing.T) {
	t.Parallel()
	env := setupReadTestEnv(t, testutil.NewFakeTmuxClient())

	require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-2", func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.FinishedAt = ""
		meta.LandingStatus = ""
		meta.TmuxSession = ""
	}))

	w := env.doInvocationRequest(t, http.MethodGet, "/invocations/inv-2/session?repo_id="+env.RepoID)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	resp := decodeAPIResponse(t, w)
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.ETmuxSessionMissing), resp.ErrorCode)
}

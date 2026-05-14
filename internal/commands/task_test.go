package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestTaskStartHeadlessPromptRequiredBeforeDaemon(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskStart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskStartOpts{
		Name: "feature",
		Mode: "headless",
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EPromptRequired, errors.GetCode(err))
}

func TestTaskStartHeadedPromptRejectedBeforeDaemon(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskStart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskStartOpts{
		Name:       "feature",
		Mode:       "headed",
		Prompt:     "do it",
		Detached:   true,
		Runner:     "claude-code",
		BaseBranch: "main",
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestTaskStartInvalidModeBeforeDaemon(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskStart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskStartOpts{
		Name:   "feature",
		Mode:   "bogus",
		Prompt: "do it",
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestTaskRetryHeadedPromptRejectedBeforeDaemon(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskRetry(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskRetryOpts{
		TaskRef:  "task-1",
		Mode:     "headed",
		Prompt:   "do it",
		Detached: true,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestTaskRetryChecksAPIVersionBeforeRetryMutation(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "tr")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0o755))

	healthCount := 0
	taskRead := false
	healthAfterTaskRead := false
	retryCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/health":
			healthCount++
			apiVersion := daemon.APIVersion
			if taskRead {
				healthAfterTaskRead = true
				apiVersion = daemon.APIVersion + 1
			}
			_ = json.NewEncoder(w).Encode(daemon.HealthResponse{OK: true, APIVersion: apiVersion})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/repo-1":
			_ = json.NewEncoder(w).Encode(daemon.APIResponse{
				OK:         true,
				APIVersion: daemon.APIVersion,
				RequestID:  "req-repo",
				Data: daemon.RepoDTO{
					RepoID:                  "repo-1",
					PreferredRoot:           dataDir,
					PreferredRootAccessible: true,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/task-1":
			taskRead = true
			_ = json.NewEncoder(w).Encode(daemon.APIResponse{
				OK:         true,
				APIVersion: daemon.APIVersion,
				RequestID:  "req-task",
				Data: daemon.TaskDTO{
					TaskID: "task-1",
					RepoID: "repo-1",
					Mode:   store.RunnerModeHeadless,
					Runner: "claude-code",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/tasks/task-1/retry":
			retryCalled = true
			_ = json.NewEncoder(w).Encode(daemon.TaskStartResponse{
				OK:         true,
				APIVersion: daemon.APIVersion,
				TaskID:     "task-1",
			})
		default:
			http.NotFound(w, r)
		}
	})
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	srv := &http.Server{Handler: handler}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	})

	var stdout, stderr bytes.Buffer
	err = TaskRetry(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", TaskRetryOpts{
		TaskRef: "task-1",
		RepoRef: "repo-1",
		Mode:    "headless",
		Prompt:  "retry prompt",
	}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EDaemonIncompatible, errors.GetCode(err))
	assert.False(t, retryCalled)
	assert.True(t, healthAfterTaskRead)
	assert.GreaterOrEqual(t, healthCount, 2)
}

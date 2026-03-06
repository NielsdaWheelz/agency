package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemon/servicemanager"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortTempDir creates a temp dir with a short path to stay under the
// ~104-byte Unix socket path limit on macOS.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "agd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startFakeHealthServer starts an HTTP server on the given Unix socket
// that responds to GET /health with a valid HealthResponse.
func startFakeHealthServer(t *testing.T, socketPath string, pid int) {
	t.Helper()
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	require.NoError(t, os.Chmod(socketPath, 0o600))

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(daemon.HealthResponse{
			OK:               true,
			APIVersion:       1,
			PID:              pid,
			DaemonInstanceID: "test-instance-id",
			UptimeSeconds:    42,
		})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
}

// --- Tier 1: Background mode (Caddy pattern) ---

func TestDaemonStart_BackgroundMode_AlreadyRunning(t *testing.T) {
	t.Parallel()
	dataDir := shortTempDir(t)
	socketPath := filepath.Join(dataDir, "agencyd.sock")
	pidPath := filepath.Join(dataDir, "agencyd.pid")

	// Pre-condition: daemon is running (PID alive + health endpoint responds)
	require.NoError(t, os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644))
	startFakeHealthServer(t, socketPath, os.Getpid())

	var stdout, stderr bytes.Buffer
	err := DaemonStart(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), DaemonStartOpts{
		Foreground:      false,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)

	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "already running")
}

func TestDaemonStart_BackgroundMode_Success(t *testing.T) {
	t.Parallel()
	dataDir := shortTempDir(t)
	socketPath := filepath.Join(dataDir, "agencyd.sock")

	var stdout, stderr bytes.Buffer
	err := DaemonStart(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), DaemonStartOpts{
		Foreground:      false,
		DataDirOverride: dataDir,
		backgroundStartFn: func(logPath string) error {
			// Simulate background daemon starting by creating a fake health server.
			startFakeHealthServer(t, socketPath, 12345)
			return nil
		},
	}, &stdout, &stderr)

	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Agency daemon started")
	assert.Contains(t, stdout.String(), "12345")
	assert.Contains(t, stdout.String(), "Socket:")
	assert.Contains(t, stdout.String(), "Instance ID:")
}

func TestDaemonStart_BackgroundMode_StartFails(t *testing.T) {
	t.Parallel()
	dataDir := shortTempDir(t)

	var stdout, stderr bytes.Buffer
	err := DaemonStart(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), DaemonStartOpts{
		Foreground:      false,
		DataDirOverride: dataDir,
		backgroundStartFn: func(logPath string) error {
			return fmt.Errorf("mock: process failed to start")
		},
	}, &stdout, &stderr)

	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonStartFailed, ae.Code)
}

func TestDaemonStart_BackgroundMode_HealthCheckFails(t *testing.T) {
	t.Parallel()
	dataDir := shortTempDir(t)

	var stdout, stderr bytes.Buffer
	err := DaemonStart(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), DaemonStartOpts{
		Foreground:      false,
		DataDirOverride: dataDir,
		backgroundStartFn: func(logPath string) error {
			// Start "succeeds" but no server is actually running.
			return nil
		},
		waitTimeout: 500 * time.Millisecond,
	}, &stdout, &stderr)

	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonStartFailed, ae.Code)
	assert.Contains(t, ae.Msg, "agencyd.log")
}

func TestDaemonStart_BackgroundMode_CleansStalePidFile(t *testing.T) {
	t.Parallel()
	dataDir := shortTempDir(t)
	socketPath := filepath.Join(dataDir, "agencyd.sock")
	pidPath := filepath.Join(dataDir, "agencyd.pid")

	// Write PID file with a dead PID (PID 1 is init, but use a very high PID unlikely to exist)
	require.NoError(t, os.WriteFile(pidPath, []byte("999999999\n"), 0o644))
	// Create a stale socket file (same path the daemon would use)
	require.NoError(t, os.WriteFile(socketPath, nil, 0o600))

	var stdout, stderr bytes.Buffer
	err := DaemonStart(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), DaemonStartOpts{
		Foreground:      false,
		DataDirOverride: dataDir,
		backgroundStartFn: func(logPath string) error {
			startFakeHealthServer(t, socketPath, 54321)
			return nil
		},
	}, &stdout, &stderr)

	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Agency daemon started")
}

func TestDaemonStart_ForegroundMode_AlreadyRunning(t *testing.T) {
	t.Parallel()
	dataDir := shortTempDir(t)
	pidPath := filepath.Join(dataDir, "agencyd.pid")

	// Write PID file with current process PID (alive)
	require.NoError(t, os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644))

	var stdout, stderr bytes.Buffer
	err := DaemonStart(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), DaemonStartOpts{
		Foreground:      true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)

	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "already running")
}

// --- Tier 2: Service manager install/uninstall ---

// fakeManager is a test double for servicemanager.Manager.
type fakeManager struct {
	name            string
	installed       bool
	filePath        string
	installErr      error
	uninstallErr    error
	installCalled   bool
	uninstallCalled bool
}

func (m *fakeManager) Name() string                                          { return m.name }
func (m *fakeManager) ServiceFilePath(_ servicemanager.ServiceConfig) string { return m.filePath }
func (m *fakeManager) IsInstalled(_ servicemanager.ServiceConfig) bool       { return m.installed }

func (m *fakeManager) Install(_ context.Context, _ servicemanager.ServiceConfig) error {
	m.installCalled = true
	return m.installErr
}

func (m *fakeManager) Uninstall(_ context.Context, _ servicemanager.ServiceConfig) error {
	m.uninstallCalled = true
	return m.uninstallErr
}

func TestDaemonInstall_Success(t *testing.T) {
	t.Parallel()
	fm := &fakeManager{name: "launchd", filePath: "/fake/path.plist"}

	var stdout, stderr bytes.Buffer
	err := DaemonInstall(context.Background(), exec.NewRealRunner(), DaemonInstallOpts{
		manager: fm,
	}, &stdout, &stderr)

	require.NoError(t, err)
	assert.True(t, fm.installCalled)
	assert.Contains(t, stdout.String(), "installed")
	assert.Contains(t, stdout.String(), "launchd")
}

func TestDaemonInstall_AlreadyInstalled(t *testing.T) {
	t.Parallel()
	fm := &fakeManager{
		name:       "launchd",
		installed:  true,
		installErr: errors.New(errors.EDaemonServiceAlreadyInstalled, "already installed"),
	}

	var stdout, stderr bytes.Buffer
	err := DaemonInstall(context.Background(), exec.NewRealRunner(), DaemonInstallOpts{
		manager: fm,
	}, &stdout, &stderr)

	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceAlreadyInstalled, ae.Code)
}

func TestDaemonInstall_UnsupportedPlatform(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := DaemonInstall(context.Background(), exec.NewRealRunner(), DaemonInstallOpts{
		detectOS: "freebsd",
	}, &stdout, &stderr)

	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceUnsupported, ae.Code)
}

func TestDaemonUninstall_Success(t *testing.T) {
	t.Parallel()
	fm := &fakeManager{name: "launchd", installed: true, filePath: "/fake/path.plist"}

	var stdout, stderr bytes.Buffer
	err := DaemonUninstall(context.Background(), exec.NewRealRunner(), DaemonUninstallOpts{
		manager: fm,
	}, &stdout, &stderr)

	require.NoError(t, err)
	assert.True(t, fm.uninstallCalled)
	assert.Contains(t, stdout.String(), "uninstalled")
}

func TestDaemonUninstall_NotInstalled(t *testing.T) {
	t.Parallel()
	fm := &fakeManager{
		name:         "systemd",
		installed:    false,
		uninstallErr: errors.New(errors.EDaemonServiceNotInstalled, "not installed"),
	}

	var stdout, stderr bytes.Buffer
	err := DaemonUninstall(context.Background(), exec.NewRealRunner(), DaemonUninstallOpts{
		manager: fm,
	}, &stdout, &stderr)

	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceNotInstalled, ae.Code)
}

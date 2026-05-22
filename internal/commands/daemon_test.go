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
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
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

func TestDaemonStart_BackgroundMode_AlreadyRunning(t *testing.T) {
	t.Parallel()
	dataDir := shortTempDir(t)
	socketPath := filepath.Join(dataDir, "agencyd.sock")
	pidPath := filepath.Join(dataDir, "agencyd.pid")

	// Pre-condition: daemon is running (PID alive + health endpoint responds)
	require.NoError(t, os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644))
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
			PID:              os.Getpid(),
			DaemonInstanceID: "test-instance-id",
			UptimeSeconds:    42,
		})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	var stdout, stderr bytes.Buffer
	err = DaemonStart(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), DaemonStartOpts{
		Foreground:      false,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)

	require.NoError(t, err)
	if got := stdout.String(); !strings.Contains(got, "already running") {
		t.Fatalf("stdout missing already running message:\n%s", got)
	}
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
	if got := stdout.String(); !strings.Contains(got, "already running") {
		t.Fatalf("stdout missing already running message:\n%s", got)
	}
}

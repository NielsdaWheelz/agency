package daemonclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
)

// EnsureDaemonRunning ensures the daemon is running, starting it if necessary.
// Returns a client connected to the running daemon.
func EnsureDaemonRunning(ctx context.Context, socketPath, logPath string) (*Client, error) {
	client := NewClient(socketPath)

	if client.IsRunning(ctx) {
		return client, nil
	}

	if err := StartDaemonBackground(ctx, logPath); err != nil {
		return nil, err
	}

	if err := client.WaitForReady(ctx, 5*time.Second); err != nil {
		return nil, err
	}

	return client, nil
}

// StartDaemonBackground starts the daemon in the background.
func StartDaemonBackground(ctx context.Context, logPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to get executable path", err)
	}
	if strings.HasSuffix(filepath.Base(exePath), ".test") {
		return errors.New(errors.EDaemonStartFailed, "refusing to autostart daemon from Go test binary")
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to open daemon log file", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to open daemon log file", err)
	}

	proc, err := agencyexec.StartProcess(ctx, exePath, []string{"daemon", "start", "--foreground"}, agencyexec.StartOpts{
		Setpgid: true,
		Stdout:  logFile,
		Stderr:  logFile,
	})
	if err != nil {
		_ = logFile.Close()
		return errors.Wrap(errors.EDaemonStartFailed, "failed to start daemon", err)
	}

	_, _ = fmt.Fprintf(logFile, "[autostart] started daemon process (pid %d) at %s\n", proc.PID, time.Now().Format(time.RFC3339))
	_ = logFile.Close()

	return nil
}

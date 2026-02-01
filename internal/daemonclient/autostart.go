package daemonclient

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// EnsureDaemonRunning ensures the daemon is running, starting it if necessary.
// Returns a client connected to the running daemon.
func EnsureDaemonRunning(ctx context.Context, socketPath, logPath string) (*Client, error) {
	client := NewClient(socketPath)

	// Check if daemon is already running
	if client.IsRunning(ctx) {
		return client, nil
	}

	// Daemon not running - start it
	if err := StartDaemonBackground(logPath); err != nil {
		return nil, err
	}

	// Wait for daemon to be ready
	if err := client.WaitForReady(ctx, 5*time.Second); err != nil {
		return nil, err
	}

	return client, nil
}

// AutoStartDaemon is a convenience function that starts the daemon using standard paths.
// PR-06: Used by CLI commands that need daemon before proceeding.
func AutoStartDaemon(ctx context.Context, dataDir string) error {
	logPath := dataDir + "/agencyd.log"
	return StartDaemonBackground(logPath)
}

// StartDaemonBackground starts the daemon in the background.
func StartDaemonBackground(logPath string) error {
	// Get the current executable path
	exePath, err := os.Executable()
	if err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to get executable path", err)
	}

	// Open log file for daemon output
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to open daemon log file", err)
	}

	// Create command to start daemon
	cmd := osexec.Command(exePath, "daemon", "start")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group so daemon survives CLI exit
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Start the daemon (do NOT Wait - we want it to continue running)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return errors.Wrap(errors.EDaemonStartFailed, "failed to start daemon", err)
	}

	// Log startup info
	_, _ = fmt.Fprintf(logFile, "[autostart] started daemon process (pid %d) at %s\n", cmd.Process.Pid, time.Now().Format(time.RFC3339))

	// Close the log file - daemon will continue using it
	_ = logFile.Close()

	return nil
}

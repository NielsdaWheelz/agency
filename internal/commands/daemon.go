// Package commands implements agency CLI commands.
// This file implements daemon commands (Slice 8 PR-04).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// DaemonStartOpts holds options for the daemon start command.
type DaemonStartOpts struct {
	// Foreground runs the daemon in the foreground (default behavior).
	Foreground bool
}

// DaemonStart starts the agency daemon.
// This runs the daemon server loop in the current process (foreground).
func DaemonStart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts DaemonStartOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	pidPath := st.DaemonPidPath()

	// Check for existing daemon
	existingPid, err := daemon.ReadPidFile(pidPath)
	if err == nil {
		// PID file exists, check if daemon is alive
		if daemon.IsPIDAlive(existingPid) {
			// Daemon is already running
			_, _ = fmt.Fprintf(stdout, "Daemon is already running (pid %d)\n", existingPid)
			return nil
		}
		// Stale PID file - clean up
		_ = daemon.RemovePidFile(pidPath)
		_ = daemon.RemoveSocketFile(socketPath)
	}

	// Clean up any stale socket file
	_ = os.Remove(socketPath)

	// Create socket listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to create socket: "+err.Error(), err)
	}
	defer func() { _ = listener.Close() }()

	// Set socket permissions to 0600 (owner-only)
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to set socket permissions: "+err.Error(), err)
	}

	// Write PID file
	if err := daemon.WritePidFile(pidPath); err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to write PID file: "+err.Error(), err)
	}

	// Clean up on exit
	defer func() {
		_ = daemon.RemovePidFile(pidPath)
		_ = daemon.RemoveSocketFile(socketPath)
	}()

	// Create the daemon server
	server := daemon.NewServer(st, cr, fsys, dirs.ConfigDir)

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		_, _ = fmt.Fprintf(stderr, "\nReceived shutdown signal, stopping daemon...\n")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	_, _ = fmt.Fprintf(stdout, "Agency daemon started (pid %d)\n", os.Getpid())
	_, _ = fmt.Fprintf(stdout, "Socket: %s\n", socketPath)
	_, _ = fmt.Fprintf(stdout, "Instance ID: %s\n", server.InstanceID)

	// Run the server
	err = server.Serve(listener)
	if err != nil && err.Error() != "http: Server closed" {
		return errors.Wrap(errors.EInternal, "daemon server error: "+err.Error(), err)
	}

	_, _ = fmt.Fprintf(stdout, "Daemon stopped\n")
	return nil
}

// DaemonStatusOpts holds options for the daemon status command.
type DaemonStatusOpts struct {
	// JSON outputs as JSON.
	JSON bool
}

// DaemonStatus shows the daemon status.
func DaemonStatus(ctx context.Context, fsys fs.FS, opts DaemonStatusOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()

	client := daemonclient.NewClient(socketPath)

	health, err := client.Health(ctx)
	if err != nil {
		return errors.Wrap(errors.EDaemonNotRunning, "daemon is not running", err)
	}

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(health)
	}

	_, _ = fmt.Fprintf(stdout, "Daemon is running\n")
	_, _ = fmt.Fprintf(stdout, "  PID:           %d\n", health.PID)
	_, _ = fmt.Fprintf(stdout, "  Instance ID:   %s\n", health.DaemonInstanceID)
	_, _ = fmt.Fprintf(stdout, "  API Version:   %d\n", health.APIVersion)
	_, _ = fmt.Fprintf(stdout, "  Build Version: %s\n", health.BuildVersion)
	_, _ = fmt.Fprintf(stdout, "  Uptime:        %ds\n", health.UptimeSeconds)

	return nil
}

// DaemonStopOpts holds options for the daemon stop command.
type DaemonStopOpts struct {
	// Force terminates all active invocations before stopping.
	Force bool
}

// DaemonStop stops the daemon.
func DaemonStop(ctx context.Context, fsys fs.FS, opts DaemonStopOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	pidPath := st.DaemonPidPath()

	client := daemonclient.NewClient(socketPath)

	// Try RPC shutdown first
	resp, err := client.Shutdown(ctx, opts.Force)
	if err == nil {
		if resp.OK {
			_, _ = fmt.Fprintf(stdout, "Daemon shutdown initiated\n")
			return nil
		}
		if resp.ErrorCode == string(errors.EDaemonBusy) {
			_, _ = fmt.Fprintf(stderr, "Daemon has active invocations:\n")
			for _, id := range resp.RunningInvocations {
				_, _ = fmt.Fprintf(stderr, "  - %s\n", id)
			}
			_, _ = fmt.Fprintf(stderr, "\nUse --force to terminate all invocations and stop the daemon.\n")
			return errors.New(errors.EDaemonBusy, resp.Message)
		}
		return errors.New(errors.Code(resp.ErrorCode), resp.Message)
	}

	// RPC failed - fall back to PID file
	_, _ = fmt.Fprintf(stderr, "RPC shutdown failed, falling back to PID file...\n")

	pid, err := daemon.ReadPidFile(pidPath)
	if err != nil {
		return errors.New(errors.EDaemonNotRunning, "daemon is not running (no PID file)")
	}

	if !daemon.IsPIDAlive(pid) {
		// Clean up stale files
		_ = daemon.RemovePidFile(pidPath)
		_ = daemon.RemoveSocketFile(socketPath)
		_, _ = fmt.Fprintf(stdout, "Cleaned up stale daemon files\n")
		return nil
	}

	// Send SIGTERM
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return errors.Wrap(errors.EInternal, "failed to send SIGTERM: "+err.Error(), err)
	}

	// Wait for process to exit (max 5s)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !daemon.IsPIDAlive(pid) {
			_ = daemon.RemovePidFile(pidPath)
			_ = daemon.RemoveSocketFile(socketPath)
			_, _ = fmt.Fprintf(stdout, "Daemon stopped\n")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Still alive - send SIGKILL
	_, _ = fmt.Fprintf(stderr, "Daemon did not stop gracefully, sending SIGKILL...\n")
	_ = syscall.Kill(pid, syscall.SIGKILL)

	// Wait a bit more
	time.Sleep(500 * time.Millisecond)
	_ = daemon.RemovePidFile(pidPath)
	_ = daemon.RemoveSocketFile(socketPath)
	_, _ = fmt.Fprintf(stdout, "Daemon killed\n")

	return nil
}

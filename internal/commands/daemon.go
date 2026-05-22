// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	stderrors "errors"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemon/servicemanager"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// DaemonStartOpts holds options for the daemon start command.
type DaemonStartOpts struct {
	// Foreground runs the daemon in the foreground.
	// When false (default), the daemon is started as a background process.
	Foreground bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// DaemonStart starts the agency daemon.
// When Foreground is true, runs the server loop in the current process.
// When Foreground is false (default), starts a background process and waits
// for it to become healthy before returning.
func DaemonStart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts DaemonStartOpts, stdout, stderr io.Writer) error {
	dataDir, configDir, err := resolveDaemonDirs(opts.DataDirOverride)
	if err != nil {
		return err
	}

	st := store.NewStore(fsys, dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	pidPath := st.DaemonPidPath()

	if opts.Foreground {
		return daemonStartForeground(ctx, cr, fsys, st, configDir, socketPath, pidPath, stdout, stderr)
	}
	return daemonStartBackground(ctx, st, socketPath, pidPath, opts, stdout)
}

// resolveDaemonDirs resolves the data and config directories, with optional override.
// All returned paths are absolute and symlink-resolved per binding rule 6.
func resolveDaemonDirs(dataDirOverride string) (dataDir, configDir string, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(os.Getenv, homeDir)

	dataDir = dirs.DataDir
	if dataDirOverride != "" {
		abs, err := filepath.Abs(dataDirOverride)
		if err != nil {
			return "", "", errors.Wrap(errors.EInternal, "failed to resolve data dir override", err)
		}
		dataDir = abs
	}
	// Resolve DataDir through EvalSymlinks so path comparisons are consistent
	// with EvalSymlinks-resolved repo_root paths in handlers.
	if resolved, err := filepath.EvalSymlinks(dataDir); err == nil {
		dataDir = resolved
	}

	configDir = dirs.ConfigDir
	if resolved, err := filepath.EvalSymlinks(configDir); err == nil {
		configDir = resolved
	}

	return dataDir, configDir, nil
}

// daemonStartForeground runs the daemon server in the current process.
func daemonStartForeground(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, st *store.Store, configDir, socketPath, pidPath string, stdout, stderr io.Writer) error {
	// Check for existing daemon
	existingPid, err := daemon.ReadPidFile(pidPath)
	if err == nil {
		if daemon.IsPIDAlive(existingPid) {
			_, _ = fmt.Fprintf(stdout, "Daemon is already running (pid %d)\n", existingPid)
			return nil
		}
		_ = os.Remove(pidPath)
		_ = os.Remove(socketPath)
	}

	// Clean up any stale socket file
	_ = os.Remove(socketPath)

	// Create socket listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to create socket: "+err.Error(), err)
	}
	defer func() { _ = listener.Close() }()

	if err := os.Chmod(socketPath, 0o600); err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to set socket permissions: "+err.Error(), err)
	}

	if err := daemon.WritePidFile(pidPath); err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to write PID file: "+err.Error(), err)
	}

	defer func() {
		_ = os.Remove(pidPath)
		_ = os.Remove(socketPath)
	}()

	server := daemon.NewServer(st, cr, fsys, configDir)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		_, _ = fmt.Fprintf(stderr, "\nReceived shutdown signal, stopping daemon...\n")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	_, _ = fmt.Fprintf(stdout, "Agency daemon started (pid %d)\n", os.Getpid())
	_, _ = fmt.Fprintf(stdout, "Socket: %s\n", socketPath)
	_, _ = fmt.Fprintf(stdout, "Instance ID: %s\n", server.InstanceID())

	err = server.Serve(listener)
	if err != nil && !stderrors.Is(err, http.ErrServerClosed) {
		return errors.Wrap(errors.EInternal, "daemon server error: "+err.Error(), err)
	}

	_, _ = fmt.Fprintf(stdout, "Daemon stopped\n")
	return nil
}

// daemonStartBackground starts the daemon as a detached background process,
// waits for it to become healthy, then returns.
func daemonStartBackground(ctx context.Context, st *store.Store, socketPath, pidPath string, opts DaemonStartOpts, stdout io.Writer) error {
	client := daemonclient.NewClient(socketPath)

	// Check if daemon is already running via health endpoint.
	if client.IsRunning(ctx) {
		health, err := client.Health(ctx)
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "Daemon is already running (pid %d)\n", health.PID)
			return nil
		}
	}

	// Clean stale PID/socket if the process is dead.
	existingPid, err := daemon.ReadPidFile(pidPath)
	if err == nil && !daemon.IsPIDAlive(existingPid) {
		_ = os.Remove(pidPath)
		_ = os.Remove(socketPath)
	}

	// Start background process.
	logPath := st.DaemonLogPath()
	if err := daemonclient.StartDaemonBackground(ctx, logPath); err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "failed to start daemon in background", err)
	}

	// Wait for the daemon to become healthy.
	if err := client.WaitForReady(ctx, 10*time.Second); err != nil {
		return errors.Wrap(errors.EDaemonStartFailed,
			fmt.Sprintf("daemon process started but failed health check; see %s", logPath), err)
	}

	// Retrieve and display daemon info.
	health, err := client.Health(ctx)
	if err != nil {
		return errors.Wrap(errors.EDaemonStartFailed, "daemon started but health query failed", err)
	}

	_, _ = fmt.Fprintf(stdout, "Agency daemon started (pid %d)\n", health.PID)
	_, _ = fmt.Fprintf(stdout, "Socket: %s\n", socketPath)
	_, _ = fmt.Fprintf(stdout, "Instance ID: %s\n", health.DaemonInstanceID)
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
	dirs := paths.ResolveDirs(os.Getenv, homeDir)

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
	dirs := paths.ResolveDirs(os.Getenv, homeDir)

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

	// RPC failed; use the PID file shutdown path.
	_, _ = fmt.Fprintf(stderr, "RPC shutdown failed, using PID file shutdown...\n")

	pid, err := daemon.ReadPidFile(pidPath)
	if err != nil {
		return errors.New(errors.EDaemonNotRunning, "daemon is not running (no PID file)")
	}

	if !daemon.IsPIDAlive(pid) {
		// Clean up stale files
		_ = os.Remove(pidPath)
		_ = os.Remove(socketPath)
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
			_ = os.Remove(pidPath)
			_ = os.Remove(socketPath)
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
	_ = os.Remove(pidPath)
	_ = os.Remove(socketPath)
	_, _ = fmt.Fprintf(stdout, "Daemon killed\n")

	return nil
}

// DaemonInstallOpts holds options for the daemon install command.
type DaemonInstallOpts struct{}

// DaemonInstall installs the daemon as an OS service (launchd on macOS,
// systemd on Linux).
func DaemonInstall(ctx context.Context, cr exec.CommandRunner, opts DaemonInstallOpts, stdout, stderr io.Writer) error {
	mgr, err := servicemanager.DetectForOS(cr, runtime.GOOS)
	if err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	exePath, err := os.Executable()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get executable path", err)
	}
	// Resolve symlinks so the plist/unit points to the real binary.
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to resolve executable path", err)
	}

	dirs := paths.ResolveDirs(os.Getenv, homeDir)
	cfg := servicemanager.ServiceConfig{
		ExePath: exePath,
		DataDir: dirs.DataDir,
		HomeDir: homeDir,
	}

	if err := mgr.Install(ctx, cfg); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Daemon installed as %s service\n", mgr.Name())
	_, _ = fmt.Fprintf(stdout, "  Service file: %s\n", mgr.ServiceFilePath(cfg))
	_, _ = fmt.Fprintf(stdout, "  Binary:       %s\n", exePath)
	_, _ = fmt.Fprintf(stdout, "\nThe daemon will start automatically on login.\n")
	return nil
}

// DaemonUninstallOpts holds options for the daemon uninstall command.
type DaemonUninstallOpts struct{}

// DaemonUninstall removes the daemon OS service.
func DaemonUninstall(ctx context.Context, cr exec.CommandRunner, opts DaemonUninstallOpts, stdout, stderr io.Writer) error {
	mgr, err := servicemanager.DetectForOS(cr, runtime.GOOS)
	if err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	dirs := paths.ResolveDirs(os.Getenv, homeDir)
	cfg := servicemanager.ServiceConfig{
		DataDir: dirs.DataDir,
		HomeDir: homeDir,
	}

	if err := mgr.Uninstall(ctx, cfg); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Daemon %s service uninstalled\n", mgr.Name())
	return nil
}

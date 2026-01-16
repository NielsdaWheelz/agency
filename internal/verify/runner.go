package verify

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// RunConfig holds the configuration for a verify run.
type RunConfig struct {
	// RepoID is the repository identifier (16 hex chars).
	RepoID string

	// RunID is the unique run identifier.
	RunID string

	// WorkDir is the worktree root directory (cwd for script execution).
	WorkDir string

	// Script is the exact script string to execute (from agency.json).
	Script string

	// Env is the full environment for the script. Caller provides merged env.
	// Verify runner does not modify it.
	Env []string

	// Timeout is the maximum duration for the script. Default 30m if zero.
	Timeout time.Duration

	// LogPath is the absolute path to write verify.log.
	LogPath string

	// VerifyJSONPath is the absolute path to read verify.json (workspace output).
	VerifyJSONPath string

	// RecordPath is the absolute path to write verify_record.json.
	RecordPath string
}

// GracePeriod is the duration to wait between SIGINT and SIGKILL when
// terminating a verify process (timeout or cancellation).
const GracePeriod = 3 * time.Second

// Run executes the verify script and writes the canonical verify_record.json.
//
// The function returns a VerifyRecord (always populated) and an error.
// The error is only returned for internal failures that prevent running or writing:
//   - log file open failure
//   - exec start failure
//   - verify_record.json write failure
//
// Verify failure (non-zero exit, timeout, cancel) is represented in
// VerifyRecord.OK/ExitCode, NOT as a returned error.
func Run(ctx context.Context, cfg RunConfig) (store.VerifyRecord, error) {
	// Default timeout if not specified
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	record := store.VerifyRecord{
		SchemaVersion: "1.0",
		RepoID:        cfg.RepoID,
		RunID:         cfg.RunID,
		ScriptPath:    cfg.Script,
		TimeoutMS:     timeout.Milliseconds(),
		LogPath:       cfg.LogPath,
	}

	// Ensure parent directories exist for log and record
	if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o755); err != nil {
		errStr := fmt.Sprintf("failed to create log directory: %v", err)
		record.Error = &errStr
		return record, fmt.Errorf("failed to create log directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.RecordPath), 0o755); err != nil {
		errStr := fmt.Sprintf("failed to create record directory: %v", err)
		record.Error = &errStr
		return record, fmt.Errorf("failed to create record directory: %w", err)
	}

	// Open log file (truncate/create)
	logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		errStr := fmt.Sprintf("failed to open log file: %v", err)
		record.Error = &errStr
		return record, fmt.Errorf("failed to open log file: %w", err)
	}

	// Record start time
	startTime := time.Now().UTC()
	record.StartedAt = startTime.Format(time.RFC3339Nano)

	// Write header to log file (matching setup.log style, best-effort diagnostic output)
	_, _ = fmt.Fprintf(logFile, "# agency verify log\n")
	_, _ = fmt.Fprintf(logFile, "# timestamp: %s\n", startTime.Format(time.RFC3339))
	_, _ = fmt.Fprintf(logFile, "# command: sh -lc %s\n", cfg.Script)
	_, _ = fmt.Fprintf(logFile, "# cwd: %s\n", cfg.WorkDir)
	_, _ = fmt.Fprintf(logFile, "# ---\n\n")

	// Create context with timeout
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, timeout)
	defer cancelTimeout()

	// Build command: sh -lc <script>
	cmd := osexec.CommandContext(timeoutCtx, "sh", "-lc", cfg.Script)
	cmd.Dir = cfg.WorkDir
	cmd.Env = cfg.Env
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Open /dev/null for stdin
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		_ = logFile.Close() // Best-effort cleanup; returning open error
		errStr := fmt.Sprintf("failed to open /dev/null: %v", err)
		record.Error = &errStr
		record.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		record.DurationMS = time.Since(startTime).Milliseconds()
		writeRecordBestEffort(cfg.RecordPath, record)
		return record, fmt.Errorf("failed to open /dev/null: %w", err)
	}
	cmd.Stdin = devnull

	// Start process in its own process group for clean signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Start the command
	if err := cmd.Start(); err != nil {
		_ = devnull.Close() // Best-effort cleanup; returning start error
		_ = logFile.Close()
		errStr := fmt.Sprintf("failed to start verify script: %v", err)
		record.Error = &errStr
		record.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		record.DurationMS = time.Since(startTime).Milliseconds()
		writeRecordBestEffort(cfg.RecordPath, record)
		return record, fmt.Errorf("failed to start verify script: %w", err)
	}

	pgid := cmd.Process.Pid

	// Wait for command completion or context cancellation
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	var runErr error
	var timedOut, cancelled bool

	select {
	case runErr = <-waitDone:
		// Command completed normally or with error
	case <-timeoutCtx.Done():
		// Check if it was timeout or parent cancellation
		if ctx.Err() != nil {
			// Parent context was cancelled (user SIGINT)
			cancelled = true
		} else {
			// Timeout fired
			timedOut = true
		}
		// Kill the process group
		killProcessGroup(pgid)
		// Wait for the command to finish
		runErr = <-waitDone
	}

	// Close resources (best-effort cleanup; process results take priority)
	_ = devnull.Close()
	_ = logFile.Close()

	// Record finish time and duration
	finishTime := time.Now().UTC()
	record.FinishedAt = finishTime.Format(time.RFC3339Nano)
	record.DurationMS = finishTime.Sub(startTime).Milliseconds()
	record.TimedOut = timedOut
	record.Cancelled = cancelled

	// Extract exit code and signal
	if runErr == nil {
		exitCode := 0
		record.ExitCode = &exitCode
	} else {
		var exitErr *osexec.ExitError
		if stderrors.As(runErr, &exitErr) {
			if exitErr.ProcessState != nil {
				// Check if terminated by signal
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					if status.Signaled() {
						sig := status.Signal().String()
						record.Signal = &sig
						// No exit code when signaled
					} else {
						exitCode := exitErr.ExitCode()
						record.ExitCode = &exitCode
					}
				} else {
					exitCode := exitErr.ExitCode()
					record.ExitCode = &exitCode
				}
			}
		}
		// If timed out or cancelled, record SIGKILL as the signal
		if timedOut || cancelled {
			sig := "SIGKILL"
			record.Signal = &sig
		}
	}

	// Read verify.json (optional structured output)
	vjResult := ReadVerifyJSON(cfg.VerifyJSONPath)
	if vjResult.Exists {
		record.VerifyJSONPath = &cfg.VerifyJSONPath
		if vjResult.Err != nil && record.Error == nil {
			// Record parse/validation error only if no other internal error
			errStr := vjResult.Err.Error()
			record.Error = &errStr
		}
	}

	// Derive OK and Summary using precedence rules
	record.OK = DeriveOK(timedOut, cancelled, record.ExitCode, vjResult.VJ)
	record.Summary = DeriveSummary(timedOut, cancelled, record.ExitCode, vjResult.VJ)

	// Write verify_record.json atomically
	if err := fs.WriteJSONAtomic(cfg.RecordPath, record, 0o644); err != nil {
		return record, fmt.Errorf("failed to write verify_record.json: %w", err)
	}

	return record, nil
}

// killProcessGroup sends SIGINT to the process group, waits GracePeriod,
// then sends SIGKILL to the process group.
func killProcessGroup(pgid int) {
	// Send SIGINT to process group (negative pgid targets the group)
	_ = syscall.Kill(-pgid, syscall.SIGINT)

	// Wait grace period
	time.Sleep(GracePeriod)

	// Send SIGKILL to process group
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// writeRecordBestEffort attempts to write the verify record but ignores errors.
// Used when we need to record state before returning an error.
func writeRecordBestEffort(path string, record store.VerifyRecord) {
	_ = fs.WriteJSONAtomic(path, record, 0o644)
}

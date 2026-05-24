package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
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

	// Script is the exact script path to execute.
	Script string

	// Env is the full environment for the script. Caller provides merged env.
	// Verify runner does not modify it.
	Env []string

	// Timeout is the maximum duration for the script. Callers must provide a
	// positive, resolved configuration value.
	Timeout time.Duration

	// LogPath is the absolute path to write verify.log.
	LogPath string

	// Clock returns the current time. Nil falls back to time.Now. Injecting a
	// clock lets tests pin started_at/finished_at timestamps deterministically.
	Clock func() time.Time
}

const gracePeriod = 3 * time.Second

// Run executes the verify script and returns the canonical verify record.
//
// The function returns a VerifyRecord (always populated) and an error.
// The error is only returned for internal failures that prevent running:
//   - log file open failure
//   - exec start failure
//
// Verify failure (non-zero exit, timeout, cancel) is represented in
// VerifyRecord.OK/ExitCode, NOT as a returned error.
func Run(ctx context.Context, cfg RunConfig) (store.VerifyRecord, error) {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	now := func() time.Time { return clock().UTC() }

	record := store.VerifyRecord{
		SchemaVersion: store.VerifyRecordSchemaVersion,
		RepoID:        cfg.RepoID,
		RunID:         cfg.RunID,
		ScriptPath:    cfg.Script,
		TimeoutMS:     cfg.Timeout.Milliseconds(),
		LogPath:       cfg.LogPath,
	}
	if cfg.Timeout <= 0 {
		errStr := "verify timeout must be positive"
		record.Error = &errStr
		return record, fmt.Errorf("%s", errStr)
	}

	// Ensure parent directory exists for the log.
	if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o755); err != nil {
		errStr := fmt.Sprintf("failed to create log directory: %v", err)
		record.Error = &errStr
		return record, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file (truncate/create)
	logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		errStr := fmt.Sprintf("failed to open log file: %v", err)
		record.Error = &errStr
		return record, fmt.Errorf("failed to open log file: %w", err)
	}

	// Record start time
	startTime := now()
	record.StartedAt = startTime.Format(time.RFC3339Nano)

	// Write header to log file (matching setup.log style, best-effort diagnostic output)
	_, _ = fmt.Fprintf(logFile, "# verify log\n")
	_, _ = fmt.Fprintf(logFile, "# timestamp: %s\n", startTime.Format(time.RFC3339))
	_, _ = fmt.Fprintf(logFile, "# command: %s\n", cfg.Script)
	_, _ = fmt.Fprintf(logFile, "# cwd: %s\n", cfg.WorkDir)
	_, _ = fmt.Fprintf(logFile, "# ---\n\n")

	// Create context with timeout
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, cfg.Timeout)
	defer cancelTimeout()

	// Open /dev/null for stdin
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		_ = logFile.Close() // Best-effort cleanup; returning open error
		errStr := fmt.Sprintf("failed to open /dev/null: %v", err)
		record.Error = &errStr
		finish := now()
		record.FinishedAt = finish.Format(time.RFC3339Nano)
		record.DurationMS = finish.Sub(startTime).Milliseconds()
		return record, fmt.Errorf("failed to open /dev/null: %w", err)
	}
	// Start process in its own process group for clean signal handling.
	proc, err := exec.StartProcess(timeoutCtx, cfg.Script, nil, exec.StartOpts{
		Dir:     cfg.WorkDir,
		EnvList: cfg.Env,
		Stdin:   devnull,
		Stdout:  logFile,
		Stderr:  logFile,
		Setpgid: true,
	})
	if err != nil {
		_ = devnull.Close() // Best-effort cleanup; returning start error
		_ = logFile.Close()
		errStr := fmt.Sprintf("failed to start verify script: %v", err)
		record.Error = &errStr
		finish := now()
		record.FinishedAt = finish.Format(time.RFC3339Nano)
		record.DurationMS = finish.Sub(startTime).Milliseconds()
		return record, fmt.Errorf("failed to start verify script: %w", err)
	}

	pgid := proc.PGID

	// Wait for command completion or context cancellation
	type waitOutcome struct {
		exit exec.ProcessExit
		err  error
	}
	waitDone := make(chan waitOutcome, 1)
	go func() {
		exit, waitErr := proc.WaitExit()
		waitDone <- waitOutcome{exit: exit, err: waitErr}
	}()

	var runErr error
	var runExit exec.ProcessExit
	var timedOut, cancelled bool

	select {
	case outcome := <-waitDone:
		// Command completed normally or with error
		runErr = outcome.err
		runExit = outcome.exit
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
		outcome := <-waitDone
		runErr = outcome.err
		runExit = outcome.exit
	}

	// Close resources (best-effort cleanup; process results take priority)
	_ = devnull.Close()
	_ = logFile.Close()

	// Record finish time and duration
	finishTime := now()
	record.FinishedAt = finishTime.Format(time.RFC3339Nano)
	record.DurationMS = finishTime.Sub(startTime).Milliseconds()
	record.TimedOut = timedOut
	record.Cancelled = cancelled

	// Preserve non-cancellation internal wait failures for diagnostics.
	if runErr != nil && !cancelled {
		errStr := runErr.Error()
		record.Error = &errStr
	}

	// Extract exit code and signal.
	// timeouts/cancellations are represented as SIGKILL in this contract.
	if runExit.Signal != "" {
		sig := runExit.Signal
		record.Signal = &sig
	} else if !timedOut && !cancelled {
		exitCode := runExit.ExitCode
		record.ExitCode = &exitCode
	}
	if timedOut || cancelled {
		sig := "SIGKILL"
		record.Signal = &sig
	}

	verifyJSONPath := filepath.Join(cfg.WorkDir, ".agency", "out", "verify.json")
	vjResult := readVerifyJSON(verifyJSONPath)
	if vjResult.exists {
		record.VerifyJSONPath = &verifyJSONPath
		if vjResult.err != nil && record.Error == nil {
			// Record parse/validation error only if no other internal error
			errStr := vjResult.err.Error()
			record.Error = &errStr
		}
	}

	// Derive OK and Summary using precedence rules
	record.OK = deriveOK(timedOut, cancelled, record.ExitCode, vjResult.value)
	record.Summary = deriveSummary(timedOut, cancelled, record.ExitCode, vjResult.value)

	return record, nil
}

// killProcessGroup sends SIGINT to the process group, waits gracePeriod,
// then sends SIGKILL to the process group.
func killProcessGroup(pgid int) {
	// Send SIGINT to process group (negative pgid targets the group)
	_ = syscall.Kill(-pgid, syscall.SIGINT)

	// Wait grace period
	time.Sleep(gracePeriod)

	// Send SIGKILL to process group
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

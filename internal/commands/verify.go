// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/verifyservice"
)

// VerifyOpts holds options for the verify command.
type VerifyOpts struct {
	// RunID is the run identifier to verify (required).
	RunID string

	// Timeout is the script timeout (default: 30m).
	Timeout time.Duration
}

// Verify runs the repo's scripts.verify for a run and records results.
// Does not require cwd to be inside a repo; resolves run globally.
func Verify(ctx context.Context, fsys fs.FS, opts VerifyOpts, stdout, stderr io.Writer) error {
	// Validate run_id provided
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
	}

	// Default timeout if not specified
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	// Get home directory for path resolution
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	// Resolve data directory
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Create verify service and run verification
	svc := verifyservice.NewService(dataDir, fsys)
	result, err := svc.VerifyRun(ctx, opts.RunID, timeout)

	// Handle the result/error based on spec output contract
	return formatVerifyOutput(result, err, stdout, stderr)
}

// formatVerifyOutput formats the verify result according to the S5 spec UX contract.
//
// Output contract (v1):
//   - success: stdout "ok verify <id> record=<path> log=<path>"
//   - failure: stderr "E_SCRIPT_FAILED: verify failed (<reason>) record=<path> log=<path>"
//   - timeout: stderr "E_SCRIPT_TIMEOUT: verify timed out record=<path> log=<path>"
func formatVerifyOutput(result *verifyservice.VerifyRunResult, err error, stdout, stderr io.Writer) error {
	// If we have no result, this is an infrastructure error
	if result == nil || result.Record == nil {
		return err
	}

	record := result.Record
	recordPath := computeRecordPath(record)
	logPath := record.LogPath

	// Handle successful verification
	if record.OK {
		fmt.Fprintf(stdout, "ok verify %s record=%s log=%s\n", record.RunID, recordPath, logPath)
		return nil
	}

	// Handle failed verification - derive reason from record fields
	reason := deriveFailureReason(record)

	// Choose error code based on timed_out
	if record.TimedOut {
		fmt.Fprintf(stderr, "E_SCRIPT_TIMEOUT: verify timed out record=%s log=%s\n", recordPath, logPath)
		return errors.New(errors.EScriptTimeout, "verify timed out")
	}

	fmt.Fprintf(stderr, "E_SCRIPT_FAILED: verify failed (%s) record=%s log=%s\n", reason, recordPath, logPath)
	return errors.New(errors.EScriptFailed, fmt.Sprintf("verify failed (%s)", reason))
}

// deriveFailureReason derives the human-readable failure reason from the verify record.
// Order per spec: timed_out, cancelled, exec failed, exit code.
func deriveFailureReason(record *store.VerifyRecord) string {
	if record.TimedOut {
		return "timed out"
	}
	if record.Cancelled {
		return "cancelled"
	}
	if record.Error != nil && *record.Error != "" {
		// Check if it's an exec failure
		if record.ExitCode == nil {
			return "exec failed"
		}
	}
	if record.ExitCode != nil && *record.ExitCode != 0 {
		return fmt.Sprintf("exit %d", *record.ExitCode)
	}
	// Fallback - verify.json said ok=false
	return "verify.json ok=false"
}

// computeRecordPath returns the record path from the verify result.
// The runner writes the record, but we need to reconstruct the path for output
// if it wasn't available (e.g., infra error before write).
func computeRecordPath(record *store.VerifyRecord) string {
	// If we have a log path, derive the record path from it
	// Log path: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log
	// Record path: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json
	if record.LogPath != "" {
		runDir := filepath.Dir(filepath.Dir(record.LogPath)) // go up from logs/verify.log
		return filepath.Join(runDir, "verify_record.json")
	}
	return "unknown"
}

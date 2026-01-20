// Package archive provides the archive pipeline for agency.
// Archive is a best-effort operation that attempts all steps regardless of earlier failures.
package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/worktree"
)

// Result holds the result of an archive operation.
type Result struct {
	// ScriptOK is true if the archive script succeeded.
	ScriptOK bool

	// TmuxOK is true if the tmux session was killed (or missing).
	TmuxOK bool

	// DeleteOK is true if the worktree was deleted.
	DeleteOK bool

	// ScriptReason is the failure reason for the script step (bounded to 512 bytes).
	ScriptReason string

	// TmuxReason is the failure reason for the tmux step (bounded to 512 bytes).
	TmuxReason string

	// DeleteReason is the failure reason for the delete step (bounded to 512 bytes).
	DeleteReason string

	// LogPath is the path to the archive log file.
	LogPath string
}

// Success returns true if all archive steps succeeded.
// Per S6 spec: success iff script succeeded AND delete succeeded.
// Tmux kill missing-session is ok.
func (r *Result) Success() bool {
	return r.ScriptOK && r.DeleteOK
}

// Config holds the configuration for an archive operation.
type Config struct {
	// Meta is the run metadata.
	Meta *store.RunMeta

	// RepoRoot is the path to the main git repository.
	// May be empty if the repo root is unknown or missing.
	RepoRoot string

	// DataDir is the resolved AGENCY_DATA_DIR.
	DataDir string

	// ArchiveScript is the path to the archive script from agency.json.
	ArchiveScript string

	// Timeout is the archive script timeout (default: 5m).
	Timeout time.Duration
}

// Deps holds the dependencies for the archive pipeline.
type Deps struct {
	CR         agencyexec.CommandRunner
	TmuxClient tmux.Client
	Stdout     io.Writer
	Stderr     io.Writer
}

// Archive executes the archive pipeline:
// 1. Run archive script (timeout, capture logs)
// 2. Kill tmux session (missing session is ok)
// 3. Delete worktree (git worktree remove, fallback to safe rm-rf)
//
// All steps are attempted regardless of earlier failures (best-effort).
// Returns a Result indicating what succeeded/failed.
func Archive(ctx context.Context, cfg Config, deps Deps, st *store.Store) *Result {
	result := &Result{}

	if cfg.Timeout == 0 {
		cfg.Timeout = config.DefaultArchiveTimeout
	}

	runID := cfg.Meta.RunID
	repoID := cfg.Meta.RepoID
	worktreePath := cfg.Meta.WorktreePath

	// Compute paths
	logsDir := st.RunLogsDir(repoID, runID)
	logPath := filepath.Join(logsDir, "archive.log")
	result.LogPath = logPath

	// Ensure logs directory exists (non-fatal: we'll still try all steps)
	_ = os.MkdirAll(logsDir, 0o700)

	// Step 1: Run archive script
	result.ScriptOK, result.ScriptReason = runArchiveScript(ctx, cfg, deps, logPath)

	// Step 2: Kill tmux session
	result.TmuxOK, result.TmuxReason = killTmuxSession(ctx, cfg.Meta, deps)

	// Step 3: Delete worktree
	result.DeleteOK, result.DeleteReason = deleteWorktree(ctx, cfg, deps, worktreePath)

	return result
}

// runArchiveScript runs the archive script with proper environment.
func runArchiveScript(ctx context.Context, cfg Config, deps Deps, logPath string) (ok bool, reason string) {
	meta := cfg.Meta
	worktreePath := meta.WorktreePath

	// Build environment per L0 contract
	env := buildArchiveEnv(meta, cfg.RepoRoot, cfg.DataDir)

	// Create or truncate log file
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Sprintf("failed to create log file: %v", err)
	}
	defer func() {
		if cerr := logFile.Close(); cerr != nil && ok {
			ok = false
			reason = fmt.Sprintf("failed to close log file: %v", cerr)
		}
	}()

	// Create context with timeout
	scriptCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Run script via sh -lc (like setup/verify)
	cmd := exec.CommandContext(scriptCtx, "sh", "-lc", cfg.ArchiveScript)
	cmd.Dir = worktreePath
	cmd.Env = env
	cmd.Stdin = nil // /dev/null
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	startTime := time.Now()
	runErr := cmd.Run()
	duration := time.Since(startTime)

	// Write duration to log (best-effort diagnostic output)
	_, _ = fmt.Fprintf(logFile, "\n--- archive script finished in %v ---\n", duration)

	if runErr != nil {
		if scriptCtx.Err() == context.DeadlineExceeded {
			return false, fmt.Sprintf("timed out after %v", cfg.Timeout)
		}
		return false, fmt.Sprintf("exit %d", cmd.ProcessState.ExitCode())
	}

	return true, ""
}

// buildArchiveEnv builds the environment variables for the archive script.
// Per L0 contract, mirrors setup/verify env.
func buildArchiveEnv(meta *store.RunMeta, repoRoot, dataDir string) []string {
	// Start with current environment
	env := os.Environ()

	runDir := filepath.Join(dataDir, "repos", meta.RepoID, "runs", meta.RunID)
	worktreePath := meta.WorktreePath

	// Add agency-specific variables per L0 contract
	agencyEnv := map[string]string{
		"AGENCY_RUN_ID":         meta.RunID,
		"AGENCY_NAME":           meta.Name,
		"AGENCY_REPO_ROOT":      repoRoot, // best-effort; may be empty
		"AGENCY_WORKSPACE_ROOT": worktreePath,
		"AGENCY_WORKTREE_ROOT":  worktreePath, // alias for clarity
		"AGENCY_BRANCH":         meta.Branch,
		"AGENCY_PARENT_BRANCH":  meta.ParentBranch,
		"AGENCY_ORIGIN_NAME":    "origin",
		"AGENCY_ORIGIN_URL":     "", // Could be populated if needed
		"AGENCY_RUNNER":         meta.Runner,
		"AGENCY_PR_URL":         meta.PRURL,
		"AGENCY_PR_NUMBER":      "",
		"AGENCY_DOTAGENCY_DIR":  filepath.Join(worktreePath, ".agency"),
		"AGENCY_OUTPUT_DIR":     filepath.Join(worktreePath, ".agency", "out"),
		"AGENCY_LOG_DIR":        filepath.Join(runDir, "logs"),
		"AGENCY_NONINTERACTIVE": "1",
		"CI":                    "1",
	}

	if meta.PRNumber != 0 {
		agencyEnv["AGENCY_PR_NUMBER"] = fmt.Sprintf("%d", meta.PRNumber)
	}

	for k, v := range agencyEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// killTmuxSession kills the tmux session for a run.
func killTmuxSession(ctx context.Context, meta *store.RunMeta, deps Deps) (ok bool, reason string) {
	sessionName := tmux.SessionName(meta.RunID)

	err := deps.TmuxClient.KillSession(ctx, sessionName)
	if err != nil {
		// Missing session is not a failure
		if tmux.IsNoSessionErr(err) {
			return true, ""
		}
		return false, err.Error()
	}

	return true, ""
}

// deleteWorktree deletes the worktree using git worktree remove,
// falling back to safe rm-rf if needed.
func deleteWorktree(ctx context.Context, cfg Config, deps Deps, worktreePath string) (ok bool, reason string) {
	// First, try git worktree remove --force
	if cfg.RepoRoot != "" {
		result := worktree.Remove(ctx, deps.CR, cfg.RepoRoot, worktreePath)
		if result.Success {
			return true, ""
		}
		// Git remove failed; try fallback
	}

	// Fallback: safe rm -rf (only if under allowed prefix)
	allowedPrefix := filepath.Join(cfg.DataDir, "repos", cfg.Meta.RepoID, "worktrees")

	err := agencyfs.SafeRemoveAll(worktreePath, allowedPrefix)
	if err != nil {
		if _, isNotUnder := err.(*agencyfs.ErrNotUnderPrefix); isNotUnder {
			return false, fmt.Sprintf("worktree path %q is outside allowed prefix %q; refusing to delete", worktreePath, allowedPrefix)
		}
		return false, err.Error()
	}

	// Check if the directory still exists
	if _, err := os.Stat(worktreePath); err == nil {
		return false, "directory still exists after removal attempt"
	}

	return true, ""
}

// ToError converts a failed Result to an AgencyError.
// Returns nil if the result indicates success.
func (r *Result) ToError() error {
	if r.Success() {
		return nil
	}

	// Build detailed message
	var parts []string
	if !r.ScriptOK {
		parts = append(parts, "script failed")
		if r.ScriptReason != "" {
			parts = append(parts, "("+truncateReason(r.ScriptReason)+")")
		}
	}
	if !r.TmuxOK {
		parts = append(parts, "tmux kill failed")
		if r.TmuxReason != "" {
			parts = append(parts, "("+truncateReason(r.TmuxReason)+")")
		}
	}
	if !r.DeleteOK {
		parts = append(parts, "worktree deletion failed")
		if r.DeleteReason != "" {
			parts = append(parts, "("+truncateReason(r.DeleteReason)+")")
		}
	}

	msg := "archive failed: " + strings.Join(parts, "; ")
	return errors.New(errors.EArchiveFailed, msg)
}

// truncateReason truncates a reason string to 128 chars for error messages.
func truncateReason(s string) string {
	const maxLen = 128
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

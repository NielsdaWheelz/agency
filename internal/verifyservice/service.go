// Package verifyservice provides the verify pipeline entrypoint for agency.
// It wires together run resolution, repo locking, verify execution, and meta updates.
package verifyservice

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/events"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/verify"
)

// NeedsAttentionReasonVerifyFailed is the reason set when verify fails.
const NeedsAttentionReasonVerifyFailed = "verify_failed"

// Service provides the verify pipeline functionality.
type Service struct {
	DataDir string
	FS      fs.FS
	Now     func() time.Time
}

// NewService creates a new verify service.
func NewService(dataDir string, filesystem fs.FS) *Service {
	return &Service{
		DataDir: dataDir,
		FS:      filesystem,
		Now:     time.Now,
	}
}

// VerifyRunResult contains the result of a verify run.
type VerifyRunResult struct {
	// Record is the verify record (always populated if verify ran).
	Record *store.VerifyRecord

	// EventAppendErrors contains any errors from appending events.
	// Non-fatal; verify still succeeded/failed based on script outcome.
	EventAppendErrors []string
}

// VerifyRun executes scripts.verify for an existing run and updates meta+events.
// It must be cwd-independent; it resolves run via global store scan.
//
// Returns:
//   - (result, nil) whenever verify ran and produced a record (even if ok=false)
//   - (nil, error) only for infra failures (lock, missing workspace, persistence failure)
func (s *Service) VerifyRun(ctx context.Context, runRef string, timeout time.Duration) (*VerifyRunResult, error) {
	// Default timeout if not specified
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	// Step 1: Resolve run_id globally (cwd-independent)
	allRuns, err := store.ScanAllRuns(s.DataDir)
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to scan runs", err)
	}

	var refs []ids.RunRef
	for _, run := range allRuns {
		refs = append(refs, ids.RunRef{
			RepoID: run.RepoID,
			RunID:  run.RunID,
			Broken: run.Broken,
		})
	}

	resolved, err := ids.ResolveRunRef(runRef, refs)
	if err != nil {
		var notFound *ids.ErrNotFound
		if stderrors.As(err, &notFound) {
			return nil, errors.New(errors.ERunNotFound, fmt.Sprintf("run not found: %s", runRef))
		}
		var ambiguous *ids.ErrAmbiguous
		if stderrors.As(err, &ambiguous) {
			return nil, errors.NewWithDetails(
				errors.ERunIDAmbiguous,
				fmt.Sprintf("ambiguous run id %q matches multiple runs", runRef),
				nil,
			)
		}
		return nil, errors.Wrap(errors.EInternal, "failed to resolve run id", err)
	}

	if resolved.Broken {
		return nil, errors.NewWithDetails(
			errors.ERunBroken,
			"run exists but meta.json is unreadable or invalid",
			map[string]string{"run_id": resolved.RunID, "repo_id": resolved.RepoID},
		)
	}

	repoID := resolved.RepoID
	runID := resolved.RunID

	// Step 2: Create store and read meta
	st := store.NewStore(s.FS, s.DataDir, s.Now)
	meta, err := st.ReadMeta(repoID, runID)
	if err != nil {
		return nil, err
	}

	// Step 3: Workspace existence check
	worktreePath := meta.WorktreePath
	if worktreePath == "" {
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"meta.json has empty worktree_path",
			map[string]string{"run_id": runID, "repo_id": repoID},
		)
	}

	// Check if archived (via archive field or worktree missing on disk)
	archived := meta.Archive != nil && meta.Archive.ArchivedAt != ""
	if !archived {
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			archived = true
		}
	}

	if archived {
		return nil, errors.NewWithDetails(
			errors.EWorkspaceArchived,
			"run exists but worktree missing or archived; cannot verify",
			map[string]string{"run_id": runID, "repo_id": repoID, "worktree_path": worktreePath},
		)
	}

	// Step 4: Acquire repo lock
	repoLock := lock.NewRepoLock(s.DataDir)
	unlock, err := repoLock.Lock(repoID, "verify")
	if err != nil {
		var lockErr *lock.ErrLocked
		if stderrors.As(err, &lockErr) {
			return nil, errors.New(errors.ERepoLocked, lockErr.Error())
		}
		return nil, errors.Wrap(errors.EInternal, "failed to acquire repo lock", err)
	}
	defer func() {
		// Unlock error is logged but not returned; verify result takes priority
		if uerr := unlock(); uerr != nil {
			// Lock package handles logging internally
			_ = uerr
		}
	}()

	// Step 5: Load agency.json to get verify script path
	agencyJSON, err := config.LoadAgencyConfig(s.FS, worktreePath)
	if err != nil {
		return nil, err
	}
	verifyScript := agencyJSON.Scripts.Verify

	// Build paths
	runDir := st.RunDir(repoID, runID)
	logPath := filepath.Join(st.RunLogsDir(repoID, runID), "verify.log")
	recordPath := st.VerifyRecordPath(repoID, runID)
	eventsPath := st.EventsPath(repoID, runID)
	verifyJSONPath := filepath.Join(worktreePath, ".agency", "out", "verify.json")

	result := &VerifyRunResult{}

	// Step 6: Emit verify_started event (best-effort)
	startedEvent := events.Event{
		SchemaVersion: "1.0",
		Timestamp:     s.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         runID,
		Event:         "verify_started",
		Data:          events.VerifyStartedData(timeout.Milliseconds(), logPath, verifyJSONPath),
	}
	if err := events.AppendEvent(eventsPath, startedEvent); err != nil {
		result.EventAppendErrors = append(result.EventAppendErrors, fmt.Sprintf("verify_started: %v", err))
	}

	// Step 7: Build environment for verify script (same as setup script per L0 contract)
	env := buildVerifyEnv(meta, worktreePath, runDir, s.DataDir)

	// Step 8: Run verify script via verify runner
	runCfg := verify.RunConfig{
		RepoID:         repoID,
		RunID:          runID,
		WorkDir:        worktreePath,
		Script:         verifyScript,
		Env:            env,
		Timeout:        timeout,
		LogPath:        logPath,
		VerifyJSONPath: verifyJSONPath,
		RecordPath:     recordPath,
	}

	record, runErr := verify.Run(ctx, runCfg)
	result.Record = &record

	// Step 9: Update meta.json atomically
	metaUpdateErr := st.UpdateMeta(repoID, runID, func(m *store.RunMeta) {
		// Only set last_verify_at if verify actually started
		if record.StartedAt != "" {
			m.LastVerifyAt = record.FinishedAt
		}

		// Update needs_attention flags based on verify result
		if record.OK {
			// Clear attention only if reason was verify_failed
			if m.Flags != nil && m.Flags.NeedsAttention && m.Flags.NeedsAttentionReason == NeedsAttentionReasonVerifyFailed {
				m.Flags.NeedsAttention = false
				m.Flags.NeedsAttentionReason = ""
			}
		} else {
			// Set attention with reason verify_failed
			if m.Flags == nil {
				m.Flags = &store.RunMetaFlags{}
			}
			m.Flags.NeedsAttention = true
			m.Flags.NeedsAttentionReason = NeedsAttentionReasonVerifyFailed
		}
	})

	// Step 10: Emit verify_finished event (best-effort)
	var vjPath string
	if record.VerifyJSONPath != nil {
		vjPath = *record.VerifyJSONPath
	}
	finishedEvent := events.Event{
		SchemaVersion: "1.0",
		Timestamp:     s.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         runID,
		Event:         "verify_finished",
		Data:          events.VerifyFinishedData(record.OK, record.ExitCode, record.TimedOut, record.Cancelled, record.DurationMS, vjPath, logPath, recordPath),
	}
	if err := events.AppendEvent(eventsPath, finishedEvent); err != nil {
		result.EventAppendErrors = append(result.EventAppendErrors, fmt.Sprintf("verify_finished: %v", err))
	}

	// Step 11: If there were event append errors, augment verify_record.json.error
	if len(result.EventAppendErrors) > 0 {
		augmentRecordError(recordPath, result.EventAppendErrors)
	}

	// Step 12: If meta update failed, augment verify_record.json.error and return error
	if metaUpdateErr != nil {
		augmentRecordError(recordPath, []string{fmt.Sprintf("meta update failed: %v", metaUpdateErr)})
		return result, errors.Wrap(errors.EPersistFailed, "failed to update meta.json", metaUpdateErr)
	}

	// Return any runner error (shouldn't happen for script failures, only infra)
	if runErr != nil {
		return result, errors.Wrap(errors.EInternal, "verify runner failed", runErr)
	}

	return result, nil
}

// buildVerifyEnv builds the environment variables for the verify script.
// Per L0 contract, uses the same env injection as other scripts.
func buildVerifyEnv(meta *store.RunMeta, worktreePath, runDir, dataDir string) []string {
	// Start with current environment
	env := os.Environ()

	// Add agency-specific variables per L0 contract
	agencyEnv := map[string]string{
		"AGENCY_RUN_ID":         meta.RunID,
		"AGENCY_TITLE":          meta.Title,
		"AGENCY_REPO_ROOT":      worktreePath, // worktree is the repo root for scripts
		"AGENCY_WORKSPACE_ROOT": worktreePath,
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

// augmentRecordError reads verify_record.json, appends error messages, and rewrites it.
// Best-effort: errors are ignored.
func augmentRecordError(recordPath string, errMsgs []string) {
	data, err := os.ReadFile(recordPath)
	if err != nil {
		return
	}

	var record store.VerifyRecord
	if err := fs.UnmarshalJSON(data, &record); err != nil {
		return
	}

	// Build combined error message
	combined := "events append failed: "
	for i, msg := range errMsgs {
		if i > 0 {
			combined += "; "
		}
		combined += msg
	}

	// Preserve existing error by concatenating
	if record.Error != nil && *record.Error != "" {
		combined = *record.Error + "; " + combined
	}
	record.Error = &combined

	// Rewrite atomically (best-effort)
	_ = fs.WriteJSONAtomic(recordPath, record, 0o644)
}

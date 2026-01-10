package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/status"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// ShowOpts holds options for the show command.
type ShowOpts struct {
	// RunID is the run identifier (exact or unique prefix).
	RunID string

	// JSON outputs machine-readable JSON.
	JSON bool

	// Path outputs only resolved filesystem paths.
	Path bool
}

// Show executes the agency show command.
// Inspects a single run by exact or unique-prefix ID resolution.
// This is a read-only command: no state files are mutated.
func Show(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts ShowOpts, stdout, stderr io.Writer) error {
	// Validate run_id provided
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
	}

	// Resolve data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Scan all runs (global resolution works regardless of cwd)
	records, err := store.ScanAllRuns(dataDir)
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to scan runs", err)
	}

	// Convert records to RunRefs for resolution
	refs := make([]ids.RunRef, len(records))
	for i, rec := range records {
		refs[i] = ids.RunRef{
			RepoID: rec.RepoID,
			RunID:  rec.RunID,
			Broken: rec.Broken,
		}
	}

	// Resolve run ID (exact or unique prefix)
	resolvedRef, err := ids.ResolveRunRef(opts.RunID, refs)
	if err != nil {
		return handleResolveError(err, opts, stdout, stderr)
	}

	// Find the matching record
	var record *store.RunRecord
	for i := range records {
		if records[i].RunID == resolvedRef.RunID && records[i].RepoID == resolvedRef.RepoID {
			record = &records[i]
			break
		}
	}
	if record == nil {
		// Should not happen if resolver worked correctly
		return errors.New(errors.EInternal, "resolved run not found in records")
	}

	// Compute paths
	runDir := filepath.Join(dataDir, "repos", record.RepoID, "runs", record.RunID)
	eventsPath := filepath.Join(runDir, "events.jsonl")
	transcriptPath := filepath.Join(runDir, "transcript.txt")
	logsDir := filepath.Join(runDir, "logs")
	setupLogPath, verifyLogPath, archiveLogPath := render.ResolveScriptLogPaths(runDir)

	// Handle broken runs
	if record.Broken {
		return handleBrokenRun(record, runDir, logsDir, eventsPath, transcriptPath, setupLogPath, verifyLogPath, archiveLogPath, opts, stdout, stderr)
	}

	// Get tmux session set (single call for efficiency)
	tmuxSessions := getTmuxSessions(ctx, cr)
	tmuxUnavailable := false // we don't know if tmux is unavailable, just that no sessions exist

	// Compute local snapshot for the run
	worktreePath := record.Meta.WorktreePath
	worktreePresent := dirExists(worktreePath)
	archived := !worktreePresent

	// Report info
	reportPath := filepath.Join(worktreePath, ".agency", "report.md")
	reportExists := false
	reportBytes := 0
	if worktreePresent {
		if info, err := os.Stat(reportPath); err == nil && info.Mode().IsRegular() {
			reportExists = true
			reportBytes = int(info.Size())
		}
	}

	// Tmux session check
	sessionName := record.Meta.TmuxSessionName
	if sessionName == "" {
		sessionName = "agency_" + record.RunID
	}
	tmuxActive := tmuxSessions[sessionName]

	// Derive status
	snapshot := status.Snapshot{
		TmuxActive:      tmuxActive,
		WorktreePresent: worktreePresent,
		ReportBytes:     reportBytes,
	}
	derived := status.Derive(record.Meta, snapshot)

	// Best-effort repo root resolution
	repoRoot := resolveRepoRootForShow(ctx, cr, cwd, record, dataDir)

	// Determine if we should show warnings
	repoNotFoundWarning := repoRoot == nil && record.Repo != nil
	worktreeMissingWarning := !worktreePresent

	// Build output based on mode
	if opts.Path {
		return outputShowPaths(stdout, repoRoot, worktreePath, runDir, logsDir, eventsPath, transcriptPath, reportPath)
	}

	if opts.JSON {
		return outputShowJSON(stdout, record, repoRoot, runDir, eventsPath, transcriptPath, derived, reportPath, reportExists, reportBytes, tmuxActive, worktreePresent, archived, setupLogPath, verifyLogPath, archiveLogPath)
	}

	// Human output
	return outputShowHuman(stdout, record, repoRoot, runDir, derived, reportPath, reportExists, reportBytes, tmuxActive, worktreePresent, archived, setupLogPath, verifyLogPath, archiveLogPath, repoNotFoundWarning, worktreeMissingWarning, tmuxUnavailable)
}

// handleResolveError handles ID resolution errors and outputs appropriate error.
func handleResolveError(err error, opts ShowOpts, stdout, stderr io.Writer) error {
	// Handle ambiguous error
	if ambErr, ok := err.(*ids.ErrAmbiguous); ok {
		// Build candidates list
		candidates := make([]string, len(ambErr.Candidates))
		for i, c := range ambErr.Candidates {
			candidates[i] = c.RunID
		}

		// For --json mode, output JSON envelope with null data
		if opts.JSON {
			_ = render.WriteShowJSON(stdout, nil)
		}

		return errors.NewWithDetails(
			errors.ERunIDAmbiguous,
			"ambiguous run id '"+ambErr.Input+"' matches multiple runs: "+strings.Join(candidates, ", "),
			map[string]string{"input": ambErr.Input},
		)
	}

	// Handle not found error
	if _, ok := err.(*ids.ErrNotFound); ok {
		// For --json mode, output JSON envelope with null data
		if opts.JSON {
			_ = render.WriteShowJSON(stdout, nil)
		}

		return errors.New(errors.ERunNotFound, "run not found: "+opts.RunID)
	}

	return err
}

// handleBrokenRun handles output for a broken run (meta.json unreadable).
func handleBrokenRun(record *store.RunRecord, runDir, logsDir, eventsPath, transcriptPath, setupLogPath, verifyLogPath, archiveLogPath string, opts ShowOpts, stdout, stderr io.Writer) error {
	// For --path mode, output best-effort paths and exit non-zero
	if opts.Path {
		data := render.ShowPathsData{
			RepoRoot:       "",
			WorktreeRoot:   "",
			RunDir:         runDir,
			LogsDir:        logsDir,
			EventsPath:     eventsPath,
			TranscriptPath: transcriptPath,
			ReportPath:     "",
		}
		_ = render.WriteShowPaths(stdout, data)
		return errors.NewWithDetails(
			errors.ERunBroken,
			"run exists but meta.json is unreadable or invalid",
			map[string]string{
				"run_id":    record.RunID,
				"meta_path": filepath.Join(runDir, "meta.json"),
				"hint":      "delete this run dir or fix meta.json",
			},
		)
	}

	// For --json mode, output broken=true envelope
	if opts.JSON {
		detail := &render.RunDetail{
			Meta:     nil,
			RepoID:   record.RepoID,
			RepoKey:  nil,
			Archived: true, // assume archived for broken runs
			Derived: render.DerivedJSON{
				DerivedStatus:   status.StatusBroken,
				TmuxActive:      false,
				WorktreePresent: false,
				Report: render.ReportJSON{
					Exists: false,
					Bytes:  0,
					Path:   "",
				},
				Logs: render.LogsJSON{
					SetupLogPath:   setupLogPath,
					VerifyLogPath:  verifyLogPath,
					ArchiveLogPath: archiveLogPath,
				},
			},
			Paths: render.PathsJSON{
				RepoRoot:       nil,
				WorktreeRoot:   "",
				RunDir:         runDir,
				EventsPath:     eventsPath,
				TranscriptPath: transcriptPath,
			},
			Broken: true,
		}

		// Join repo info if available
		if record.Repo != nil {
			detail.RepoKey = &record.Repo.RepoKey
			detail.OriginURL = record.Repo.OriginURL
		}

		_ = render.WriteShowJSON(stdout, detail)
		return errors.NewWithDetails(
			errors.ERunBroken,
			"run exists but meta.json is unreadable or invalid",
			map[string]string{
				"run_id":    record.RunID,
				"meta_path": filepath.Join(runDir, "meta.json"),
				"hint":      "delete this run dir or fix meta.json",
			},
		)
	}

	// Human output for broken run
	metaPath := filepath.Join(runDir, "meta.json")
	return errors.NewWithDetails(
		errors.ERunBroken,
		"run exists but meta.json is unreadable or invalid",
		map[string]string{
			"run_id":    record.RunID,
			"meta_path": metaPath,
			"hint":      "delete this run dir or fix meta.json",
		},
	)
}

// resolveRepoRootForShow attempts to resolve the repo root for display purposes.
// Returns nil if unknown.
func resolveRepoRootForShow(ctx context.Context, cr agencyexec.CommandRunner, cwd string, record *store.RunRecord, dataDir string) *string {
	// If we have no repo info, we can't resolve
	if record.Repo == nil {
		return nil
	}

	// Try to match cwd repo root to repo_key
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err == nil {
		// Check if this matches the record's repo_key (best-effort)
		// For now, just return cwd repo root if we're inside any repo
		return &repoRoot.Path
	}

	// Try to load repo_index.json and use PickRepoRoot
	idx, err := store.LoadRepoIndexForScan(dataDir)
	if err != nil || idx == nil {
		return nil
	}

	return store.PickRepoRoot(record.Repo.RepoKey, nil, idx)
}

// outputShowPaths writes the --path output.
func outputShowPaths(stdout io.Writer, repoRoot *string, worktreePath, runDir, logsDir, eventsPath, transcriptPath, reportPath string) error {
	repoRootStr := ""
	if repoRoot != nil {
		repoRootStr = *repoRoot
	}

	data := render.ShowPathsData{
		RepoRoot:       repoRootStr,
		WorktreeRoot:   worktreePath,
		RunDir:         runDir,
		LogsDir:        logsDir,
		EventsPath:     eventsPath,
		TranscriptPath: transcriptPath,
		ReportPath:     reportPath,
	}
	return render.WriteShowPaths(stdout, data)
}

// outputShowJSON writes the --json output.
func outputShowJSON(stdout io.Writer, record *store.RunRecord, repoRoot *string, runDir, eventsPath, transcriptPath string, derived status.Derived, reportPath string, reportExists bool, reportBytes int, tmuxActive, worktreePresent, archived bool, setupLogPath, verifyLogPath, archiveLogPath string) error {
	detail := &render.RunDetail{
		Meta:     record.Meta,
		RepoID:   record.RepoID,
		Archived: archived,
		Derived: render.DerivedJSON{
			DerivedStatus:   derived.DerivedStatus,
			TmuxActive:      tmuxActive,
			WorktreePresent: worktreePresent,
			Report: render.ReportJSON{
				Exists: reportExists,
				Bytes:  reportBytes,
				Path:   reportPath,
			},
			Logs: render.LogsJSON{
				SetupLogPath:   setupLogPath,
				VerifyLogPath:  verifyLogPath,
				ArchiveLogPath: archiveLogPath,
			},
		},
		Paths: render.PathsJSON{
			RepoRoot:       repoRoot,
			WorktreeRoot:   record.Meta.WorktreePath,
			RunDir:         runDir,
			EventsPath:     eventsPath,
			TranscriptPath: transcriptPath,
		},
		Broken: false,
	}

	// Join repo info if available
	if record.Repo != nil {
		detail.RepoKey = &record.Repo.RepoKey
		detail.OriginURL = record.Repo.OriginURL
	}

	return render.WriteShowJSON(stdout, detail)
}

// outputShowHuman writes the human-readable output.
func outputShowHuman(stdout io.Writer, record *store.RunRecord, repoRoot *string, runDir string, derived status.Derived, reportPath string, reportExists bool, reportBytes int, tmuxActive, worktreePresent, archived bool, setupLogPath, verifyLogPath, archiveLogPath string, repoNotFoundWarning, worktreeMissingWarning, tmuxUnavailable bool) error {
	meta := record.Meta

	data := render.ShowHumanData{
		// Core
		RunID:     meta.RunID,
		Title:     meta.Title,
		Runner:    meta.Runner,
		CreatedAt: meta.CreatedAt,
		RepoID:    record.RepoID,

		// Git/workspace
		ParentBranch:    meta.ParentBranch,
		Branch:          meta.Branch,
		WorktreePath:    meta.WorktreePath,
		WorktreePresent: worktreePresent,
		TmuxSessionName: meta.TmuxSessionName,
		TmuxActive:      tmuxActive,

		// PR
		PRNumber:   meta.PRNumber,
		PRURL:      meta.PRURL,
		LastPushAt: meta.LastPushAt,

		// Report
		ReportPath:   reportPath,
		ReportExists: reportExists,
		ReportBytes:  reportBytes,

		// Logs
		SetupLogPath:   setupLogPath,
		VerifyLogPath:  verifyLogPath,
		ArchiveLogPath: archiveLogPath,

		// Derived
		DerivedStatus: derived.DerivedStatus,
		Archived:      archived,

		// Warnings
		RepoNotFoundWarning:    repoNotFoundWarning,
		WorktreeMissingWarning: worktreeMissingWarning,
		TmuxUnavailableWarning: tmuxUnavailable,
	}

	// Repo identity
	if record.Repo != nil {
		data.RepoKey = record.Repo.RepoKey
		if record.Repo.OriginURL != nil {
			data.OriginURL = *record.Repo.OriginURL
		}
	}

	return render.WriteShowHuman(stdout, data)
}

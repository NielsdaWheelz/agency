package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/status"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// LSOpts holds options for the ls command.
type LSOpts struct {
	// All includes archived runs in the output.
	All bool

	// AllRepos lists runs across all repos (ignores current repo scope).
	AllRepos bool

	// JSON outputs machine-readable JSON.
	JSON bool
}

// LS executes the agency ls command.
// Lists runs with sane defaults and stable JSON output.
// This is a read-only command: no state files are mutated.
func LS(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts LSOpts, stdout, stderr io.Writer) error {
	// Resolve data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Determine scope: in-repo vs not-in-repo
	var repoID string
	var inRepo bool

	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err == nil {
		// We're inside a repo
		inRepo = true
		originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
		repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)
		repoID = repoIdentity.RepoID
	}

	// --all-repos forces all-repos mode regardless of cwd
	useAllRepos := opts.AllRepos || !inRepo

	// Scan runs based on scope
	var records []store.RunRecord
	if useAllRepos {
		records, err = store.ScanAllRuns(dataDir)
	} else {
		records, err = store.ScanRunsForRepo(dataDir, repoID)
	}
	if err != nil {
		return err
	}

	// Get tmux session set (single call)
	tmuxSessions := getTmuxSessions(ctx, cr)

	// Convert records to summaries with snapshot data
	summaries := make([]render.RunSummary, 0, len(records))
	for _, rec := range records {
		summary := recordToSummary(rec, tmuxSessions, fsys)

		// Filter archived unless --all
		if summary.Archived && !opts.All {
			continue
		}

		summaries = append(summaries, summary)
	}

	// Sort: created_at descending (newest first), broken runs last
	sortSummaries(summaries)

	// Output
	if opts.JSON {
		return render.WriteLSJSON(stdout, summaries)
	}

	// Human output
	now := time.Now()
	rows := render.FormatHumanRows(summaries, now)

	// Build context for empty state message
	lsCtx := render.LSContext{
		IncludesArchived: opts.All,
	}
	if useAllRepos {
		lsCtx.Scope = render.LSScopeAllRepos
	} else {
		lsCtx.Scope = render.LSScopeRepo
	}

	return render.WriteLSHuman(stdout, rows, lsCtx)
}

// recordToSummary converts a RunRecord to a RunSummary with snapshot data.
func recordToSummary(rec store.RunRecord, tmuxSessions map[string]bool, fsys fs.FS) render.RunSummary {
	summary := render.RunSummary{
		RunID:  rec.RunID,
		RepoID: rec.RepoID,
		Broken: rec.Broken,
	}

	// Join repo info (best-effort)
	if rec.Repo != nil {
		summary.RepoKey = &rec.Repo.RepoKey
		summary.OriginURL = rec.Repo.OriginURL
	}

	// Handle broken runs
	if rec.Broken {
		summary.Name = render.NameBroken
		summary.DerivedStatus = status.StatusBroken

		// Check tmux even for broken runs
		sessionName := "agency_" + rec.RunID
		summary.TmuxActive = tmuxSessions[sessionName]

		// Worktree can't be checked without meta; assume absent
		summary.WorktreePresent = false
		summary.Archived = true

		return summary
	}

	// Non-broken run: extract from meta
	meta := rec.Meta
	summary.Name = meta.Name
	summary.Runner = &meta.Runner

	// Parse created_at
	if t, err := time.Parse(time.RFC3339, meta.CreatedAt); err == nil {
		summary.CreatedAt = &t
	}

	// Parse last_push_at
	if meta.LastPushAt != "" {
		if t, err := time.Parse(time.RFC3339, meta.LastPushAt); err == nil {
			summary.LastPushAt = &t
		}
	}

	// PR info
	if meta.PRNumber != 0 {
		summary.PRNumber = &meta.PRNumber
	}
	if meta.PRURL != "" {
		summary.PRURL = &meta.PRURL
	}

	// Check tmux session existence
	sessionName := meta.TmuxSessionName
	if sessionName == "" {
		// Fallback to constructed name if not set in meta
		sessionName = "agency_" + rec.RunID
	}
	summary.TmuxActive = tmuxSessions[sessionName]

	// Check worktree presence
	summary.WorktreePresent = dirExists(meta.WorktreePath)
	summary.Archived = !summary.WorktreePresent

	// Get report bytes (0 if missing or worktree absent)
	reportBytes := 0
	if summary.WorktreePresent {
		reportPath := filepath.Join(meta.WorktreePath, ".agency", "report.md")
		if info, err := os.Stat(reportPath); err == nil {
			reportBytes = int(info.Size())
		}
	}

	// Derive status
	snapshot := status.Snapshot{
		TmuxActive:      summary.TmuxActive,
		WorktreePresent: summary.WorktreePresent,
		ReportBytes:     reportBytes,
	}
	derived := status.Derive(meta, snapshot)
	summary.DerivedStatus = derived.DerivedStatus

	return summary
}

// getTmuxSessions returns a set of active tmux session names.
// Returns empty map if tmux is not available or server not running.
// This is a single call per ls invocation.
func getTmuxSessions(ctx context.Context, cr agencyexec.CommandRunner) map[string]bool {
	sessions := make(map[string]bool)

	result, err := cr.Run(ctx, "tmux", []string{"list-sessions", "-F", "#{session_name}"}, agencyexec.RunOpts{})
	if err != nil {
		// tmux not installed or execution failed
		return sessions
	}

	if result.ExitCode != 0 {
		// tmux server not running or no sessions
		return sessions
	}

	// Parse session names
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			sessions[line] = true
		}
	}

	return sessions
}

// sortSummaries sorts summaries by created_at descending (newest first).
// Broken runs (nil created_at) are sorted last.
// Tie-breaker: run_id ascending.
func sortSummaries(summaries []render.RunSummary) {
	sort.Slice(summaries, func(i, j int) bool {
		a, b := summaries[i], summaries[j]

		// Both broken: sort by run_id ascending
		if a.CreatedAt == nil && b.CreatedAt == nil {
			return a.RunID < b.RunID
		}

		// One broken: broken goes last
		if a.CreatedAt == nil {
			return false
		}
		if b.CreatedAt == nil {
			return true
		}

		// Both have timestamps: descending (newer first)
		if !a.CreatedAt.Equal(*b.CreatedAt) {
			return a.CreatedAt.After(*b.CreatedAt)
		}

		// Tie-breaker: run_id ascending
		return a.RunID < b.RunID
	})
}

// dirExists checks if a path exists and is a directory.
func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

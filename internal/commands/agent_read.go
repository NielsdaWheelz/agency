package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/watch"
)

// AgentLSOpts holds options for the agent ls command.
type AgentLSOpts struct {
	// WorktreeRef filters by integration worktree (optional).
	WorktreeRef string

	// RepoRef is the --repo flag value.
	RepoRef string

	// AllRepos lists across all repos.
	AllRepos bool

	// All includes all invocations.
	All bool

	// JSON outputs as JSON.
	JSON bool
}

// AgentLS lists agent invocations.
func AgentLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLSOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllRepos:      opts.AllRepos,
		AllowAllRepos: true,
		CmdName:       "agent ls",
	})
	if err != nil {
		return err
	}

	state := "unresolved"
	if opts.All {
		state = "all"
	}

	var repoID string
	if !repoCtx.AllRepos {
		repoID = repoCtx.RepoID
	}

	result, fetchErr := ns.client.ListInvocations(ctx, daemonclient.ListInvocationsOpts{
		RepoID:      repoID,
		WorktreeRef: opts.WorktreeRef,
		State:       state,
	})
	if fetchErr != nil {
		return fetchErr
	}
	if opts.JSON {
		return writeAgentLSJSONFromDTO(stdout, result.Data.Invocations)
	}
	return writeAgentLSHumanFromDTO(stdout, result.Data.Invocations)
}

// AgentShowOpts holds options for the agent show command.
type AgentShowOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// RepoRef is the --repo flag value.
	RepoRef string

	// JSON outputs as JSON.
	JSON bool
}

// AgentShow shows details of an agent invocation.
func AgentShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentShowOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent show",
	})
	if err != nil {
		return err
	}

	result, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

	// Output
	if opts.JSON {
		return writeAgentShowJSONFromDTO(stdout, &result.Data)
	}

	return writeAgentShowHumanFromDTO(stdout, &result.Data)
}

// AgentCheckOpts holds options for the agent check command.
type AgentCheckOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoRef is the --repo flag value.
	RepoRef string

	// JSON outputs as JSON.
	JSON bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentCheck reports canonical readiness state for invocation progression.
func AgentCheck(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentCheckOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent check",
	})
	if err != nil {
		return err
	}

	result, err := ns.client.GetInvocationCheck(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Data)
	}
	return writeAgentCheckHumanFromDTO(stdout, &result.Data)
}

// AgentHistoryOpts holds options for the agent history command.
type AgentHistoryOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoRef is the --repo flag value.
	RepoRef string

	// JSON outputs as JSON.
	JSON bool

	// Last requests only the chronologically last timeline entry.
	// Mutually exclusive with Cursor.
	Last bool

	// Limit controls page size (must be in [1, 500]).
	Limit int

	// Cursor continues from a prior page.
	Cursor string

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string

	// IsInteractive, when set, decides whether bare history opens the full-screen view.
	IsInteractive func() bool

	// HistoryInput and HistoryOutput override the watch history IO during tests.
	HistoryInput  io.Reader
	HistoryOutput io.Writer

	// RunHistory overrides the shared watch history runtime during tests.
	RunHistory func([]daemon.Turn, watch.HistoryRunOptions) error
}

// AgentHistory reads the unified invocation timeline via daemon read API.
func AgentHistory(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentHistoryOpts, stdout, stderr io.Writer) error {
	if opts.Last && opts.Cursor != "" {
		return errors.New(errors.EInvalidArgument, "--last cannot be used with --cursor")
	}

	if opts.Limit < 1 || opts.Limit > 500 {
		return errors.NewWithDetails(
			errors.EInvalidArgument,
			fmt.Sprintf("invalid value for parameter 'limit': %d", opts.Limit),
			map[string]string{
				"param": "limit",
				"min":   "1",
				"max":   "500",
			},
		)
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent history",
	})
	if err != nil {
		return err
	}

	// JSON remains the machine-fidelity escape hatch for raw timeline entries.
	// For default human history and --last resolution we project from shared turns
	// so history aligns with restore --turn semantics.
	if opts.JSON && !opts.Last {
		result, err := ns.client.GetInvocationTimeline(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationTimelineOpts{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		})
		if err != nil {
			return err
		}
		return writeAgentHistoryJSONFromDTO(stdout, result.Data.Entries, result.Data.NextCursor)
	}

	entries, err := fetchAllTimelineEntries(ctx, ns.client, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}
	checkpoints, err := fetchAllCheckpoints(ctx, ns.client, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}
	turns := daemon.ProjectTimelineTurns(entries, checkpoints)

	if opts.Last {
		if opts.JSON {
			if len(turns) == 0 {
				return writeAgentHistoryJSONFromDTO(stdout, []daemon.TimelineEntryDTO{}, "")
			}
			return writeAgentHistoryJSONFromDTO(stdout, daemon.TimelineEntriesForTurn(entries, turns, turns[len(turns)-1].EntryID), "")
		}
		if len(turns) == 0 {
			return writeAgentHistoryHumanFromTurns(stdout, nil, "")
		}
		return writeAgentHistoryHumanFromTurns(stdout, []daemon.Turn{turns[len(turns)-1]}, "")
	}

	if cursor := strings.TrimSpace(opts.Cursor); cursor != "" && !daemon.HistoryTurnExists(turns, cursor) {
		return errors.NewWithDetails(
			errors.EInvalidArgument,
			"invalid value for parameter 'cursor': turn id not found",
			map[string]string{
				"param":  "cursor",
				"cursor": cursor,
			},
		)
	}

	isInteractive := opts.IsInteractive
	if isInteractive == nil {
		isInteractive = func() bool {
			return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stdout.Fd())
		}
	}
	if isInteractive() && opts.Cursor == "" && opts.Limit == 50 {
		historyOutput := opts.HistoryOutput
		if historyOutput == nil {
			historyOutput = stdout
		}
		runHistory := opts.RunHistory
		if runHistory == nil {
			runHistory = watch.RunHistory
		}
		return runHistory(turns, watch.HistoryRunOptions{
			Input:  opts.HistoryInput,
			Output: historyOutput,
		})
	}

	page, nextCursor := daemon.PaginateHistoryTurns(turns, opts.Cursor, opts.Limit)
	return writeAgentHistoryHumanFromTurns(stdout, page, nextCursor)
}

// AgentHistoryLogsOpts holds options for the agent history logs command.
type AgentHistoryLogsOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoRef is the --repo flag value.
	RepoRef string

	// Kind is the log kind: raw, stderr, stream, hooks, terminal (default: raw).
	Kind string

	// Follow enables follow mode: poll for new data after reaching EOF.
	Follow bool

	// Offset is the byte offset to start reading from (default 0).
	Offset int64

	// SleepFn overrides time.Sleep for testing. If nil, uses time.Sleep.
	SleepFn func(time.Duration)

	// MaxIterations limits follow iterations for testing. 0 = unlimited.
	MaxIterations int

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentHistoryLogs views raw invocation logs via daemon offset-based API.
// Without --follow: pages to EOF and exits.
// With --follow: pages to EOF, then polls for new data until interrupted.
func AgentHistoryLogs(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentHistoryLogsOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent history logs",
	})
	if err != nil {
		return err
	}

	kind := strings.TrimSpace(opts.Kind)
	if kind == "" {
		kind = "raw"
	}

	offset := opts.Offset
	sleepFn := opts.SleepFn
	if sleepFn == nil {
		sleepFn = time.Sleep
	}

	pollInterval := 500 * time.Millisecond

	// Page to EOF
	for {
		result, err := ns.client.GetInvocationLogsOffset(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationLogsOffsetOpts{
			Kind:   kind,
			Offset: offset,
			Limit:  65536,
		})
		if err != nil {
			return err
		}

		if result.Data.DataB64 != "" {
			decoded, decErr := base64Decode(result.Data.DataB64)
			if decErr != nil {
				return errors.Wrap(errors.EInternal, "failed to decode log data", decErr)
			}
			_, _ = stdout.Write(decoded)
		}

		// No new data — we've reached EOF
		if result.Data.NextOffset == offset {
			break
		}
		offset = result.Data.NextOffset
	}

	// If not following, we're done
	if !opts.Follow {
		return nil
	}

	// Follow mode: poll for new data
	iterations := 0
	for {
		// Check context cancellation before sleeping
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		sleepFn(pollInterval)

		// Re-check after sleep — context may have been cancelled during sleep
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		result, err := ns.client.GetInvocationLogsOffset(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationLogsOffsetOpts{
			Kind:   kind,
			Offset: offset,
			Limit:  65536,
		})
		if err != nil {
			return err
		}

		if result.Data.DataB64 != "" {
			decoded, decErr := base64Decode(result.Data.DataB64)
			if decErr != nil {
				return errors.Wrap(errors.EInternal, "failed to decode log data", decErr)
			}
			_, _ = stdout.Write(decoded)
		}

		offset = result.Data.NextOffset

		iterations++
		if opts.MaxIterations > 0 && iterations >= opts.MaxIterations {
			break
		}
	}

	return nil
}

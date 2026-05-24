package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
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
	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, "", ResolveRepoContextOpts{
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

	invocations, fetchErr := ns.client.DrainInvocations(ctx, daemon.ListInvocationsParams{
		RepoID:      repoID,
		WorktreeRef: opts.WorktreeRef,
		State:       state,
	})
	if fetchErr != nil {
		return fetchErr
	}
	if opts.JSON {
		return writeCommandJSON(stdout, invocations)
	}
	return writeAgentLSHumanFromDTO(stdout, invocations)
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
	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, "", ResolveRepoContextOpts{
		RepoRef: opts.RepoRef,
		CmdName: "agent show",
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
		return writeCommandJSON(stdout, &result.Data)
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
	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, opts.DataDirOverride, ResolveRepoContextOpts{
		RepoRef: opts.RepoRef,
		CmdName: "agent check",
	})
	if err != nil {
		return err
	}

	result, err := ns.client.GetInvocationCheck(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeCommandJSON(stdout, result.Data)
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

	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, opts.DataDirOverride, ResolveRepoContextOpts{
		RepoRef: opts.RepoRef,
		CmdName: "agent history",
	})
	if err != nil {
		return err
	}

	isInteractive := opts.IsInteractive
	if isInteractive == nil {
		isInteractive = func() bool {
			return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stdout.Fd())
		}
	}
	if isInteractive() && !opts.JSON && !opts.Last && opts.Cursor == "" && opts.Limit == 50 {
		return launchWatchWorkspace(ctx, cr, fsys, cwd, stdout, stderr, watchLaunchOptions{
			initialPage:     watch.InitialPageHistory,
			invocationID:    opts.InvocationRef,
			repoID:          repoCtx.RepoID,
			isInteractive:   isInteractive,
			dataDirOverride: opts.DataDirOverride,
		})
	}

	// JSON remains the machine-fidelity escape hatch for raw timeline entries.
	// For default human history and --last resolution we project from shared turns
	// so history aligns with restore --turn semantics.
	if opts.JSON && !opts.Last {
		result, err := ns.client.GetInvocationTimeline(ctx, opts.InvocationRef, repoCtx.RepoID, daemon.GetTimelineParams{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		})
		if err != nil {
			return err
		}
		return writeCommandJSON(stdout, result.Data)
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
				return writeCommandJSON(stdout, daemon.InvocationTimelineData{
					Entries: []daemon.TimelineEntryDTO{},
				})
			}
			return writeCommandJSON(stdout, daemon.InvocationTimelineData{
				Entries: daemon.TimelineEntriesForTurn(entries, turns, turns[len(turns)-1].EntryID),
			})
		}
		if len(turns) == 0 {
			return writeAgentHistoryHumanFromTurns(stdout, nil, "")
		}
		return writeAgentHistoryHumanFromTurns(stdout, []daemon.Turn{turns[len(turns)-1]}, "")
	}

	if cursor := strings.TrimSpace(opts.Cursor); cursor != "" {
		found := false
		for _, turn := range turns {
			if turn.EntryID == cursor {
				found = true
				break
			}
		}
		if !found {
			return errors.NewWithDetails(
				errors.EInvalidArgument,
				"invalid value for parameter 'cursor': turn id not found",
				map[string]string{
					"param":  "cursor",
					"cursor": cursor,
				},
			)
		}
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

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentHistoryLogs views raw invocation logs via daemon offset-based API.
// Without --follow: pages to EOF and exits.
// With --follow: pages to EOF, then polls for new data until interrupted.
func AgentHistoryLogs(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentHistoryLogsOpts, stdout, stderr io.Writer) error {
	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, opts.DataDirOverride, ResolveRepoContextOpts{
		RepoRef: opts.RepoRef,
		CmdName: "agent history logs",
	})
	if err != nil {
		return err
	}

	kind := strings.TrimSpace(opts.Kind)
	if kind == "" {
		kind = daemon.InvocationLogKindRaw
	}

	offset := opts.Offset
	pollInterval := 500 * time.Millisecond

	nextOffset, err := ns.client.DrainInvocationLogs(ctx, opts.InvocationRef, repoCtx.RepoID, daemon.GetLogsParams{
		Kind:   kind,
		Offset: offset,
		Limit:  65536,
	}, stdout)
	if err != nil {
		return err
	}
	offset = nextOffset

	// If not following, we're done
	if !opts.Follow {
		return nil
	}

	// Follow mode: poll for new data
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}

		nextOffset, err := ns.client.DrainInvocationLogs(ctx, opts.InvocationRef, repoCtx.RepoID, daemon.GetLogsParams{
			Kind:   kind,
			Offset: offset,
			Limit:  65536,
		}, stdout)
		if err != nil {
			return err
		}
		offset = nextOffset
	}
}

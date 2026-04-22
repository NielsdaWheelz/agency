package commands

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/watch"
)

const defaultWatchInterval = 2 * time.Second

// WatchOpts holds options for the full-screen agency watch workspace.
type WatchOpts struct {
	Interval        time.Duration
	IsInteractive   func() bool
	DataDirOverride string
	Input           io.Reader
	Output          io.Writer
}

type watchActionDispatcher struct {
	cr              exec.CommandRunner
	fsys            fs.FS
	cwd             string
	dataDirOverride string
}

type watchLaunchOptions struct {
	initialPage     watch.InitialPage
	invocationID    string
	repoID          string
	interval        time.Duration
	input           io.Reader
	output          io.Writer
	isInteractive   func() bool
	dataDirOverride string
	runWatch        func(context.Context, *daemonclient.Client, watch.RunOptions) (watch.RunResult, error)
}

func (d *watchActionDispatcher) Open(ctx context.Context, invocationID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return AgentOpen(ctx, d.cr, d.fsys, d.cwd, AgentOpenOpts{
			InvocationRef:   invocationID,
			RepoRef:         repoID,
			DataDirOverride: d.dataDirOverride,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) PRSync(ctx context.Context, worktreeID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return WorktreePRSync(ctx, d.cr, d.fsys, d.cwd, WorktreePRSyncOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			DataDirOverride: d.dataDirOverride,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) Stop(ctx context.Context, invocationID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return AgentStop(ctx, d.cr, d.fsys, d.cwd, AgentStopOpts{
			InvocationRef: invocationID,
			RepoRef:       repoID,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) Kill(ctx context.Context, invocationID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return AgentKill(ctx, d.cr, d.fsys, d.cwd, AgentKillOpts{
			InvocationRef: invocationID,
			RepoRef:       repoID,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) Land(ctx context.Context, invocationID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return AgentLand(ctx, d.cr, d.fsys, d.cwd, AgentLandOpts{
			InvocationRef: invocationID,
			RepoRef:       repoID,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) Discard(ctx context.Context, invocationID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return AgentDiscard(ctx, d.cr, d.fsys, d.cwd, AgentDiscardOpts{
			InvocationRef: invocationID,
			RepoRef:       repoID,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) Recreate(ctx context.Context, invocationID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return AgentRecreate(ctx, d.cr, d.fsys, d.cwd, AgentRecreateOpts{
			InvocationRef:   invocationID,
			RepoRef:         repoID,
			Detached:        true,
			DataDirOverride: d.dataDirOverride,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) Followup(ctx context.Context, invocationID, repoID, prompt string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return AgentFollowup(ctx, d.cr, d.fsys, d.cwd, AgentFollowupOpts{
			InvocationRef:   invocationID,
			RepoRef:         repoID,
			Prompt:          prompt,
			DataDirOverride: d.dataDirOverride,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) PRMerge(ctx context.Context, worktreeID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return WorktreePRMerge(ctx, d.cr, d.fsys, d.cwd, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			Yes:             true,
			DataDirOverride: d.dataDirOverride,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) Rebase(ctx context.Context, worktreeID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return WorktreeRebase(ctx, d.cr, d.fsys, d.cwd, WorktreeRebaseOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			DataDirOverride: d.dataDirOverride,
		}, stdout, stderr)
	})
}

func (d *watchActionDispatcher) capture(run func(stdout, stderr io.Writer) error) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(&stdout, &stderr)

	output := strings.TrimSpace(stdout.String())
	if errText := strings.TrimSpace(stderr.String()); errText != "" {
		if output != "" {
			output += "\n"
		}
		output += errText
	}

	return output, err
}

func newWatchActionDispatcher(cr exec.CommandRunner, fsys fs.FS, cwd, dataDirOverride string) *watchActionDispatcher {
	return &watchActionDispatcher{
		cr:              cr,
		fsys:            fsys,
		cwd:             cwd,
		dataDirOverride: dataDirOverride,
	}
}

func launchWatchWorkspace(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, stdout, stderr io.Writer, opts watchLaunchOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	isInteractive := opts.isInteractive
	if isInteractive == nil {
		isInteractive = func() bool {
			return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stdout.Fd())
		}
	}
	if !isInteractive() {
		return errors.NewWithDetails(
			errors.ENotInteractive,
			"watch requires an interactive terminal",
			map[string]string{
				"hint": "run this command in an interactive terminal session",
			},
		)
	}

	var dataDir string
	if opts.dataDirOverride != "" {
		dataDir = opts.dataDirOverride
	} else {
		dirs, err := resolveCommandDirs("", "")
		if err != nil {
			return err
		}
		dataDir = dirs.DataDir
	}

	st := store.NewStore(fsys, dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}
	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	interval := opts.interval
	if interval <= 0 {
		interval = defaultWatchInterval
	}
	input := opts.input
	if input == nil {
		input = os.Stdin
	}
	output := opts.output
	if output == nil {
		output = stdout
	}

	dispatcher := newWatchActionDispatcher(cr, fsys, cwd, opts.dataDirOverride)
	runWatch := opts.runWatch
	if runWatch == nil {
		runWatch = watch.Run
	}

	runOpts := watch.RunOptions{
		InitialPage:  opts.initialPage,
		InvocationID: opts.invocationID,
		RepoID:       opts.repoID,
		Interval:     interval,
		Input:        input,
		Output:       output,
		Open:         dispatcher.Open,
		Stop:         dispatcher.Stop,
		Kill:         dispatcher.Kill,
		Land:         dispatcher.Land,
		Discard:      dispatcher.Discard,
		Recreate:     dispatcher.Recreate,
		Followup:     dispatcher.Followup,
		PRSync:       dispatcher.PRSync,
		PRMerge:      dispatcher.PRMerge,
		Rebase:       dispatcher.Rebase,
		Restore: func(ctx context.Context, invocationID, repoID, turnID string) (string, error) {
			return dispatcher.capture(func(stdout, stderr io.Writer) error {
				return AgentRestore(ctx, cr, fsys, cwd, AgentRestoreOpts{
					InvocationRef:   invocationID,
					RepoRef:         repoID,
					TurnID:          turnID,
					DataDirOverride: opts.dataDirOverride,
				}, stdout, stderr)
			})
		},
	}

	result, err := runWatch(ctx, client, runOpts)
	if err != nil {
		return err
	}
	if result.AttachInvocationID == "" || result.AttachRepoID == "" {
		return nil
	}
	return AgentAttach(ctx, cr, fsys, cwd, AgentAttachOpts{
		InvocationRef:   result.AttachInvocationID,
		RepoRef:         result.AttachRepoID,
		IsInteractive:   isInteractive,
		DataDirOverride: opts.dataDirOverride,
	}, stdout, stderr)
}

// Watch launches the full-screen, daemon-backed watch workspace.
func Watch(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WatchOpts, stdout, stderr io.Writer) error {
	return launchWatchWorkspace(ctx, cr, fsys, cwd, stdout, stderr, watchLaunchOptions{
		initialPage:     watch.InitialPageWorkspace,
		interval:        opts.Interval,
		input:           opts.Input,
		output:          opts.Output,
		isInteractive:   opts.IsInteractive,
		dataDirOverride: opts.DataDirOverride,
		runWatch:        watch.Run,
	})
}

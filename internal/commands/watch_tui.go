package commands

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
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

type watchEnterDelegate func(context.Context, exec.CommandRunner, fs.FS, string, AgentEnterOpts, io.Writer, io.Writer) error
type watchOpenDelegate func(context.Context, exec.CommandRunner, fs.FS, string, AgentOpenOpts, io.Writer, io.Writer) error
type watchPRSyncDelegate func(context.Context, exec.CommandRunner, fs.FS, string, WorktreePRSyncOpts, io.Writer, io.Writer) error

// watchActionDelegates forwards in-watch actions to canonical command contracts.
// It intentionally does not implement watch-specific policy logic.
type watchActionDelegates struct {
	cr              exec.CommandRunner
	fsys            fs.FS
	cwd             string
	dataDirOverride string
	stdout          io.Writer
	stderr          io.Writer

	enterFn  watchEnterDelegate
	openFn   watchOpenDelegate
	prSyncFn watchPRSyncDelegate
}

func newWatchActionDelegates(cr exec.CommandRunner, fsys fs.FS, cwd, dataDirOverride string, stdout, stderr io.Writer) *watchActionDelegates {
	return &watchActionDelegates{
		cr:              cr,
		fsys:            fsys,
		cwd:             cwd,
		dataDirOverride: dataDirOverride,
		stdout:          stdout,
		stderr:          stderr,
		enterFn: func(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentEnterOpts, stdout, stderr io.Writer) error {
			return AgentEnter(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
		openFn: func(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentOpenOpts, stdout, stderr io.Writer) error {
			return AgentOpen(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
		prSyncFn: func(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePRSyncOpts, stdout, stderr io.Writer) error {
			return WorktreePRSync(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}
}

func (d *watchActionDelegates) Enter(ctx context.Context, invocationID, repoID string) error {
	return d.enterFn(ctx, d.cr, d.fsys, d.cwd, AgentEnterOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		DataDirOverride: d.dataDirOverride,
	}, d.stdout, d.stderr)
}

func (d *watchActionDelegates) Open(ctx context.Context, invocationID, repoID string) error {
	return d.openFn(ctx, d.cr, d.fsys, d.cwd, AgentOpenOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		DataDirOverride: d.dataDirOverride,
	}, d.stdout, d.stderr)
}

func (d *watchActionDelegates) PRSync(ctx context.Context, worktreeID, repoID string) error {
	return d.prSyncFn(ctx, d.cr, d.fsys, d.cwd, WorktreePRSyncOpts{
		WorktreeRef:     worktreeID,
		RepoFlag:        repoID,
		DataDirOverride: d.dataDirOverride,
	}, d.stdout, d.stderr)
}

// Watch launches the full-screen, daemon-backed watch workspace.
func Watch(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WatchOpts, stdout, stderr io.Writer) error {
	_ = stderr
	if ctx == nil {
		ctx = context.Background()
	}

	isInteractive := opts.IsInteractive
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
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(errors.EInternal, "failed to get home directory", err)
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
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

	interval := opts.Interval
	if interval <= 0 {
		interval = defaultWatchInterval
	}
	input := opts.Input
	if input == nil {
		input = os.Stdin
	}
	output := opts.Output
	if output == nil {
		output = stdout
	}

	loader := watch.NewSnapshotLoader(client)
	actionDelegates := newWatchActionDelegates(cr, fsys, cwd, opts.DataDirOverride, io.Discard, io.Discard)
	return watch.Run(ctx, loader, watch.RunOptions{
		Interval: interval,
		Input:    input,
		Output:   output,
		Actions:  actionDelegates,
	})
}

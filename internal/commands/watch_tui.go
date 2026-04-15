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

type watchActionDispatcher struct {
	cr              exec.CommandRunner
	fsys            fs.FS
	cwd             string
	dataDirOverride string
}

func (d *watchActionDispatcher) Enter(ctx context.Context, invocationID, repoID string) (string, error) {
	return d.capture(func(stdout, stderr io.Writer) error {
		return AgentEnter(ctx, d.cr, d.fsys, d.cwd, AgentEnterOpts{
			InvocationRef:   invocationID,
			RepoRef:         repoID,
			DataDirOverride: d.dataDirOverride,
		}, stdout, stderr)
	})
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
	actionDelegates := &watchActionDispatcher{
		cr:              cr,
		fsys:            fsys,
		cwd:             cwd,
		dataDirOverride: opts.DataDirOverride,
	}
	return watch.Run(ctx, loader, watch.RunOptions{
		Interval: interval,
		Input:    input,
		Output:   output,
		Actions:  actionDelegates,
	})
}

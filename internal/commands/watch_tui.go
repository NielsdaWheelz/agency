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

// Watch launches the full-screen, daemon-backed watch workspace.
func Watch(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WatchOpts, stdout, stderr io.Writer) error {
	_ = cr
	_ = cwd
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
	return watch.Run(ctx, loader, watch.RunOptions{
		Interval: interval,
		Input:    input,
		Output:   output,
	})
}

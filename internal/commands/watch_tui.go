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

	capture := func(run func(stdout, stderr io.Writer) error) (string, error) {
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
		Open: func(ctx context.Context, invocationID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return AgentOpen(ctx, cr, fsys, cwd, AgentOpenOpts{
					InvocationRef:   invocationID,
					RepoRef:         repoID,
					DataDirOverride: opts.dataDirOverride,
				}, stdout, stderr)
			})
		},
		Stop: func(ctx context.Context, invocationID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return AgentStop(ctx, cr, fsys, cwd, AgentStopOpts{
					InvocationRef: invocationID,
					RepoRef:       repoID,
				}, stdout, stderr)
			})
		},
		Kill: func(ctx context.Context, invocationID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return AgentKill(ctx, cr, fsys, cwd, AgentKillOpts{
					InvocationRef: invocationID,
					RepoRef:       repoID,
				}, stdout, stderr)
			})
		},
		Land: func(ctx context.Context, invocationID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return AgentLand(ctx, cr, fsys, cwd, AgentLandOpts{
					InvocationRef: invocationID,
					RepoRef:       repoID,
				}, stdout, stderr)
			})
		},
		Discard: func(ctx context.Context, invocationID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return AgentDiscard(ctx, cr, fsys, cwd, AgentDiscardOpts{
					InvocationRef: invocationID,
					RepoRef:       repoID,
				}, stdout, stderr)
			})
		},
		Recreate: func(ctx context.Context, invocationID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return AgentRecreate(ctx, cr, fsys, cwd, AgentRecreateOpts{
					InvocationRef:   invocationID,
					RepoRef:         repoID,
					Detached:        true,
					DataDirOverride: opts.dataDirOverride,
				}, stdout, stderr)
			})
		},
		Followup: func(ctx context.Context, invocationID, repoID, prompt string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return AgentFollowup(ctx, cr, fsys, cwd, AgentFollowupOpts{
					InvocationRef:   invocationID,
					RepoRef:         repoID,
					Prompt:          prompt,
					DataDirOverride: opts.dataDirOverride,
				}, stdout, stderr)
			})
		},
		PRSync: func(ctx context.Context, worktreeID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return WorktreePRSync(ctx, cr, fsys, cwd, WorktreePRSyncOpts{
					WorktreeRef:     worktreeID,
					RepoRef:         repoID,
					DataDirOverride: opts.dataDirOverride,
				}, stdout, stderr)
			})
		},
		PRMerge: func(ctx context.Context, worktreeID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return WorktreePRMerge(ctx, cr, fsys, cwd, WorktreePRMergeOpts{
					WorktreeRef:     worktreeID,
					RepoRef:         repoID,
					Yes:             true,
					DataDirOverride: opts.dataDirOverride,
				}, stdout, stderr)
			})
		},
		Rebase: func(ctx context.Context, worktreeID, repoID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
				return WorktreeRebase(ctx, cr, fsys, cwd, WorktreeRebaseOpts{
					WorktreeRef:     worktreeID,
					RepoRef:         repoID,
					DataDirOverride: opts.dataDirOverride,
				}, stdout, stderr)
			})
		},
		Restore: func(ctx context.Context, invocationID, repoID, turnID string) (string, error) {
			return capture(func(stdout, stderr io.Writer) error {
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

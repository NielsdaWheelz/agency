package commands

import (
	"context"
	"io"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// TaskTargetOpts holds options for target-first task commands.
type TaskTargetOpts struct {
	Args    []string
	RepoRef string

	JSON               bool
	Mode               string
	Runner             string
	InvocationName     string
	Detached           bool
	Prompt             string
	PromptFile         string
	AgencyConfigPath   string
	ExecutionProfile   string
	RunnerArgs         []string
	Model              string
	Effort             string
	PermissionMode     string
	NoIncludeUntracked bool
}

const (
	// TaskTargetActionArchive archives one task.
	TaskTargetActionArchive = "archive"

	// TaskTargetActionWatch watches one task's primary invocation.
	TaskTargetActionWatch = "watch"

	// TaskTargetActionRetry starts a new invocation for one task.
	TaskTargetActionRetry = "retry"
)

// TaskTargetFlagPolicy returns the target-level flag policy for `agency task` args.
func TaskTargetFlagPolicy(args []string) (targetFlagPolicy, bool) {
	switch {
	case len(args) == 0:
		return targetFlagPolicy{}, false
	case len(args) == 1:
		return newTargetFlagPolicy("<task-ref>", "json"), true
	case len(args) == 2:
		switch args[1] {
		case TaskTargetActionArchive:
			return newTargetFlagPolicy(TaskTargetActionArchive, "json"), true
		case TaskTargetActionWatch:
			return newTargetFlagPolicy(TaskTargetActionWatch), true
		case TaskTargetActionRetry:
			return newTargetFlagPolicy(TaskTargetActionRetry, "json", "mode", "runner", "name", "detached", "prompt", "prompt-file", "agency-config", "execution-profile", "runner-arg", "model", "effort", "permission-mode", "no-include-untracked"), true
		}
	}
	return targetFlagPolicy{}, false
}

// TaskTarget dispatches target-first task commands owned by internal/commands.
func TaskTarget(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts TaskTargetOpts, stdout, stderr io.Writer) error {
	args := opts.Args
	if len(args) == 0 {
		return errors.New(errors.EUsage, "specify a task ref")
	}

	taskRef := args[0]
	switch {
	case len(args) == 1:
		return TaskShow(ctx, cr, fsys, cwd, TaskShowOpts{
			TaskRef: taskRef,
			RepoRef: opts.RepoRef,
			JSON:    opts.JSON,
		}, stdout, stderr)
	case len(args) == 2:
		switch args[1] {
		case TaskTargetActionArchive:
			return TaskArchive(ctx, cr, fsys, cwd, TaskArchiveOpts{
				TaskRef: taskRef,
				RepoRef: opts.RepoRef,
				JSON:    opts.JSON,
			}, stdout, stderr)
		case TaskTargetActionWatch:
			return TaskWatch(ctx, cr, fsys, cwd, TaskWatchOpts{
				TaskRef:  taskRef,
				RepoRef:  opts.RepoRef,
				Interval: (2 * time.Second).String(),
			}, stdout, stderr)
		case TaskTargetActionRetry:
			return TaskRetry(ctx, cr, fsys, cwd, TaskRetryOpts{
				TaskRef:            taskRef,
				RepoRef:            opts.RepoRef,
				Mode:               opts.Mode,
				Runner:             opts.Runner,
				InvocationName:     opts.InvocationName,
				Detached:           opts.Detached,
				Prompt:             opts.Prompt,
				PromptFile:         opts.PromptFile,
				AgencyConfigPath:   opts.AgencyConfigPath,
				ExecutionProfile:   opts.ExecutionProfile,
				RunnerArgs:         opts.RunnerArgs,
				Model:              opts.Model,
				Effort:             opts.Effort,
				PermissionMode:     opts.PermissionMode,
				JSON:               opts.JSON,
				NoIncludeUntracked: opts.NoIncludeUntracked,
			}, stdout, stderr)
		default:
			return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency task\"")
		}
	default:
		return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency task\"")
	}
}

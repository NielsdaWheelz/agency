package watch

import (
	"context"
	stderrors "errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

// RunOptions controls watch runtime behavior.
type RunOptions struct {
	InitialPage  InitialPage
	InvocationID string
	RepoID       string
	Interval     time.Duration
	Input        io.Reader
	Output       io.Writer
	Open         func(context.Context, string, string) (string, error)
	Stop         func(context.Context, string, string) (string, error)
	Kill         func(context.Context, string, string) (string, error)
	Land         func(context.Context, string, string) (string, error)
	Discard      func(context.Context, string, string) (string, error)
	Recreate     func(context.Context, string, string) (string, error)
	Followup     func(context.Context, string, string, string) (string, error)
	PRSync       func(context.Context, string, string) (string, error)
	PRMerge      func(context.Context, string, string) (string, error)
	Rebase       func(context.Context, string, string) (string, error)
	Restore      func(context.Context, string, string, string) (string, error)
}

type RunResult struct {
	AttachInvocationID string
	AttachRepoID       string
}

// Run launches the full-screen watch TUI.
func Run(ctx context.Context, client *daemonclient.Client, opts RunOptions) (RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return RunResult{}, errors.New(errors.EInternal, "watch runtime requires a daemon client")
	}

	switch opts.InitialPage {
	case "", InitialPageWorkspace:
	case InitialPageHistory:
		if opts.InvocationID == "" || opts.RepoID == "" {
			return RunResult{}, errors.New(errors.EInvalidArgument, "history page requires an invocation and repo")
		}
	default:
		return RunResult{}, errors.New(errors.EInternal, "unknown watch initial page")
	}

	m := newModel(ctx, client, opts)
	if opts.InitialPage == InitialPageHistory {
		invocation, err := client.GetInvocation(ctx, opts.InvocationID, opts.RepoID)
		if err != nil {
			return RunResult{}, err
		}
		m.snapshot.Invocations = []daemon.InvocationDTO{invocation.Data}
		m.selectedInvocationID = invocation.Data.InvocationID
		m.selectedRepoID = invocation.Data.RepoID
		m.selectedMode = invocation.Data.Mode
		m.selectedStatus = invocation.Data.Status
		repos, err := client.ListRepos(ctx)
		if err == nil {
			for _, repo := range repos.Data.Repos {
				if repo.RepoID == opts.RepoID {
					m.snapshot.Repos = []daemon.RepoDTO{repo}
					break
				}
			}
		}
		if invocation.Data.WorktreeID != "" {
			worktree, err := client.GetWorktree(ctx, invocation.Data.WorktreeID, opts.RepoID)
			if err == nil {
				m.snapshot.Worktrees = []daemon.WorktreeDTO{worktree.Data}
			}
		}
		check, err := client.GetInvocationCheck(ctx, opts.InvocationID, opts.RepoID)
		if err == nil {
			m.snapshot.Checks = map[string]daemon.InvocationCheckData{
				opts.InvocationID: check.Data,
			}
		}
		turns, err := loadHistoryTurns(ctx, client, opts.InvocationID, opts.RepoID)
		if err != nil {
			return RunResult{}, err
		}
		m.historyTurns = turns
		m.reconcileHistorySelection()
	}

	programOptions := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.Input != nil {
		programOptions = append(programOptions, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		programOptions = append(programOptions, tea.WithOutput(opts.Output))
	}

	program := tea.NewProgram(m, programOptions...)
	finalModel, err := program.Run()
	if err == nil {
		resolvedModel, ok := finalModel.(model)
		if !ok {
			return RunResult{}, nil
		}
		invocationID, repoID, ok := resolvedModel.requestedAttach()
		if !ok {
			return RunResult{}, nil
		}
		return RunResult{
			AttachInvocationID: invocationID,
			AttachRepoID:       repoID,
		}, nil
	}
	if stderrors.Is(err, context.Canceled) || stderrors.Is(err, tea.ErrInterrupted) {
		return RunResult{}, nil
	}
	return RunResult{}, err
}

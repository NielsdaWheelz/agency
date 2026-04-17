package watch

import (
	"context"
	stderrors "errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"

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
	Attach       func(context.Context, string, string) (string, error)
	Open         func(context.Context, string, string) (string, error)
	PRSync       func(context.Context, string, string) (string, error)
	Restore      func(context.Context, string, string, string) (string, error)
}

// Run launches the full-screen watch TUI.
func Run(ctx context.Context, client *daemonclient.Client, opts RunOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return errors.New(errors.EInternal, "watch runtime requires a daemon client")
	}

	switch opts.InitialPage {
	case "", InitialPageWorkspace:
	case InitialPageHistory:
		if opts.InvocationID == "" || opts.RepoID == "" {
			return errors.New(errors.EInvalidArgument, "history page requires an invocation and repo")
		}
	default:
		return errors.New(errors.EInternal, "unknown watch initial page")
	}

	m := newModel(ctx, client, opts)
	if opts.InitialPage == InitialPageHistory {
		turns, err := loadHistoryTurns(ctx, client, opts.InvocationID, opts.RepoID)
		if err != nil {
			return err
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
	_, err := program.Run()
	if err == nil {
		return nil
	}
	if stderrors.Is(err, context.Canceled) || stderrors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

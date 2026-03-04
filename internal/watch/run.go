package watch

import (
	"context"
	stderrors "errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// RunOptions controls watch runtime behavior.
type RunOptions struct {
	Interval time.Duration
	Input    io.Reader
	Output   io.Writer
	Actions  ActionDispatcher
}

// Run launches the full-screen watch TUI.
func Run(ctx context.Context, loader loader, opts RunOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if loader == nil {
		return errors.New(errors.EInternal, "watch runtime requires a snapshot loader")
	}

	m := newModel(ctx, loader, opts.Interval, opts.Actions)
	programOptions := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.Input != nil {
		programOptions = append(programOptions, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		programOptions = append(programOptions, tea.WithOutput(opts.Output))
	}

	p := tea.NewProgram(m, programOptions...)
	_, err := p.Run()
	if err == nil {
		return nil
	}
	if stderrors.Is(err, context.Canceled) || stderrors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

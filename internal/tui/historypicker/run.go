package historypicker

import (
	stderrors "errors"
	"io"

	tea "charm.land/bubbletea/v2"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// RunOptions controls picker runtime behavior.
type RunOptions struct {
	Input   io.Reader
	Output  io.Writer
	NoColor bool
}

// Run launches the interactive history picker and returns the selected Turn.
// Returns EAborted if the user cancels.
func Run(turns []Turn, opts RunOptions) (Turn, error) {
	if len(turns) == 0 {
		return Turn{}, errors.New(errors.ECheckpointNotFound, "no history entries available for selection")
	}

	m := newModel(turns, Options{NoColor: opts.NoColor})
	programOptions := []tea.ProgramOption{}
	if opts.Input != nil {
		programOptions = append(programOptions, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		programOptions = append(programOptions, tea.WithOutput(opts.Output))
	}

	p := tea.NewProgram(m, programOptions...)
	finalModel, err := p.Run()
	if err != nil {
		if stderrors.Is(err, tea.ErrInterrupted) {
			return Turn{}, errors.New(errors.EAborted, "history selection canceled")
		}
		return Turn{}, errors.Wrap(errors.EInternal, "history picker failed", err)
	}

	fm := finalModel.(model)
	if fm.canceled {
		return Turn{}, errors.New(errors.EAborted, "history selection canceled")
	}
	if !fm.confirmed || len(fm.turns) == 0 {
		return Turn{}, errors.New(errors.EAborted, "history selection canceled")
	}

	return fm.turns[fm.selectedIndex], nil
}

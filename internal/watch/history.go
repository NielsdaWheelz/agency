package watch

import (
	stderrors "errors"
	"io"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/render"
)

type historyKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Top    key.Binding
	Bottom key.Binding
	Quit   key.Binding
}

func (k historyKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Quit}
}

var defaultHistoryKeys = historyKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("up/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("down/j", "move down"),
	),
	Top: key.NewBinding(
		key.WithKeys("home", "g"),
		key.WithHelp("home/g", "jump to top"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("end", "G"),
		key.WithHelp("end/G", "jump to bottom"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "esc", "ctrl+c"),
		key.WithHelp("q/esc", "quit"),
	),
}

var (
	historyHeaderStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	historySelectedStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	historyMarkerStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	historyCheckpointStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	historyToolCallStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	historyDimStyle        = lipgloss.NewStyle().Faint(true)
	historySeparatorStyle  = lipgloss.NewStyle().Faint(true)
	historyHelpStyle       = lipgloss.NewStyle().Faint(true)
)

type HistoryRunOptions struct {
	Input   io.Reader
	Output  io.Writer
	NoColor bool
}

func RunHistory(turns []daemon.Turn, opts HistoryRunOptions) error {
	if len(turns) == 0 {
		return errors.New(errors.ECheckpointNotFound, "no history entries available")
	}

	m := newHistoryModel(turns, opts.NoColor)
	programOptions := []tea.ProgramOption{}
	if opts.Input != nil {
		programOptions = append(programOptions, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		programOptions = append(programOptions, tea.WithOutput(opts.Output))
	}

	program := tea.NewProgram(m, programOptions...)
	_, err := program.Run()
	if err != nil {
		if stderrors.Is(err, tea.ErrInterrupted) {
			return nil
		}
		return errors.Wrap(errors.EInternal, "history view failed", err)
	}
	return nil
}

func newHistoryModel(turns []daemon.Turn, noColor bool) model {
	selectedIndex := len(turns) - 1
	if selectedIndex < 0 {
		selectedIndex = 0
	}

	return model{
		mode:                 modeHistory,
		historyTurns:         turns,
		historySelectedIndex: selectedIndex,
		historyNoColor:       noColor,
		historyKeys:          defaultHistoryKeys,
	}
}

func (m model) updateHistory(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.historyKeys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.historyKeys.Up):
		if m.historySelectedIndex > 0 {
			m.historySelectedIndex--
		}
		return m, nil
	case key.Matches(msg, m.historyKeys.Down):
		if m.historySelectedIndex < len(m.historyTurns)-1 {
			m.historySelectedIndex++
		}
		return m, nil
	case key.Matches(msg, m.historyKeys.Top):
		m.historySelectedIndex = 0
		return m, nil
	case key.Matches(msg, m.historyKeys.Bottom):
		if len(m.historyTurns) > 0 {
			m.historySelectedIndex = len(m.historyTurns) - 1
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m model) renderHistory() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	if len(m.historyTurns) == 0 {
		return "no history entries available"
	}

	var builder strings.Builder
	builder.WriteString(m.renderHistoryHeader("invocation history"))
	builder.WriteString("\n\n")

	for index, turn := range m.historyTurns {
		m.renderHistoryTurn(&builder, index, turn, width)
	}

	builder.WriteString("\n")
	bindings := m.historyKeys.ShortHelp()
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		parts = append(parts, help.Key+": "+help.Desc)
	}
	builder.WriteString(m.renderHistoryHelp(strings.Join(parts, " • ")))

	return builder.String()
}

func (m model) renderHistoryTurn(builder *strings.Builder, index int, turn daemon.Turn, width int) {
	isSelected := index == m.historySelectedIndex
	marker := "  "
	if isSelected {
		marker = m.renderHistoryMarker("> ")
	}

	timestamp := strings.TrimSpace(turn.ShortTimestamp)
	if timestamp == "" {
		timestamp = strings.TrimSpace(turn.Timestamp)
	}
	if timestamp == "" {
		timestamp = "-"
	}

	kindLabel := "[" + render.NormalizeActivityKind(string(turn.Kind)) + "]"
	header := marker + kindLabel + " (" + timestamp + ")"
	if turn.Restorable && turn.CheckpointID > 0 {
		header += " " + m.renderHistoryCheckpoint("cp:"+strconv.Itoa(turn.CheckpointID))
	}

	visibleLen := historyVisibleLen(header)
	remaining := width - visibleLen - 1
	if remaining > 2 {
		header += " " + m.renderHistorySeparator(strings.Repeat("─", remaining))
	}
	if !turn.Restorable && !isSelected {
		header = m.renderHistoryDim(header)
	}
	if isSelected {
		header = m.renderHistorySelected(header)
	}
	builder.WriteString(header)
	builder.WriteString("\n")

	summaryText := render.ActivitySummaryText(string(turn.Kind), turn.Summary)
	if summaryText != "" {
		summaryLine := "    " + historyTruncate(summaryText, width-4)
		if !turn.Restorable && !isSelected {
			summaryLine = m.renderHistoryDim(summaryLine)
		}
		builder.WriteString(summaryLine)
		builder.WriteString("\n")
	}

	if turn.Restorable && len(turn.CheckpointChangedPaths) > 0 {
		pathsSummary := render.FormatChangedPathSummary(turn.CheckpointChangedPaths, turn.CheckpointChangedCount, turn.CheckpointPathsTrimmed)
		builder.WriteString("    files: ")
		builder.WriteString(historyTruncate(pathsSummary, width-11))
		builder.WriteString("\n")
	}

	for _, toolCall := range turn.ToolCalls {
		builder.WriteString("    ")
		builder.WriteString(m.renderHistoryToolCall(render.FormatToolCallSummary(toolCall.Name, toolCall.Command, toolCall.HasExit, toolCall.ExitCode)))
		builder.WriteString("\n")
	}

	if index < len(m.historyTurns)-1 {
		builder.WriteString("\n")
	}
}

func historyVisibleLen(value string) int {
	visibleLen := 0
	inEscape := false
	for _, r := range value {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		visibleLen++
	}
	return visibleLen
}

func historyTruncate(value string, maxWidth int) string {
	trimmed := strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	return truncateWithEllipsis(trimmed, maxWidth)
}

func (m model) renderHistoryHeader(value string) string {
	if m.historyNoColor {
		return value
	}
	return historyHeaderStyle.Render(value)
}

func (m model) renderHistorySelected(value string) string {
	if m.historyNoColor {
		return value
	}
	return historySelectedStyle.Render(value)
}

func (m model) renderHistoryMarker(value string) string {
	if m.historyNoColor {
		return value
	}
	return historyMarkerStyle.Render(value)
}

func (m model) renderHistoryCheckpoint(value string) string {
	if m.historyNoColor {
		return value
	}
	return historyCheckpointStyle.Render(value)
}

func (m model) renderHistoryToolCall(value string) string {
	if m.historyNoColor {
		return value
	}
	return historyToolCallStyle.Render(value)
}

func (m model) renderHistoryDim(value string) string {
	if m.historyNoColor {
		return value
	}
	return historyDimStyle.Render(value)
}

func (m model) renderHistorySeparator(value string) string {
	if m.historyNoColor {
		return value
	}
	return historySeparatorStyle.Render(value)
}

func (m model) renderHistoryHelp(value string) string {
	if m.historyNoColor {
		return value
	}
	return historyHelpStyle.Render(value)
}

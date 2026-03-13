package historypicker

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/render"
)

// Options controls picker behavior.
type Options struct {
	NoColor bool
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Top     key.Binding
	Bottom  key.Binding
	Confirm key.Binding
	Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Confirm, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Confirm, k.Quit},
	}
}

var defaultKeys = keyMap{
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
	Confirm: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select checkpoint"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "esc", "ctrl+c"),
		key.WithHelp("q/esc", "quit"),
	),
}

type model struct {
	turns         []Turn
	selectedIndex int
	keys          keyMap
	noColor       bool

	width  int
	height int

	confirmed bool
	canceled  bool
}

func newModel(turns []Turn, opts Options) model {
	selected := len(turns) - 1
	if selected < 0 {
		selected = 0
	}

	return model{
		turns:         turns,
		selectedIndex: selected,
		keys:          defaultKeys,
		noColor:       opts.NoColor,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.canceled = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Confirm):
			if len(m.turns) > 0 {
				m.confirmed = true
				return m, tea.Quit
			}
			return m, nil
		case key.Matches(msg, m.keys.Up):
			if m.selectedIndex > 0 {
				m.selectedIndex--
			}
			return m, nil
		case key.Matches(msg, m.keys.Down):
			if m.selectedIndex < len(m.turns)-1 {
				m.selectedIndex++
			}
			return m, nil
		case key.Matches(msg, m.keys.Top):
			m.selectedIndex = 0
			return m, nil
		case key.Matches(msg, m.keys.Bottom):
			if len(m.turns) > 0 {
				m.selectedIndex = len(m.turns) - 1
			}
			return m, nil
		}
	}

	return m, nil
}

func (m model) View() tea.View {
	content := m.renderView()
	v := tea.NewView(content)
	v.AltScreen = true
	v.Cursor = nil
	return v
}

func (m model) renderView() string {
	width := m.width
	if width <= 0 {
		width = 120
	}

	if len(m.turns) == 0 {
		return "no history entries available"
	}

	var b strings.Builder

	b.WriteString(m.styleHeader("select history point to restart from"))
	b.WriteString("\n\n")

	for i, turn := range m.turns {
		m.renderTurn(&b, i, turn, width)
	}

	b.WriteString("\n")
	b.WriteString(m.styleHelp(formatHelp(m.keys)))

	return b.String()
}

func (m model) renderTurn(b *strings.Builder, idx int, turn Turn, width int) {
	isSelected := idx == m.selectedIndex

	// Selection marker
	marker := "  "
	if isSelected {
		marker = m.styleMarker("> ")
	}

	// Build the header line: marker + kind label + timestamp + checkpoint badge
	kindLabel := "[" + render.NormalizeActivityKind(string(turn.Kind)) + "]"
	header := fmt.Sprintf("%s%s (%s)", marker, kindLabel, turn.ShortTimestamp)

	if turn.Restorable && turn.CheckpointID > 0 {
		header += " " + m.styleCheckpoint(fmt.Sprintf("cp:%d", turn.CheckpointID))
	}

	// Separator line
	remaining := width - visibleLen(header) - 1
	if remaining > 2 {
		header += " " + m.styleSeparator(strings.Repeat("─", remaining))
	}

	if !turn.Restorable && !isSelected {
		header = m.styleDim(header)
	}
	if isSelected {
		header = m.styleSelected(header)
	}

	b.WriteString(header)
	b.WriteString("\n")

	// Summary text (indented)
	summaryText := render.ActivitySummaryText(string(turn.Kind), turn.Summary)
	if summaryText != "" {
		summary := truncate(summaryText, width-4)
		summaryLine := "    " + summary
		if !turn.Restorable && !isSelected {
			summaryLine = m.styleDim(summaryLine)
		}
		b.WriteString(summaryLine)
		b.WriteString("\n")
	}

	if turn.Restorable && len(turn.CheckpointChangedPaths) > 0 {
		pathsSummary := formatCheckpointChangedPaths(turn)
		pathsLine := "    files: " + truncate(pathsSummary, width-11)
		b.WriteString(pathsLine)
		b.WriteString("\n")
	}

	// Tool calls (indented)
	for _, tc := range turn.ToolCalls {
		tcLine := "    " + m.styleToolCall(formatToolCall(tc))
		b.WriteString(tcLine)
		b.WriteString("\n")
	}

	// Blank line between turns for readability (except after last)
	if idx < len(m.turns)-1 {
		b.WriteString("\n")
	}
}

func formatToolCall(tc ToolCall) string {
	return render.FormatToolCallSummary(tc.Name, tc.Command, tc.HasExit, tc.ExitCode)
}

func formatHelp(keys keyMap) string {
	bindings := keys.ShortHelp()
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		parts = append(parts, h.Key+": "+h.Desc)
	}
	return strings.Join(parts, " • ")
}

func formatCheckpointChangedPaths(turn Turn) string {
	if len(turn.CheckpointChangedPaths) == 0 {
		return ""
	}
	joined := strings.Join(turn.CheckpointChangedPaths, ", ")
	if turn.CheckpointPathsTrimmed {
		remaining := turn.CheckpointChangedCount - len(turn.CheckpointChangedPaths)
		if remaining < 0 {
			remaining = 0
		}
		if remaining > 0 {
			joined += fmt.Sprintf(", ... (+%d more)", remaining)
		}
	}
	return joined
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	runes := []rune(s)
	if max <= 0 || len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func visibleLen(s string) int {
	// Strip ANSI escape sequences for length calculation
	inEscape := false
	n := 0
	for _, r := range s {
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
		n++
	}
	return n
}

// Styling methods — all respect NoColor.

var (
	headerStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selectedStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	markerStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	checkpointStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	toolCallStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	dimStyle        = lipgloss.NewStyle().Faint(true)
	separatorStyle  = lipgloss.NewStyle().Faint(true)
	helpStyle       = lipgloss.NewStyle().Faint(true)
)

func (m model) styleHeader(s string) string {
	if m.noColor {
		return s
	}
	return headerStyle.Render(s)
}

func (m model) styleSelected(s string) string {
	if m.noColor {
		return s
	}
	return selectedStyle.Render(s)
}

func (m model) styleMarker(s string) string {
	if m.noColor {
		return s
	}
	return markerStyle.Render(s)
}

func (m model) styleCheckpoint(s string) string {
	if m.noColor {
		return s
	}
	return checkpointStyle.Render(s)
}

func (m model) styleToolCall(s string) string {
	if m.noColor {
		return s
	}
	return toolCallStyle.Render(s)
}

func (m model) styleDim(s string) string {
	if m.noColor {
		return s
	}
	return dimStyle.Render(s)
}

func (m model) styleSeparator(s string) string {
	if m.noColor {
		return s
	}
	return separatorStyle.Render(s)
}

func (m model) styleHelp(s string) string {
	if m.noColor {
		return s
	}
	return helpStyle.Render(s)
}

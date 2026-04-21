package watch

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

type reviewFile struct {
	key      string
	title    string
	section  string
	diffstat string
	added    int
	deleted  int
	lines    []string
}

var (
	reviewFocusedTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	reviewMetaStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	reviewAddedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	reviewDeletedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

func (m model) renderReview() string {
	width := m.width
	if width <= 0 {
		width = 120
	}

	lines := m.renderPageHeader("review")
	lines = append(lines, m.renderReviewSummary(width)...)
	lines = append(lines, "")

	if m.lastActionMessage != "" {
		actionLine := "action: " + truncateWithEllipsis(m.lastActionMessage, width-10)
		switch {
		case m.lastActionError:
			lines = append(lines, errorStyle.Render(actionLine))
		case m.actionRunning:
			lines = append(lines, warningStyle.Render(actionLine))
		default:
			lines = append(lines, actionStyle.Render(actionLine))
		}
		lines = append(lines, "")
	}
	if m.reviewError != "" {
		lines = append(lines, errorStyle.Render("review error: "+truncateWithEllipsis(m.reviewError, width-4)))
		lines = append(lines, "")
	}
	if m.reviewLoading {
		lines = append(lines, warningStyle.Render("loading review..."))
		lines = append(lines, "")
	}

	lines = append(lines, m.renderReviewPanels(width))
	lines = append(lines, "")
	lines = append(lines, warningStyle.Render("j/k move • tab pane • space reviewed • n/N unreviewed • a attach • x actions • r refresh • b back • q quit"))
	return strings.Join(lines, "\n")
}

func (m model) renderReviewSummary(width int) []string {
	selected, ok := m.selectedInvocation()
	if !ok {
		return []string{dimStyle.Render("selected invocation unavailable")}
	}

	lines := make([]string, 0, 12)

	state := firstNonEmpty(strings.TrimSpace(m.reviewCheck.State), strings.TrimSpace(selected.State), "-")
	reason := firstNonEmpty(strings.TrimSpace(m.reviewCheck.Reason), strings.TrimSpace(selected.Reason))
	stateLine := "State:      " + state
	if reason != "" {
		stateLine += " (" + reason + ")"
	}
	lines = append(lines, stateLine)

	scope := "Scope:      full invocation diff"
	if turnContext := m.reviewDiff.TurnContext; turnContext != nil {
		switch turnContext.Selector.Kind {
		case "single":
			scope = fmt.Sprintf(
				"Scope:      turn %s  checkpoints %d -> %d  commits %s..%s",
				firstNonEmpty(strings.TrimSpace(turnContext.Selector.TurnID), strings.TrimSpace(m.reviewTurnID), "<turn>"),
				turnContext.StartCheckpointID,
				turnContext.EndCheckpointID,
				shortID(turnContext.FromCommit, 8),
				shortID(turnContext.ToCommit, 8),
			)
		case "range":
			scope = fmt.Sprintf(
				"Scope:      %s..%s  checkpoints %d -> %d  commits %s..%s",
				firstNonEmpty(strings.TrimSpace(turnContext.Selector.StartTurnID), "<start>"),
				firstNonEmpty(strings.TrimSpace(turnContext.Selector.EndTurnID), "<end>"),
				turnContext.StartCheckpointID,
				turnContext.EndCheckpointID,
				shortID(turnContext.FromCommit, 8),
				shortID(turnContext.ToCommit, 8),
			)
		default:
			scope = fmt.Sprintf(
				"Scope:      selector %s  checkpoints %d -> %d  commits %s..%s",
				firstNonEmpty(strings.TrimSpace(turnContext.Selector.Kind), "<unknown>"),
				turnContext.StartCheckpointID,
				turnContext.EndCheckpointID,
				shortID(turnContext.FromCommit, 8),
				shortID(turnContext.ToCommit, 8),
			)
		}
	}
	lines = append(lines, scope)

	reviewedCount := 0
	for _, file := range m.reviewFiles {
		if m.reviewReviewed[file.key] {
			reviewedCount++
		}
	}

	changeParts := make([]string, 0, 5)
	if diffRange := m.reviewDiff.CommittedRange; diffRange != nil {
		if diffstat := strings.TrimSpace(diffRange.Diffstat); diffstat != "" {
			changeParts = append(changeParts, "committed "+diffstat)
		} else if len(diffRange.Commits) > 0 {
			changeParts = append(changeParts, fmt.Sprintf("committed %d commit(s)", len(diffRange.Commits)))
		}
	}
	if diffRange := m.reviewDiff.WorkingTree; diffRange != nil {
		if diffstat := strings.TrimSpace(diffRange.Diffstat); diffstat != "" {
			changeParts = append(changeParts, "working "+diffstat)
		} else {
			changeParts = append(changeParts, "working tree changes")
		}
	}
	changeParts = append(changeParts, fmt.Sprintf("files %d", len(m.reviewFiles)))
	changeParts = append(changeParts, fmt.Sprintf("reviewed %d", reviewedCount))
	lines = append(lines, "Changes:    "+strings.Join(changeParts, "  "))

	workflowParts := make([]string, 0, 4)
	if landing := strings.TrimSpace(m.reviewCheck.LandingStatus); landing != "" {
		workflowParts = append(workflowParts, "landing "+landing)
	}
	if m.reviewCheck.PRSyncEligible {
		workflowParts = append(workflowParts, "pr sync eligible")
	} else {
		workflowParts = append(workflowParts, "pr sync not yet")
	}
	for _, worktree := range m.snapshot.Worktrees {
		if worktree.WorktreeID != selected.WorktreeID || worktree.Merge == nil {
			continue
		}
		mergeSummary := firstNonEmpty(strings.TrimSpace(worktree.Merge.StatusSummary), strings.TrimSpace(worktree.Merge.State), "merge state unavailable")
		if worktree.Merge.PRNumber > 0 {
			mergeSummary += fmt.Sprintf(" (#%d)", worktree.Merge.PRNumber)
		}
		workflowParts = append(workflowParts, "pr merge "+mergeSummary)
		if strings.TrimSpace(worktree.Merge.PRURL) != "" {
			lines = append(lines, "PR:         "+worktree.Merge.PRURL)
		}
		if strings.TrimSpace(worktree.Merge.ErrorMessage) != "" {
			lines = append(lines, "PR error:   "+worktree.Merge.ErrorMessage)
		}
		if strings.TrimSpace(worktree.Merge.Hint) != "" {
			lines = append(lines, "PR hint:    "+worktree.Merge.Hint)
		}
		break
	}
	if len(workflowParts) > 0 {
		lines = append(lines, "Workflow:   "+strings.Join(workflowParts, "  "))
	}

	if summary := strings.TrimSpace(m.reviewCheck.RunnerSummary); summary != "" {
		lines = append(lines, "Summary:    "+summary)
	}
	if howToTest := strings.TrimSpace(m.reviewCheck.HowToTest); howToTest != "" {
		lines = append(lines, "Test:       "+howToTest)
	}
	if diffRange := m.reviewDiff.CommittedRange; diffRange != nil && diffRange.PatchTruncated {
		lines = append(lines, fmt.Sprintf("Patch:      committed patch truncated at %d bytes", diffRange.PatchBytes))
	}
	if diffRange := m.reviewDiff.WorkingTree; diffRange != nil && diffRange.PatchTruncated {
		lines = append(lines, fmt.Sprintf("Patch:      working tree patch truncated at %d bytes", diffRange.PatchBytes))
	}

	if len(m.reviewCheck.BlockingReasons) > 0 {
		lines = append(lines, "Before workflow progression:")
		for idx, reason := range m.reviewCheck.BlockingReasons {
			if idx == 3 {
				lines = append(lines, fmt.Sprintf("  ... %d more", len(m.reviewCheck.BlockingReasons)-idx))
				break
			}
			lines = append(lines, "  - "+reason.Message)
			if hint := strings.TrimSpace(reason.Hint); hint != "" {
				lines = append(lines, "    "+hint)
			}
		}
	}

	return splitTruncatedLines(lines, width)
}

func (m model) renderReviewPanels(width int) string {
	fileHeight, patchHeight := m.reviewPanelHeights()
	panelWidth := max(1, width-2)

	if width < 100 {
		filesPanel := panelStyle.Width(panelWidth).Height(fileHeight).Render(m.renderReviewFilesPanel(max(1, panelWidth-2), max(5, fileHeight-2)))
		patchPanel := panelStyle.Width(panelWidth).Height(patchHeight).Render(m.renderReviewPatchPanel(max(1, panelWidth-2), max(6, patchHeight-2)))
		return lipgloss.JoinVertical(lipgloss.Left, filesPanel, patchPanel)
	}

	leftWidth := width / 3
	if leftWidth < minPanelWidth {
		leftWidth = minPanelWidth
	}
	rightWidth := width - leftWidth - 1
	if rightWidth < minPanelWidth {
		rightWidth = minPanelWidth
		leftWidth = max(minPanelWidth, width-rightWidth-1)
	}

	filesPanel := panelStyle.Width(leftWidth).Height(fileHeight).Render(m.renderReviewFilesPanel(max(1, leftWidth-2), max(5, fileHeight-2)))
	patchPanel := panelStyle.Width(rightWidth).Height(patchHeight).Render(m.renderReviewPatchPanel(max(1, rightWidth-2), max(6, patchHeight-2)))
	return lipgloss.JoinHorizontal(lipgloss.Top, filesPanel, patchPanel)
}

func (m model) renderReviewFilesPanel(width, height int) string {
	title := "files"
	if m.reviewFilesFocus {
		title = reviewFocusedTitleStyle.Render("files *")
	}

	lines := []string{title, ""}
	if len(m.reviewFiles) == 0 {
		lines = append(lines, "(no file-level diff available)")
		return strings.Join(lines, "\n")
	}

	maxRows := max(3, height-2)
	start, end := windowForSelection(len(m.reviewFiles), m.reviewSelectedIndex, maxRows)
	for idx := start; idx < end; idx++ {
		file := m.reviewFiles[idx]
		prefix := " "
		if idx == m.reviewSelectedIndex {
			prefix = ">"
		}

		reviewed := " "
		if m.reviewReviewed[file.key] {
			reviewed = "x"
		}

		row := fmt.Sprintf("%s [%s] %-9s %s", prefix, reviewed, reviewSectionLabel(file.section), file.title)
		switch {
		case file.added > 0 || file.deleted > 0:
			row += fmt.Sprintf("  +%d -%d", file.added, file.deleted)
		case strings.TrimSpace(file.diffstat) != "":
			row += "  " + file.diffstat
		}
		row = truncateWithEllipsis(row, width)
		if idx == m.reviewSelectedIndex {
			row = selectedRowStyle.Render(row)
		}
		lines = append(lines, row)
	}

	if start > 0 || end < len(m.reviewFiles) {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(m.reviewFiles))))
	}

	return strings.Join(lines, "\n")
}

func (m model) renderReviewPatchPanel(width, height int) string {
	title := "patch"
	if !m.reviewFilesFocus {
		title = reviewFocusedTitleStyle.Render("patch *")
	}

	lines := []string{title, ""}
	selected, ok := m.selectedReviewFile()
	if !ok {
		lines = append(lines, "(no diff loaded)")
		return strings.Join(lines, "\n")
	}

	label := fmt.Sprintf("[%s] %s", reviewSectionLabel(selected.section), selected.title)
	if m.reviewReviewed[selected.key] {
		label += "  [reviewed]"
	}
	switch {
	case selected.added > 0 || selected.deleted > 0:
		label += fmt.Sprintf("  +%d -%d", selected.added, selected.deleted)
	case strings.TrimSpace(selected.diffstat) != "":
		label += "  " + selected.diffstat
	}
	lines = append(lines, truncateWithEllipsis(label, width))
	lines = append(lines, "")

	visible := max(4, height-len(lines))
	start := 0
	end := len(selected.lines)
	if len(selected.lines) > visible {
		start = clamp(m.reviewScroll, 0, max(0, len(selected.lines)-visible))
		end = clamp(start+visible, 0, len(selected.lines))
	}

	for _, line := range selected.lines[start:end] {
		lines = append(lines, renderReviewPatchLine(line, width))
	}
	if len(selected.lines) > visible {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(selected.lines))))
	}

	return strings.Join(lines, "\n")
}

func renderReviewPatchLine(line string, width int) string {
	line = truncateWithEllipsis(line, width)
	switch {
	case strings.HasPrefix(line, "diff --git "), strings.HasPrefix(line, "index "), strings.HasPrefix(line, "@@ "):
		return reviewMetaStyle.Render(line)
	case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "rename "), strings.HasPrefix(line, "new file "), strings.HasPrefix(line, "deleted file "):
		return dimStyle.Render(line)
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return reviewAddedStyle.Render(line)
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return reviewDeletedStyle.Render(line)
	default:
		return line
	}
}

func reviewFilesFromDiff(diff daemon.InvocationDiffData) []reviewFile {
	files := make([]reviewFile, 0, 16)
	files = append(files, reviewFilesFromRange("committed", diff.CommittedRange)...)
	files = append(files, reviewFilesFromRange("working tree", diff.WorkingTree)...)
	if len(files) == 0 {
		return []reviewFile{
			{
				key:     "summary:001:no-changes",
				title:   "no changes",
				section: "summary",
				lines:   []string{"(no committed or working tree changes)"},
			},
		}
	}
	return files
}

func reviewFilesFromRange(section string, diffRange *daemon.DiffRange) []reviewFile {
	if diffRange == nil {
		return nil
	}

	if strings.TrimSpace(diffRange.Patch) == "" {
		lines := []string{section + " changes"}
		if strings.TrimSpace(diffRange.From) != "" || strings.TrimSpace(diffRange.To) != "" {
			lines = append(lines, "", fmt.Sprintf("range: %s..%s", firstNonEmpty(strings.TrimSpace(diffRange.From), "<from>"), firstNonEmpty(strings.TrimSpace(diffRange.To), "<to>")))
		}
		if diffstat := strings.TrimSpace(diffRange.Diffstat); diffstat != "" {
			lines = append(lines, "", "diffstat: "+diffstat)
		}
		if len(diffRange.Commits) > 0 {
			lines = append(lines, "", "commits:")
			for _, commit := range diffRange.Commits {
				lines = append(lines, "  "+shortID(commit.SHA, 8)+" "+strings.TrimSpace(commit.Summary))
			}
		}
		if len(lines) == 1 {
			lines = append(lines, "", "(no patch available)")
		}
		return []reviewFile{
			{
				key:      fmt.Sprintf("%s:%03d:%s", section, 1, "summary"),
				title:    section + " changes",
				section:  section,
				diffstat: strings.TrimSpace(diffRange.Diffstat),
				lines:    lines,
			},
		}
	}

	lines := strings.Split(strings.ReplaceAll(diffRange.Patch, "\r\n", "\n"), "\n")
	files := make([]reviewFile, 0, 8)
	var current *reviewFile

	flush := func() {
		if current == nil {
			return
		}
		if strings.TrimSpace(current.title) == "" {
			current.title = section + " changes"
		}
		current.key = fmt.Sprintf("%s:%03d:%s", section, len(files)+1, current.title)
		if strings.TrimSpace(current.diffstat) == "" && (current.added > 0 || current.deleted > 0) {
			current.diffstat = fmt.Sprintf("+%d -%d", current.added, current.deleted)
		}
		files = append(files, *current)
		current = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = &reviewFile{section: section}
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				current.title = reviewNormalizeDiffPath(fields[3])
			}
		}
		if current == nil {
			current = &reviewFile{section: section, title: section + " changes"}
		}

		current.lines = append(current.lines, line)

		switch {
		case strings.HasPrefix(line, "rename to "):
			if path := reviewNormalizeDiffPath(strings.TrimPrefix(line, "rename to ")); path != "" {
				current.title = path
			}
		case strings.HasPrefix(line, "+++ "):
			if path := reviewNormalizeDiffPath(strings.TrimPrefix(line, "+++ ")); path != "" && path != "/dev/null" {
				current.title = path
			}
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			current.added++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			current.deleted++
		}
	}

	flush()
	if len(files) > 0 {
		return files
	}

	return []reviewFile{
		{
			key:      fmt.Sprintf("%s:%03d:%s", section, 1, "patch"),
			title:    section + " patch",
			section:  section,
			diffstat: strings.TrimSpace(diffRange.Diffstat),
			lines:    lines,
		},
	}
}

func reviewNormalizeDiffPath(path string) string {
	path = strings.TrimSpace(strings.Trim(path, `"`))
	switch {
	case strings.HasPrefix(path, "a/"):
		return strings.TrimPrefix(path, "a/")
	case strings.HasPrefix(path, "b/"):
		return strings.TrimPrefix(path, "b/")
	default:
		return path
	}
}

func reviewSectionLabel(section string) string {
	switch strings.TrimSpace(section) {
	case "committed":
		return "committed"
	case "working tree":
		return "working"
	default:
		return "summary"
	}
}

func splitTruncatedLines(lines []string, width int) []string {
	if width <= 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, truncateWithEllipsis(line, width))
	}
	return out
}

func (m model) reviewPanelHeights() (int, int) {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 36
	}

	lineCount := len(m.renderPageHeader("review"))
	lineCount += len(m.renderReviewSummary(width))
	lineCount += 2
	if m.lastActionMessage != "" {
		lineCount += 2
	}
	if m.reviewError != "" {
		lineCount += 2
	}
	if m.reviewLoading {
		lineCount += 2
	}

	contentHeight := height - lineCount
	if contentHeight < 10 {
		contentHeight = 10
	}
	if width < 100 {
		fileHeight := max(5, contentHeight/3)
		patchHeight := max(6, contentHeight-fileHeight)
		return fileHeight, patchHeight
	}
	return contentHeight, contentHeight
}

func (m model) reviewPatchVisibleLines() int {
	_, patchHeight := m.reviewPanelHeights()
	visible := patchHeight - 5
	if visible < 4 {
		visible = 4
	}
	return visible
}

func (m model) maxReviewScroll() int {
	selected, ok := m.selectedReviewFile()
	if !ok {
		return 0
	}
	visible := m.reviewPatchVisibleLines()
	if len(selected.lines) <= visible {
		return 0
	}
	return len(selected.lines) - visible
}

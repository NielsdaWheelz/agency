package render

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Constants for human output formatting.
const (
	// TitleMaxLen is the maximum display length for title in human output.
	TitleMaxLen = 50

	// TitleBroken is displayed for broken runs.
	TitleBroken = "<broken>"

	// TitleUntitled is displayed for runs with empty titles.
	TitleUntitled = "<untitled>"
)

// RunSummaryHumanRow holds the fields for a single human-output row.
// This is separate from RunSummary to allow formatting before display.
type RunSummaryHumanRow struct {
	RunID     string
	Title     string
	Runner    string
	CreatedAt string
	Status    string
	PR        string
}

// WriteLSHuman writes the ls output in human-readable format.
// Fields are separated by whitespace columns for easy scanning.
func WriteLSHuman(w io.Writer, rows []RunSummaryHumanRow) error {
	if len(rows) == 0 {
		return nil
	}

	// Calculate column widths
	widths := columnWidths(rows)

	// Write header
	header := formatRow(
		"RUN_ID", widths.runID,
		"TITLE", widths.title,
		"RUNNER", widths.runner,
		"CREATED", widths.createdAt,
		"STATUS", widths.status,
		"PR", widths.pr,
	)
	if _, err := fmt.Fprintln(w, header); err != nil {
		return err
	}

	// Write rows
	for _, row := range rows {
		line := formatRow(
			row.RunID, widths.runID,
			row.Title, widths.title,
			row.Runner, widths.runner,
			row.CreatedAt, widths.createdAt,
			row.Status, widths.status,
			row.PR, widths.pr,
		)
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}

	return nil
}

// colWidths holds the calculated column widths.
type colWidths struct {
	runID     int
	title     int
	runner    int
	createdAt int
	status    int
	pr        int
}

// columnWidths calculates the maximum width for each column.
func columnWidths(rows []RunSummaryHumanRow) colWidths {
	widths := colWidths{
		runID:     len("RUN_ID"),
		title:     len("TITLE"),
		runner:    len("RUNNER"),
		createdAt: len("CREATED"),
		status:    len("STATUS"),
		pr:        len("PR"),
	}

	for _, row := range rows {
		if len(row.RunID) > widths.runID {
			widths.runID = len(row.RunID)
		}
		if len(row.Title) > widths.title {
			widths.title = len(row.Title)
		}
		if len(row.Runner) > widths.runner {
			widths.runner = len(row.Runner)
		}
		if len(row.CreatedAt) > widths.createdAt {
			widths.createdAt = len(row.CreatedAt)
		}
		if len(row.Status) > widths.status {
			widths.status = len(row.Status)
		}
		if len(row.PR) > widths.pr {
			widths.pr = len(row.PR)
		}
	}

	return widths
}

// formatRow formats a row with the given column values and widths.
func formatRow(runID string, runIDW int, title string, titleW int, runner string, runnerW int, created string, createdW int, status string, statusW int, pr string, prW int) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s  %s",
		runIDW, runID,
		titleW, title,
		runnerW, runner,
		createdW, created,
		statusW, status,
		pr,
	)
}

// FormatHumanRow converts a RunSummary to a RunSummaryHumanRow for display.
func FormatHumanRow(s RunSummary, now time.Time) RunSummaryHumanRow {
	row := RunSummaryHumanRow{
		RunID: s.RunID,
	}

	// Format title
	if s.Broken {
		row.Title = TitleBroken
	} else if s.Title == "" {
		row.Title = TitleUntitled
	} else {
		row.Title = truncateTitle(s.Title)
	}

	// Format runner (empty for broken)
	if s.Runner != nil {
		row.Runner = *s.Runner
	}

	// Format created_at
	if s.CreatedAt != nil {
		row.CreatedAt = formatRelativeTime(*s.CreatedAt, now)
	}

	// Format status with archived suffix
	row.Status = formatStatus(s.DerivedStatus, s.Archived)

	// Format PR
	if s.PRNumber != nil {
		row.PR = fmt.Sprintf("#%d", *s.PRNumber)
	}

	return row
}

// truncateTitle truncates the title to TitleMaxLen, adding ellipsis if needed.
func truncateTitle(title string) string {
	// Count runes for proper Unicode handling
	runes := []rune(title)
	if len(runes) <= TitleMaxLen {
		return title
	}
	return string(runes[:TitleMaxLen-1]) + "…"
}

// formatStatus adds "(archived)" suffix if archived.
func formatStatus(status string, archived bool) string {
	if archived {
		return status + " (archived)"
	}
	return status
}

// formatRelativeTime formats a time as a human-friendly relative string.
func formatRelativeTime(t time.Time, now time.Time) string {
	diff := now.Sub(t)
	if diff < 0 {
		diff = -diff
	}

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / (24 * 7))
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	default:
		// Fall back to date format for older entries
		return t.Format("2006-01-02")
	}
}

// FormatHumanRows converts a slice of RunSummary to RunSummaryHumanRow.
func FormatHumanRows(summaries []RunSummary, now time.Time) []RunSummaryHumanRow {
	rows := make([]RunSummaryHumanRow, len(summaries))
	for i, s := range summaries {
		rows[i] = FormatHumanRow(s, now)
	}
	return rows
}

// TruncateForDisplay is a helper to safely truncate any string for display.
func TruncateForDisplay(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// JoinStrings joins non-empty strings with the given separator.
func JoinStrings(sep string, strs ...string) string {
	var parts []string
	for _, s := range strs {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, sep)
}

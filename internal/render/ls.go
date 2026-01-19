package render

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Constants for human output formatting.
const (
	// NameMaxLen is the maximum display length for name in human output.
	NameMaxLen = 50

	// SummaryMaxLen is the maximum display length for summary in human output.
	SummaryMaxLen = 40

	// NameBroken is displayed for broken runs.
	NameBroken = "<broken>"

	// NameUntitled is displayed for runs with empty names.
	NameUntitled = "<untitled>"
)

// LSScope indicates the scope of the ls command.
type LSScope string

const (
	// LSScopeRepo indicates ls is scoped to the current repo.
	LSScopeRepo LSScope = "repo"

	// LSScopeAllRepos indicates ls is showing all repos.
	LSScopeAllRepos LSScope = "all-repos"
)

// LSContext provides context for formatting empty ls output.
type LSContext struct {
	// Scope indicates whether listing is repo-scoped or global.
	Scope LSScope

	// IncludesArchived indicates whether --all flag was used.
	IncludesArchived bool
}

// RunSummaryHumanRow holds the fields for a single human-output row.
// This is separate from RunSummary to allow formatting before display.
type RunSummaryHumanRow struct {
	RunID   string
	Name    string
	Status  string
	Summary string
	PR      string
}

// WriteLSHuman writes the ls output in human-readable format.
// Fields are separated by whitespace columns for easy scanning.
func WriteLSHuman(w io.Writer, rows []RunSummaryHumanRow, ctx LSContext) error {
	if len(rows) == 0 {
		msg := emptyLSMessage(ctx)
		_, err := fmt.Fprintln(w, msg)
		return err
	}

	// Calculate column widths
	widths := columnWidths(rows)

	// Write header
	header := formatRow(
		"RUN_ID", widths.runID,
		"NAME", widths.name,
		"STATUS", widths.status,
		"SUMMARY", widths.summary,
		"PR", widths.pr,
	)
	if _, err := fmt.Fprintln(w, header); err != nil {
		return err
	}

	// Write rows
	for _, row := range rows {
		line := formatRow(
			row.RunID, widths.runID,
			row.Name, widths.name,
			row.Status, widths.status,
			row.Summary, widths.summary,
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
	runID   int
	name    int
	status  int
	summary int
	pr      int
}

// columnWidths calculates the maximum width for each column.
func columnWidths(rows []RunSummaryHumanRow) colWidths {
	widths := colWidths{
		runID:   len("RUN_ID"),
		name:    len("NAME"),
		status:  len("STATUS"),
		summary: len("SUMMARY"),
		pr:      len("PR"),
	}

	for _, row := range rows {
		if len(row.RunID) > widths.runID {
			widths.runID = len(row.RunID)
		}
		if len(row.Name) > widths.name {
			widths.name = len(row.Name)
		}
		if len(row.Status) > widths.status {
			widths.status = len(row.Status)
		}
		if len(row.Summary) > widths.summary {
			widths.summary = len(row.Summary)
		}
		if len(row.PR) > widths.pr {
			widths.pr = len(row.PR)
		}
	}

	return widths
}

// formatRow formats a row with the given column values and widths.
func formatRow(runID string, runIDW int, name string, nameW int, status string, statusW int, summary string, summaryW int, pr string, prW int) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		runIDW, runID,
		nameW, name,
		statusW, status,
		summaryW, summary,
		pr,
	)
}

// FormatHumanRow converts a RunSummary to a RunSummaryHumanRow for display.
func FormatHumanRow(s RunSummary, now time.Time) RunSummaryHumanRow {
	row := RunSummaryHumanRow{
		RunID: s.RunID,
	}

	// Format name
	if s.Broken {
		row.Name = NameBroken
	} else if s.Name == "" {
		row.Name = NameUntitled
	} else {
		row.Name = truncateName(s.Name)
	}

	// Format status with archived suffix
	row.Status = formatStatus(s.DerivedStatus, s.Archived)

	// Format summary
	row.Summary = formatSummary(s.Summary, s.StalledDuration, s.DerivedStatus)

	// Format PR
	if s.PRNumber != nil {
		row.PR = fmt.Sprintf("#%d", *s.PRNumber)
	}

	return row
}

// formatSummary formats the summary field for display.
// For stalled runs, shows "(no activity for Xm)" instead of summary.
func formatSummary(summary *string, stalledDuration *string, status string) string {
	// For stalled runs, show stall duration
	if status == "stalled" && stalledDuration != nil {
		return fmt.Sprintf("(no activity for %s)", *stalledDuration)
	}

	// If we have a summary, truncate it
	if summary != nil && *summary != "" {
		return truncateSummary(*summary)
	}

	// No summary available
	return "-"
}

// truncateSummary truncates the summary to SummaryMaxLen, adding ellipsis if needed.
func truncateSummary(summary string) string {
	// Count runes for proper Unicode handling
	runes := []rune(summary)
	if len(runes) <= SummaryMaxLen {
		return summary
	}
	return string(runes[:SummaryMaxLen-1]) + "…"
}

// truncateName truncates the name to NameMaxLen, adding ellipsis if needed.
func truncateName(name string) string {
	// Count runes for proper Unicode handling
	runes := []rune(name)
	if len(runes) <= NameMaxLen {
		return name
	}
	return string(runes[:NameMaxLen-1]) + "…"
}

// formatStatus adds "(archived)" suffix if archived.
func formatStatus(status string, archived bool) string {
	if archived {
		return status + " (archived)"
	}
	return status
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

// emptyLSMessage returns the appropriate message for an empty ls result.
func emptyLSMessage(ctx LSContext) string {
	switch {
	case ctx.Scope == LSScopeRepo && !ctx.IncludesArchived:
		return "no active runs (use --all to include archived)"
	case ctx.Scope == LSScopeAllRepos:
		return "no runs found"
	default:
		return "no runs found"
	}
}

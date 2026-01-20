// Package render provides output formatting utilities for agency commands.
package render

import (
	"fmt"
	"io"
	"strings"
)

// ConflictCardInputs holds all inputs needed to render a conflict action card.
type ConflictCardInputs struct {
	// Ref is the reference the user invoked (e.g., "feature-x" or run_id).
	// Used in printed commands so user can copy-paste.
	Ref string

	// PRURL is the full PR URL. If empty, pr: line is omitted.
	PRURL string

	// PRNumber is the PR number. If 0, omitted from error message.
	PRNumber int

	// Base is the parent branch (e.g., "main"). Required.
	Base string

	// Branch is the run branch (e.g., "agency/feature-x-a3f2"). Required.
	Branch string

	// WorktreePath is the full path to the worktree. Required for full card.
	WorktreePath string
}

// WriteConflictCard writes the full conflict resolution action card to w.
// Per spec: plain text, no color, no markdown. Used by both merge and resolve.
//
// Output format:
//
//	pr: https://github.com/owner/repo/pull/93
//	base: main
//	branch: agency/feature-x-a3f2
//	worktree: /path/to/worktree
//
//	next:
//
//	1. agency open feature-x
//	2. git fetch origin
//	3. git rebase origin/main
//	4. resolve conflicts, then:
//	   git add -A && git rebase --continue
//	5. agency push feature-x --force-with-lease
//	6. agency merge feature-x
//
//	alt: cd "/path/to/worktree"
func WriteConflictCard(w io.Writer, inputs ConflictCardInputs) {
	// Metadata section
	if inputs.PRURL != "" {
		_, _ = fmt.Fprintf(w, "pr: %s\n", inputs.PRURL)
	}
	_, _ = fmt.Fprintf(w, "base: %s\n", inputs.Base)
	_, _ = fmt.Fprintf(w, "branch: %s\n", inputs.Branch)
	_, _ = fmt.Fprintf(w, "worktree: %s\n", inputs.WorktreePath)

	// Next steps section
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "next:")
	_, _ = fmt.Fprintln(w)

	ref := inputs.Ref
	base := inputs.Base

	_, _ = fmt.Fprintf(w, "1. agency open %s\n", ref)
	_, _ = fmt.Fprintln(w, "2. git fetch origin")
	_, _ = fmt.Fprintf(w, "3. git rebase origin/%s\n", base)
	_, _ = fmt.Fprintln(w, "4. resolve conflicts, then:")
	_, _ = fmt.Fprintln(w, "   git add -A && git rebase --continue")
	_, _ = fmt.Fprintf(w, "5. agency push %s --force-with-lease\n", ref)
	_, _ = fmt.Fprintf(w, "6. agency merge %s\n", ref)
	_, _ = fmt.Fprintln(w)

	// Alt section - cd fallback
	_, _ = fmt.Fprintf(w, "alt: cd \"%s\"\n", inputs.WorktreePath)
}

// WritePartialConflictCard writes a partial card when worktree is missing.
// Used by resolve when worktree is archived.
//
// Output format (to stderr typically):
//
//	pr: https://github.com/owner/repo/pull/93
//	base: main
//	branch: agency/feature-x-a3f2
//
//	hint: worktree no longer exists; resolve conflicts via GitHub web UI or restore locally
func WritePartialConflictCard(w io.Writer, inputs ConflictCardInputs) {
	if inputs.PRURL != "" {
		_, _ = fmt.Fprintf(w, "pr: %s\n", inputs.PRURL)
	}
	_, _ = fmt.Fprintf(w, "base: %s\n", inputs.Base)
	_, _ = fmt.Fprintf(w, "branch: %s\n", inputs.Branch)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "hint: worktree no longer exists; resolve conflicts via GitHub web UI or restore locally")
}

// FormatConflictErrorMessage formats the one-line error message for merge conflicts.
// Example: "PR #93 has conflicts with main and cannot be merged."
func FormatConflictErrorMessage(prNumber int, base string) string {
	if prNumber > 0 {
		return fmt.Sprintf("PR #%d has conflicts with %s and cannot be merged.", prNumber, base)
	}
	return fmt.Sprintf("PR has conflicts with %s and cannot be merged.", base)
}

// WriteConflictError writes the full conflict error output to w (stderr).
// This is the complete error output including error_code, message, and action card.
//
// Output format:
//
//	error_code: E_PR_NOT_MERGEABLE
//	PR #93 has conflicts with main and cannot be merged.
//
//	pr: https://github.com/owner/repo/pull/93
//	base: main
//	branch: agency/feature-x-a3f2
//	worktree: /path/to/worktree
//
//	next:
//	...
func WriteConflictError(w io.Writer, inputs ConflictCardInputs) {
	_, _ = fmt.Fprintln(w, "error_code: E_PR_NOT_MERGEABLE")
	_, _ = fmt.Fprintln(w, FormatConflictErrorMessage(inputs.PRNumber, inputs.Base))
	_, _ = fmt.Fprintln(w)
	WriteConflictCard(w, inputs)
}

// FormatNonFastForwardHint returns the hint message for non-fast-forward push failures.
func FormatNonFastForwardHint(ref string) string {
	var sb strings.Builder
	sb.WriteString("hint: branch was rebased or amended; retry with:\n")
	_, _ = fmt.Fprintf(&sb, "  agency push %s --force-with-lease\n", ref)
	return sb.String()
}

// IsNonFastForwardError checks if git push stderr indicates a non-fast-forward rejection.
// Per spec: detect via pattern matching for "non-fast-forward", "fetch first",
// or "[rejected]" combined with "non-fast-forward".
func IsNonFastForwardError(stderr string) bool {
	lower := strings.ToLower(stderr)

	// Check for explicit non-fast-forward
	if strings.Contains(lower, "non-fast-forward") {
		return true
	}

	// Check for "fetch first" pattern
	if strings.Contains(lower, "fetch first") {
		return true
	}

	// Check for [rejected] combined with non-fast-forward (already covered above)
	// but also check for rejected + updates were rejected pattern
	if strings.Contains(lower, "[rejected]") && strings.Contains(lower, "updates were rejected") {
		return true
	}

	return false
}

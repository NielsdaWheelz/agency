// Package commands implements agency CLI commands.
package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func hintFromError(err error) string {
	ae, ok := errors.AsAgencyError(err)
	if !ok || ae.Details == nil {
		return ""
	}
	return ae.Details["hint"]
}

func prViewHint(repoRef ghRepoRef, branch string, prNumber int) string {
	if branch == "" {
		return ""
	}

	if prNumber != 0 && repoRef.NameWithOwner != "" {
		return fmt.Sprintf("gh pr view %d -R %s", prNumber, repoRef.NameWithOwner)
	}

	head := headRef(repoRef, branch)
	if repoRef.NameWithOwner != "" {
		return fmt.Sprintf("gh pr view --head %s -R %s", head, repoRef.NameWithOwner)
	}
	return fmt.Sprintf("gh pr view --head %s", head)
}

func printHint(w io.Writer, hint string) {
	if w == nil || hint == "" {
		return
	}
	if strings.HasPrefix(hint, "hint:") {
		_, _ = fmt.Fprintln(w, hint)
		return
	}
	_, _ = fmt.Fprintf(w, "hint: %s\n", hint)
}

func shouldPrintPRViewHint(code errors.Code) bool {
	switch code {
	case errors.EGHPRViewFailed, errors.EGHPRCreateFailed, errors.ENoPR, errors.EPRNotOpen:
		return true
	default:
		return false
	}
}

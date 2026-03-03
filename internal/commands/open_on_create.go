package commands

import (
	"fmt"
	"io"
)

// emitOpenOnCreateStatus prints deterministic open-on-create signaling.
// Open failures are warning-only because creation has already succeeded.
func emitOpenOnCreateStatus(stdout, stderr io.Writer, openErr error) {
	if openErr != nil {
		_, _ = fmt.Fprintf(stderr, "warning: workspace created but open dispatch failed: %v\n", openErr)
		_, _ = fmt.Fprintln(stdout, "open_status: failed")
		return
	}

	_, _ = fmt.Fprintln(stdout, "open_status: opened")
}

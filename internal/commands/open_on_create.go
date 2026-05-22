package commands

import (
	"fmt"
	"io"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// emitOpenOnCreateStatus prints deterministic open-on-create signaling.
// Open failures are warning-only because creation has already succeeded.
func emitOpenOnCreateStatus(stdout, stderr io.Writer, openErr error) {
	if openErr != nil {
		msg := openErr.Error()
		if ae, ok := errors.AsAgencyError(openErr); ok {
			msg = ae.Msg
		}
		_, _ = fmt.Fprintf(stderr, "warning: workspace created but open dispatch failed: %s\n", msg)
		_, _ = fmt.Fprintln(stdout, "open_status: failed")
		return
	}

	_, _ = fmt.Fprintln(stdout, "open_status: opened")
}

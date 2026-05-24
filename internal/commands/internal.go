package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

type HeadedHookOpts struct {
	RepoID          string
	InvocationID    string
	Runner          string
	Stdin           io.Reader
	DataDirOverride string
}

func HeadedHook(ctx context.Context, fsys fs.FS, opts HeadedHookOpts) error {
	warn := func(message string, err error) {
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: headed hook ingest skipped: %s: %v\n", message, err)
			return
		}
		_, _ = fmt.Fprintf(os.Stderr, "warning: headed hook ingest skipped: %s\n", message)
	}

	if strings.TrimSpace(opts.RepoID) == "" {
		return errors.New(errors.EInvalidArgument, "repo_id is required")
	}
	if strings.TrimSpace(opts.InvocationID) == "" {
		return errors.New(errors.EInvalidArgument, "invocation_id is required")
	}
	in := opts.Stdin
	if in == nil {
		in = strings.NewReader("{}")
	}
	payload, err := io.ReadAll(io.LimitReader(in, stream.MaxLineSize))
	if err != nil {
		warn("failed to read hook payload", err)
		return nil
	}
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		warn("failed to initialize daemon client", err)
		return nil
	}
	if _, err := ns.client.IngestHeadedHook(ctx, opts.RepoID, opts.InvocationID, opts.Runner, payload); err != nil {
		warn("daemon rejected hook payload", err)
	}
	return nil
}

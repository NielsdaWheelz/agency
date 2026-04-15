package commands

import (
	"context"
	"io"
	"strings"

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
	payload, err := io.ReadAll(in)
	if err != nil {
		return nil
	}
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return nil
	}
	_, _ = ns.client.IngestHeadedHook(ctx, opts.RepoID, opts.InvocationID, opts.Runner, payload)
	return nil
}

// Package commands implements CLI command logic for agency.
// This file implements checkpoint commands (Slice 8 PR-08).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/render"
)

// CheckpointLSOpts holds options for the checkpoint ls command.
type CheckpointLSOpts struct {
	InvocationRef string
	RepoFlag      string
	JSON          bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// CheckpointLS lists checkpoints for an invocation.
// PR-12: Routes through daemon read API - CLI never reads store directly.
func CheckpointLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts CheckpointLSOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	if err := ns.client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	// Resolve repo context through daemon-first contract.
	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "checkpoint ls",
	})
	if err != nil {
		return err
	}

	// Call daemon ListCheckpoints endpoint.
	result, err := ns.client.ListCheckpoints(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.ListCheckpointsOpts{})
	if err != nil {
		return err
	}

	// Output
	if opts.JSON {
		return json.NewEncoder(stdout).Encode(result.Checkpoints)
	}

	if len(result.Checkpoints) == 0 {
		_, _ = fmt.Fprintln(stdout, "No checkpoints found.")
		return nil
	}

	for _, cp := range result.Checkpoints {
		timestamp := cp.CreatedAt
		if parsed, err := time.Parse(time.RFC3339, cp.CreatedAt); err == nil {
			timestamp = parsed.Local().Format("2006-01-02 15:04:05")
		}

		triggerSummary := strings.TrimSpace(cp.Description)
		if triggerSummary == "" && strings.TrimSpace(cp.ToolName) != "" {
			triggerSummary = "after " + strings.TrimSpace(cp.ToolName)
		}
		if triggerSummary == "" {
			triggerSummary = "checkpoint snapshot"
		}

		_, _ = fmt.Fprintf(stdout, "%s  cp:%d  %s\n",
			timestamp,
			cp.ID,
			render.FormatActivityLabel("checkpoint", triggerSummary),
		)

		detailParts := make([]string, 0, 4)
		if commit := strings.TrimSpace(cp.SnapshotCommit); commit != "" {
			if len(commit) > 8 {
				commit = commit[:8]
			}
			detailParts = append(detailParts, "commit:"+commit)
		}
		if cp.StreamSeq > 0 {
			detailParts = append(detailParts, fmt.Sprintf("stream:%d", cp.StreamSeq))
		}
		if diffstat := strings.TrimSpace(cp.Diffstat); diffstat != "" {
			detailParts = append(detailParts, diffstat)
		}
		totalChanged := cp.ChangedPathCount
		if totalChanged <= 0 {
			totalChanged = len(cp.ChangedPaths)
		}
		if totalChanged > 0 {
			trimmed := cp.ChangedPathTruncated || totalChanged > len(cp.ChangedPaths)
			summary := render.FormatChangedPathSummary(cp.ChangedPaths, totalChanged, trimmed)
			if summary == "" {
				detailParts = append(detailParts, fmt.Sprintf("paths:%d", totalChanged))
			} else {
				detailParts = append(detailParts, "paths:"+summary)
			}
		}
		if len(detailParts) > 0 {
			_, _ = fmt.Fprintf(stdout, "    %s\n", strings.Join(detailParts, " | "))
		}
	}

	return nil
}

// CheckpointApplyOpts holds options for the checkpoint apply command.
type CheckpointApplyOpts struct {
	InvocationRef string
	CheckpointID  string // Parsed as int

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// CheckpointApply restores a sandbox to a checkpoint state.
func CheckpointApply(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts CheckpointApplyOpts, stdout, stderr io.Writer) error {
	checkpointID, err := strconv.Atoi(opts.CheckpointID)
	if err != nil {
		return errors.New(errors.EUsage, "checkpoint_id must be a positive integer")
	}
	if checkpointID <= 0 {
		return errors.New(errors.EUsage, "checkpoint_id must be a positive integer")
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		CmdName: "checkpoint apply",
	})
	if err != nil {
		return err
	}

	resp, err := ns.client.CheckpointApply(ctx, repoCtx.RepoID, opts.InvocationRef, checkpointID)
	if err != nil {
		return errors.Wrap(errors.EInternal, "checkpoint apply request failed", err)
	}

	if !resp.OK {
		return errors.NewWithDetails(errors.Code(resp.ErrorCode), resp.Message, map[string]string{"hint": resp.Hint})
	}

	snapshotCommit := resp.SnapshotCommit
	if len(snapshotCommit) > 8 {
		snapshotCommit = snapshotCommit[:8]
	}
	_, _ = fmt.Fprintf(stdout, "Restored to checkpoint %d (commit %s)\n", resp.CheckpointID, snapshotCommit)

	return nil
}

// Package commands implements CLI command logic for agency.
// This file implements checkpoint commands (Slice 8 PR-08).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// CheckpointLSOpts holds options for the checkpoint ls command.
type CheckpointLSOpts struct {
	InvocationRef string
	JSON          bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string

	// DaemonSocketOverride, if set, uses this socket path directly and
	// assumes the daemon is already running. Bypasses EnsureDaemonRunning.
	DaemonSocketOverride string
}

// CheckpointLS lists checkpoints for an invocation.
// PR-12: Routes through daemon read API - CLI never reads store directly.
func CheckpointLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts CheckpointLSOpts, stdout, stderr io.Writer) error {
	// 1. Resolve paths
	var dataDir string
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(errors.EInternal, "failed to get home directory", err)
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		dataDir = dirs.DataDir
	}

	// 2. Get repo context
	gitRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.Wrap(errors.ENoRepo, "not inside a git repository", err)
	}

	originInfo := git.GetOriginInfo(ctx, cr, gitRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(gitRoot.Path, originInfo.URL)

	// 3. Ensure daemon is running
	var client *daemonclient.Client
	if opts.DaemonSocketOverride != "" {
		client = daemonclient.NewClient(opts.DaemonSocketOverride)
	} else {
		st := store.NewStore(fsys, dataDir, time.Now)
		socketPath := st.DaemonSocketPath()
		logPath := st.DaemonLogPath()

		client, err = daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
		if err != nil {
			return err
		}
	}

	// PR-12: Call daemon ListCheckpoints endpoint
	result, err := client.ListCheckpoints(ctx, opts.InvocationRef, repoIdentity.RepoID, daemonclient.ListCheckpointsOpts{})
	if err != nil {
		return err
	}

	// Output
	if opts.JSON {
		return json.NewEncoder(stdout).Encode(result.Checkpoints)
	}

	// Human-readable output
	if len(result.Checkpoints) == 0 {
		_, _ = fmt.Fprintln(stdout, "No checkpoints found.")
		return nil
	}

	_, _ = fmt.Fprintf(stdout, "Checkpoints:\n\n")
	_, _ = fmt.Fprintf(stdout, "%-4s  %-20s  %-10s  %-10s  %s\n", "ID", "Created", "Untracked", "Commit", "Diffstat")
	_, _ = fmt.Fprintf(stdout, "%-4s  %-20s  %-10s  %-10s  %s\n", "----", "--------------------", "----------", "----------", "--------")

	for _, cp := range result.Checkpoints {
		// Parse and format timestamp
		createdAt := cp.CreatedAt
		if t, err := time.Parse(time.RFC3339, cp.CreatedAt); err == nil {
			createdAt = t.Local().Format("2006-01-02 15:04:05")
		}

		untracked := "yes"
		if !cp.IncludesUntracked {
			untracked = "no"
		}

		// Truncate snapshot commit
		snapshotCommit := cp.SnapshotCommit
		if len(snapshotCommit) > 8 {
			snapshotCommit = snapshotCommit[:8]
		}

		_, _ = fmt.Fprintf(stdout, "%-4d  %-20s  %-10s  %-10s  %s\n",
			cp.ID, createdAt, untracked, snapshotCommit, cp.Diffstat)
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
	// 1. Parse checkpoint ID
	checkpointID, err := strconv.Atoi(opts.CheckpointID)
	if err != nil {
		return errors.New(errors.EUsage, "checkpoint_id must be a positive integer")
	}
	if checkpointID <= 0 {
		return errors.New(errors.EUsage, "checkpoint_id must be a positive integer")
	}

	// 2. Resolve paths
	var applyDataDir string
	if opts.DataDirOverride != "" {
		applyDataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(errors.EInternal, "failed to get home directory", err)
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		applyDataDir = dirs.DataDir
	}

	// 3. Get repo context
	gitRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.Wrap(errors.ENoRepo, "not inside a git repository", err)
	}

	originInfo := git.GetOriginInfo(ctx, cr, gitRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(gitRoot.Path, originInfo.URL)

	// 4. Set up store
	st := store.NewStore(fsys, applyDataDir, time.Now)

	// 5. Scan invocations and resolve
	records, err := store.ScanInvocationsForRepo(applyDataDir, repoIdentity.RepoID)
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to scan invocations", err)
	}

	// Convert to refs
	refs := make([]ids.InvocationRef, 0, len(records))
	for _, r := range records {
		ref := ids.InvocationRef{
			InvocationID: r.InvocationID,
			RepoID:       repoIdentity.RepoID,
			Broken:       r.Broken,
		}
		if r.Meta != nil {
			ref.IntegrationWorktreeID = r.Meta.IntegrationWorktreeID
			ref.InvocationName = r.Meta.InvocationName
			ref.Status = string(r.Meta.Status)
			ref.LandingStatus = string(r.Meta.LandingStatus)
		}
		refs = append(refs, ref)
	}

	resolved, err := ids.ResolveInvocationRef(opts.InvocationRef, refs, ids.ResolveInvocationRefOpts{
		IncludeFinished: true, // Allow applying checkpoints to finished invocations
	})
	if err != nil {
		if _, ok := err.(*ids.ErrInvocationNotFound); ok {
			return errors.NewWithDetails(errors.EInvocationNotFound, err.Error(), nil)
		}
		if _, ok := err.(*ids.ErrInvocationAmbiguous); ok {
			return errors.NewWithDetails(errors.EInvocationIDAmbiguous, err.Error(), nil)
		}
		return err
	}

	// Find the record with full meta
	var record *store.InvocationRecord
	for i := range records {
		if records[i].InvocationID == resolved.InvocationID {
			record = &records[i]
			break
		}
	}
	if record == nil || record.Meta == nil {
		return errors.New(errors.EInvocationNotFound, "invocation not found")
	}

	// 6. Verify invocation is headless
	if record.Meta.Mode != store.RunnerModeHeadless {
		return errors.New(errors.EInvocationInvalidMode, "checkpoint apply is only supported for headless invocations")
	}

	// 7. Connect to daemon
	socketPath := st.DaemonSocketPath()
	client := daemonclient.NewClient(socketPath)

	// 8. Call daemon checkpoint apply endpoint
	resp, err := client.CheckpointApply(ctx, repoIdentity.RepoID, record.InvocationID, checkpointID)
	if err != nil {
		return errors.Wrap(errors.EInternal, "checkpoint apply request failed", err)
	}

	if !resp.OK {
		hint := ""
		if resp.Hint != "" {
			hint = resp.Hint
		}
		return errors.NewWithDetails(errors.Code(resp.ErrorCode), resp.Message, map[string]string{"hint": hint})
	}

	// 9. Success output
	_, _ = fmt.Fprintf(stdout, "Restored to checkpoint %d (commit %s)\n", resp.CheckpointID, resp.SnapshotCommit[:8])

	return nil
}

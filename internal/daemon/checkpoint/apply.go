package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Applier handles checkpoint rollback operations.
type Applier struct {
	invocationID   string
	sandboxPath    string
	checkpointsDir string
	eventsPath     string
	runner         exec.CommandRunner
	fsys           fs.FS
	clock          func() time.Time
}

// NewApplier creates a new checkpoint applier.
func NewApplier(
	invocationID, sandboxPath, checkpointsDir, eventsPath string,
	runner exec.CommandRunner,
	fsys fs.FS,
	clock func() time.Time,
) *Applier {
	return &Applier{
		invocationID:   invocationID,
		sandboxPath:    sandboxPath,
		checkpointsDir: checkpointsDir,
		eventsPath:     eventsPath,
		runner:         runner,
		fsys:           fsys,
		clock:          clock,
	}
}

// Apply restores the sandbox to the state at the given checkpoint.
// Returns the applied checkpoint details on success.
func (a *Applier) Apply(ctx context.Context, checkpointID int) (*Checkpoint, error) {
	// 1. Load checkpoints file
	cpFile, err := a.loadCheckpoints()
	if err != nil {
		return nil, errors.Wrap(errors.ECheckpointFailed, "failed to load checkpoints", err)
	}

	// 2. Find the checkpoint
	cp := cpFile.FindByID(checkpointID)
	if cp == nil {
		return nil, errors.New(errors.ECheckpointNotFound, fmt.Sprintf("checkpoint %d not found", checkpointID))
	}

	// 3. Verify the snapshot commit exists
	verifyResult, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"cat-file", "-t", cp.SnapshotCommit,
	}, exec.RunOpts{})
	if err != nil || verifyResult.ExitCode != 0 {
		return nil, errors.New(errors.ECheckpointNotFound, fmt.Sprintf("snapshot commit %s not found or inaccessible", cp.SnapshotCommit))
	}

	// 4. Reset sandbox to clean state (removes any staged/unstaged changes)
	resetResult, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"reset", "--hard",
	}, exec.RunOpts{})
	if err != nil {
		return nil, errors.Wrap(errors.ERollbackFailed, "failed to execute git reset", err)
	}
	if resetResult.ExitCode != 0 {
		return nil, errors.New(errors.ERollbackFailed, fmt.Sprintf("git reset --hard failed: %s", resetResult.Stderr))
	}

	// 5. Clean untracked files (removes any untracked files/directories)
	cleanResult, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"clean", "-fd",
	}, exec.RunOpts{})
	if err != nil {
		return nil, errors.Wrap(errors.ERollbackFailed, "failed to execute git clean", err)
	}
	if cleanResult.ExitCode != 0 {
		return nil, errors.New(errors.ERollbackFailed, fmt.Sprintf("git clean -fd failed: %s", cleanResult.Stderr))
	}

	// 6. Restore exact state from snapshot commit
	// This checks out the entire tree from the snapshot commit
	checkoutResult, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"checkout", cp.SnapshotCommit, "--", ".",
	}, exec.RunOpts{})
	if err != nil {
		return nil, errors.Wrap(errors.ERollbackFailed, "failed to execute git checkout", err)
	}
	if checkoutResult.ExitCode != 0 {
		return nil, errors.New(errors.ERollbackFailed, fmt.Sprintf("git checkout failed: %s", checkoutResult.Stderr))
	}

	// 7. Emit checkpoint_applied event
	a.emitCheckpointApplied(checkpointID, cp.SnapshotCommit)

	return cp, nil
}

// loadCheckpoints reads checkpoints.json.
func (a *Applier) loadCheckpoints() (*CheckpointsFile, error) {
	cpPath := filepath.Join(a.checkpointsDir, "checkpoints.json")
	data, err := a.fsys.ReadFile(cpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewCheckpointsFile(), nil
		}
		return nil, err
	}

	var cpFile CheckpointsFile
	if err := json.Unmarshal(data, &cpFile); err != nil {
		return nil, err
	}

	return &cpFile, nil
}

// emitCheckpointApplied emits a checkpoint_applied event.
func (a *Applier) emitCheckpointApplied(checkpointID int, snapshotCommit string) {
	event := Event{
		SchemaVersion: "1.0",
		Seq:           1, // Events during apply get fresh sequence
		Timestamp:     a.clock().UTC().Format(time.RFC3339),
		InvocationID:  a.invocationID,
		Kind:          EventKindCheckpointApplied,
		Data:          CheckpointAppliedData(checkpointID, snapshotCommit),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	data = append(data, '\n')

	// Best-effort append
	f, err := os.OpenFile(a.eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	_, _ = f.Write(data)
}

// LoadCheckpointsFile is a helper to load checkpoints.json from a path.
func LoadCheckpointsFile(fsys fs.FS, checkpointsDir string) (*CheckpointsFile, error) {
	cpPath := filepath.Join(checkpointsDir, "checkpoints.json")
	data, err := fsys.ReadFile(cpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewCheckpointsFile(), nil
		}
		return nil, err
	}

	var cpFile CheckpointsFile
	if err := json.Unmarshal(data, &cpFile); err != nil {
		return nil, err
	}

	return &cpFile, nil
}

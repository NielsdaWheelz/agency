package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// ApplyOptions controls checkpoint apply behavior for different callers.
type ApplyOptions struct {
	// RewindHeadToSnapshotBase resets HEAD to checkpoint.sandbox_head_sha before
	// restoring the snapshot tree.
	RewindHeadToSnapshotBase bool

	// BackupRefPrefix controls where pre-apply HEAD backup refs are written.
	// Defaults to RestoreBackupRefPrefix when empty.
	BackupRefPrefix string

	// Env overlays every git command the applier runs.
	Env map[string]string
}

// Applier handles checkpoint rollback operations.
type Applier struct {
	invocationID   string
	sandboxPath    string
	checkpointsDir string
	eventsPath     string
	runner         exec.CommandRunner
	fsys           fs.FS
	clock          func() time.Time
	eventWriter    invocationevents.Appender
}

// NewApplier creates a new checkpoint applier.
func NewApplier(
	invocationID, sandboxPath, checkpointsDir, eventsPath string,
	runner exec.CommandRunner,
	fsys fs.FS,
	clock func() time.Time,
) *Applier {
	return NewApplierWithWriter(
		invocationID,
		sandboxPath,
		checkpointsDir,
		eventsPath,
		runner,
		fsys,
		clock,
		invocationevents.NewWriter(clock),
	)
}

// NewApplierWithWriter creates a checkpoint applier using a shared invocation
// event writer.
func NewApplierWithWriter(
	invocationID, sandboxPath, checkpointsDir, eventsPath string,
	runner exec.CommandRunner,
	fsys fs.FS,
	clock func() time.Time,
	eventWriter invocationevents.Appender,
) *Applier {
	if eventWriter == nil {
		eventWriter = invocationevents.NewWriter(clock)
	}
	return &Applier{
		invocationID:   invocationID,
		sandboxPath:    sandboxPath,
		checkpointsDir: checkpointsDir,
		eventsPath:     eventsPath,
		runner:         runner,
		fsys:           fsys,
		clock:          clock,
		eventWriter:    eventWriter,
	}
}

// Apply restores the sandbox to the state at the given checkpoint.
// Returns the applied checkpoint details on success.
func (a *Applier) Apply(ctx context.Context, checkpointID int) (*Checkpoint, error) {
	return a.ApplyWithOptions(ctx, checkpointID, ApplyOptions{})
}

// ApplyWithOptions restores the sandbox to the state at the given checkpoint
// with caller-controlled restore semantics.
func (a *Applier) ApplyWithOptions(ctx context.Context, checkpointID int, opts ApplyOptions) (*Checkpoint, error) {
	backupPrefix := strings.TrimSpace(opts.BackupRefPrefix)
	if backupPrefix == "" {
		backupPrefix = RestoreBackupRefPrefix
	}
	if !strings.HasSuffix(backupPrefix, "/") {
		backupPrefix += "/"
	}

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
	if err := a.verifyCommitExists(ctx, cp.SnapshotCommit, opts.Env); err != nil {
		return nil, errors.New(errors.ECheckpointNotFound, fmt.Sprintf("snapshot commit %s not found or inaccessible", cp.SnapshotCommit))
	}

	snapshotBase := ""
	if opts.RewindHeadToSnapshotBase {
		snapshotBase = strings.TrimSpace(cp.SandboxHeadSHA)
		if snapshotBase == "" {
			return nil, errors.New(errors.ECheckpointFailed, "checkpoint missing sandbox_head_sha")
		}
		if err := a.verifyCommitExists(ctx, snapshotBase, opts.Env); err != nil {
			return nil, errors.New(errors.ECheckpointNotFound, fmt.Sprintf("checkpoint sandbox head commit %s not found", snapshotBase))
		}
	}

	// 4. Emit checkpoint_apply_started before mutation.
	if err := a.emitCheckpointApplyStarted(checkpointID, cp.SnapshotCommit, opts.RewindHeadToSnapshotBase); err != nil {
		return nil, errors.Wrap(errors.ECheckpointFailed, "failed to append checkpoint_apply_started event", err)
	}

	// 5. Capture and persist pre-apply HEAD for safety/recovery.
	preApplyHead, err := a.currentHead(ctx, opts.Env)
	if err != nil {
		return nil, errors.Wrap(errors.ERollbackFailed, "failed to resolve pre-apply HEAD", err)
	}
	backupRef := a.buildBackupRef(backupPrefix, checkpointID)
	if err := a.createBackupRef(ctx, backupRef, preApplyHead, opts.Env); err != nil {
		return nil, errors.Wrap(errors.ERollbackFailed, "failed to create pre-apply backup ref", err)
	}

	restoreWithRecovery := func(restoreErr error, message string) error {
		recoverErr := a.recoverPreApplyState(preApplyHead, opts.Env)
		if recoverErr != nil {
			return errors.New(
				errors.ERollbackFailed,
				fmt.Sprintf("%s: %v (failed to recover pre-apply state from %s: %v)", message, restoreErr, backupRef, recoverErr),
			)
		}
		return errors.New(
			errors.ERollbackFailed,
			fmt.Sprintf("%s: %v (recovered pre-apply state from %s)", message, restoreErr, backupRef),
		)
	}

	// 6. Reset sandbox and optionally rewind HEAD to checkpoint base.
	resetTarget := ""
	if opts.RewindHeadToSnapshotBase {
		resetTarget = snapshotBase
	}
	if err := a.gitResetHard(ctx, resetTarget, opts.Env); err != nil {
		return nil, restoreWithRecovery(err, "failed to reset sandbox")
	}

	// 7. Remove untracked files/directories.
	if err := a.gitClean(ctx, opts.Env); err != nil {
		return nil, restoreWithRecovery(err, "failed to clean sandbox")
	}

	// 8. Restore exact tree/index state from snapshot commit.
	if err := a.gitReadTree(ctx, cp.SnapshotCommit, opts.Env); err != nil {
		return nil, restoreWithRecovery(err, "failed to restore snapshot tree")
	}

	// 9. Emit checkpoint_applied event
	if err := a.emitCheckpointApplied(checkpointID, cp.SnapshotCommit); err != nil {
		return nil, errors.Wrap(errors.ECheckpointFailed, "failed to append checkpoint_applied event", err)
	}

	return cp, nil
}

func (a *Applier) verifyCommitExists(ctx context.Context, sha string, env map[string]string) error {
	verifyResult, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"cat-file", "-t", sha,
	}, exec.RunOpts{Env: env})
	if err != nil {
		return err
	}
	if verifyResult.ExitCode != 0 {
		return fmt.Errorf("git cat-file -t %s failed: %s", sha, verifyResult.Stderr)
	}
	return nil
}

func (a *Applier) currentHead(ctx context.Context, env map[string]string) (string, error) {
	result, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"rev-parse", "HEAD",
	}, exec.RunOpts{Env: env})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("git rev-parse HEAD failed: %s", result.Stderr)
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (a *Applier) buildBackupRef(prefix string, checkpointID int) string {
	return fmt.Sprintf(
		"%s%s/%s-cp%d",
		prefix,
		a.invocationID,
		a.clock().UTC().Format("20060102T150405.000000000Z"),
		checkpointID,
	)
}

func (a *Applier) createBackupRef(ctx context.Context, backupRef, headSHA string, env map[string]string) error {
	result, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"update-ref", backupRef, headSHA,
	}, exec.RunOpts{Env: env})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git update-ref %s %s failed: %s", backupRef, headSHA, result.Stderr)
	}
	return nil
}

func (a *Applier) gitResetHard(ctx context.Context, target string, env map[string]string) error {
	args := []string{"-C", a.sandboxPath, "reset", "--hard"}
	if strings.TrimSpace(target) != "" {
		args = append(args, target)
	}
	result, err := a.runner.Run(ctx, "git", args, exec.RunOpts{Env: env})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git reset --hard failed: %s", result.Stderr)
	}
	return nil
}

func (a *Applier) gitClean(ctx context.Context, env map[string]string) error {
	result, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"clean", "-fd",
	}, exec.RunOpts{Env: env})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git clean -fd failed: %s", result.Stderr)
	}
	return nil
}

func (a *Applier) gitReadTree(ctx context.Context, snapshotCommit string, env map[string]string) error {
	result, err := a.runner.Run(ctx, "git", []string{
		"-C", a.sandboxPath,
		"read-tree", "--reset", "-u", snapshotCommit,
	}, exec.RunOpts{Env: env})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git read-tree --reset -u %s failed: %s", snapshotCommit, result.Stderr)
	}
	return nil
}

func (a *Applier) recoverPreApplyState(preApplyHead string, env map[string]string) error {
	recoverCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.gitResetHard(recoverCtx, preApplyHead, env); err != nil {
		return err
	}
	if err := a.gitClean(recoverCtx, env); err != nil {
		return err
	}
	return nil
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
	if !ValidSchemaVersion(cpFile.SchemaVersion) {
		return nil, fmt.Errorf("unknown checkpoints.json schema_version %q", cpFile.SchemaVersion)
	}

	return &cpFile, nil
}

// emitCheckpointApplied emits a checkpoint_applied event.
func (a *Applier) emitCheckpointApplied(checkpointID int, snapshotCommit string) error {
	_, err := a.eventWriter.Append(
		a.eventsPath,
		a.invocationID,
		string(EventKindCheckpointApplied),
		CheckpointAppliedData(checkpointID, snapshotCommit),
		invocationevents.AppendOptions{},
	)
	return err
}

// emitCheckpointApplyStarted emits a checkpoint_apply_started event.
func (a *Applier) emitCheckpointApplyStarted(checkpointID int, snapshotCommit string, rewindHead bool) error {
	_, err := a.eventWriter.Append(
		a.eventsPath,
		a.invocationID,
		string(EventKindCheckpointApplyStarted),
		CheckpointApplyStartedData(checkpointID, snapshotCommit, rewindHead),
		invocationevents.AppendOptions{},
	)
	return err
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

	if !ValidSchemaVersion(cpFile.SchemaVersion) {
		return nil, fmt.Errorf("unknown checkpoints.json schema_version %q", cpFile.SchemaVersion)
	}

	return &cpFile, nil
}

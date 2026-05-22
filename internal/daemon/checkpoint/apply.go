package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
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
	// Defaults to restoreBackupRefPrefix when empty.
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
	eventWriter    eventlog.Appender
}

// NewApplierWithWriter creates a checkpoint applier using a shared invocation
// event writer.
func NewApplierWithWriter(
	invocationID, sandboxPath, checkpointsDir, eventsPath string,
	runner exec.CommandRunner,
	fsys fs.FS,
	clock func() time.Time,
	eventWriter eventlog.Appender,
) *Applier {
	if eventWriter == nil {
		eventWriter = eventlog.NewWriter("invocation_id", clock)
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

// ApplyWithOptions restores the sandbox to the state at the given checkpoint
// with caller-controlled restore semantics.
func (a *Applier) ApplyWithOptions(ctx context.Context, checkpointID int, opts ApplyOptions) (*Checkpoint, error) {
	backupPrefix := strings.TrimSpace(opts.BackupRefPrefix)
	if backupPrefix == "" {
		backupPrefix = restoreBackupRefPrefix
	}
	if !strings.HasSuffix(backupPrefix, "/") {
		backupPrefix += "/"
	}

	// 1. Load checkpoints file
	cpFile, err := a.loadCheckpoints()
	if err != nil {
		if errors.GetCode(err) != "" {
			return nil, err
		}
		return nil, errors.Wrap(errors.ECheckpointFailed, "failed to load checkpoints", err)
	}

	// 2. Find the checkpoint
	cp := cpFile.findByID(checkpointID)
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

// runGit runs `git -C <sandbox> <args...>` and maps non-zero exit / process error to a single labelled failure.
func (a *Applier) runGit(ctx context.Context, env map[string]string, label string, args ...string) (exec.CmdResult, error) {
	fullArgs := append([]string{"-C", a.sandboxPath}, args...)
	result, err := a.runner.Run(ctx, "git", fullArgs, exec.RunOpts{Env: env})
	if err != nil {
		return result, err
	}
	if result.ExitCode != 0 {
		return result, fmt.Errorf("%s failed: %s", label, result.Stderr)
	}
	return result, nil
}

func (a *Applier) verifyCommitExists(ctx context.Context, sha string, env map[string]string) error {
	_, err := a.runGit(ctx, env, "git cat-file -t "+sha, "cat-file", "-t", sha)
	return err
}

func (a *Applier) currentHead(ctx context.Context, env map[string]string) (string, error) {
	result, err := a.runGit(ctx, env, "git rev-parse HEAD", "rev-parse", "HEAD")
	if err != nil {
		return "", err
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
	_, err := a.runGit(ctx, env, fmt.Sprintf("git update-ref %s %s", backupRef, headSHA), "update-ref", backupRef, headSHA)
	return err
}

func (a *Applier) gitResetHard(ctx context.Context, target string, env map[string]string) error {
	args := []string{"reset", "--hard"}
	if strings.TrimSpace(target) != "" {
		args = append(args, target)
	}
	_, err := a.runGit(ctx, env, "git reset --hard", args...)
	return err
}

func (a *Applier) gitClean(ctx context.Context, env map[string]string) error {
	_, err := a.runGit(ctx, env, "git clean -fd", "clean", "-fd")
	return err
}

func (a *Applier) gitReadTree(ctx context.Context, snapshotCommit string, env map[string]string) error {
	_, err := a.runGit(ctx, env, "git read-tree --reset -u "+snapshotCommit, "read-tree", "--reset", "-u", snapshotCommit)
	return err
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
	return LoadCheckpointsFile(a.fsys, cpPath)
}

// emitCheckpointApplied emits a checkpoint_applied event.
func (a *Applier) emitCheckpointApplied(checkpointID int, snapshotCommit string) error {
	_, err := a.eventWriter.Append(
		a.eventsPath,
		a.invocationID,
		string(eventKindCheckpointApplied),
		checkpointAppliedData(checkpointID, snapshotCommit),
		eventlog.AppendOptions{},
	)
	return err
}

// emitCheckpointApplyStarted emits a checkpoint_apply_started event.
func (a *Applier) emitCheckpointApplyStarted(checkpointID int, snapshotCommit string, rewindHead bool) error {
	_, err := a.eventWriter.Append(
		a.eventsPath,
		a.invocationID,
		string(eventKindCheckpointApplyStarted),
		checkpointApplyStartedData(checkpointID, snapshotCommit, rewindHead),
		eventlog.AppendOptions{},
	)
	return err
}

// LoadCheckpointsFile loads checkpoints.json from its exact file path.
func LoadCheckpointsFile(fsys fs.FS, checkpointsPath string) (*CheckpointsFile, error) {
	data, err := fsys.ReadFile(checkpointsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewCheckpointsFile(), nil
		}
		return nil, errors.Wrap(errors.EStoreCorrupt, fmt.Sprintf("failed to read checkpoints.json at %s", checkpointsPath), err)
	}

	var cpFile CheckpointsFile
	if err := json.Unmarshal(data, &cpFile); err != nil {
		return nil, errors.Wrap(errors.EStoreCorrupt, fmt.Sprintf("invalid checkpoints.json at %s", checkpointsPath), err)
	}

	if strings.TrimSpace(cpFile.SchemaVersion) == "" {
		return nil, errors.New(errors.EStoreCorrupt, fmt.Sprintf("checkpoints.json at %s missing schema_version", checkpointsPath))
	}
	if cpFile.SchemaVersion != SchemaVersion {
		return nil, errors.New(errors.EStoreCorrupt, fmt.Sprintf("unknown checkpoints.json schema_version %q at %s", cpFile.SchemaVersion, checkpointsPath))
	}

	return &cpFile, nil
}

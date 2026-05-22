package checkpoint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/NielsdaWheelz/agency/internal/exec"
)

type checkpointPlan struct {
	cpFile     *CheckpointsFile
	checkpoint Checkpoint
	createdAt  time.Time
}

func (e *Engine) runWithWatcher(ctx context.Context, watcher *fsnotify.Watcher, debounceInterval time.Duration, hasTriggerCh bool) error {
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		select {
		case <-debounceTimer.C:
		default:
		}
	}
	debouncePending := false

	pollTicker := time.NewTicker(e.config.PollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.doFinalCheckpoint(ctx)
			return ctx.Err()

		case <-e.done:
			e.doFinalCheckpoint(context.Background())
			return nil

		case trigger, ok := <-e.triggerChanOrNil():
			if !ok {
				e.triggerCh = nil
				continue
			}
			if err := e.CreateSemanticCheckpoint(ctx, &trigger); err != nil {
				_ = e.emitCheckpointFailed(err.Error())
			}

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if e.shouldIgnorePath(event.Name) {
				continue
			}
			e.handleWatchEvent(event, debounceTimer, debounceInterval, &debouncePending)

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			_ = err

		case <-debounceTimer.C:
			debouncePending = false
			e.handleDebounceFire(ctx, hasTriggerCh)

		case <-pollTicker.C:
			e.tryCheckpointIfDirty(ctx)
		}
	}
}

func (e *Engine) handleWatchEvent(event fsnotify.Event, debounceTimer *time.Timer, debounceInterval time.Duration, debouncePending *bool) {
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			_ = e.addWatchRecursive(event.Name)
		}
	}

	if !*debouncePending {
		*debouncePending = true
		debounceTimer.Reset(debounceInterval)
		return
	}

	if !debounceTimer.Stop() {
		select {
		case <-debounceTimer.C:
		default:
		}
	}
	debounceTimer.Reset(debounceInterval)
}

func (e *Engine) handleDebounceFire(ctx context.Context, hasTriggerCh bool) {
	if hasTriggerCh {
		e.tryDriftCheckpoint(ctx)
		return
	}
	e.tryCheckpoint(ctx)
}

func (e *Engine) createCheckpointFlow(ctx context.Context, trigger *TriggerEvent) error {
	plan, err := e.buildCheckpointPlan(ctx, trigger)
	if err != nil || plan == nil {
		return err
	}

	plan.cpFile.Checkpoints = append(plan.cpFile.Checkpoints, plan.checkpoint)
	if len(plan.cpFile.Checkpoints) > maxCheckpoints {
		e.pruneCheckpoints(ctx, plan.cpFile)
	}

	if err := e.saveCheckpoints(plan.cpFile); err != nil {
		return fmt.Errorf("failed to save checkpoints: %w", err)
	}

	e.mu.Lock()
	e.lastCheckpoint = plan.createdAt
	e.mu.Unlock()

	if err := e.emitCheckpointCreated(plan.checkpoint.ID, plan.checkpoint.IncludesUntracked, plan.checkpoint.SandboxHeadSHA); err != nil {
		return fmt.Errorf("failed to append checkpoint_created event: %w", err)
	}

	return nil
}

func (e *Engine) buildCheckpointPlan(ctx context.Context, trigger *TriggerEvent) (*checkpointPlan, error) {
	dirty, err := e.isDirty(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check dirty state: %w", err)
	}
	if !dirty {
		return nil, nil
	}

	includeUntracked, err := e.resolveCheckpointIncludeUntracked(ctx)
	if err != nil {
		return nil, err
	}

	sandboxHeadSHA, err := e.getCurrentSandboxHead(ctx)
	if err != nil {
		return nil, err
	}

	tempIndexPath, cleanupTempIndex, err := e.createTempCheckpointIndex()
	if err != nil {
		return nil, err
	}
	defer cleanupTempIndex()

	treeHash, err := e.writeCheckpointTree(ctx, tempIndexPath, includeUntracked)
	if err != nil {
		return nil, err
	}

	cpFile, err := e.loadCheckpoints()
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoints: %w", err)
	}
	if n := len(cpFile.Checkpoints); n > 0 {
		if lastTree := cpFile.Checkpoints[n-1].TreeSHA; lastTree == treeHash && treeHash != "" {
			return nil, nil
		}
	}

	checkpointID := cpFile.nextID()
	snapshotRef := fmt.Sprintf("%s%s/%d", RefPrefix, e.invocationID, checkpointID)
	snapshotCommit, err := e.createCheckpointCommit(ctx, treeHash, checkpointID)
	if err != nil {
		return nil, err
	}
	if err := e.createCheckpointRef(ctx, snapshotRef, snapshotCommit); err != nil {
		return nil, err
	}

	diffBaseCommit := sandboxHeadSHA
	if n := len(cpFile.Checkpoints); n > 0 {
		if prev := strings.TrimSpace(cpFile.Checkpoints[n-1].SnapshotCommit); prev != "" {
			diffBaseCommit = prev
		}
	}

	diffstat := e.computeDiffstat(ctx, diffBaseCommit, snapshotCommit)
	changedPaths, changedPathCount, changedPathTruncated := e.computeChangedPaths(
		ctx,
		diffBaseCommit,
		snapshotCommit,
		maxChangedPathsPreview,
	)

	now := e.clock()
	checkpoint := Checkpoint{
		ID:                   checkpointID,
		SnapshotRef:          snapshotRef,
		SnapshotCommit:       snapshotCommit,
		SandboxHeadSHA:       sandboxHeadSHA,
		CreatedAt:            now.UTC().Format(time.RFC3339),
		IncludesUntracked:    includeUntracked,
		Diffstat:             diffstat,
		TreeSHA:              treeHash,
		ChangedPaths:         changedPaths,
		ChangedPathCount:     changedPathCount,
		ChangedPathTruncated: changedPathTruncated,
	}
	if trigger != nil {
		checkpoint.Trigger = trigger.Kind
		checkpoint.ToolName = trigger.ToolName
		checkpoint.StreamSeq = trigger.Seq
		checkpoint.Description = describeTrigger(trigger)
	}

	return &checkpointPlan{
		cpFile:     cpFile,
		checkpoint: checkpoint,
		createdAt:  now,
	}, nil
}

func (e *Engine) resolveCheckpointIncludeUntracked(ctx context.Context) (bool, error) {
	includeUntracked := e.config.IncludeUntracked
	if !includeUntracked {
		return false, nil
	}

	deniedFiles, err := e.checkDenylist(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check denylist: %w", err)
	}
	if len(deniedFiles) == 0 {
		return true, nil
	}

	if err := e.emitDenylistTriggered(deniedFiles); err != nil {
		return false, fmt.Errorf("failed to append checkpoint_denylist_triggered event: %w", err)
	}
	return false, nil
}

// runGit runs `git -C <dir> <args...>` and maps non-zero exit / process error to a single labelled failure.
func (e *Engine) runGit(ctx context.Context, dir string, env map[string]string, label string, args ...string) (exec.CmdResult, error) {
	fullArgs := append([]string{"-C", dir}, args...)
	result, err := e.runner.Run(ctx, "git", fullArgs, exec.RunOpts{Env: env})
	if err != nil {
		return result, err
	}
	if result.ExitCode != 0 {
		return result, fmt.Errorf("%s failed: %s", label, result.Stderr)
	}
	return result, nil
}

func (e *Engine) getCurrentSandboxHead(ctx context.Context) (string, error) {
	result, err := e.runGit(ctx, e.sandboxPath, e.config.Env, "git rev-parse HEAD", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (e *Engine) createTempCheckpointIndex() (string, func(), error) {
	tempIndex, err := os.CreateTemp("", "agency-checkpoint-index-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp index: %w", err)
	}
	tempIndexPath := tempIndex.Name()
	_ = tempIndex.Close()

	cleanup := func() {
		_ = os.Remove(tempIndexPath)
	}
	return tempIndexPath, cleanup, nil
}

func (e *Engine) writeCheckpointTree(ctx context.Context, tempIndexPath string, includeUntracked bool) (string, error) {
	gitDir, err := e.getGitDir(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get git dir: %w", err)
	}
	indexPath := filepath.Join(gitDir, "index")
	if data, err := os.ReadFile(indexPath); err == nil {
		if err := os.WriteFile(tempIndexPath, data, 0o600); err != nil {
			return "", fmt.Errorf("failed to copy index: %w", err)
		}
	}

	env := gitEnv(e.config.Env, map[string]string{"GIT_INDEX_FILE": tempIndexPath})
	if err := e.stageCheckpointIndex(ctx, includeUntracked, env); err != nil {
		return "", err
	}

	result, err := e.runGit(ctx, e.sandboxPath, env, "git write-tree", "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (e *Engine) stageCheckpointIndex(ctx context.Context, includeUntracked bool, env map[string]string) error {
	addFlag := "-u"
	if includeUntracked {
		addFlag = "-A"
	}
	addResult, err := e.runGit(ctx, e.sandboxPath, env, "git add", "add", addFlag)
	if err != nil {
		if strings.Contains(addResult.Stderr, "index.lock") {
			return fmt.Errorf("index lock detected: %s", addResult.Stderr)
		}
		return err
	}

	for _, exclude := range []string{".agency", ".git"} {
		if _, err := e.runGit(ctx, e.sandboxPath, env, "git rm --cached "+exclude, "rm", "-r", "--cached", "--ignore-unmatch", exclude); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) createCheckpointCommit(ctx context.Context, treeHash string, checkpointID int) (string, error) {
	commitMessage := fmt.Sprintf("agency snapshot %s %d", e.invocationID, checkpointID)
	result, err := e.runGit(ctx, e.sandboxPath, e.config.Env, "git commit-tree", "commit-tree", treeHash, "-p", "HEAD", "-m", commitMessage)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (e *Engine) createCheckpointRef(ctx context.Context, snapshotRef, snapshotCommit string) error {
	_, err := e.runGit(ctx, e.repoRoot, e.config.Env, "git update-ref", "update-ref", snapshotRef, snapshotCommit)
	return err
}

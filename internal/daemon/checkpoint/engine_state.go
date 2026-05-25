package checkpoint

import (
	"context"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func (e *Engine) pruneCheckpoints(ctx context.Context, cpFile *CheckpointsFile) {
	excess := len(cpFile.Checkpoints) - maxCheckpoints
	if excess <= 0 {
		return
	}

	for i := 0; i < excess; i++ {
		cp := cpFile.Checkpoints[i]
		_, _ = e.runner.Run(ctx, "git", []string{
			"-C", e.repoRoot,
			"update-ref", "-d", cp.SnapshotRef,
		}, exec.RunOpts{Env: e.config.Env})
	}

	cpFile.Checkpoints = cpFile.Checkpoints[excess:]
}

func (e *Engine) loadCheckpoints() (*CheckpointsFile, error) {
	cpPath := filepath.Join(e.checkpointsDir, "checkpoints.json")
	return LoadCheckpointsFile(e.fsys, cpPath)
}

func (e *Engine) saveCheckpoints(cpFile *CheckpointsFile) error {
	cpPath := filepath.Join(e.checkpointsDir, "checkpoints.json")
	return fs.WriteJSONAtomic(e.fsys, cpPath, cpFile, 0o644)
}

func describeTrigger(trigger *TriggerEvent) string {
	if trigger == nil {
		return ""
	}
	switch trigger.Kind {
	case TriggerToolEnd:
		if trigger.ToolName != "" {
			return "After " + trigger.ToolName
		}
		return "After tool completion"
	case triggerDrift:
		return "Drift checkpoint"
	case triggerShutdown:
		return "Final checkpoint"
	case triggerPoll:
		return "Poll checkpoint"
	case triggerManual:
		return "Manual checkpoint"
	default:
		return "Checkpoint"
	}
}

func (e *Engine) emit(kind eventKind, data map[string]any) error {
	_, err := e.eventWriter.Append(e.eventsPath, e.invocationID, string(kind), data, eventlog.AppendOptions{})
	return err
}

func (e *Engine) emitCheckpointCreated(checkpointID int, includesUntracked bool, sandboxHeadSHA string) error {
	return e.emit(eventKindCheckpointCreated, checkpointCreatedData(checkpointID, includesUntracked, sandboxHeadSHA))
}

func (e *Engine) emitCheckpointFailed(reason string) error {
	return e.emit(eventKindCheckpointFailed, checkpointFailedData(reason))
}

func (e *Engine) emitDenylistTriggered(files []string) error {
	return e.emit(eventKindCheckpointDenylistTriggered, checkpointDenylistTriggeredData(files))
}

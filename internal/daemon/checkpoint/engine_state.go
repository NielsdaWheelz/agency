package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func (e *Engine) pruneCheckpoints(ctx context.Context, cpFile *CheckpointsFile) {
	excess := len(cpFile.Checkpoints) - MaxCheckpoints
	if excess <= 0 {
		return
	}

	for i := 0; i < excess; i++ {
		cp := cpFile.Checkpoints[i]
		_, _ = e.runner.Run(ctx, "git", []string{
			"-C", e.repoRoot,
			"update-ref", "-d", cp.SnapshotRef,
		}, exec.RunOpts{})
	}

	cpFile.Checkpoints = cpFile.Checkpoints[excess:]
}

func (e *Engine) loadCheckpoints() (*CheckpointsFile, error) {
	cpPath := filepath.Join(e.checkpointsDir, "checkpoints.json")
	data, err := e.fsys.ReadFile(cpPath)
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

func (e *Engine) saveCheckpoints(cpFile *CheckpointsFile) error {
	cpPath := filepath.Join(e.checkpointsDir, "checkpoints.json")
	return fs.WriteJSONAtomic(cpPath, cpFile, 0o644)
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
	case TriggerDrift:
		return "Drift checkpoint"
	case TriggerShutdown:
		return "Final checkpoint"
	case TriggerPoll:
		return "Poll checkpoint"
	case TriggerManual:
		return "Manual checkpoint"
	default:
		return "Checkpoint"
	}
}

func (e *Engine) emitCheckpointCreated(checkpointID int, includesUntracked bool, sandboxHeadSHA string) error {
	_, err := e.eventWriter.Append(
		e.eventsPath,
		e.invocationID,
		string(EventKindCheckpointCreated),
		CheckpointCreatedData(checkpointID, includesUntracked, sandboxHeadSHA),
		invocationevents.AppendOptions{},
	)
	return err
}

func (e *Engine) emitCheckpointFailed(reason string) error {
	_, err := e.eventWriter.Append(
		e.eventsPath,
		e.invocationID,
		string(EventKindCheckpointFailed),
		CheckpointFailedData(reason),
		invocationevents.AppendOptions{},
	)
	return err
}

func (e *Engine) emitDenylistTriggered(files []string) error {
	_, err := e.eventWriter.Append(
		e.eventsPath,
		e.invocationID,
		string(EventKindCheckpointDenylistTriggered),
		CheckpointDenylistTriggeredData(files),
		invocationevents.AppendOptions{},
	)
	return err
}

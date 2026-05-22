package daemon

import (
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
)

const checkpointTriggerDropTimeout = 250 * time.Millisecond

func (s *Server) attachCheckpointTriggers(repoID, invocationID string, parser *stream.Parser, cpEngine *checkpoint.Engine) {
	if parser == nil || !parser.CanParse() || cpEngine == nil {
		return
	}

	triggerCh := make(chan checkpoint.TriggerEvent, 32)
	cpEngine.SetTriggerChannel(triggerCh)
	parser.SetCheckpointNotify(func(n stream.CheckpointNotification) {
		s.enqueueCheckpointTrigger(repoID, invocationID, triggerCh, n)
	})
}

func (s *Server) enqueueCheckpointTrigger(repoID, invocationID string, triggerCh chan<- checkpoint.TriggerEvent, n stream.CheckpointNotification) {
	trigger := checkpoint.TriggerEvent{
		Kind:      checkpoint.TriggerToolEnd,
		ToolName:  n.ToolName,
		ToolNames: n.ToolNames,
		Seq:       n.Seq,
	}
	select {
	case triggerCh <- trigger:
		return
	default:
	}

	timer := time.NewTimer(checkpointTriggerDropTimeout)
	defer timer.Stop()
	select {
	case triggerCh <- trigger:
	case <-timer.C:
		s.recordInvocationWarning(repoID, invocationID, "checkpoint_trigger_dropped", "checkpoint trigger queue full; dropped semantic trigger", map[string]any{
			"seq":       n.Seq,
			"tool_name": n.ToolName,
		})
	}
}

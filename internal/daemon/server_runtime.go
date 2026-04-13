package daemon

import (
	stderrors "errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) flushLastOutputAt(proc *SupervisedProcess) {
	lastOutput := time.Unix(0, s.latestOutputAtUnixNano(proc))
	if lastOutput.IsZero() {
		return
	}

	_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
		meta.LastOutputAt = lastOutput.UTC().Format(time.RFC3339)
	})
}

func (s *Server) latestOutputAtUnixNano(proc *SupervisedProcess) int64 {
	if proc == nil {
		return 0
	}
	lastOutput := proc.lastOutputAt.Load()
	if proc.Parser != nil {
		if parserLastOutput := proc.Parser.GetLastOutputAt(); parserLastOutput > lastOutput {
			lastOutput = parserLastOutput
		}
	}
	return lastOutput
}

func (s *Server) LoadUserConfig() (config.UserConfig, error) {
	cfg, _, err := config.LoadUserConfig(s.FS, s.ConfigDir)
	return cfg, err
}

func (s *Server) streamOutput(proc *SupervisedProcess, reader io.Reader, file *os.File) {
	defer proc.streamWg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			_, _ = file.Write(buf[:n])
			proc.lastOutputAt.Store(s.Clock().UnixNano())
		}
		if err != nil {
			break
		}
	}
}

func (s *Server) streamAndParseOutput(proc *SupervisedProcess, reader io.Reader, rawFile, streamFile *os.File) {
	defer proc.streamWg.Done()
	if proc.Parser == nil {
		s.streamOutput(proc, reader, rawFile)
		return
	}

	if err := proc.Parser.StreamAndParse(reader, rawFile, streamFile); err != nil {
		if !stderrors.Is(err, stream.ErrRawLogWriteFailed) && !stderrors.Is(err, stream.ErrNormalizedStreamWriteFailed) {
			return
		}

		proc.exitReason.Store("stream_write_failed")
		proc.failureReason.Store("stream_write_failed")
		_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
			meta.Flags.NeedsAttention = true
			meta.FailureReason = "stream_write_failed"
		})
		s.recordInvocationWarning(proc.RepoID, proc.InvocationID, "stream_write_failed", err.Error(), nil)

		if proc.PGID > 0 {
			_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
		}
	}
}

func (s *Server) runSemanticStatusFlushLoop(proc *SupervisedProcess) {
	if proc.Parser == nil {
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastStatus *runnerstatus.Status
	var lastUpdatedAt time.Time

	for {
		select {
		case <-proc.done:
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			currentStatus := proc.Parser.GetSemanticStatus()
			currentUpdatedAt := proc.Parser.GetSemanticStatusUpdatedAt()

			statusChanged := false
			if currentStatus == nil && lastStatus != nil {
				statusChanged = true
			} else if currentStatus != nil && lastStatus == nil {
				statusChanged = true
			} else if currentStatus != nil && lastStatus != nil && *currentStatus != *lastStatus {
				statusChanged = true
			} else if !currentUpdatedAt.IsZero() && currentUpdatedAt.After(lastUpdatedAt) {
				statusChanged = true
			}

			if statusChanged {
				s.flushSemanticStatus(proc, currentStatus, currentUpdatedAt)
				lastStatus = currentStatus
				lastUpdatedAt = currentUpdatedAt
			}
		}
	}
}

func (s *Server) flushSemanticStatus(proc *SupervisedProcess, status *runnerstatus.Status, updatedAt time.Time) {
	_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
		meta.SemanticStatus = status
		if status != nil && !updatedAt.IsZero() {
			meta.SemanticStatusUpdatedAt = updatedAt.UTC().Format(time.RFC3339)
			return
		}
		meta.SemanticStatusUpdatedAt = ""
	})
}

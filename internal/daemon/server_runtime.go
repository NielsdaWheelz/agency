package daemon

import (
	stderrors "errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) flushLastOutputAt(proc *supervisedProcess) {
	lastOutput := time.Unix(0, s.latestOutputAtUnixNano(proc))
	if lastOutput.IsZero() {
		return
	}

	_, _ = s.store.UpdateInvocationMeta(proc.repoID, proc.invocationID, func(meta *store.InvocationMeta) {
		meta.LastOutputAt = lastOutput.UTC().Format(time.RFC3339)
	})
}

func (s *Server) latestOutputAtUnixNano(proc *supervisedProcess) int64 {
	if proc == nil {
		return 0
	}
	lastOutput := proc.lastOutputAt.Load()
	if proc.parser != nil {
		if parserLastOutput := proc.parser.GetLastOutputAt(); parserLastOutput > lastOutput {
			lastOutput = parserLastOutput
		}
	}
	return lastOutput
}

func (s *Server) LoadUserConfig() (config.UserConfig, error) {
	return config.LoadUserConfig(s.fsys, s.configDir)
}

func (s *Server) streamOutput(proc *supervisedProcess, reader io.Reader, file *os.File) {
	defer proc.streamWg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			_, _ = file.Write(buf[:n])
			proc.lastOutputAt.Store(s.clock().UnixNano())
		}
		if err != nil {
			break
		}
	}
}

func (s *Server) streamAndParseOutput(proc *supervisedProcess, reader io.Reader, rawFile, streamFile *os.File) {
	defer proc.streamWg.Done()
	if proc.parser == nil {
		s.streamOutput(proc, reader, rawFile)
		return
	}

	if err := proc.parser.StreamAndParse(reader, rawFile, streamFile); err != nil {
		if !stderrors.Is(err, stream.ErrRawLogWriteFailed) && !stderrors.Is(err, stream.ErrNormalizedStreamWriteFailed) {
			return
		}

		proc.exitReason.Store("stream_write_failed")
		proc.failureReason.Store("stream_write_failed")
		s.persistInvocationMeta(proc.repoID, proc.invocationID, func(meta *store.InvocationMeta) {
			meta.Flags.NeedsAttention = true
			meta.FailureReason = "stream_write_failed"
		})
		s.recordInvocationWarning(proc.repoID, proc.invocationID, "stream_write_failed", err.Error(), nil)

		if proc.pgid > 0 {
			_ = syscall.Kill(-proc.pgid, syscall.SIGKILL)
		}
	}
}

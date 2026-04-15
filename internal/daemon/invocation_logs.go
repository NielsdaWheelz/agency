package daemon

import (
	"fmt"
	"os"
)

type invocationLogFiles struct {
	RawPath    string
	RawFile    *os.File
	StderrPath string
	StderrFile *os.File
	StreamPath string
	StreamFile *os.File
}

func (f *invocationLogFiles) Close() {
	if f == nil {
		return
	}
	if f.RawFile != nil {
		_ = f.RawFile.Close()
	}
	if f.StderrFile != nil {
		_ = f.StderrFile.Close()
	}
	if f.StreamFile != nil {
		_ = f.StreamFile.Close()
	}
}

func (s *Server) preferredInvocationLogsDir(repoID, invocationID string) string {
	return s.Store.InvocationLogsDir(repoID, invocationID)
}

func (s *Server) readableInvocationLogPath(repoID, invocationID, kind string) string {
	switch kind {
	case "stderr":
		return s.Store.InvocationStderrLogPath(repoID, invocationID)
	case "stream":
		return s.Store.InvocationStreamLogPath(repoID, invocationID)
	case "hooks":
		return s.Store.InvocationHooksLogPath(repoID, invocationID)
	case "terminal":
		return s.Store.InvocationTerminalLogPath(repoID, invocationID)
	default:
		return s.Store.InvocationRawLogPath(repoID, invocationID)
	}
}

func (s *Server) prepareWritableInvocationLogPath(repoID, invocationID, kind string) (string, error) {
	path, err := s.Store.PrepareInvocationLogPath(repoID, invocationID, kind)
	if err != nil {
		return "", fmt.Errorf("prepare invocation %s log path: %w", kind, err)
	}
	return path, nil
}

func (s *Server) openInvocationLogFiles(repoID, invocationID string) (*invocationLogFiles, error) {
	rawPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, "raw")
	if err != nil {
		return nil, err
	}
	stderrPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, "stderr")
	if err != nil {
		return nil, err
	}
	streamPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, "stream")
	if err != nil {
		return nil, err
	}

	files := &invocationLogFiles{
		RawPath:    rawPath,
		StderrPath: stderrPath,
		StreamPath: streamPath,
	}

	files.RawFile, err = os.OpenFile(rawPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open raw log file: %w", err)
	}
	files.StderrFile, err = os.OpenFile(stderrPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		files.Close()
		return nil, fmt.Errorf("open stderr log file: %w", err)
	}
	files.StreamFile, err = os.OpenFile(streamPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		files.Close()
		return nil, fmt.Errorf("open stream log file: %w", err)
	}

	return files, nil
}

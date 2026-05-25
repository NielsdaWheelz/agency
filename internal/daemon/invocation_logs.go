package daemon

import (
	"fmt"
	"os"
)

// openAppendLog opens path for append+create with private (0o600) permission,
// the canonical mode for invocation, hook, terminal, and other daemon-owned
// log files.
func openAppendLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
}

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

func (s *Server) readableInvocationLogPath(repoID, invocationID, kind string) string {
	switch kind {
	case InvocationLogKindStderr:
		return s.store.InvocationStderrLogPath(repoID, invocationID)
	case InvocationLogKindStream:
		return s.store.InvocationStreamLogPath(repoID, invocationID)
	case InvocationLogKindHooks:
		return s.store.InvocationHooksLogPath(repoID, invocationID)
	case InvocationLogKindTerminal:
		return s.store.InvocationTerminalLogPath(repoID, invocationID)
	default:
		return s.store.InvocationRawLogPath(repoID, invocationID)
	}
}

func (s *Server) prepareWritableInvocationLogPath(repoID, invocationID, kind string) (string, error) {
	if _, err := s.store.EnsureInvocationLogsDir(repoID, invocationID); err != nil {
		return "", fmt.Errorf("prepare invocation %s log path: %w", kind, err)
	}
	return s.readableInvocationLogPath(repoID, invocationID, kind), nil
}

func (s *Server) openInvocationLogFiles(repoID, invocationID string) (*invocationLogFiles, error) {
	rawPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, InvocationLogKindRaw)
	if err != nil {
		return nil, err
	}
	stderrPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, InvocationLogKindStderr)
	if err != nil {
		return nil, err
	}
	streamPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, InvocationLogKindStream)
	if err != nil {
		return nil, err
	}

	files := &invocationLogFiles{
		RawPath:    rawPath,
		StderrPath: stderrPath,
		StreamPath: streamPath,
	}

	files.RawFile, err = openAppendLog(rawPath)
	if err != nil {
		return nil, fmt.Errorf("open raw log file: %w", err)
	}
	files.StderrFile, err = openAppendLog(stderrPath)
	if err != nil {
		files.Close()
		return nil, fmt.Errorf("open stderr log file: %w", err)
	}
	files.StreamFile, err = openAppendLog(streamPath)
	if err != nil {
		files.Close()
		return nil, fmt.Errorf("open stream log file: %w", err)
	}

	return files, nil
}

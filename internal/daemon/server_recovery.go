package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func (s *Server) runRecoveryScan() error {
	data, err := s.FS.ReadFile(s.Store.RepoIndexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var index store.RepoIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return err
	}
	for repoID := range index.Repos {
		if err := s.recoverRepoInvocations(repoID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: recovery scan for repo %s failed: %v\n", repoID, err)
		}
	}
	return nil
}

func (s *Server) recoverRepoInvocations(repoID string) error {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := s.Clock().UTC().Format(time.RFC3339)
	nowTime := s.Clock()
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		if r.Meta.Mode == store.RunnerModeHeaded {
			s.recoverHeadedInvocation(ctx, repoID, r, now, nowTime)
			continue
		}
		if r.Meta.Status == store.InvocationStatusStarting {
			startedAt, err := time.Parse(time.RFC3339, r.Meta.StartedAt)
			if err == nil && nowTime.Sub(startedAt) > 60*time.Second && r.Meta.PID == nil {
				_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
					meta.Status = store.InvocationStatusFailed
					meta.ExitReason = "start_failed"
					meta.FailureReason = "start_incomplete"
					meta.FinishedAt = now
					meta.Flags.NeedsAttention = true
					meta.LifecycleOwner = ""
				})
			}
			continue
		}
		if r.Meta.Status != store.InvocationStatusRunning || r.Meta.PID == nil {
			continue
		}

		pid := *r.Meta.PID
		if !s.PIDChecker(pid) {
			_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "unknown"
				meta.FailureReason = "runner_exit_nonzero"
				meta.FinishedAt = now
				meta.PID = nil
				meta.Flags.NeedsAttention = true
				meta.Flags.Orphaned = true
				meta.LifecycleOwner = ""
			})
			continue
		}
		if r.Meta.DaemonInstanceID != s.InstanceID {
			_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
				meta.Flags.NeedsAttention = true
				meta.Flags.Orphaned = true
				meta.OrphanedAt = now
			})
		}
	}
	return nil
}

func (s *Server) recoverHeadedInvocation(ctx context.Context, repoID string, r store.InvocationRecord, now string, nowTime time.Time) {
	if r.Meta.Status != store.InvocationStatusStarting && r.Meta.Status != store.InvocationStatusRunning {
		return
	}

	sessionName := r.Meta.TmuxSession
	if sessionName == "" {
		sessionName = tmux.SessionName(r.InvocationID)
	}
	exists, err := s.TmuxClient.HasSession(ctx, sessionName)
	if err != nil && !tmux.IsNoSessionErr(err) {
		fmt.Fprintf(os.Stderr, "warning: recovery: could not check tmux session %s for invocation %s: %v\n", sessionName, r.InvocationID, err)
		return
	}
	if exists {
		return
	}

	switch r.Meta.Status {
	case store.InvocationStatusStarting:
		startedAt, parseErr := time.Parse(time.RFC3339, r.Meta.StartedAt)
		if parseErr != nil {
			_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "start_failed"
				meta.FailureReason = "tmux_session_missing"
				meta.FinishedAt = now
				meta.LifecycleOwner = ""
			})
			return
		}
		if nowTime.Sub(startedAt) > 30*time.Second {
			_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "start_failed"
				meta.FailureReason = "tmux_session_missing"
				meta.FinishedAt = now
				meta.LifecycleOwner = ""
			})
		}
	case store.InvocationStatusRunning:
		_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
			meta.Status = store.InvocationStatusFinished
			meta.ExitReason = "exited"
			meta.FinishedAt = now
			meta.LifecycleOwner = ""
		})
	}
}

func WritePidFile(pidPath string) error {
	dir := filepath.Dir(pidPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func ReadPidFile(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data[:len(data)-1]))
}

func RemovePidFile(pidPath string) error {
	return os.Remove(pidPath)
}

func RemoveSocketFile(socketPath string) error {
	return os.Remove(socketPath)
}

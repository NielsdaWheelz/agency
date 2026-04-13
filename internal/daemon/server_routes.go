package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/shutdown", s.handleShutdown)
	mux.HandleFunc("/invocations/", s.handleInvocations)
	mux.HandleFunc("/worktrees/", s.handleWorktrees)
	mux.HandleFunc("/worktrees", s.handleWorktrees)
	mux.HandleFunc("/repos/", s.handleRepos)
	mux.HandleFunc("/repos", s.handleRepos)
	mux.HandleFunc("/spec/v2.1/s1/release/", s.handleS1Release)
}

func (s *Server) newHTTPHandler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return s.withRequestID(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
		return
	}

	uptime := int64(s.Clock().Sub(s.startedAt).Seconds())
	resp := HealthResponse{
		OK:               true,
		APIVersion:       APIVersion,
		BuildVersion:     version.FullVersion(),
		GitSHA:           version.Commit,
		PID:              os.Getpid(),
		DaemonInstanceID: s.InstanceID,
		UptimeSeconds:    uptime,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
		return
	}

	force := r.URL.Query().Get("force") == "true"

	s.mu.RLock()
	runningIDs := make([]string, 0)
	for id := range s.processes {
		runningIDs = append(runningIDs, id)
	}
	s.mu.RUnlock()

	if len(runningIDs) > 0 && !force {
		resp := ShutdownResponse{
			OK:                 false,
			ErrorCode:          string(errors.EDaemonBusy),
			Message:            fmt.Sprintf("%d active headless invocations; use --force to override", len(runningIDs)),
			Hint:               "use 'agency daemon stop --force' to terminate all invocations and stop the daemon",
			RunningInvocations: runningIDs,
		}
		s.writeJSON(w, http.StatusConflict, resp)
		return
	}

	if force && len(runningIDs) > 0 {
		s.terminateAllInvocations()
	}

	s.writeJSON(w, http.StatusOK, ShutdownResponse{OK: true})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()
}

func (s *Server) terminateAllInvocations() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, proc := range s.processes {
		proc.exitReason.Store("killed")
		proc.failureReason.Store("killed")

		if proc.Mode == "headed" {
			if proc.TmuxSession != "" {
				_ = s.TmuxClient.KillSession(ctx, proc.TmuxSession)
			}
			proc.CloseDone()

			now := s.Clock().UTC().Format(time.RFC3339)
			_ = s.Store.UpdateInvocationMeta(proc.RepoID, id, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "killed"
				meta.FailureReason = "killed"
				meta.FinishedAt = now
				meta.LifecycleOwner = ""
			})

			delete(s.processes, id)
			continue
		}

		if proc.PGID <= 0 {
			delete(s.processes, id)
			continue
		}

		_ = syscall.Kill(-proc.PGID, syscall.SIGINT)

		select {
		case <-proc.done:
			continue
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
		}

		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(proc.RepoID, id, func(meta *store.InvocationMeta) {
			meta.Status = store.InvocationStatusFailed
			meta.ExitReason = "killed"
			meta.FailureReason = "killed"
			meta.FinishedAt = now
			meta.PID = nil
			meta.LifecycleOwner = ""
		})

		delete(s.processes, id)
	}
}

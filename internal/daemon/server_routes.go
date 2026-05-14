package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/version"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/shutdown", s.handleShutdown)
	mux.HandleFunc("/invocations", s.handleInvocations)
	mux.HandleFunc("/invocations/", s.handleInvocations)
	mux.HandleFunc("/tasks/", s.handleTasks)
	mux.HandleFunc("/tasks", s.handleTasks)
	mux.HandleFunc("/worktrees/", s.handleWorktrees)
	mux.HandleFunc("/worktrees", s.handleWorktrees)
	mux.HandleFunc("/repos/", s.handleRepos)
	mux.HandleFunc("/repos", s.handleRepos)
}

func (s *Server) newHTTPHandler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return s.withRequestID(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.requireMethod(w, r, http.MethodGet) {
		return
	}

	uptime := int64(s.Clock().Sub(s.startedAt).Seconds())
	resp := HealthResponse{
		OK:               true,
		APIVersion:       APIVersion,
		BuildVersion:     daemonBuildVersion(),
		GitSHA:           version.Commit,
		PID:              os.Getpid(),
		DaemonInstanceID: s.InstanceID,
		UptimeSeconds:    uptime,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !s.requireMethod(w, r, http.MethodPost) {
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

	type runningInvocation struct {
		id   string
		proc *SupervisedProcess
	}
	s.mu.RLock()
	running := make([]runningInvocation, 0, len(s.processes))
	for id, proc := range s.processes {
		running = append(running, runningInvocation{id: id, proc: proc})
	}
	s.mu.RUnlock()

	for _, entry := range running {
		id := entry.id
		proc := entry.proc
		proc.exitReason.Store("killed")
		proc.failureReason.Store("killed")

		if proc.Mode == "headed" {
			if proc.TmuxSession != "" {
				_ = s.TmuxClient.KillSession(ctx, proc.TmuxSession)
			}
			s.failInvocationKilled(proc.RepoID, id)
			s.clearInvocationProcess(id)
			continue
		}

		if proc.PGID <= 0 {
			s.clearInvocationProcess(id)
			continue
		}

		_ = syscall.Kill(-proc.PGID, syscall.SIGINT)

		select {
		case <-proc.done:
			s.clearInvocationProcess(id)
			continue
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
		}

		s.failInvocationKilled(proc.RepoID, id)
		s.clearInvocationProcess(id)
	}
}

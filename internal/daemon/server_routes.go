package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
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

	uptime := int64(s.clock().Sub(s.startedAt).Seconds())
	resp := HealthResponse{
		OK:               true,
		APIVersion:       APIVersion,
		BuildVersion:     daemonBuildVersion(),
		GitSHA:           version.Commit,
		PID:              os.Getpid(),
		DaemonInstanceID: s.instanceID,
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

	// Active invocations (allowed past the busy check only when force is set)
	// are terminated and fully drained by Shutdown below: it kills the runner
	// process groups and joins their supervision goroutines.
	s.writeJSON(w, http.StatusOK, ShutdownResponse{OK: true})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()
}

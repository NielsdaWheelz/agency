package daemon

import (
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

type taskAmbiguousError struct {
	err        error
	candidates []string
}

func (e *taskAmbiguousError) Error() string { return e.err.Error() }
func (e *taskAmbiguousError) Unwrap() error { return e.err }

func taskDTOFromMeta(s *Server, meta *store.TaskMeta) TaskDTO {
	return TaskDTO{
		TaskID:              meta.TaskID,
		Name:                meta.Name,
		State:               meta.State,
		RepoID:              meta.RepoID,
		RepoName:            s.repoName(meta.RepoID),
		BaseBranch:          meta.BaseBranch,
		CheckoutRoot:        meta.CheckoutRoot,
		ExecutionProfile:    meta.ExecutionProfile,
		WorktreeID:          meta.WorktreeID,
		WorktreeName:        meta.WorktreeName,
		WorktreePath:        meta.WorktreePath,
		Branch:              meta.Branch,
		PrimaryInvocationID: meta.PrimaryInvocationID,
		Mode:                meta.Mode,
		Runner:              meta.Runner,
		ClientRequestID:     meta.ClientRequestID,
		CreatedAt:           meta.CreatedAt,
		UpdatedAt:           meta.UpdatedAt,
		FailedPhase:         meta.FailedPhase,
		ErrorCode:           meta.ErrorCode,
		Error:               meta.Error,
	}
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)
	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument), "repo_id query parameter is required", "pass ?repo_id=<repo_id>", nil)
		return
	}

	records, err := s.store.ScanTasksForRepo(repoID)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to scan tasks: "+err.Error(), "", nil)
		return
	}
	tasks := make([]TaskDTO, 0, len(records))
	for _, record := range records {
		if record.Broken || record.Meta == nil {
			tasks = append(tasks, TaskDTO{
				TaskID: record.TaskID,
				RepoID: repoID,
				State:  store.TaskStateFailed,
				Error:  "task meta.json is unreadable",
			})
			continue
		}
		if record.Meta.State == store.TaskStateArchived && r.URL.Query().Get("all") != "true" {
			continue
		}
		tasks = append(tasks, taskDTOFromMeta(s, record.Meta))
	}
	s.writeAPIResponse(w, requestID, ListTasksData{Tasks: tasks})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request, taskRef string) {
	requestID := getOrCreateRequestID(r)
	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument), "repo_id query parameter is required", "pass ?repo_id=<repo_id>", nil)
		return
	}
	record, err := s.resolveTaskRecord(repoID, taskRef)
	if err != nil {
		s.writeTaskResolveError(w, requestID, err)
		return
	}
	if record.Broken || record.Meta == nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.ETaskBroken), "task exists but meta.json is unreadable", "inspect or recreate the task", nil)
		return
	}
	s.writeAPIResponse(w, requestID, taskDTOFromMeta(s, record.Meta))
}

func (s *Server) resolveTaskRecord(repoID, taskRef string) (*store.TaskRecord, error) {
	records, err := s.store.ScanTasksForRepo(repoID)
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to scan tasks", err)
	}
	ref := strings.TrimSpace(taskRef)
	if ref == "" {
		return nil, errors.New(errors.ETaskNotFound, "task not found")
	}
	var matches []*store.TaskRecord
	for i := range records {
		record := &records[i]
		if record.TaskID == ref || strings.HasPrefix(record.TaskID, ref) || record.Name == ref {
			matches = append(matches, record)
		}
	}
	switch len(matches) {
	case 0:
		return nil, errors.NewWithDetails(errors.ETaskNotFound, "task not found: "+ref, map[string]string{"input": ref})
	case 1:
		return matches[0], nil
	default:
		candidates := make([]string, 0, len(matches))
		for _, match := range matches {
			candidates = append(candidates, match.TaskID)
		}
		return nil, &taskAmbiguousError{
			err:        errors.NewWithDetails(errors.ETaskIDAmbiguous, "ambiguous task identifier '"+ref+"' matches multiple tasks: "+strings.Join(candidates, ", "), map[string]string{"input": ref}),
			candidates: candidates,
		}
	}
}

func (s *Server) writeTaskResolveError(w http.ResponseWriter, requestID string, err error) {
	status, code, hint := taskResolveErrorPresentation(err)
	if code == errors.ETaskIDAmbiguous {
		var ambiguous *taskAmbiguousError
		var details interface{}
		if stderrors.As(err, &ambiguous) {
			details = AmbiguousDetails{Candidates: ambiguous.candidates}
		}
		s.writeAPIError(w, status, requestID, string(code), err.Error(), hint, details)
		return
	}
	s.writeAPIError(w, status, requestID, string(code), err.Error(), hint, nil)
}

func (s *Server) writeTaskStartResolveError(w http.ResponseWriter, requestID string, err error, clientRequestID string) {
	status, code, hint := taskResolveErrorPresentation(err)
	s.writeTaskStartError(w, status, requestID, code, err.Error(), hint, clientRequestID, nil)
}

func taskResolveErrorPresentation(err error) (int, errors.Code, string) {
	code := errors.GetCode(err)
	if code == "" {
		return http.StatusInternalServerError, errors.EInternal, ""
	}
	switch code {
	case errors.ETaskIDAmbiguous:
		return http.StatusConflict, code, "use a more specific task id"
	case errors.ETaskNotFound:
		return http.StatusNotFound, code, "use 'agency task ls' to list tasks"
	default:
		return http.StatusInternalServerError, code, ""
	}
}

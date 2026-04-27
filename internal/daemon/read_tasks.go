package daemon

import (
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func taskDTOFromMeta(s *Server, meta *store.TaskMeta) TaskDTO {
	return TaskDTO{
		TaskID:              meta.TaskID,
		Name:                meta.Name,
		State:               meta.State,
		RepoID:              meta.RepoID,
		RepoName:            s.repoName(meta.RepoID),
		BaseBranch:          meta.BaseBranch,
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

	records, err := store.ScanTasksForRepo(s.Store.DataDir, repoID)
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
	record, err := s.resolveTaskRecord(repoID, taskRef, true)
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

func (s *Server) resolveTaskRecord(repoID, taskRef string, includeArchived bool) (*store.TaskRecord, error) {
	records, err := store.ScanTasksForRepo(s.Store.DataDir, repoID)
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
		if !includeArchived && record.Meta != nil && record.Meta.State == store.TaskStateArchived {
			continue
		}
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
		return nil, errors.NewWithDetails(errors.ETaskIDAmbiguous, "ambiguous task identifier '"+ref+"' matches multiple tasks: "+strings.Join(candidates, ", "), map[string]string{"input": ref})
	}
}

func (s *Server) writeTaskResolveError(w http.ResponseWriter, requestID string, err error) {
	code := errors.GetCode(err)
	if code == errors.ETaskIDAmbiguous {
		s.writeAPIError(w, http.StatusConflict, requestID, string(code), err.Error(), "use a more specific task id", nil)
		return
	}
	if code == "" {
		code = errors.EInternal
	}
	s.writeAPIError(w, http.StatusNotFound, requestID, string(code), err.Error(), "use 'agency task ls' to list tasks", nil)
}

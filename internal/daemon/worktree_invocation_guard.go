package daemon

import (
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) unresolvedInvocationsForWorktree(repoID, worktreeID string) ([]store.InvocationRecord, error) {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to scan invocations", err)
	}

	var unresolved []store.InvocationRecord
	for _, record := range records {
		if record.Broken || record.Meta == nil {
			return nil, errors.NewWithDetails(
				errors.EStoreCorrupt,
				"invocation record is broken",
				map[string]string{
					"invocation_id": record.InvocationID,
					"repo_id":       repoID,
					"hint":          "repair or delete the broken invocation record before merging or removing the worktree",
				},
			)
		}

		switch record.Meta.LandingStatus {
		case store.LandingStatusLanded, store.LandingStatusDiscarded:
			continue
		case "", store.LandingStatusPending:
			if record.Meta.IntegrationWorktreeID == worktreeID {
				unresolved = append(unresolved, record)
			}
		default:
			return nil, errors.NewWithDetails(
				errors.EStoreCorrupt,
				"invocation landing status is invalid",
				map[string]string{
					"invocation_id":  record.InvocationID,
					"repo_id":        repoID,
					"landing_status": string(record.Meta.LandingStatus),
					"hint":           "repair or delete the broken invocation record before merging or removing the worktree",
				},
			)
		}
	}

	return unresolved, nil
}

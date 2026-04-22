package daemon

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/store"
)

func parseListWorktreesParams(r *http.Request) (ListWorktreesParams, *InvalidQueryArgumentDetails) {
	params := ListWorktreesParams{
		State: "present",
		Limit: 100,
	}

	if repoID := r.URL.Query().Get("repo_id"); repoID != "" {
		params.RepoID = repoID
	}
	if state := r.URL.Query().Get("state"); state != "" {
		if !isValidWorktreeState(state) {
			return params, &InvalidQueryArgumentDetails{
				Param:         "state",
				Value:         state,
				AllowedValues: validWorktreeStates,
			}
		}
		params.State = state
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		l, err := strconv.Atoi(limit)
		if err != nil || l < 1 || l > 500 {
			return params, &InvalidQueryArgumentDetails{
				Param: "limit",
				Value: limit,
			}
		}
		params.Limit = l
	}
	params.Cursor = r.URL.Query().Get("cursor")

	return params, nil
}

func parseListInvocationsParams(r *http.Request) (ListInvocationsParams, *InvalidQueryArgumentDetails) {
	params := ListInvocationsParams{
		State: "all",
		Mode:  "all",
		Limit: 100,
	}

	if repoID := r.URL.Query().Get("repo_id"); repoID != "" {
		params.RepoID = repoID
	}
	if worktreeRef := r.URL.Query().Get("worktree_ref"); worktreeRef != "" {
		params.WorktreeRef = worktreeRef
	}
	if state := r.URL.Query().Get("state"); state != "" {
		if !isValidInvocationState(state) {
			return params, &InvalidQueryArgumentDetails{
				Param:         "state",
				Value:         state,
				AllowedValues: validInvocationStates,
			}
		}
		params.State = state
	}
	if mode := r.URL.Query().Get("mode"); mode != "" {
		if !isValidInvocationMode(mode) {
			return params, &InvalidQueryArgumentDetails{
				Param:         "mode",
				Value:         mode,
				AllowedValues: validInvocationModes,
			}
		}
		params.Mode = mode
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		l, err := strconv.Atoi(limit)
		if err != nil || l < 1 || l > 500 {
			return params, &InvalidQueryArgumentDetails{
				Param: "limit",
				Value: limit,
			}
		}
		params.Limit = l
	}
	params.Cursor = r.URL.Query().Get("cursor")

	return params, nil
}

func parseGetDiffParams(r *http.Request) (GetDiffParams, *InvalidQueryArgumentDetails) {
	params := GetDiffParams{
		IncludePatch:       true,
		MaxPatchBytes:      2097152,
		IncludeUncommitted: true,
	}

	if includePatch := r.URL.Query().Get("include_patch"); includePatch == "0" || includePatch == "false" {
		params.IncludePatch = false
	}
	if maxPatch := r.URL.Query().Get("max_patch_bytes"); maxPatch != "" {
		m, err := strconv.Atoi(maxPatch)
		if err != nil || m < 1 || m > 5242880 {
			return params, &InvalidQueryArgumentDetails{
				Param: "max_patch_bytes",
				Value: maxPatch,
			}
		}
		params.MaxPatchBytes = m
	}
	if includeUncommitted := r.URL.Query().Get("include_uncommitted"); includeUncommitted == "0" || includeUncommitted == "false" {
		params.IncludeUncommitted = false
	}
	params.TurnID = strings.TrimSpace(r.URL.Query().Get("turn"))
	params.TurnStartID = strings.TrimSpace(r.URL.Query().Get("turn_start"))
	params.TurnEndID = strings.TrimSpace(r.URL.Query().Get("turn_end"))

	return params, nil
}

func parseGetLogsParams(r *http.Request) (GetLogsParams, *InvalidQueryArgumentDetails) {
	params := GetLogsParams{
		Kind:  "raw",
		Limit: 65536,
	}

	if kind := r.URL.Query().Get("kind"); kind != "" {
		if !isValidInvocationLogKind(kind) {
			return params, &InvalidQueryArgumentDetails{
				Param:         "kind",
				Value:         kind,
				AllowedValues: validInvocationLogKinds,
			}
		}
		params.Kind = kind
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		o, err := strconv.ParseInt(offsetStr, 10, 64)
		if err != nil || o < 0 {
			return params, &InvalidQueryArgumentDetails{
				Param: "offset",
				Value: offsetStr,
			}
		}
		params.Offset = o
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err != nil || l < 1 || l > MaxLogChunk {
			return params, &InvalidQueryArgumentDetails{
				Param: "limit",
				Value: limitStr,
			}
		}
		params.Limit = l
	}

	return params, nil
}

var (
	validWorktreeStates     = []string{"present", "archived", "all"}
	validInvocationStates   = []string{"unresolved", "finished", "all"}
	validInvocationModes    = []string{"headed", "headless", "all"}
	validInvocationLogKinds = []string{"raw", "stderr", "stream", "hooks", "terminal"}
)

func isValidInvocationLogKind(kind string) bool {
	for _, valid := range validInvocationLogKinds {
		if kind == valid {
			return true
		}
	}
	return false
}

func isValidWorktreeState(state string) bool {
	for _, valid := range validWorktreeStates {
		if state == valid {
			return true
		}
	}
	return false
}

func isValidInvocationState(state string) bool {
	for _, valid := range validInvocationStates {
		if state == valid {
			return true
		}
	}
	return false
}

func isValidInvocationMode(mode string) bool {
	for _, valid := range validInvocationModes {
		if mode == valid {
			return true
		}
	}
	return false
}

func matchesWorktreeState(state store.WorktreeState, filter string) bool {
	switch filter {
	case "all":
		return true
	case "archived":
		return state == store.WorktreeStateArchived
	case "present":
		return state == store.WorktreeStatePresent
	}
	return false
}

func matchesInvocationState(status store.InvocationStatus, landing store.LandingStatus, filter string) bool {
	switch filter {
	case "all":
		return true
	case "unresolved":
		switch status {
		case store.InvocationStatusStarting, store.InvocationStatusRunning, store.InvocationStatusStopping:
			return true
		case store.InvocationStatusFinished, store.InvocationStatusFailed:
			return landing != store.LandingStatusLanded && landing != store.LandingStatusDiscarded
		}
		return false
	case "finished":
		switch status {
		case store.InvocationStatusFinished, store.InvocationStatusFailed:
			return true
		}
		return false
	}
	return false
}

func matchesInvocationMode(mode store.RunnerMode, filter string) bool {
	switch filter {
	case "all":
		return true
	case "headed":
		return mode == store.RunnerModeHeaded
	case "headless":
		return mode == store.RunnerModeHeadless
	}
	return false
}

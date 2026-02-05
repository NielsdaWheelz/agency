// Package daemon implements the agency daemon supervisor.
// This file implements read API handlers (PR-12).
package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// ----- Request ID Middleware -----

// getOrCreateRequestID extracts or generates a request ID.
func getOrCreateRequestID(r *http.Request) string {
	if reqID := r.Header.Get("X-Request-ID"); reqID != "" {
		return reqID
	}
	return uuid.New().String()
}

// ----- Response Helpers -----

// writeAPIResponse writes a successful API response with the envelope.
func (s *Server) writeAPIResponse(w http.ResponseWriter, requestID string, data interface{}) {
	resp := APIResponse{
		OK:           true,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		GitSHA:       version.Commit,
		RequestID:    requestID,
		Data:         data,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// writeAPIError writes an error API response with the envelope.
func (s *Server) writeAPIError(w http.ResponseWriter, status int, requestID, code, message, hint string, details interface{}) {
	resp := APIResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		GitSHA:       version.Commit,
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
		Details:      details,
	}
	s.writeJSON(w, status, resp)
}

// ----- Worktree Read Handlers -----

// handleListWorktrees handles GET /worktrees.
func (s *Server) handleListWorktrees(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	// Parse query params
	params := parseListWorktreesParams(r)

	// Get all repos to scan
	repoIDs, err := s.getRepoIDsForQuery(params.RepoID)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return
	}

	// Scan all repos for worktrees
	var allWorktrees []WorktreeDTO
	for _, repoID := range repoIDs {
		records, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, repoID)
		if err != nil {
			continue // Skip repos with errors
		}

		for _, r := range records {
			if r.Broken || r.Meta == nil {
				continue
			}

			// Apply state filter
			if !matchesWorktreeState(r.Meta.State, params.State) {
				continue
			}

			dto := WorktreeMetaToDTO(r.Meta)
			allWorktrees = append(allWorktrees, dto)
		}
	}

	// Sort by last_used_at desc, worktree_id asc
	sort.Slice(allWorktrees, func(i, j int) bool {
		if allWorktrees[i].LastUsedAt != allWorktrees[j].LastUsedAt {
			return allWorktrees[i].LastUsedAt > allWorktrees[j].LastUsedAt
		}
		return allWorktrees[i].WorktreeID < allWorktrees[j].WorktreeID
	})

	// Apply pagination
	worktrees, nextCursor := paginateWorktrees(allWorktrees, params.Cursor, params.Limit)

	s.writeAPIResponse(w, requestID, ListWorktreesData{
		Worktrees:  worktrees,
		NextCursor: nextCursor,
	})
}

// handleGetWorktree handles GET /worktrees/{ref}.
func (s *Server) handleGetWorktree(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	// Get repo_id from query params (optional)
	repoID := r.URL.Query().Get("repo_id")

	// Resolve worktree ref
	record, resolveErr := s.resolveWorktreeRef(worktreeRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		status := http.StatusNotFound
		var details interface{}

		if code == errors.EWorktreeIDAmbiguous {
			status = http.StatusConflict
			// Extract candidates from error
			if ae, ok := errors.AsAgencyError(resolveErr); ok && ae.Details != nil {
				if candidates, ok := ae.Details["candidates"]; ok {
					details = AmbiguousDetails{Candidates: strings.Split(candidates, ",")}
				}
			}
		}

		hint := "use 'agency worktree ls' to list available worktrees"
		s.writeAPIError(w, status, requestID, string(code), resolveErr.Error(), hint, details)
		return
	}

	dto := WorktreeMetaToDTO(record.Meta)
	s.writeAPIResponse(w, requestID, dto)
}

// ----- Invocation Read Handlers -----

// handleListInvocations handles GET /invocations.
func (s *Server) handleListInvocations(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	// Parse query params
	params := parseListInvocationsParams(r)

	// Get all repos to scan
	repoIDs, err := s.getRepoIDsForQuery(params.RepoID)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return
	}

	// Resolve worktree filter if provided
	var worktreeIDFilter string
	if params.WorktreeRef != "" {
		// Try to resolve worktree ref to ID
		for _, repoID := range repoIDs {
			record, err := s.resolveWorktreeRefForRepo(params.WorktreeRef, repoID)
			if err == nil && record != nil {
				worktreeIDFilter = record.WorktreeID
				break
			}
		}
	} else if params.WorktreeID != "" {
		worktreeIDFilter = params.WorktreeID
	}

	now := s.Clock()

	// Scan all repos for invocations
	var allInvocations []InvocationDTO
	for _, repoID := range repoIDs {
		records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
		if err != nil {
			continue // Skip repos with errors
		}

		for _, r := range records {
			if r.Broken || r.Meta == nil {
				continue
			}

			// Apply worktree filter
			if worktreeIDFilter != "" && r.Meta.IntegrationWorktreeID != worktreeIDFilter {
				continue
			}

			// Apply state filter
			if !matchesInvocationState(r.Meta.Status, r.Meta.LandingStatus, params.State) {
				continue
			}

			// Apply mode filter
			if !matchesInvocationMode(r.Meta.Mode, params.Mode) {
				continue
			}

			logsDir := s.Store.SandboxLogsDir(repoID, r.InvocationID)
			dto := InvocationMetaToDTO(r.Meta, repoID, logsDir, now)
			allInvocations = append(allInvocations, dto)
		}
	}

	// Sort by started_at desc, invocation_id asc
	sort.Slice(allInvocations, func(i, j int) bool {
		if allInvocations[i].StartedAt != allInvocations[j].StartedAt {
			return allInvocations[i].StartedAt > allInvocations[j].StartedAt
		}
		return allInvocations[i].InvocationID < allInvocations[j].InvocationID
	})

	// Apply pagination
	invocations, nextCursor := paginateInvocations(allInvocations, params.Cursor, params.Limit)

	s.writeAPIResponse(w, requestID, ListInvocationsData{
		Invocations: invocations,
		NextCursor:  nextCursor,
	})
}

// handleGetInvocation handles GET /invocations/{ref}.
func (s *Server) handleGetInvocation(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	// Get repo_id from query params (optional)
	repoID := r.URL.Query().Get("repo_id")

	// Resolve invocation ref
	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		status := http.StatusNotFound
		var details interface{}

		if code == errors.EInvocationIDAmbiguous {
			status = http.StatusConflict
			if ae, ok := errors.AsAgencyError(resolveErr); ok && ae.Details != nil {
				if candidates, ok := ae.Details["candidates"]; ok {
					details = AmbiguousDetails{Candidates: strings.Split(candidates, ",")}
				}
			}
		}

		hint := "use 'agent ls' to list invocations"
		s.writeAPIError(w, status, requestID, string(code), resolveErr.Error(), hint, details)
		return
	}

	now := s.Clock()
	logsDir := s.Store.SandboxLogsDir(record.RepoID, record.InvocationID)
	dto := InvocationMetaToDTO(record.Meta, record.RepoID, logsDir, now)
	s.writeAPIResponse(w, requestID, dto)
}

// handleGetInvocationDiff handles GET /invocations/{ref}/diff.
func (s *Server) handleGetInvocationDiff(w http.ResponseWriter, r *http.Request, invocationRef string) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	// Get repo_id from query params (optional)
	repoID := r.URL.Query().Get("repo_id")

	// Parse diff params
	params := parseGetDiffParams(r)

	// Resolve invocation ref
	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		s.writeAPIError(w, http.StatusNotFound, requestID, string(code), resolveErr.Error(), "use 'agent ls' to list invocations", nil)
		return
	}

	// Build diff data
	diffData, err := s.buildInvocationDiff(ctx, record, params)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return
	}

	s.writeAPIResponse(w, requestID, diffData)
}

// handleGetInvocationLogs handles GET /invocations/{ref}/logs.
func (s *Server) handleGetInvocationLogs(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	// Get repo_id from query params (optional)
	repoID := r.URL.Query().Get("repo_id")

	// Parse logs params
	params := parseGetLogsParams(r)

	// Resolve invocation ref
	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		s.writeAPIError(w, http.StatusNotFound, requestID, string(code), resolveErr.Error(), "use 'agent ls' to list invocations", nil)
		return
	}

	// Determine log file path
	var logPath string
	switch params.Kind {
	case "stderr":
		logPath = s.Store.SandboxStderrLogPath(record.RepoID, record.InvocationID)
	case "stream":
		logPath = s.Store.SandboxStreamLogPath(record.RepoID, record.InvocationID)
	default:
		logPath = s.Store.SandboxRawLogPath(record.RepoID, record.InvocationID)
		params.Kind = "raw"
	}

	// Read log file
	logsData, err := s.readLogFile(logPath, params)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty content for non-existent log files
			s.writeAPIResponse(w, requestID, InvocationLogsData{
				Kind:          params.Kind,
				Content:       "",
				Truncated:     false,
				TotalBytes:    0,
				ReturnedBytes: 0,
				StartsMidline: false,
				EndsMidline:   false,
			})
			return
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return
	}

	logsData.Kind = params.Kind
	s.writeAPIResponse(w, requestID, logsData)
}

// handleGetInvocationCheckpoints handles GET /invocations/{ref}/checkpoints.
func (s *Server) handleGetInvocationCheckpoints(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	// Get repo_id from query params (optional)
	repoID := r.URL.Query().Get("repo_id")

	// Parse pagination params
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	cursor := r.URL.Query().Get("cursor")

	// Resolve invocation ref
	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		s.writeAPIError(w, http.StatusNotFound, requestID, string(code), resolveErr.Error(), "use 'agent ls' to list invocations", nil)
		return
	}

	// Load checkpoints.json
	checkpointsDir := s.Store.SandboxDir(record.RepoID, record.InvocationID)
	cpFile, err := checkpoint.LoadCheckpointsFile(s.FS, checkpointsDir)
	if err != nil {
		// Return empty list if no checkpoints
		s.writeAPIResponse(w, requestID, ListCheckpointsData{
			Checkpoints: []CheckpointDTO{},
			NextCursor:  "",
		})
		return
	}

	// Convert to DTOs (reverse order - latest first)
	var allCheckpoints []CheckpointDTO
	for i := len(cpFile.Checkpoints) - 1; i >= 0; i-- {
		cp := cpFile.Checkpoints[i]
		allCheckpoints = append(allCheckpoints, CheckpointDTO{
			ID:                cp.ID,
			CreatedAt:         cp.CreatedAt,
			Diffstat:          cp.Diffstat,
			SnapshotCommit:    cp.SnapshotCommit,
			IncludesUntracked: cp.IncludesUntracked,
			Degraded:          !cp.IncludesUntracked,
		})
	}

	// Apply pagination
	checkpoints, nextCursor := paginateCheckpoints(allCheckpoints, cursor, limit)

	s.writeAPIResponse(w, requestID, ListCheckpointsData{
		Checkpoints: checkpoints,
		NextCursor:  nextCursor,
	})
}

// ----- Resolution Helpers -----

// resolveWorktreeRef resolves a worktree reference across all repos.
func (s *Server) resolveWorktreeRef(ref string, repoID string) (*store.IntegrationWorktreeRecord, error) {
	repoIDs, err := s.getRepoIDsForQuery(repoID)
	if err != nil {
		return nil, err
	}

	for _, rid := range repoIDs {
		record, err := s.resolveWorktreeRefForRepo(ref, rid)
		if err == nil && record != nil {
			return record, nil
		}
	}

	return nil, errors.New(errors.EWorktreeNotFound, "worktree not found: "+ref)
}

// resolveWorktreeRefForRepo resolves a worktree reference within a specific repo.
func (s *Server) resolveWorktreeRefForRepo(ref string, repoID string) (*store.IntegrationWorktreeRecord, error) {
	records, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return nil, err
	}

	// Build refs for resolver
	var refs []ids.WorktreeRef
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		refs = append(refs, ids.WorktreeRef{
			WorktreeID: r.WorktreeID,
			RepoID:     repoID,
			Name:       r.Meta.Name,
			State:      string(r.Meta.State),
			Broken:     r.Broken,
		})
	}

	// Resolve using standard resolver
	resolved, err := ids.ResolveWorktreeRef(ref, refs, ids.ResolveWorktreeRefOpts{
		IncludeArchived: true, // Always allow exact ID match
	})
	if err != nil {
		return nil, err
	}

	// Find the matching record
	for _, r := range records {
		if r.WorktreeID == resolved.WorktreeID {
			return &r, nil
		}
	}

	return nil, errors.New(errors.EWorktreeNotFound, "worktree not found: "+ref)
}

// resolveInvocationRef resolves an invocation reference across all repos.
func (s *Server) resolveInvocationRef(ref string, repoID string) (*resolvedInvocation, error) {
	repoIDs, err := s.getRepoIDsForQuery(repoID)
	if err != nil {
		return nil, err
	}

	for _, rid := range repoIDs {
		record, err := s.resolveInvocationRefForRepo(ref, rid)
		if err == nil && record != nil {
			return record, nil
		}
	}

	return nil, errors.New(errors.EInvocationNotFound, "invocation not found: "+ref)
}

// resolvedInvocation contains the resolved invocation with its repo ID.
type resolvedInvocation struct {
	InvocationID string
	RepoID       string
	Meta         *store.InvocationMeta
}

// resolveInvocationRefForRepo resolves an invocation reference within a specific repo.
func (s *Server) resolveInvocationRefForRepo(ref string, repoID string) (*resolvedInvocation, error) {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return nil, err
	}

	// Build refs for resolver
	var refs []ids.InvocationRef
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		refs = append(refs, ids.InvocationRef{
			InvocationID:          r.InvocationID,
			RepoID:                repoID,
			IntegrationWorktreeID: r.Meta.IntegrationWorktreeID,
			InvocationName:        r.Meta.InvocationName,
			Status:                string(r.Meta.Status),
			LandingStatus:         string(r.Meta.LandingStatus),
			Broken:                r.Broken,
		})
	}

	// Resolve using standard resolver
	resolved, err := ids.ResolveInvocationRef(ref, refs, ids.ResolveInvocationRefOpts{
		IncludeFinished: true, // Always allow exact ID match
	})
	if err != nil {
		return nil, err
	}

	// Find the matching record
	for _, r := range records {
		if r.InvocationID == resolved.InvocationID {
			return &resolvedInvocation{
				InvocationID: r.InvocationID,
				RepoID:       repoID,
				Meta:         r.Meta,
			}, nil
		}
	}

	return nil, errors.New(errors.EInvocationNotFound, "invocation not found: "+ref)
}

// getRepoIDsForQuery returns repo IDs to scan based on filter.
func (s *Server) getRepoIDsForQuery(filterRepoID string) ([]string, error) {
	if filterRepoID != "" {
		return []string{filterRepoID}, nil
	}

	// Load repo index
	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		return nil, err
	}

	var repoIDs []string
	for _, entry := range idx.Repos {
		repoIDs = append(repoIDs, entry.RepoID)
	}
	return repoIDs, nil
}

// ----- Diff Building -----

// buildInvocationDiff builds the diff data for an invocation.
func (s *Server) buildInvocationDiff(ctx context.Context, record *resolvedInvocation, params GetDiffParams) (*InvocationDiffData, error) {
	sandboxPath := record.Meta.SandboxPath
	baseCommit := record.Meta.BaseCommit

	// Get sandbox branch tip
	tipResult, err := s.Runner.Run(ctx, "git", []string{"-C", sandboxPath, "rev-parse", "HEAD"}, exec.RunOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox HEAD: %w", err)
	}
	sandboxTip := strings.TrimSpace(tipResult.Stdout)

	data := &InvocationDiffData{
		BaseCommit:       baseCommit,
		SandboxBranchTip: sandboxTip,
	}

	// Check if there are commits
	logResult, err := s.Runner.Run(ctx, "git", []string{"-C", sandboxPath, "log", "--oneline", baseCommit + ".." + sandboxTip}, exec.RunOpts{})
	if err == nil && strings.TrimSpace(logResult.Stdout) != "" {
		data.HasCommits = true

		// Build committed range
		committedRange := &DiffRange{
			From: baseCommit,
			To:   sandboxTip,
		}

		// Get commits
		lines := strings.Split(strings.TrimSpace(logResult.Stdout), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			commit := DiffCommit{SHA: parts[0]}
			if len(parts) > 1 {
				commit.Summary = parts[1]
			}
			committedRange.Commits = append(committedRange.Commits, commit)
		}

		// Get diffstat
		statResult, _ := s.Runner.Run(ctx, "git", []string{"-C", sandboxPath, "diff", "--stat", baseCommit + ".." + sandboxTip}, exec.RunOpts{})
		committedRange.Diffstat = extractDiffstat(statResult.Stdout)

		// Get patch if requested
		if params.IncludePatch {
			patchResult, _ := s.Runner.Run(ctx, "git", []string{"-C", sandboxPath, "diff", baseCommit + ".." + sandboxTip}, exec.RunOpts{})
			patch := patchResult.Stdout
			committedRange.PatchBytes = len(patch)
			if len(patch) > params.MaxPatchBytes {
				patch = patch[:params.MaxPatchBytes]
				committedRange.PatchTruncated = true
			}
			committedRange.Patch = patch
		}

		data.CommittedRange = committedRange
	}

	// Check for uncommitted changes
	if params.IncludeUncommitted {
		statusResult, err := s.Runner.Run(ctx, "git", []string{"-C", sandboxPath, "status", "--porcelain"}, exec.RunOpts{})
		if err == nil && strings.TrimSpace(statusResult.Stdout) != "" {
			data.HasUncommitted = true

			workingTree := &DiffRange{}

			// Get diffstat for working tree
			statResult, _ := s.Runner.Run(ctx, "git", []string{"-C", sandboxPath, "diff", "--stat"}, exec.RunOpts{})
			workingTree.Diffstat = extractDiffstat(statResult.Stdout)

			// Get patch if requested
			if params.IncludePatch {
				patchResult, _ := s.Runner.Run(ctx, "git", []string{"-C", sandboxPath, "diff"}, exec.RunOpts{})
				patch := patchResult.Stdout
				workingTree.PatchBytes = len(patch)
				if len(patch) > params.MaxPatchBytes {
					patch = patch[:params.MaxPatchBytes]
					workingTree.PatchTruncated = true
				}
				workingTree.Patch = patch
			}

			data.WorkingTree = workingTree
			data.PatchIncludesUncommitted = true
		}
	}

	return data, nil
}

// extractDiffstat extracts the summary line from git diff --stat output.
func extractDiffstat(statOutput string) string {
	lines := strings.Split(strings.TrimSpace(statOutput), "\n")
	if len(lines) == 0 {
		return ""
	}
	// Return the last line which contains the summary
	return strings.TrimSpace(lines[len(lines)-1])
}

// ----- Log Reading -----

// readLogFile reads a log file with tail/truncation support.
func (s *Server) readLogFile(logPath string, params GetLogsParams) (*InvocationLogsData, error) {
	// Get file info
	info, err := os.Stat(logPath)
	if err != nil {
		return nil, err
	}

	totalBytes := info.Size()
	tailBytes := int64(params.TailBytes)
	if tailBytes <= 0 {
		tailBytes = 65536 // 64KB default
	}
	if tailBytes > 1048576 { // 1MB max
		tailBytes = 1048576
	}

	// Open file
	f, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var content []byte
	var startsMidline, endsMidline, truncated bool

	if totalBytes <= tailBytes {
		// Read entire file
		content, err = io.ReadAll(f)
		if err != nil {
			return nil, err
		}
	} else {
		// Seek to tail position
		offset := totalBytes - tailBytes
		_, err = f.Seek(offset, io.SeekStart)
		if err != nil {
			return nil, err
		}
		content, err = io.ReadAll(f)
		if err != nil {
			return nil, err
		}
		truncated = true
		startsMidline = true // We're starting from a non-zero offset
	}

	// Check if ends midline (no trailing newline)
	if len(content) > 0 && content[len(content)-1] != '\n' {
		endsMidline = true
	}

	return &InvocationLogsData{
		Content:       string(content),
		Truncated:     truncated,
		TotalBytes:    totalBytes,
		ReturnedBytes: len(content),
		StartsMidline: startsMidline,
		EndsMidline:   endsMidline,
	}, nil
}

// ----- Parameter Parsing -----

func parseListWorktreesParams(r *http.Request) ListWorktreesParams {
	params := ListWorktreesParams{
		State: "present",
		Limit: 100,
	}

	if repoID := r.URL.Query().Get("repo_id"); repoID != "" {
		params.RepoID = repoID
	}
	if state := r.URL.Query().Get("state"); state != "" {
		params.State = state
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 500 {
			params.Limit = l
		}
	}
	params.Cursor = r.URL.Query().Get("cursor")

	return params
}

func parseListInvocationsParams(r *http.Request) ListInvocationsParams {
	params := ListInvocationsParams{
		State: "all",
		Mode:  "all",
		Limit: 100,
	}

	if repoID := r.URL.Query().Get("repo_id"); repoID != "" {
		params.RepoID = repoID
	}
	if worktreeID := r.URL.Query().Get("worktree_id"); worktreeID != "" {
		params.WorktreeID = worktreeID
	}
	if worktreeRef := r.URL.Query().Get("worktree_ref"); worktreeRef != "" {
		params.WorktreeRef = worktreeRef
	}
	if state := r.URL.Query().Get("state"); state != "" {
		params.State = state
	}
	if mode := r.URL.Query().Get("mode"); mode != "" {
		params.Mode = mode
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 500 {
			params.Limit = l
		}
	}
	params.Cursor = r.URL.Query().Get("cursor")

	return params
}

func parseGetDiffParams(r *http.Request) GetDiffParams {
	params := GetDiffParams{
		IncludePatch:       true,
		MaxPatchBytes:      2097152, // 2MB
		IncludeUncommitted: true,
	}

	if includePatch := r.URL.Query().Get("include_patch"); includePatch == "0" || includePatch == "false" {
		params.IncludePatch = false
	}
	if maxPatch := r.URL.Query().Get("max_patch_bytes"); maxPatch != "" {
		if m, err := strconv.Atoi(maxPatch); err == nil && m > 0 && m <= 5242880 {
			params.MaxPatchBytes = m
		}
	}
	if includeUncommitted := r.URL.Query().Get("include_uncommitted"); includeUncommitted == "0" || includeUncommitted == "false" {
		params.IncludeUncommitted = false
	}

	return params
}

func parseGetLogsParams(r *http.Request) GetLogsParams {
	params := GetLogsParams{
		Kind:      "raw",
		TailBytes: 65536, // 64KB
	}

	if kind := r.URL.Query().Get("kind"); kind != "" {
		params.Kind = kind
	}
	if tailBytes := r.URL.Query().Get("tail_bytes"); tailBytes != "" {
		if t, err := strconv.Atoi(tailBytes); err == nil && t > 0 && t <= 1048576 {
			params.TailBytes = t
		}
	}

	return params
}

// ----- Filter Helpers -----

func matchesWorktreeState(state store.WorktreeState, filter string) bool {
	switch filter {
	case "all":
		return true
	case "archived":
		return state == store.WorktreeStateArchived
	default: // "present" or empty
		return state == store.WorktreeStatePresent
	}
}

func matchesInvocationState(status store.InvocationStatus, landing store.LandingStatus, filter string) bool {
	switch filter {
	case "all":
		return true
	case "active":
		// Active means not in terminal state
		return status == store.InvocationStatusStarting ||
			status == store.InvocationStatusRunning ||
			(status == store.InvocationStatusFinished &&
				landing != store.LandingStatusLanded &&
				landing != store.LandingStatusDiscarded)
	case "finished":
		return status == store.InvocationStatusFinished || status == store.InvocationStatusFailed
	default:
		return true
	}
}

func matchesInvocationMode(mode store.RunnerMode, filter string) bool {
	switch filter {
	case "all", "":
		return true
	case "headed":
		return mode == store.RunnerModeHeaded
	case "headless":
		return mode == store.RunnerModeHeadless
	default:
		return true
	}
}

// ----- Pagination Helpers -----

func paginateWorktrees(all []WorktreeDTO, cursor string, limit int) ([]WorktreeDTO, string) {
	if len(all) == 0 {
		return []WorktreeDTO{}, ""
	}

	// Apply cursor: skip past the cursor item (exclusive boundary).
	// Sorted desc by LastUsedAt, asc by WorktreeID as tiebreaker.
	startIdx := 0
	if cursor != "" {
		var c WorktreeCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, w := range all {
				if w.LastUsedAt < c.LastUsedAt || (w.LastUsedAt == c.LastUsedAt && w.WorktreeID > c.WorktreeID) {
					startIdx = i
					break
				}
			}
		}
	}

	// Apply limit
	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	// Build next cursor
	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := WorktreeCursor{LastUsedAt: last.LastUsedAt, WorktreeID: last.WorktreeID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}

func paginateInvocations(all []InvocationDTO, cursor string, limit int) ([]InvocationDTO, string) {
	if len(all) == 0 {
		return []InvocationDTO{}, ""
	}

	// Apply cursor: skip past the cursor item (exclusive boundary).
	// Sorted desc by StartedAt, asc by InvocationID as tiebreaker.
	startIdx := 0
	if cursor != "" {
		var c InvocationCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, inv := range all {
				if inv.StartedAt < c.StartedAt || (inv.StartedAt == c.StartedAt && inv.InvocationID > c.InvocationID) {
					startIdx = i
					break
				}
			}
		}
	}

	// Apply limit
	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	// Build next cursor
	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := InvocationCursor{StartedAt: last.StartedAt, InvocationID: last.InvocationID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}

func paginateCheckpoints(all []CheckpointDTO, cursor string, limit int) ([]CheckpointDTO, string) {
	if len(all) == 0 {
		return []CheckpointDTO{}, ""
	}

	// Apply cursor: skip past the cursor item (exclusive boundary).
	// Sorted desc by ID.
	startIdx := 0
	if cursor != "" {
		var c CheckpointCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, cp := range all {
				if cp.ID < c.ID {
					startIdx = i
					break
				}
			}
		}
	}

	// Apply limit
	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	// Build next cursor
	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := CheckpointCursor{ID: last.ID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}

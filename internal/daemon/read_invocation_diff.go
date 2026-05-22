package daemon

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
)

// handleGetInvocationDiff handles GET /invocations/{ref}/diff.
func (s *Server) handleGetInvocationDiff(w http.ResponseWriter, r *http.Request, invocationRef string) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)

	params, invalid := parseGetDiffParams(r)
	if invalid != nil {
		s.writeAPIError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			fmt.Sprintf("invalid value for parameter '%s': %q", invalid.Param, invalid.Value),
			"",
			*invalid,
		)
		return
	}
	if err := validateGetDiffParams(params); err != nil {
		s.writeAPIError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			err.Error(),
			"use a timeline turn id from 'agency agent <invocation-ref> history' and pass either turn or turn_start/turn_end",
			nil,
		)
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, r.URL.Query().Get("repo_id"))
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}

	diffData, err := s.buildInvocationDiff(ctx, record, params)
	if err != nil {
		code := errors.GetCode(err)
		status := http.StatusInternalServerError
		hint := ""
		switch code {
		case errors.EInvalidArgument:
			status = http.StatusBadRequest
			hint = "use 'agency agent <invocation-ref> history' to list valid turn selectors"
		case errors.ECheckpointNotFound:
			status = http.StatusNotFound
			hint = "ensure checkpoints exist for the selected turn context"
		case errors.EInternal:
			code = errors.EInternal
		}
		s.writeAPIError(w, status, requestID, string(code), err.Error(), hint, nil)
		return
	}

	s.writeAPIResponse(w, requestID, diffData)
}

// buildInvocationDiff builds the diff data for an invocation.
func (s *Server) buildInvocationDiff(ctx context.Context, record *resolvedInvocation, params GetDiffParams) (*InvocationDiffData, error) {
	sandboxPath := record.Meta.SandboxPath
	baseCommit := record.Meta.BaseCommit
	profileEnv, err := s.executionProfileEnv(record.Meta.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	gitEnv := prSyncNonInteractiveEnv(profileEnv)

	tipResult, err := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "rev-parse", "HEAD"}, exec.RunOpts{Env: gitEnv})
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox HEAD: %w", err)
	}
	sandboxTip := strings.TrimSpace(tipResult.Stdout)

	diffFrom := baseCommit
	diffTo := sandboxTip
	var turnContext *DiffTurnContext
	if hasTurnSelector(params) {
		resolved, err := s.resolveTurnDiffContext(record, params)
		if err != nil {
			return nil, err
		}
		diffFrom = resolved.FromCommit
		diffTo = resolved.ToCommit
		turnContext = &resolved.TurnContext
	}

	data := &InvocationDiffData{
		BaseCommit:       baseCommit,
		SandboxBranchTip: sandboxTip,
		TurnContext:      turnContext,
	}

	logResult, err := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "log", "--oneline", diffFrom + ".." + diffTo}, exec.RunOpts{Env: gitEnv})
	if err == nil && strings.TrimSpace(logResult.Stdout) != "" {
		data.HasCommits = true

		committedRange := &DiffRange{From: diffFrom, To: diffTo}
		lines := strings.Split(strings.TrimSpace(logResult.Stdout), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			sha, summary, ok := strings.Cut(line, " ")
			commit := DiffCommit{SHA: sha}
			if ok {
				commit.Summary = summary
			}
			committedRange.Commits = append(committedRange.Commits, commit)
		}

		statResult, _ := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "diff", "--stat", diffFrom + ".." + diffTo}, exec.RunOpts{Env: gitEnv})
		committedRange.Diffstat = extractDiffstat(statResult.Stdout)

		if params.includePatch() {
			patchResult, _ := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "diff", diffFrom + ".." + diffTo}, exec.RunOpts{Env: gitEnv})
			patch := patchResult.Stdout
			committedRange.PatchBytes = len(patch)
			maxPatchBytes := params.maxPatchBytes()
			if len(patch) > maxPatchBytes {
				patch = patch[:maxPatchBytes]
				committedRange.PatchTruncated = true
			}
			committedRange.Patch = patch
		}

		data.CommittedRange = committedRange
	}

	if params.includeUncommitted() && !hasTurnSelector(params) {
		statusResult, err := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "status", "--porcelain"}, exec.RunOpts{Env: gitEnv})
		if err == nil && strings.TrimSpace(statusResult.Stdout) != "" {
			data.HasUncommitted = true

			workingTree := &DiffRange{}
			statResult, _ := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "diff", "--stat"}, exec.RunOpts{Env: gitEnv})
			workingTree.Diffstat = extractDiffstat(statResult.Stdout)

			if params.includePatch() {
				patchResult, _ := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "diff"}, exec.RunOpts{Env: gitEnv})
				patch := patchResult.Stdout
				workingTree.PatchBytes = len(patch)
				maxPatchBytes := params.maxPatchBytes()
				if len(patch) > maxPatchBytes {
					patch = patch[:maxPatchBytes]
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

func extractDiffstat(statOutput string) string {
	lines := strings.Split(strings.TrimSpace(statOutput), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// resolveTurnDiffContext and related helpers live in read_diff_turn.go.

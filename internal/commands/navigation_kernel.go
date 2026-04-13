package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

// TargetKind is the entity kind of a navigation target.
type TargetKind string

const (
	TargetWorktree   TargetKind = "worktree"
	TargetInvocation TargetKind = "invocation"
)

// NavigationSelection represents a CLI-selected target used by path/open/shell/enter flows.
type NavigationSelection struct {
	TargetKind TargetKind
	Ref        string
}

// NavigationIntent describes what a CLI navigation command wants to do.
type NavigationIntent struct {
	Selection   NavigationSelection
	RequiresTTY bool
}

// NavigationResult holds the resolved navigation target after daemon-first resolution.
type NavigationResult struct {
	TargetKind     TargetKind
	ResolvedRepoID string
	ResolvedID     string
	ResolvedPath   string
}

// NavigationDeps holds injected dependencies for the navigation resolution kernel.
// Tests inject fakes; real wiring happens in command setup.
type NavigationDeps struct {
	ResolveRepo   func(ctx context.Context) (*RepoContextResult, error)
	GetWorktree   func(ctx context.Context, ref, repoID string) (*NavigationResult, error)
	GetInvocation func(ctx context.Context, ref, repoID string) (*NavigationResult, error)
	IsInteractive func() bool
}

// ResolveNavigation executes daemon-first CLI target resolution.
func ResolveNavigation(ctx context.Context, intent NavigationIntent, deps NavigationDeps) (*NavigationResult, error) {
	if intent.RequiresTTY {
		if deps.IsInteractive == nil || !deps.IsInteractive() {
			return nil, errors.NewWithDetails(
				errors.ENotInteractive,
				"this command requires an interactive terminal",
				map[string]string{
					"hint": "run this command in an interactive terminal, or use a non-interactive alternative",
				},
			)
		}
	}

	repoCtx, err := deps.ResolveRepo(ctx)
	if err != nil {
		return nil, err
	}

	resolveRepoID := repoCtx.RepoID
	sel := intent.Selection

	switch sel.TargetKind {
	case TargetWorktree:
		result, err := deps.GetWorktree(ctx, sel.Ref, resolveRepoID)
		if err != nil {
			return nil, translateNavigationError(err, sel.TargetKind)
		}
		return result, nil
	case TargetInvocation:
		result, err := deps.GetInvocation(ctx, sel.Ref, resolveRepoID)
		if err != nil {
			return nil, translateNavigationError(err, sel.TargetKind)
		}
		return result, nil
	default:
		return nil, errors.New(errors.EInternal, fmt.Sprintf("unknown target kind: %q", sel.TargetKind))
	}
}

// translateNavigationError normalizes daemon target resolution errors for
// the navigation resolution contract. Entity-specific ambiguity codes are
// normalized to E_AMBIGUOUS with preserved candidate details.
func translateNavigationError(err error, targetKind TargetKind) error {
	dre, isDaemonErr := daemonclient.AsDaemonReadError(err)

	code := errors.GetCode(err)
	if code == errors.EWorktreeIDAmbiguous || code == errors.EInvocationIDAmbiguous {
		details := map[string]string{
			"target_kind": string(targetKind),
		}

		if isDaemonErr {
			candidates := dre.Candidates()
			if len(candidates) > 0 {
				candidatesJSON, _ := json.Marshal(candidates)
				details["candidates"] = string(candidatesJSON)
				details["candidate_count"] = strconv.Itoa(len(candidates))
			}
			if dre.Hint != "" {
				details["hint"] = dre.Hint
			}
		}

		ae, _ := errors.AsAgencyError(err)
		msg := "ambiguous target"
		if ae != nil {
			msg = ae.Msg
		}

		return errors.NewWithDetails(errors.EAmbiguous, msg, details)
	}

	if isDaemonErr && dre.Hint != "" {
		return errors.NewWithDetails(code, dre.AgencyErr.Msg, map[string]string{"hint": dre.Hint})
	}

	return err
}

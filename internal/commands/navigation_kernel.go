// Package commands implements agency CLI commands.
// This file implements the shared CLI navigation resolution kernel (S2 PR-02).
// The kernel is command-family agnostic: no cobra wiring, no editor/shell/tmux
// dispatch, no compatibility alias behavior. Downstream PRs (03/04/05) consume it.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

// ---------------------------------------------------------------------------
// Domain model types (L2 NavigationSelection, NavigationCommandIntent)
// ---------------------------------------------------------------------------

// SelectorSource identifies how a navigation target was specified.
type SelectorSource string

const (
	SelectorExplicitRef SelectorSource = "explicit_ref"
	SelectorMachineRef  SelectorSource = "machine_ref"
	SelectorListRow     SelectorSource = "list_row"
)

// TargetKind is the entity kind of a navigation target.
type TargetKind string

const (
	TargetWorktree   TargetKind = "worktree"
	TargetInvocation TargetKind = "invocation"
)

// NavigationSelection represents a CLI-selected target used by path/open/shell/enter flows.
type NavigationSelection struct {
	SelectorSource SelectorSource
	TargetKind     TargetKind
	Ref            string // non-empty CLI/user-provided selector
	RepoID         string // from list_row/machine_ref, or resolved via repo context
}

// NavigationIntent describes what a CLI navigation command wants to do.
type NavigationIntent struct {
	CommandFamily            string // "agent", "worktree", or "legacy" — metadata for downstream
	Verb                     string // "path", "open", "shell", "enter", etc.
	Selection                NavigationSelection
	Interactive              bool
	RequiresTTY              bool
	BootstrapFallbackAllowed bool
}

// NavigationResult holds the resolved navigation target after daemon-first resolution.
type NavigationResult struct {
	TargetKind       TargetKind
	ResolvedRepoID   string
	ResolvedID       string
	ResolvedPath     string
	ResolutionSource string // e.g., "daemon_get_worktree", "daemon_get_invocation", "bootstrap_fallback"
}

// ---------------------------------------------------------------------------
// Dependency injection (all fields required unless documented otherwise)
// ---------------------------------------------------------------------------

// NavigationDeps holds injected dependencies for the navigation resolution kernel.
// Tests inject fakes; real wiring happens in PR-03/PR-04 command migrations.
type NavigationDeps struct {
	// ResolveRepo resolves the repo context (wraps ResolveRepoViaClient or equivalent).
	ResolveRepo func(ctx context.Context) (*RepoContextResult, error)

	// EnsureDaemon ensures the daemon is running and connectable.
	EnsureDaemon func(ctx context.Context) error

	// CheckAPIVersion verifies CLI/daemon API version compatibility.
	CheckAPIVersion func(ctx context.Context) error

	// GetWorktree resolves a worktree target via daemon read API.
	// Errors may be *daemonclient.DaemonReadError for rich detail preservation.
	GetWorktree func(ctx context.Context, ref, repoID string) (*NavigationResult, error)

	// GetInvocation resolves an invocation target via daemon read API.
	// Errors may be *daemonclient.DaemonReadError for rich detail preservation.
	GetInvocation func(ctx context.Context, ref, repoID string) (*NavigationResult, error)

	// IsInteractive returns true if the session has a TTY.
	IsInteractive func() bool

	// FallbackCallback is invoked only when BootstrapFallbackAllowed is true in the
	// intent AND daemon ensure/version check fails. If nil, no fallback is attempted
	// even when the policy allows it.
	FallbackCallback func(ctx context.Context) (*NavigationResult, error)
}

// ---------------------------------------------------------------------------
// Selection normalization
// ---------------------------------------------------------------------------

// NormalizeSelection validates and normalizes a navigation selection input.
// machine_ref and list_row require an explicit repo_id.
func NormalizeSelection(source SelectorSource, targetKind TargetKind, ref string, repoID string) (*NavigationSelection, error) {
	if ref == "" {
		return nil, errors.New(errors.EInvalidArgument, "navigation ref must be non-empty")
	}

	switch source {
	case SelectorExplicitRef:
		return &NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     targetKind,
			Ref:            ref,
			RepoID:         repoID,
		}, nil

	case SelectorMachineRef:
		if repoID == "" {
			return nil, errors.New(errors.EInvalidArgument, "machine_ref selection requires explicit repo_id")
		}
		return &NavigationSelection{
			SelectorSource: SelectorMachineRef,
			TargetKind:     targetKind,
			Ref:            ref,
			RepoID:         repoID,
		}, nil

	case SelectorListRow:
		if repoID == "" {
			return nil, errors.New(errors.EInvalidArgument, "list_row selection requires repo_id from daemon list output")
		}
		return &NavigationSelection{
			SelectorSource: SelectorListRow,
			TargetKind:     targetKind,
			Ref:            ref,
			RepoID:         repoID,
		}, nil

	default:
		return nil, errors.New(errors.EInvalidArgument, fmt.Sprintf("unknown selector source: %q", source))
	}
}

// ---------------------------------------------------------------------------
// Main resolution lifecycle
// ---------------------------------------------------------------------------

// ResolveNavigation executes the S2 daemon-first CLI read-routing and target-resolution
// lifecycle. The ordering is enforced:
//
//  1. TTY preflight (fail fast for interactive commands)
//  2. Repo context resolution
//  3. Daemon ensure + API version check
//  4. Daemon-first target resolution (no local store discovery)
//
// Bootstrap fallback is the only exception path: it is invoked only when
// BootstrapFallbackAllowed is true, a FallbackCallback is provided, and the
// daemon ensure/version step fails.
func ResolveNavigation(ctx context.Context, intent NavigationIntent, deps NavigationDeps) (*NavigationResult, error) {
	// Phase 1: TTY preflight — fail before any I/O if interactive session is required
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

	// Phase 2: Repo context resolution
	repoCtx, err := deps.ResolveRepo(ctx)
	if err != nil {
		return nil, err
	}

	// Phase 3: Daemon ensure + API version check
	if err := deps.EnsureDaemon(ctx); err != nil {
		return handleDaemonFailure(ctx, err, intent, deps)
	}
	if err := deps.CheckAPIVersion(ctx); err != nil {
		return handleDaemonFailure(ctx, err, intent, deps)
	}

	// Phase 4: Daemon-first target resolution — no local store discovery
	sel := intent.Selection
	resolveRepoID := sel.RepoID
	if resolveRepoID == "" {
		resolveRepoID = repoCtx.RepoID
	}

	var result *NavigationResult
	switch sel.TargetKind {
	case TargetWorktree:
		result, err = deps.GetWorktree(ctx, sel.Ref, resolveRepoID)
	case TargetInvocation:
		result, err = deps.GetInvocation(ctx, sel.Ref, resolveRepoID)
	default:
		return nil, errors.New(errors.EInternal, fmt.Sprintf("unknown target kind: %q", sel.TargetKind))
	}
	if err != nil {
		return nil, translateNavigationError(err, sel.TargetKind)
	}

	return result, nil
}

// handleDaemonFailure is called when EnsureDaemon or CheckAPIVersion fails.
// If bootstrap fallback is allowed AND a callback is provided, invokes the callback.
// Otherwise returns the daemon failure directly — no silent local-store fallback.
func handleDaemonFailure(ctx context.Context, daemonErr error, intent NavigationIntent, deps NavigationDeps) (*NavigationResult, error) {
	if intent.BootstrapFallbackAllowed && deps.FallbackCallback != nil {
		return deps.FallbackCallback(ctx)
	}
	return nil, daemonErr
}

// ---------------------------------------------------------------------------
// Error translation (D-002: normalize ambiguity to E_AMBIGUOUS for navigation)
// ---------------------------------------------------------------------------

// translateNavigationError normalizes daemon target resolution errors for
// the navigation resolution contract. Entity-specific ambiguity codes are
// normalized to E_AMBIGUOUS with preserved candidate details.
func translateNavigationError(err error, targetKind TargetKind) error {
	dre, isDaemonErr := daemonclient.AsDaemonReadError(err)

	code := errors.GetCode(err)

	// Normalize entity-specific ambiguity to E_AMBIGUOUS
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

	// For non-ambiguity DaemonReadErrors: preserve hint if present
	if isDaemonErr && dre.Hint != "" {
		return errors.NewWithDetails(code, dre.AgencyErr.Msg, map[string]string{"hint": dre.Hint})
	}

	return err
}

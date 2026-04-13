package commands

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func successRepo(repoID string) func(context.Context) (*RepoContextResult, error) {
	return func(context.Context) (*RepoContextResult, error) {
		return &RepoContextResult{RepoID: repoID}, nil
	}
}

func successDaemon() func(context.Context) error {
	return func(context.Context) error { return nil }
}

func worktreeResult(repoID, id, path string) *NavigationResult {
	return &NavigationResult{
		TargetKind:     TargetWorktree,
		ResolvedRepoID: repoID,
		ResolvedID:     id,
		ResolvedPath:   path,
	}
}

func invocationResult(repoID, id, path string) *NavigationResult {
	return &NavigationResult{
		TargetKind:     TargetInvocation,
		ResolvedRepoID: repoID,
		ResolvedID:     id,
		ResolvedPath:   path,
	}
}

func ambiguousDaemonErr(code errors.Code, msg, hint string, candidates []string) *daemonclient.DaemonReadError {
	rawDetails, _ := json.Marshal(map[string]interface{}{"candidates": candidates})
	return &daemonclient.DaemonReadError{
		AgencyErr:  &errors.AgencyError{Code: code, Msg: msg},
		Hint:       hint,
		RawDetails: rawDetails,
	}
}

func TestNavigationKernel_ReadRoutingLifecycle_OrderAndGuards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var callOrder []string
	expected := worktreeResult("repo-1", "wt-1", "/abs/path")

	deps := NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			callOrder = append(callOrder, "resolve_repo")
			return &RepoContextResult{RepoID: "repo-1"}, nil
		},
		EnsureDaemon: func(ctx context.Context) error {
			callOrder = append(callOrder, "ensure_daemon")
			return nil
		},
		CheckAPIVersion: func(ctx context.Context) error {
			callOrder = append(callOrder, "check_api_version")
			return nil
		},
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			callOrder = append(callOrder, "get_worktree")
			assert.Equal(t, "alpha", ref)
			assert.Equal(t, "repo-1", repoID)
			return expected, nil
		},
		IsInteractive: func() bool { return true },
	}

	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            "alpha",
		},
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	require.NoError(t, err)
	assert.Equal(t, []string{"resolve_repo", "ensure_daemon", "check_api_version", "get_worktree"}, callOrder)
	assert.Equal(t, "repo-1", result.ResolvedRepoID)
	assert.Equal(t, "wt-1", result.ResolvedID)
	assert.Equal(t, "/abs/path", result.ResolvedPath)
}

func TestNavigationKernel_RepoContextRequired_ReturnsENoRepoContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	daemonCalled := false
	resolveCalled := false

	deps := NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			return nil, errors.NewWithDetails(
				errors.ENoRepoContext,
				"no repo context",
				map[string]string{"hint": "pass --repo"},
			)
		},
		EnsureDaemon: func(ctx context.Context) error {
			daemonCalled = true
			return nil
		},
		CheckAPIVersion: func(ctx context.Context) error { return nil },
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			resolveCalled = true
			return nil, nil
		},
		IsInteractive: func() bool { return true },
	}

	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            "alpha",
		},
	}

	_, err := ResolveNavigation(ctx, intent, deps)
	require.Error(t, err)
	assert.Equal(t, errors.ENoRepoContext, errors.GetCode(err))
	assert.False(t, daemonCalled, "daemon must not be contacted after repo context failure")
	assert.False(t, resolveCalled, "target resolution must not be attempted")
}

func TestNavigationKernel_NoLocalDiscoveryAfterDaemonRouting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	localDiscoveryCalled := false

	deps := NavigationDeps{
		ResolveRepo: successRepo("repo-1"),
		EnsureDaemon: func(ctx context.Context) error {
			return errors.New(errors.EDaemonConnectionFailed, "daemon not reachable")
		},
		CheckAPIVersion: func(ctx context.Context) error {
			t.Fatal("CheckAPIVersion must not be called after EnsureDaemon failure")
			return nil
		},
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			t.Fatal("GetWorktree must not be called after daemon failure")
			return nil, nil
		},
		IsInteractive: func() bool { return true },
		FallbackCallback: func(ctx context.Context) (*NavigationResult, error) {
			localDiscoveryCalled = true
			return nil, nil
		},
	}

	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            "alpha",
		},
		BootstrapFallbackAllowed: false,
	}

	_, err := ResolveNavigation(ctx, intent, deps)
	require.Error(t, err)
	assert.Equal(t, errors.EDaemonConnectionFailed, errors.GetCode(err))
	assert.False(t, localDiscoveryCalled, "local discovery spy must not be called when bootstrap fallback is disabled")
}

func TestNavigationKernel_BootstrapFallbackBoundary_GuardedCallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("enabled_callback_present_daemon_fails", func(t *testing.T) {
		var order []string
		fbResult := worktreeResult("repo-1", "wt-fb", "/fallback/path")

		deps := NavigationDeps{
			ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
				order = append(order, "resolve_repo")
				return &RepoContextResult{RepoID: "repo-1"}, nil
			},
			EnsureDaemon: func(ctx context.Context) error {
				order = append(order, "ensure_daemon")
				return errors.New(errors.EDaemonConnectionFailed, "daemon down")
			},
			CheckAPIVersion: func(ctx context.Context) error {
				t.Fatal("CheckAPIVersion must not be called after EnsureDaemon failure")
				return nil
			},
			GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
				t.Fatal("GetWorktree must not be called in fallback path")
				return nil, nil
			},
			IsInteractive: func() bool { return true },
			FallbackCallback: func(ctx context.Context) (*NavigationResult, error) {
				order = append(order, "fallback")
				return fbResult, nil
			},
		}

		intent := NavigationIntent{
			Selection: NavigationSelection{
				SelectorSource: SelectorExplicitRef,
				TargetKind:     TargetWorktree,
				Ref:            "alpha",
			},
			BootstrapFallbackAllowed: true,
		}

		result, err := ResolveNavigation(ctx, intent, deps)
		require.NoError(t, err)
		assert.Equal(t, []string{"resolve_repo", "ensure_daemon", "fallback"}, order)
		assert.Equal(t, "wt-fb", result.ResolvedID)
		assert.Equal(t, "/fallback/path", result.ResolvedPath)
	})

	t.Run("disabled_callback_present_returns_daemon_error", func(t *testing.T) {
		fallbackCalled := false

		deps := NavigationDeps{
			ResolveRepo:     successRepo("repo-1"),
			EnsureDaemon:    func(ctx context.Context) error { return errors.New(errors.EDaemonConnectionFailed, "down") },
			CheckAPIVersion: func(ctx context.Context) error { return nil },
			GetWorktree:     func(ctx context.Context, ref, repoID string) (*NavigationResult, error) { return nil, nil },
			IsInteractive:   func() bool { return true },
			FallbackCallback: func(ctx context.Context) (*NavigationResult, error) {
				fallbackCalled = true
				return nil, nil
			},
		}

		intent := NavigationIntent{
			Selection: NavigationSelection{
				SelectorSource: SelectorExplicitRef,
				TargetKind:     TargetWorktree,
				Ref:            "alpha",
			},
			BootstrapFallbackAllowed: false,
		}

		_, err := ResolveNavigation(ctx, intent, deps)
		require.Error(t, err)
		assert.Equal(t, errors.EDaemonConnectionFailed, errors.GetCode(err))
		assert.False(t, fallbackCalled)
	})

	t.Run("enabled_callback_nil_returns_daemon_error", func(t *testing.T) {
		deps := NavigationDeps{
			ResolveRepo:      successRepo("repo-1"),
			EnsureDaemon:     func(ctx context.Context) error { return errors.New(errors.EDaemonConnectionFailed, "down") },
			CheckAPIVersion:  func(ctx context.Context) error { return nil },
			GetWorktree:      func(ctx context.Context, ref, repoID string) (*NavigationResult, error) { return nil, nil },
			IsInteractive:    func() bool { return true },
			FallbackCallback: nil,
		}

		intent := NavigationIntent{
			Selection: NavigationSelection{
				SelectorSource: SelectorExplicitRef,
				TargetKind:     TargetWorktree,
				Ref:            "alpha",
			},
			BootstrapFallbackAllowed: true,
		}

		_, err := ResolveNavigation(ctx, intent, deps)
		require.Error(t, err)
		assert.Equal(t, errors.EDaemonConnectionFailed, errors.GetCode(err))
	})

	t.Run("enabled_version_check_fails_triggers_fallback", func(t *testing.T) {
		var order []string
		fbResult := worktreeResult("repo-1", "wt-vfb", "/version-fallback")

		deps := NavigationDeps{
			ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
				order = append(order, "resolve_repo")
				return &RepoContextResult{RepoID: "repo-1"}, nil
			},
			EnsureDaemon: func(ctx context.Context) error {
				order = append(order, "ensure_daemon")
				return nil
			},
			CheckAPIVersion: func(ctx context.Context) error {
				order = append(order, "check_api_version")
				return errors.New(errors.EDaemonIncompatible, "version mismatch")
			},
			GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
				t.Fatal("GetWorktree must not be called after version check failure")
				return nil, nil
			},
			IsInteractive: func() bool { return true },
			FallbackCallback: func(ctx context.Context) (*NavigationResult, error) {
				order = append(order, "fallback")
				return fbResult, nil
			},
		}

		intent := NavigationIntent{
			Selection: NavigationSelection{
				SelectorSource: SelectorExplicitRef,
				TargetKind:     TargetWorktree,
				Ref:            "alpha",
			},
			BootstrapFallbackAllowed: true,
		}

		result, err := ResolveNavigation(ctx, intent, deps)
		require.NoError(t, err)
		assert.Equal(t, []string{"resolve_repo", "ensure_daemon", "check_api_version", "fallback"}, order)
		assert.Equal(t, "wt-vfb", result.ResolvedID)
	})
}

func TestNavigationKernel_DaemonUnavailable_NoFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	localScanAttempted := false

	deps := NavigationDeps{
		ResolveRepo: successRepo("repo-1"),
		EnsureDaemon: func(ctx context.Context) error {
			return errors.New(errors.EDaemonConnectionFailed, "connection refused")
		},
		CheckAPIVersion: func(ctx context.Context) error { return nil },
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			localScanAttempted = true
			return nil, nil
		},
		IsInteractive: func() bool { return true },
	}

	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            "alpha",
		},
	}

	_, err := ResolveNavigation(ctx, intent, deps)
	require.Error(t, err)
	assert.Equal(t, errors.EDaemonConnectionFailed, errors.GetCode(err))
	assert.False(t, localScanAttempted, "no local store scan after daemon failure")
}

func TestNavigationKernel_DaemonIncompatible_NoFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	localScanAttempted := false

	deps := NavigationDeps{
		ResolveRepo:  successRepo("repo-1"),
		EnsureDaemon: successDaemon(),
		CheckAPIVersion: func(ctx context.Context) error {
			return errors.New(errors.EDaemonIncompatible, "daemon API v2, client v1")
		},
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			localScanAttempted = true
			return nil, nil
		},
		IsInteractive: func() bool { return true },
	}

	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            "alpha",
		},
	}

	_, err := ResolveNavigation(ctx, intent, deps)
	require.Error(t, err)
	assert.Equal(t, errors.EDaemonIncompatible, errors.GetCode(err))
	assert.False(t, localScanAttempted, "no local store scan after version mismatch")
}

func TestNavigationKernel_InteractivePreflight_RequiresTTY(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repoCalled := false
	daemonCalled := false

	deps := NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			repoCalled = true
			return &RepoContextResult{RepoID: "repo-1"}, nil
		},
		EnsureDaemon: func(ctx context.Context) error {
			daemonCalled = true
			return nil
		},
		CheckAPIVersion: func(ctx context.Context) error { return nil },
		GetInvocation: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			return nil, nil
		},
		IsInteractive: func() bool { return false },
	}

	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetInvocation,
			Ref:            "inv-1",
		},
		RequiresTTY: true,
	}

	_, err := ResolveNavigation(ctx, intent, deps)
	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))
	assert.False(t, repoCalled, "repo resolution must not happen before TTY preflight failure")
	assert.False(t, daemonCalled, "daemon must not be contacted before TTY preflight failure")

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	require.NotNil(t, ae.Details)
	assert.NotEmpty(t, ae.Details["hint"], "must include interactive-terminal recovery cue")
}

func TestNavigationKernel_SelectionIdentity_StableForDispatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	expectedWT := worktreeResult("repo-1", "wt-1", "/abs/worktree")
	expectedInv := invocationResult("repo-1", "inv-1", "/abs/sandbox")

	for _, tc := range []struct {
		name       string
		targetKind TargetKind
		ref        string
		expected   *NavigationResult
	}{
		{"worktree_path", TargetWorktree, "alpha", expectedWT},
		{"worktree_open", TargetWorktree, "alpha", expectedWT},
		{"worktree_shell", TargetWorktree, "alpha", expectedWT},
		{"worktree_enter", TargetWorktree, "alpha", expectedWT},
		{"invocation_path", TargetInvocation, "inv-ref", expectedInv},
		{"invocation_open", TargetInvocation, "inv-ref", expectedInv},
		{"invocation_shell", TargetInvocation, "inv-ref", expectedInv},
		{"invocation_enter", TargetInvocation, "inv-ref", expectedInv},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := NavigationDeps{
				ResolveRepo:     successRepo("repo-1"),
				EnsureDaemon:    successDaemon(),
				CheckAPIVersion: successDaemon(),
				GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
					return expectedWT, nil
				},
				GetInvocation: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
					return expectedInv, nil
				},
				IsInteractive: func() bool { return true },
			}

			intent := NavigationIntent{
				Selection: NavigationSelection{
					SelectorSource: SelectorExplicitRef,
					TargetKind:     tc.targetKind,
					Ref:            tc.ref,
				},
			}

			result, err := ResolveNavigation(ctx, intent, deps)
			require.NoError(t, err)
			assert.Equal(t, tc.expected.ResolvedRepoID, result.ResolvedRepoID)
			assert.Equal(t, tc.expected.ResolvedID, result.ResolvedID)
			assert.Equal(t, tc.expected.ResolvedPath, result.ResolvedPath)
		})
	}
}

func TestNavigationKernel_ResolveWorktree_AmbiguousPreservesCandidates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	deps := NavigationDeps{
		ResolveRepo:     successRepo("repo-1"),
		EnsureDaemon:    successDaemon(),
		CheckAPIVersion: successDaemon(),
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			return nil, ambiguousDaemonErr(
				errors.EWorktreeIDAmbiguous,
				"worktree ref 'a' is ambiguous",
				"use full worktree ID",
				[]string{"wt-alpha", "wt-apex"},
			)
		},
		IsInteractive: func() bool { return true },
	}

	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            "a",
		},
	}

	_, err := ResolveNavigation(ctx, intent, deps)
	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	require.NotNil(t, ae.Details)
	assert.Equal(t, "worktree", ae.Details["target_kind"])
	assert.Equal(t, "2", ae.Details["candidate_count"])

	var candidates []string
	require.NoError(t, json.Unmarshal([]byte(ae.Details["candidates"]), &candidates))
	assert.Equal(t, []string{"wt-alpha", "wt-apex"}, candidates)
	assert.Equal(t, "use full worktree ID", ae.Details["hint"])
}

func TestNavigationKernel_ResolveInvocation_AmbiguousPreservesCandidates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	deps := NavigationDeps{
		ResolveRepo:     successRepo("repo-1"),
		EnsureDaemon:    successDaemon(),
		CheckAPIVersion: successDaemon(),
		GetInvocation: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			return nil, ambiguousDaemonErr(
				errors.EInvocationIDAmbiguous,
				"invocation ref 'run' is ambiguous",
				"use full invocation ID",
				[]string{"inv-runA", "inv-runB", "inv-runC"},
			)
		},
		IsInteractive: func() bool { return true },
	}

	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetInvocation,
			Ref:            "run",
		},
	}

	_, err := ResolveNavigation(ctx, intent, deps)
	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	require.NotNil(t, ae.Details)
	assert.Equal(t, "invocation", ae.Details["target_kind"])
	assert.Equal(t, "3", ae.Details["candidate_count"])

	var candidates []string
	require.NoError(t, json.Unmarshal([]byte(ae.Details["candidates"]), &candidates))
	assert.Equal(t, []string{"inv-runA", "inv-runB", "inv-runC"}, candidates)
	assert.Equal(t, "use full invocation ID", ae.Details["hint"])
}

package landing_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon/landing"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// setupTestService creates a landing.Service backed by a real store in a temp dir.
// It also writes the minimum invocation + worktree meta needed for Land() precondition checks.
func setupTestService(t *testing.T, status store.InvocationStatus, landingStatus store.LandingStatus) *landing.Service {
	t.Helper()

	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	st := store.NewStore(realFS, dataDir, time.Now)

	repoID := "test-repo"
	invocationID := "test-inv"
	worktreeID := "test-wt"

	// Create integration worktree meta (so Land() can read it at line 95).
	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)
	wtMeta := store.NewIntegrationWorktreeMeta(worktreeID, "test", repoID, "agency/integration-test", "main", "/nonexistent/tree", time.Now())
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, worktreeID, wtMeta))

	// Create invocation meta with the desired status.
	_, err = st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	meta := store.NewInvocationMeta(invocationID, "", worktreeID, "/nonexistent/sandbox", "agency/sandbox-test", "abc123", "claude", store.RunnerModeHeadless, time.Now())
	meta.Status = status
	meta.LandingStatus = landingStatus
	if status == store.InvocationStatusFinished || status == store.InvocationStatusFailed {
		meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta))

	return landing.NewService(st, exec.NewRealRunner(), realFS, time.Now)
}

func TestLand_Preconditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        store.InvocationStatus
		landingStatus store.LandingStatus
		wantCode      errors.Code
	}{
		{
			name:          "still running",
			status:        store.InvocationStatusRunning,
			landingStatus: "",
			wantCode:      errors.EInvocationStillRunning,
		},
		{
			name:          "still starting",
			status:        store.InvocationStatusStarting,
			landingStatus: "",
			wantCode:      errors.EInvocationStillRunning,
		},
		{
			name:          "already landed",
			status:        store.InvocationStatusFinished,
			landingStatus: store.LandingStatusLanded,
			wantCode:      errors.ELandAlreadyLanded,
		},
		{
			name:          "already discarded",
			status:        store.InvocationStatusFinished,
			landingStatus: store.LandingStatusDiscarded,
			wantCode:      errors.ELandAlreadyDiscarded,
		},
		{
			name:          "finished passes preconditions",
			status:        store.InvocationStatusFinished,
			landingStatus: store.LandingStatusPending,
			wantCode:      errors.ESandboxMissing, // passes preconditions, fails at path check
		},
		{
			name:          "failed passes preconditions",
			status:        store.InvocationStatusFailed,
			landingStatus: "",
			wantCode:      errors.ESandboxMissing, // passes preconditions, fails at path check
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := setupTestService(t, tt.status, tt.landingStatus)
			_, err := svc.Land(context.Background(), landing.LandOpts{
				RepoID:       "test-repo",
				InvocationID: "test-inv",
				RepoRoot:     "/nonexistent",
			})

			require.Error(t, err)
			assert.Equal(t, tt.wantCode, errors.GetCode(err))
		})
	}
}

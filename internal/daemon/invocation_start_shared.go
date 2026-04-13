package daemon

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	agencylock "github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	controlPlaneRepoLockAcquireTimeout = 2 * time.Second
	controlPlaneRepoLockPollInterval   = 25 * time.Millisecond
)

type controlPlaneStartErrorWriter func(status int, code, message, hint string)

type controlPlaneStartResolved struct {
	repoRoot     string
	repoIdentity identity.RepoIdentity
	wtRecord     *store.IntegrationWorktreeRecord
	unlockRepo   func() error
}

func safeIntPtr(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func validateControlPlaneStartRunner(runner string, args []string, headless bool) (string, error) {
	canonicalRunner, err := runners.Canonicalize(runner)
	if err != nil {
		return "", err
	}
	if headless {
		if err := runners.ValidateHeadlessArgs(canonicalRunner, args); err != nil {
			return "", err
		}
		return canonicalRunner, nil
	}
	if err := runners.ValidateArgs(canonicalRunner, args); err != nil {
		return "", err
	}
	return canonicalRunner, nil
}

func validateControlPlaneStartInvocationName(name string) error {
	if name == "" {
		return nil
	}
	return core.ValidateName(name)
}

func isInsideAgencyManagedWorktree(path, dataDir string) bool {
	cleanPath := filepath.Clean(path)
	cleanDataDir := filepath.Clean(dataDir)
	reposDir := filepath.Join(cleanDataDir, "repos")
	if !strings.HasPrefix(cleanPath, reposDir) {
		return false
	}

	rel, err := filepath.Rel(reposDir, cleanPath)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 4 {
		return false
	}
	if parts[1] == "integration_worktrees" || parts[1] == "sandboxes" {
		return len(parts) >= 4 && parts[3] == "tree"
	}
	return false
}

func (s *Server) ensureRepoRegistered(repoIdentity identity.RepoIdentity, repoRoot string) error {
	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		return err
	}
	idx = s.Store.UpsertRepoIndexEntry(idx, repoIdentity.RepoKey, repoIdentity.RepoID, repoRoot)
	return s.Store.SaveRepoIndex(idx)
}

func (s *Server) checkInvocationNameUniqueness(repoID, name string) error {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return fmt.Errorf("failed to scan invocations: %w", err)
	}
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		if r.Meta.Status == store.InvocationStatusFinished || r.Meta.Status == store.InvocationStatusFailed {
			continue
		}
		if r.Meta.LandingStatus == store.LandingStatusLanded || r.Meta.LandingStatus == store.LandingStatusDiscarded {
			continue
		}
		if r.Meta.InvocationName == name {
			return fmt.Errorf("invocation name '%s' is already used by active invocation %s", name, r.InvocationID)
		}
	}
	return nil
}

func (s *Server) acquireControlPlaneRepoLock(repoID, op string) (func() error, error) {
	deadline := s.Clock().Add(controlPlaneRepoLockAcquireTimeout)
	for {
		unlock, err := s.repoLock.Lock(repoID, op)
		if err == nil {
			return unlock, nil
		}
		var lockedErr *agencylock.ErrLocked
		if !stderrors.As(err, &lockedErr) {
			return nil, err
		}
		if !s.Clock().Before(deadline) {
			return nil, err
		}
		time.Sleep(controlPlaneRepoLockPollInterval)
	}
}

func (s *Server) resolveControlPlaneRepoRoot(ctx context.Context, repoRoot string, writeErr controlPlaneStartErrorWriter) (string, identity.RepoIdentity, bool) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "failed to resolve repo_root: "+err.Error(), "")
		return "", identity.RepoIdentity{}, false
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "failed to resolve repo_root symlinks: "+err.Error(), "")
		return "", identity.RepoIdentity{}, false
	}
	if isInsideAgencyManagedWorktree(repoRoot, s.Store.DataDir) {
		writeErr(http.StatusBadRequest, string(errors.EUnsafeRepoRoot), "repo_root is inside an agency-managed worktree", "use the original repository, not a sandbox or integration worktree")
		return "", identity.RepoIdentity{}, false
	}

	gitRoot, err := git.GetRepoRoot(ctx, s.Runner, repoRoot)
	if err != nil {
		writeErr(http.StatusBadRequest, string(errors.ENoRepo), "repo_root is not inside a git repository: "+err.Error(), "")
		return "", identity.RepoIdentity{}, false
	}
	repoRoot = gitRoot.Path
	originInfo := git.GetOriginInfo(ctx, s.Runner, repoRoot)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, originInfo.URL)
	return repoRoot, repoIdentity, true
}

func (s *Server) prepareControlPlaneStart(ctx context.Context, repoRoot, worktreeRef, lockOp string, writeErr controlPlaneStartErrorWriter, repoIdentity identity.RepoIdentity) (*controlPlaneStartResolved, bool) {
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to register repo: "+err.Error(), "")
		return nil, false
	}

	wtSvc := integrationworktree.NewService(s.Store, s.Runner, s.FS, s.Clock)
	wtRecord, err := wtSvc.Resolve(repoIdentity.RepoID, worktreeRef, false)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		writeErr(http.StatusNotFound, string(code), err.Error(), "run 'agency worktree ls' to see available worktrees")
		return nil, false
	}
	if wtRecord.Broken || wtRecord.Meta == nil {
		writeErr(http.StatusBadRequest, string(errors.EWorktreeBroken), "integration worktree exists but meta.json is unreadable", "inspect or recreate the worktree")
		return nil, false
	}
	if wtRecord.Meta.State != store.WorktreeStatePresent {
		writeErr(http.StatusBadRequest, string(errors.EWorktreeNotFound), "integration worktree is archived", "use a present (non-archived) integration worktree")
		return nil, false
	}

	unlockRepo, err := s.acquireControlPlaneRepoLock(repoIdentity.RepoID, lockOp)
	if err != nil {
		var lockedErr *agencylock.ErrLocked
		if !stderrors.As(err, &lockedErr) {
			writeErr(http.StatusInternalServerError, string(errors.EInternal), "failed to acquire repository lock: "+err.Error(), "")
			return nil, false
		}
		writeErr(http.StatusConflict, string(errors.ERepoLocked), "repository is locked by another operation", "wait for the other operation to complete")
		return nil, false
	}

	return &controlPlaneStartResolved{
		repoRoot:     repoRoot,
		repoIdentity: repoIdentity,
		wtRecord:     wtRecord,
		unlockRepo:   unlockRepo,
	}, true
}

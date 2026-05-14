package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/invocation"
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

func controlPlaneStartFingerprint(repoRoot, worktreeID, checkoutRoot string, mode store.RunnerMode, req ControlPlaneStartRequest, requestEnv map[string]string) string {
	promptHash := sha256.Sum256([]byte(req.Prompt))
	payload, _ := json.Marshal(struct {
		RepoRoot           string           `json:"repo_root"`
		WorktreeID         string           `json:"worktree_id"`
		CheckoutRoot       string           `json:"checkout_root"`
		ExecutionProfile   string           `json:"execution_profile"`
		Mode               store.RunnerMode `json:"mode"`
		Runner             string           `json:"runner"`
		EnvKeys            []string         `json:"env_keys,omitempty"`
		PromptSHA256       string           `json:"prompt_sha256,omitempty"`
		InvocationName     string           `json:"invocation_name,omitempty"`
		RunnerArgs         []string         `json:"runner_args,omitempty"`
		NoIncludeUntracked bool             `json:"no_include_untracked,omitempty"`
	}{
		RepoRoot:           repoRoot,
		WorktreeID:         worktreeID,
		CheckoutRoot:       checkoutRoot,
		ExecutionProfile:   req.ExecutionProfile,
		Mode:               mode,
		Runner:             req.Runner,
		EnvKeys:            sortedEnvKeys(requestEnv),
		PromptSHA256:       hex.EncodeToString(promptHash[:]),
		InvocationName:     req.InvocationName,
		RunnerArgs:         append([]string(nil), req.RunnerArgs...),
		NoIncludeUntracked: req.NoIncludeUntracked,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func isInsideAgencyManagedWorktree(path string) bool {
	cleanPath := filepath.Clean(path)
	for {
		if _, err := os.Stat(filepath.Join(cleanPath, ".agency", integrationworktree.IntegrationMarkerFileName)); err == nil {
			return true
		}
		if _, err := os.Stat(filepath.Join(cleanPath, ".agency", invocation.SandboxMarkerFileName)); err == nil {
			return true
		}
		parent := filepath.Dir(cleanPath)
		if parent == cleanPath {
			return false
		}
		cleanPath = parent
	}
}

func (s *Server) isInsideAgencyManagedTree(path string) (bool, error) {
	if isInsideAgencyManagedWorktree(path) {
		return true, nil
	}

	cleanPath := canonicalPathForContainment(path)
	repoIDs, err := s.discoverDurableRepoIDs()
	if err != nil {
		return false, err
	}
	for _, repoID := range repoIDs {
		worktrees, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, repoID)
		if err != nil {
			return false, err
		}
		for _, record := range worktrees {
			if record.Meta != nil && pathContains(canonicalPathForContainment(record.Meta.TreePath), cleanPath) {
				return true, nil
			}
		}

		invocations, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
		if err != nil {
			return false, err
		}
		for _, record := range invocations {
			if record.Meta != nil && pathContains(canonicalPathForContainment(record.Meta.SandboxPath), cleanPath) {
				return true, nil
			}
		}
	}
	return false, nil
}

func canonicalPathForContainment(path string) string {
	clean := filepath.Clean(path)
	if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = resolved
	}
	return clean
}

func pathContains(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)))
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
	insideManagedTree, err := s.isInsideAgencyManagedTree(repoRoot)
	if err != nil {
		writeErr(http.StatusInternalServerError, string(errors.EInternal), "failed to inspect managed worktrees: "+err.Error(), "")
		return "", identity.RepoIdentity{}, false
	}
	if insideManagedTree {
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

	if err := s.ensureWorktreeMergeInactive(repoIdentity.RepoID, wtRecord.WorktreeID, "start an invocation"); err != nil {
		_ = unlockRepo()
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EWorktreeMergeActive
		}
		writeErr(http.StatusConflict, string(code), err.Error(), mergeHintFromError(err))
		return nil, false
	}

	return &controlPlaneStartResolved{
		repoRoot:     repoRoot,
		repoIdentity: repoIdentity,
		wtRecord:     wtRecord,
		unlockRepo:   unlockRepo,
	}, true
}

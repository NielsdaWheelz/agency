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
	"slices"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
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

// startFailure carries the HTTP status and error metadata for a failed
// invocation or task start. Returned by the shared start helpers so callers
// can render their own response shape and, for task starts, persist failure
// state via markTaskFailed.
type startFailure struct {
	status int
	code   errors.Code
	msg    string
	hint   string
}

func (e startFailure) Error() string {
	if e.code == "" {
		return e.msg
	}
	return string(e.code) + ": " + e.msg
}

func newStartFailure(status int, code errors.Code, msg, hint string) startFailure {
	if code == "" {
		code = errors.EInternal
	}
	return startFailure{status: status, code: code, msg: msg, hint: hint}
}

func startFailureFromError(status int, defaultCode errors.Code, err error, hint string) startFailure {
	code := errors.CodeOr(err, defaultCode)
	return newStartFailure(status, code, apiErrorMessage(err), hint)
}

func asStartFailure(err error) startFailure {
	var failure startFailure
	if stderrors.As(err, &failure) {
		return failure
	}
	return startFailureFromError(http.StatusInternalServerError, errors.EInternal, err, "")
}

// runnerValidationFailure converts an error from validateControlPlaneStartRunner
// into a typed startFailure with the appropriate HTTP status and hint.
func runnerValidationFailure(err error) *startFailure {
	code := errors.CodeOr(err, errors.ERunnerArgConflict)
	hint := "remove reserved flags from runner_args"
	if code == errors.ERunnerNotFound {
		hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
	}
	f := newStartFailure(http.StatusBadRequest, code, err.Error(), hint)
	return &f
}

// headedRunnerArgsFailure converts an error from buildRunnerArgsForHeaded into
// a typed startFailure with the appropriate HTTP status and hint.
func headedRunnerArgsFailure(err error) *startFailure {
	code := errors.CodeOr(err, errors.EInternal)
	hint := ""
	switch code {
	case errors.ERunnerNotFound:
		hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
	case errors.EInvocationInvalidMode:
		hint = "runner does not support headed mode"
	}
	status := http.StatusInternalServerError
	if code == errors.ERunnerNotFound || code == errors.EInvocationInvalidMode {
		status = http.StatusBadRequest
	}
	f := newStartFailure(status, code, err.Error(), hint)
	return &f
}

// evaluateIdempotentStartRecord inspects an existing invocation meta found by
// a client_request_id lookup. It returns nil if the record represents a valid
// completed or post-claim state suitable for idempotent reuse, or a typed
// startFailure describing why the record cannot be reused.
func evaluateIdempotentStartRecord(meta *store.InvocationMeta) *startFailure {
	switch meta.Status {
	case store.InvocationStatusRunning, store.InvocationStatusStopping, store.InvocationStatusFinished:
		return nil
	case store.InvocationStatusStarting:
		f := newStartFailure(http.StatusConflict, errors.EInvocationStartFailed,
			"client_request_id was already accepted but invocation start has not reached running state",
			"inspect invocation state before retrying")
		return &f
	case store.InvocationStatusFailed:
		if !directStartFailedBeforeClaim(meta) {
			return nil
		}
		message := strings.TrimSpace(meta.FailureReason)
		if message == "" {
			message = "invocation start previously failed"
		}
		f := newStartFailure(http.StatusConflict, errors.EInvocationStartFailed, message, "inspect invocation state before retrying")
		return &f
	default:
		f := newStartFailure(http.StatusConflict, errors.EStoreCorrupt,
			"client_request_id record has unsupported invocation status",
			"inspect invocation state before retrying")
		return &f
	}
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
		RunnerArgs:         slices.Clone(req.RunnerArgs),
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

	cleanPath := agencyfs.CanonicalizePath(path)
	repoIDs, err := s.discoverDurableRepoIDs()
	if err != nil {
		return false, err
	}
	for _, repoID := range repoIDs {
		worktrees, err := s.store.ScanIntegrationWorktreesForRepo(repoID)
		if err != nil {
			return false, err
		}
		for _, record := range worktrees {
			if record.Meta != nil && agencyfs.PathContains(agencyfs.CanonicalizePath(record.Meta.TreePath), cleanPath) {
				return true, nil
			}
		}

		invocations, err := s.store.ScanInvocationsForRepo(repoID)
		if err != nil {
			return false, err
		}
		for _, record := range invocations {
			if record.Meta != nil && agencyfs.PathContains(agencyfs.CanonicalizePath(record.Meta.SandboxPath), cleanPath) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Server) directStartRequestConflictsWithRecord(repoID, repoRoot string, mode store.RunnerMode, req ControlPlaneStartRequest, requestEnv map[string]string, meta *store.InvocationMeta) bool {
	if explicitProfile := strings.TrimSpace(req.ExecutionProfile); explicitProfile != "" && explicitProfile != meta.ExecutionProfile {
		return true
	}

	wtSvc := integrationworktree.NewService(s.store, s.runner, s.fsys, s.clock)
	wtRecord, err := wtSvc.Resolve(repoID, req.WorktreeRef, true)
	if err != nil {
		if req.WorktreeRef != meta.IntegrationWorktreeID {
			return true
		}
	} else if wtRecord.WorktreeID != meta.IntegrationWorktreeID {
		return true
	}

	replayReq := req
	replayReq.ExecutionProfile = meta.ExecutionProfile
	return meta.RequestFingerprint != controlPlaneStartFingerprint(repoRoot, meta.IntegrationWorktreeID, meta.CheckoutRoot, mode, replayReq, requestEnv)
}

func directStartFailedBeforeClaim(meta *store.InvocationMeta) bool {
	return meta.ExitReason == store.ExitReasonStartFailed || meta.ClaimedAt == ""
}

func (s *Server) ensureRepoRegistered(repoIdentity identity.RepoIdentity, repoRoot string) error {
	idx, err := s.store.LoadRepoIndex()
	if err != nil {
		return err
	}
	idx = s.store.UpsertRepoIndexEntry(idx, repoIdentity.RepoKey, repoIdentity.RepoID, repoRoot)
	return s.store.SaveRepoIndex(idx)
}

func (s *Server) checkInvocationNameUniqueness(repoID, name string) error {
	records, err := s.store.ScanInvocationsForRepo(repoID)
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

// acquireControlPlaneRepoLockRaw is the retry loop the control-plane uses to
// soak short bursts of contention before reporting ERepoLocked. Returns the
// underlying *agencylock.ErrLocked on contention or the underlying error on
// other failures; prefer acquireControlPlaneRepoLock for typed mapping.
func (s *Server) acquireControlPlaneRepoLockRaw(repoID, op string) (func() error, error) {
	deadline := s.clock().Add(controlPlaneRepoLockAcquireTimeout)
	for {
		unlock, err := s.repoLock.Lock(repoID, op)
		if err == nil {
			return unlock, nil
		}
		var lockedErr *agencylock.ErrLocked
		if !stderrors.As(err, &lockedErr) {
			return nil, err
		}
		if !s.clock().Before(deadline) {
			return nil, err
		}
		time.Sleep(controlPlaneRepoLockPollInterval)
	}
}

// acquireControlPlaneRepoLock acquires the repo lock with the control-plane
// retry policy and maps failure to a typed startFailure.
func (s *Server) acquireControlPlaneRepoLock(repoID, op string) (func() error, *startFailure) {
	unlock, err := s.acquireControlPlaneRepoLockRaw(repoID, op)
	return unlock, lockFailureFromError(err)
}

// lockRepoOrFailure acquires the repo lock once (no retry) and maps any
// acquisition error to a typed startFailure.
func (s *Server) lockRepoOrFailure(repoID, op string) (func() error, *startFailure) {
	unlock, err := s.repoLock.Lock(repoID, op)
	return unlock, lockFailureFromError(err)
}

// lockFailureFromError translates a repo-lock error to a typed startFailure:
// ERepoLocked → 409 for *agencylock.ErrLocked, EInternal → 500 otherwise.
// Returns nil when err is nil.
func lockFailureFromError(err error) *startFailure {
	if err == nil {
		return nil
	}
	var lockedErr *agencylock.ErrLocked
	if stderrors.As(err, &lockedErr) {
		f := newStartFailure(http.StatusConflict, errors.ERepoLocked, "repository is locked by another operation", "wait for the other operation to complete")
		return &f
	}
	f := newStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to acquire repo lock: "+err.Error(), "")
	return &f
}

func (s *Server) resolveControlPlaneRepoRoot(ctx context.Context, repoRoot string, writeErr controlPlaneStartErrorWriter) (string, identity.RepoIdentity, bool) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		writeErr(http.StatusBadRequest, string(errors.EInvalidArgument), "failed to resolve repo_root: "+err.Error(), "")
		return "", identity.RepoIdentity{}, false
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		writeErr(http.StatusBadRequest, string(errors.EInvalidArgument), "failed to resolve repo_root symlinks: "+err.Error(), "")
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

	gitRoot, err := git.GetRepoRoot(ctx, s.runner, repoRoot, nil)
	if err != nil {
		writeErr(http.StatusBadRequest, string(errors.ENoRepo), "repo_root is not inside a git repository: "+err.Error(), "")
		return "", identity.RepoIdentity{}, false
	}
	repoRoot = gitRoot.Path
	originInfo := git.GetOriginInfo(ctx, s.runner, repoRoot, nil)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, originInfo.URL)
	return repoRoot, repoIdentity, true
}

func (s *Server) prepareControlPlaneStart(ctx context.Context, repoRoot, worktreeRef, lockOp string, writeErr controlPlaneStartErrorWriter, repoIdentity identity.RepoIdentity) (*controlPlaneStartResolved, bool) {
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		writeErr(http.StatusInternalServerError, string(errors.EInternal), "failed to register repo: "+err.Error(), "")
		return nil, false
	}

	wtSvc := integrationworktree.NewService(s.store, s.runner, s.fsys, s.clock)
	wtRecord, err := wtSvc.Resolve(repoIdentity.RepoID, worktreeRef, false)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
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

	unlockRepo, fail := s.acquireControlPlaneRepoLock(repoIdentity.RepoID, lockOp)
	if fail != nil {
		writeErr(fail.status, string(fail.code), fail.msg, fail.hint)
		return nil, false
	}

	if err := s.ensureWorktreeMergeInactive(repoIdentity.RepoID, wtRecord.WorktreeID, "start an invocation"); err != nil {
		_ = unlockRepo()
		code := errors.CodeOr(err, errors.EWorktreeMergeActive)
		writeErr(http.StatusConflict, string(code), err.Error(), errors.Hint(err))
		return nil, false
	}

	return &controlPlaneStartResolved{
		repoRoot:     repoRoot,
		repoIdentity: repoIdentity,
		wtRecord:     wtRecord,
		unlockRepo:   unlockRepo,
	}, true
}

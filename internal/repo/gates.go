// Package repo provides repository safety checks and context for agency run operations.
package repo

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// CheckRepoSafeOpts holds options for CheckRepoSafe.
type CheckRepoSafeOpts struct {
	// ParentBranch is the local branch name to branch from, e.g. "main".
	ParentBranch string
}

// RepoContext holds the resolved repository context after safety checks pass.
type RepoContext struct {
	// RepoRoot is the absolute path to the git repository root.
	RepoRoot string

	// RepoID is the computed repo_id via S0 rules (sha256(repo_key) truncated to 16 hex chars).
	RepoID string

	// RepoKey is the repo_key (github:owner/repo or path:<sha256>).
	RepoKey string

	// OriginURL is the origin remote URL, or empty string if not present.
	OriginURL string

	// DataDir is the resolved AGENCY_DATA_DIR.
	DataDir string
}

// osEnv implements paths.Env using os.Getenv.
type osEnv struct{}

func (osEnv) Get(key string) string {
	return os.Getenv(key)
}

// CheckRepoSafe resolves repo root + repo_id, updates repo.json,
// and applies safety gates required for starting a run.
//
// Order of operations (deterministic):
//  1. Resolve repo root from cwd
//  2. Compute repo_id using S0 rules
//  3. Read origin URL (best-effort)
//  4. Write/update repo.json (last_seen_at, origin_url)
//  5. Run gates (empty repo, dirty, parent branch)
//
// Error codes:
//   - E_NO_REPO: not inside a git repository
//   - E_EMPTY_REPO: repo has no commits (fresh git init)
//   - E_PARENT_DIRTY: working tree has uncommitted changes
//   - E_PARENT_BRANCH_NOT_FOUND: local parent branch does not exist
func CheckRepoSafe(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts CheckRepoSafeOpts) (*RepoContext, error) {
	// 1. Resolve repo root from cwd
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		// E_NO_REPO is already set by GetRepoRoot
		return nil, err
	}

	// 2. Read origin URL (best-effort, never fails)
	originURL := git.GetOriginURL(ctx, cr, repoRoot.Path)

	// 3. Compute repo_id using S0 rules
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originURL)

	// 4. Resolve data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// 5. Write/update repo.json
	if err := updateRepoJSON(fsys, dirs.DataDir, repoRoot.Path, repoIdentity, originURL); err != nil {
		return nil, err
	}

	// 6. Run gates

	// 6a. Empty repo check
	hasCommits, err := git.HasCommits(ctx, cr, repoRoot.Path)
	if err != nil {
		return nil, err
	}
	if !hasCommits {
		return nil, errors.New(errors.EEmptyRepo, "repository has no commits; create an initial commit first")
	}

	// 6b. Parent working tree dirty check
	isClean, err := git.IsClean(ctx, cr, repoRoot.Path)
	if err != nil {
		return nil, err
	}
	if !isClean {
		return nil, errors.New(errors.EParentDirty, "working tree has uncommitted changes; commit or stash them first")
	}

	// 6c. Local parent branch existence check
	branchExists, err := git.BranchExists(ctx, cr, repoRoot.Path, opts.ParentBranch)
	if err != nil {
		return nil, err
	}
	if !branchExists {
		return nil, errors.NewWithDetails(
			errors.EParentBranchNotFound,
			"local branch '"+opts.ParentBranch+"' not found; checkout or fetch parent locally (no auto-fetch in v1)",
			map[string]string{"branch": opts.ParentBranch},
		)
	}

	return &RepoContext{
		RepoRoot:  repoRoot.Path,
		RepoID:    repoIdentity.RepoID,
		RepoKey:   repoIdentity.RepoKey,
		OriginURL: originURL,
		DataDir:   dirs.DataDir,
	}, nil
}

// updateRepoJSON creates or updates repo.json atomically.
// This function reuses the S0 store package and follows its schema.
func updateRepoJSON(fsys fs.FS, dataDir, repoRoot string, repoIdentity identity.RepoIdentity, originURL string) error {
	st := store.NewStore(fsys, dataDir, time.Now)

	// Load existing repo record (if any)
	existingRec, exists, err := st.LoadRepoRecord(repoIdentity.RepoID)
	if err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to load repo.json", err)
	}

	var existingPtr *store.RepoRecord
	if exists {
		existingPtr = &existingRec
	}

	// Build repo record input
	// Note: We don't have agency.json path here, so we preserve existing or use empty
	// This matches the spec requirement to only update last_seen_at and origin_url
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	if exists && existingRec.AgencyJSONPath != "" {
		agencyJSONPath = existingRec.AgencyJSONPath
	}

	// Preserve existing capabilities or set minimal defaults
	capabilities := store.Capabilities{
		GitHubOrigin: repoIdentity.GitHubFlowAvailable,
		OriginHost:   repoIdentity.Origin.Host,
		GhAuthed:     false, // Not checking gh auth in this function
	}
	if exists {
		// Preserve gh_authed from existing record
		capabilities.GhAuthed = existingRec.Capabilities.GhAuthed
	}

	rec := st.UpsertRepoRecord(existingPtr, store.BuildRepoRecordInput{
		RepoKey:          repoIdentity.RepoKey,
		RepoID:           repoIdentity.RepoID,
		RepoRootLastSeen: repoRoot,
		AgencyJSONPath:   agencyJSONPath,
		OriginPresent:    originURL != "",
		OriginURL:        originURL,
		OriginHost:       repoIdentity.Origin.Host,
		Capabilities:     capabilities,
	})

	// Save repo record atomically
	if err := st.SaveRepoRecord(rec); err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to write repo.json", err)
	}

	return nil
}

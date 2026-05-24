// Package commands implements agency CLI commands.
package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// RepoContextResult holds the resolved repo context.
type RepoContextResult struct {
	RepoID string
	// Empty when --all-repos was used (list commands only)
	AllRepos bool
}

// ResolveRepoContextOpts controls how repo context resolution works.
type ResolveRepoContextOpts struct {
	// RepoRef is the value of the --repo flag (name, owner/repo, repo key, id, or prefix).
	RepoRef string
	// AllRepos is the value of the --all-repos flag (list commands only).
	AllRepos bool
	// AllowAllRepos controls whether --all-repos is accepted.
	// Must be true for list commands, false for single-ref commands.
	AllowAllRepos bool
	// CmdName is used in error messages ("agency agent <invocation-ref>", etc.).
	CmdName string
}

// ResolveRepoViaClient resolves repo context for a CLI command.
func ResolveRepoViaClient(ctx context.Context, cr exec.CommandRunner, client *daemonclient.Client, cwd string, opts ResolveRepoContextOpts) (*RepoContextResult, error) {
	// Mutual exclusion
	if opts.RepoRef != "" && opts.AllRepos {
		return nil, errors.New(errors.EUsage, "--repo and --all-repos are mutually exclusive")
	}

	// --all-repos only for list commands
	if opts.AllRepos && !opts.AllowAllRepos {
		return nil, errors.New(errors.EUsage, "--all-repos is not supported for "+opts.CmdName+"; specify --repo instead")
	}

	// Explicit --repo: resolve once here, then pass canonical repo_id below the command boundary.
	if opts.RepoRef != "" {
		result, err := client.GetRepo(ctx, opts.RepoRef)
		if err != nil {
			return nil, err
		}

		return &RepoContextResult{RepoID: result.Data.RepoID}, nil
	}

	// --all-repos: return empty repo_id (list globally)
	if opts.AllRepos {
		return &RepoContextResult{AllRepos: true}, nil
	}

	// Try CWD-based auto-registration
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd, nil)
	if err != nil {
		// Not in a repo — error with hints
		if opts.AllowAllRepos {
			return nil, errors.NewWithDetails(
				errors.ENoRepoContext,
				"no repo context (not in a git repo)",
				map[string]string{
					"hint": "run \"agency repo ls\" then re-run with --repo <repo_ref>, or pass --all-repos, or register a repo with \"agency repo add /path/to/repo\"",
				},
			)
		}
		return nil, errors.NewWithDetails(
			errors.ENoRepoContext,
			fmt.Sprintf("cannot resolve %s without a repo context", opts.CmdName),
			map[string]string{
				"hint": "run \"agency repo ls\" and re-run with \"--repo <repo_ref>\", or register a repo: \"agency repo add /path/to/repo\"",
			},
		)
	}

	// Auto-register via daemon
	result, err := client.RegisterRepo(ctx, repoRoot.Path)
	if err != nil {
		return nil, err
	}

	return &RepoContextResult{RepoID: result.Data.RepoID}, nil
}

// RepoAddOpts holds options for the repo add command.
type RepoAddOpts struct {
	// Path is the optional repo checkout path from the positional [path] arg.
	Path string
	JSON bool
}

// RepoAdd registers a repository with the daemon.
func RepoAdd(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts RepoAddOpts, stdout, stderr io.Writer) error {
	fail := commandFail(stdout, opts.JSON)

	client, err := ensureDaemonClient(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	path := opts.Path
	if path == "" {
		path, err = os.Getwd()
		if err != nil {
			return fail(errors.Wrap(errors.EInternal, "failed to get cwd", err))
		}
	}

	result, err := client.RegisterRepo(ctx, path)
	if err != nil {
		return fail(err)
	}

	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			RepoID        string `json:"repo_id,omitempty"`
			RepoKey       string `json:"repo_key,omitempty"`
			PreferredRoot string `json:"preferred_root,omitempty"`
		}{
			commandJSONBase: newCommandJSONSuccess(0, "", "", result.RequestID),
			RepoID:          result.Data.RepoID,
			RepoKey:         result.Data.RepoKey,
			PreferredRoot:   result.Data.PreferredRoot,
		})
	}

	_, _ = fmt.Fprintf(stdout, "Registered repo\n")
	_, _ = fmt.Fprintf(stdout, "  repo:           %s\n", namedLabel(result.Data.RepoName, result.Data.RepoID))
	_, _ = fmt.Fprintf(stdout, "  repo_key:       %s\n", result.Data.RepoKey)
	_, _ = fmt.Fprintf(stdout, "  preferred_root: %s\n", result.Data.PreferredRoot)
	if len(result.Data.Paths) > 1 {
		_, _ = fmt.Fprintf(stdout, "  paths:          %d registered\n", len(result.Data.Paths))
	}

	return nil
}

// RepoLSOpts holds options for the repo ls command.
type RepoLSOpts struct {
	JSON bool
}

// RepoLS lists registered repositories.
func RepoLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts RepoLSOpts, stdout, stderr io.Writer) error {
	client, err := ensureDaemonClient(ctx, fsys, "")
	if err != nil {
		return err
	}

	result, err := client.ListRepos(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeCommandJSON(stdout, result.Data.Repos)
	}

	if len(result.Data.Repos) == 0 {
		_, _ = fmt.Fprintln(stdout, "No repos registered.")
		_, _ = fmt.Fprintln(stdout, "Register one with: agency repo add /path/to/repo")
		return nil
	}

	for _, r := range result.Data.Repos {
		repoLabel := namedLabel(r.RepoName, r.RepoID)
		if strings.TrimSpace(r.RepoKey) != "" {
			_, _ = fmt.Fprintf(stdout, "%s  %s  %s\n", repoLabel, r.RepoKey, r.PreferredRoot)
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%s  %s\n", repoLabel, r.PreferredRoot)
	}

	return nil
}

func canonicalCommandDir(pathValue, label string) (string, error) {
	absPath, err := filepath.Abs(pathValue)
	if err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to resolve "+label, err)
	}
	if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolvedPath, nil
	}
	return absPath, nil
}

func registeredRepoRootFromStore(st *store.Store, repoID string) (string, error) {
	rec, exists, err := st.LoadRepoRecord(repoID)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", errors.NewWithDetails(
			errors.ERepoNotFound,
			"repo is not registered",
			map[string]string{"hint": "run `agency repo add <path>` first"},
		)
	}
	for _, root := range []string{rec.PreferredRoot, rec.RepoRootLastSeen} {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		resolved, err := canonicalCommandDir(root, "registered repo root")
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err == nil && info.IsDir() {
			return resolved, nil
		}
	}
	return "", errors.NewWithDetails(
		errors.ERepoRootInaccessible,
		"registered repo root is not accessible",
		map[string]string{"hint": "run `agency repo add <path>` from an accessible checkout"},
	)
}

func ensureAccessibleRepo(repo daemon.RepoDTO, repoRef string) (daemon.RepoDTO, error) {
	if repo.PreferredRoot != "" && repo.PreferredRootAccessible {
		return repo, nil
	}
	if strings.TrimSpace(repoRef) == "" {
		repoRef = repo.RepoID
	}
	return daemon.RepoDTO{}, errors.NewWithDetails(
		errors.ERepoRootInaccessible,
		"repo preferred_root is not accessible",
		map[string]string{
			"repo": repoRef,
			"hint": "re-register this repo from an accessible checkout, then re-run the command",
		},
	)
}

func resolveAccessibleRepo(ctx context.Context, client *daemonclient.Client, repoRef string) (daemon.RepoDTO, error) {
	repo, err := client.GetRepo(ctx, repoRef)
	if err != nil {
		return daemon.RepoDTO{}, err
	}
	return ensureAccessibleRepo(repo.Data, repoRef)
}

func resolveAccessibleRegisteredRepo(ctx context.Context, client *daemonclient.Client, root string) (daemon.RepoDTO, error) {
	repo, err := client.RegisterRepo(ctx, root)
	if err != nil {
		return daemon.RepoDTO{}, err
	}
	return ensureAccessibleRepo(daemon.RepoDTO{
		RepoID:                  repo.Data.RepoID,
		RepoName:                repo.Data.RepoName,
		RepoKey:                 repo.Data.RepoKey,
		Paths:                   repo.Data.Paths,
		PreferredRoot:           repo.Data.PreferredRoot,
		PreferredRootAccessible: repo.Data.PreferredRootAccessible,
		LastSeenAt:              repo.Data.LastSeenAt,
	}, repo.Data.RepoID)
}

func requirePresentWorktree(worktree daemon.WorktreeDTO, message string) (daemon.WorktreeDTO, error) {
	if worktree.State == "present" {
		return worktree, nil
	}
	if strings.TrimSpace(message) == "" {
		message = "worktree must be present"
	}
	return daemon.WorktreeDTO{}, errors.NewWithDetails(
		errors.EWorktreeNotFound,
		message,
		map[string]string{
			"hint": "pick a present worktree from `agency worktree ls`",
		},
	)
}

// RepoShowOpts holds options for the repo show command.
type RepoShowOpts struct {
	RepoRef string
	JSON    bool
}

// RepoRmOpts holds options for the repo rm command.
type RepoRmOpts struct {
	RepoRef        string
	Yes            bool
	JSON           bool
	IsInteractive  func() bool
	ConfirmationIn io.Reader
}

// RepoRm removes a registered repository from the daemon registry.
func RepoRm(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts RepoRmOpts, stdout, stderr io.Writer) error {
	_ = cr
	fail := commandFail(stdout, opts.JSON)

	if strings.TrimSpace(opts.RepoRef) == "" {
		return fail(errors.New(errors.EUsage, "repo_ref is required"))
	}

	if !opts.Yes {
		if !resolveIsInteractive(opts.IsInteractive, defaultIsInteractivePrompt)() {
			return fail(errors.NewWithDetails(
				errors.EConfirmationRequired,
				"non-interactive repo removal requires explicit confirmation",
				map[string]string{"hint": "re-run with --yes"},
			))
		}

		_, _ = fmt.Fprint(stderr, "confirm: type 'rm' to proceed: ")
		confirmationIn := opts.ConfirmationIn
		if confirmationIn == nil {
			confirmationIn = os.Stdin
		}
		line, err := bufio.NewReader(io.LimitReader(confirmationIn, maxConfirmationBytes+1)).ReadString('\n')
		if err != nil && err != io.EOF {
			return fail(errors.Wrap(errors.EInternal, "failed to read repo remove confirmation input", err))
		}
		if len(line) > maxConfirmationBytes {
			return fail(errors.NewWithDetails(
				errors.EInvalidArgument,
				"confirmation input exceeds maximum length",
				map[string]string{"hint": "type 'rm' exactly"},
			))
		}
		if strings.TrimSpace(line) != "rm" {
			return fail(errors.New(errors.EAborted, "repo remove confirmation failed; expected 'rm'"))
		}
	}

	client, err := ensureDaemonClient(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	result, err := client.RepoRm(ctx, opts.RepoRef)
	if err != nil {
		return fail(err)
	}

	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			RepoID           string `json:"repo_id,omitempty"`
			RepoKey          string `json:"repo_key,omitempty"`
			RemovedFromIndex bool   `json:"removed_from_index,omitempty"`
		}{
			commandJSONBase:  newCommandJSONSuccess(0, "", "", result.RequestID),
			RepoID:           result.Data.RepoID,
			RepoKey:          result.Data.RepoKey,
			RemovedFromIndex: result.Data.RemovedFromIndex,
		})
	}

	repoLabel := result.Data.RepoID
	if strings.TrimSpace(result.Data.RepoName) != "" {
		repoLabel = result.Data.RepoName + " (" + result.Data.RepoID + ")"
	}
	_, _ = fmt.Fprintf(stdout, "Removed repository %s\n", repoLabel)
	return nil
}

// RepoShow shows details for a registered repository.
func RepoShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts RepoShowOpts, stdout, stderr io.Writer) error {
	client, err := ensureDaemonClient(ctx, fsys, "")
	if err != nil {
		return err
	}

	result, err := client.GetRepo(ctx, opts.RepoRef)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeCommandJSON(stdout, result.Data)
	}

	r := result.Data
	repoLabel := namedLabel(r.RepoName, r.RepoID)
	_, _ = fmt.Fprintf(stdout, "repo:           %s\n", repoLabel)
	_, _ = fmt.Fprintf(stdout, "repo_key:       %s\n", r.RepoKey)
	_, _ = fmt.Fprintf(stdout, "preferred_root: %s\n", r.PreferredRoot)
	accessible := "yes"
	if !r.PreferredRootAccessible {
		accessible = "no"
	}
	_, _ = fmt.Fprintf(stdout, "accessible:     %s\n", accessible)
	if r.Origin != nil && r.Origin.Present {
		_, _ = fmt.Fprintf(stdout, "origin_url:     %s\n", r.Origin.URL)
		_, _ = fmt.Fprintf(stdout, "origin_host:    %s\n", r.Origin.Host)
	}
	_, _ = fmt.Fprintf(stdout, "last_seen_at:   %s\n", r.LastSeenAt)
	if len(r.Paths) > 0 {
		_, _ = fmt.Fprintf(stdout, "paths:\n")
		for _, p := range r.Paths {
			_, _ = fmt.Fprintf(stdout, "  - %s\n", p)
		}
	}

	return nil
}

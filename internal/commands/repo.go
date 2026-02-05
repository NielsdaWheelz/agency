// Package commands implements agency CLI commands.
// This file implements repo registry commands (PR-A / PR-14).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/paths"
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
	// RepoFlag is the value of the --repo flag (repo_id or unique prefix).
	RepoFlag string
	// AllRepos is the value of the --all-repos flag (list commands only).
	AllRepos bool
	// AllowAllRepos controls whether --all-repos is accepted.
	// Must be true for list commands, false for single-ref commands.
	AllowAllRepos bool
	// CmdName is used in error messages ("agency agent show", etc.).
	CmdName string
}

// ResolveRepoViaClient resolves the repo context for a CLI command.
// It implements the PR-A auto-registration flow:
//   - If --repo flag is set, use it directly.
//   - If --all-repos is set (list commands only), return AllRepos=true.
//   - If CWD is inside a git repo, auto-register via daemon and return repo_id.
//   - Otherwise, return a helpful error with hints.
func ResolveRepoViaClient(ctx context.Context, cr exec.CommandRunner, client *daemonclient.Client, cwd string, opts ResolveRepoContextOpts) (*RepoContextResult, error) {
	// Mutual exclusion
	if opts.RepoFlag != "" && opts.AllRepos {
		return nil, errors.New(errors.EUsage, "--repo and --all-repos are mutually exclusive")
	}

	// --all-repos only for list commands
	if opts.AllRepos && !opts.AllowAllRepos {
		return nil, errors.New(errors.EUsage, "--all-repos is not supported for "+opts.CmdName+"; specify --repo instead")
	}

	// Explicit --repo flag: use directly
	if opts.RepoFlag != "" {
		return &RepoContextResult{RepoID: opts.RepoFlag}, nil
	}

	// --all-repos: return empty repo_id (list globally)
	if opts.AllRepos {
		return &RepoContextResult{AllRepos: true}, nil
	}

	// Try CWD-based auto-registration
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		// Not in a repo — error with hints
		if opts.AllowAllRepos {
			return nil, errors.NewWithDetails(
				errors.ENoRepoContext,
				"no repo context (not in a git repo)",
				map[string]string{
					"hint": "run \"agency repo ls\" then re-run with --repo <repo_id>, or pass --all-repos, or register a repo with \"agency repo add /path/to/repo\"",
				},
			)
		}
		return nil, errors.NewWithDetails(
			errors.ENoRepoContext,
			fmt.Sprintf("cannot resolve %s without a repo context", opts.CmdName),
			map[string]string{
				"hint": "run \"agency repo ls\" and re-run with \"--repo <repo_id>\", or register a repo: \"agency repo add /path/to/repo\"",
			},
		)
	}

	// Auto-register via daemon
	result, err := client.RegisterRepo(ctx, repoRoot.Path)
	if err != nil {
		return nil, err
	}

	return &RepoContextResult{RepoID: result.RepoID}, nil
}

// RepoAddOpts holds options for the repo add command.
type RepoAddOpts struct {
	Path string
	JSON bool
}

// RepoAdd registers a repository with the daemon.
func RepoAdd(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts RepoAddOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Ensure daemon is running
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	result, err := client.RegisterRepo(ctx, opts.Path)
	if err != nil {
		return err
	}

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	_, _ = fmt.Fprintf(stdout, "Registered repo\n")
	_, _ = fmt.Fprintf(stdout, "  repo_id:        %s\n", result.RepoID)
	_, _ = fmt.Fprintf(stdout, "  repo_key:       %s\n", result.RepoKey)
	_, _ = fmt.Fprintf(stdout, "  preferred_root: %s\n", result.PreferredRoot)
	if len(result.Paths) > 1 {
		_, _ = fmt.Fprintf(stdout, "  paths:          %d registered\n", len(result.Paths))
	}

	return nil
}

// RepoLSOpts holds options for the repo ls command.
type RepoLSOpts struct {
	JSON bool
}

// RepoLS lists registered repositories.
func RepoLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts RepoLSOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Ensure daemon is running
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	result, err := client.ListRepos(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Repos)
	}

	if len(result.Repos) == 0 {
		_, _ = fmt.Fprintln(stdout, "No repos registered.")
		_, _ = fmt.Fprintln(stdout, "Register one with: agency repo add /path/to/repo")
		return nil
	}

	for _, r := range result.Repos {
		origin := r.RepoKey
		if r.Origin != nil && r.Origin.Present {
			origin = r.RepoKey
		}
		shortID := r.RepoID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		_, _ = fmt.Fprintf(stdout, "%s  %s  %s\n", shortID, origin, r.PreferredRoot)
	}

	return nil
}

// RepoShowOpts holds options for the repo show command.
type RepoShowOpts struct {
	RepoID string
	JSON   bool
}

// RepoShow shows details for a registered repository.
func RepoShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts RepoShowOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Ensure daemon is running
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	result, err := client.GetRepo(ctx, opts.RepoID)
	if err != nil {
		return err
	}

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Repo)
	}

	r := result.Repo
	_, _ = fmt.Fprintf(stdout, "repo_id:        %s\n", r.RepoID)
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

// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/ids"
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
	// RepoRef is the value of the --repo flag (name, owner/repo, repo key, id, or prefix).
	RepoRef string
	// AllRepos is the value of the --all-repos flag (list commands only).
	AllRepos bool
	// AllowAllRepos controls whether --all-repos is accepted.
	// Must be true for list commands, false for single-ref commands.
	AllowAllRepos bool
	// CmdName is used in error messages ("agency agent show", etc.).
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
		if client == nil {
			return nil, errors.New(errors.EInternal, "daemon client is required to resolve --repo")
		}

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
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
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
	Path string
	JSON bool
}

// RepoAdd registers a repository with the daemon.
func RepoAdd(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts RepoAddOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

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
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.RequestID = result.RequestID
			envelope.RepoID = result.Data.RepoID
			envelope.RepoKey = result.Data.RepoKey
			envelope.PreferredRoot = result.Data.PreferredRoot
		})
	}

	_, _ = fmt.Fprintf(stdout, "Registered repo\n")
	_, _ = fmt.Fprintf(stdout, "  repo_id:        %s\n", result.Data.RepoID)
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
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Data.Repos)
	}

	if len(result.Data.Repos) == 0 {
		_, _ = fmt.Fprintln(stdout, "No repos registered.")
		_, _ = fmt.Fprintln(stdout, "Register one with: agency repo add /path/to/repo")
		return nil
	}

	for _, r := range result.Data.Repos {
		// Show short name as primary column; fall back to truncated ID for path-based repos
		label := ids.RepoShortName(r.RepoKey)
		if label == "" {
			label = r.RepoID
		}
		if len(label) > 20 {
			label = label[:20]
		}
		_, _ = fmt.Fprintf(stdout, "%-20s %s  %s\n", label, r.RepoKey, r.PreferredRoot)
	}

	return nil
}

type daemonNavSetup struct {
	dirs   paths.Dirs
	client *daemonclient.Client
}

func resolveCommandDirs(dataDirOverride string) (paths.Dirs, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return paths.Dirs{}, errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	if dataDirOverride != "" {
		dirs.DataDir = dataDirOverride
	}
	return dirs, nil
}

func ensureDaemonClient(ctx context.Context, fsys fs.FS, dataDirOverride string) (*daemonclient.Client, error) {
	dirs, err := resolveCommandDirs(dataDirOverride)
	if err != nil {
		return nil, err
	}
	return ensureDaemonClientFromDirs(ctx, fsys, dirs)
}

func ensureDaemonClientFromDirs(ctx context.Context, fsys fs.FS, dirs paths.Dirs) (*daemonclient.Client, error) {
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return nil, err
	}
	if err := client.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func setupDaemonNav(ctx context.Context, fsys fs.FS, dataDirOverride string) (*daemonNavSetup, error) {
	dirs, err := resolveCommandDirs(dataDirOverride)
	if err != nil {
		return nil, err
	}
	client, err := ensureDaemonClientFromDirs(ctx, fsys, dirs)
	if err != nil {
		return nil, err
	}
	return &daemonNavSetup{dirs: dirs, client: client}, nil
}

func runAttachedInDir(ctx context.Context, command string, args []string, dir string) (exec.CmdResult, error) {
	return exec.RunAttached(ctx, command, args, exec.AttachedRunOpts{
		Dir:    dir,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
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
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	if strings.TrimSpace(opts.RepoRef) == "" {
		return fail(errors.New(errors.EUsage, "repo_ref is required"))
	}

	if !opts.Yes {
		isInteractive := opts.IsInteractive
		if isInteractive == nil {
			isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stderr.Fd()) }
		}
		if !isInteractive() {
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
		token, err := readBoundedMergeConfirmationToken(confirmationIn, maxMergeConfirmationBytes)
		if err != nil {
			return fail(err)
		}
		if token != "rm" {
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
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.RequestID = result.RequestID
			envelope.RepoID = result.Data.RepoID
			envelope.RepoKey = result.Data.RepoKey
			envelope.RemovedFromIndex = result.Data.RemovedFromIndex
		})
	}

	_, _ = fmt.Fprintf(stdout, "Removed repository %s\n", result.Data.RepoID)
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
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Data)
	}

	r := result.Data
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

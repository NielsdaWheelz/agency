// Package cli handles command-line parsing and dispatch for agency.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const usageText = `agency - local-first runner manager for AI coding sessions

usage: agency [--verbose] <command> [options]

commands:
  init        create agency.json template and stub scripts
  doctor      check prerequisites and show resolved paths
  run         create workspace, setup, and start tmux runner session
  ls          list runs and their statuses
  show        show run details
  path        output worktree path for a run (for scripting)
  open        open run worktree in editor
  attach      attach to a tmux session for an existing run
  resume      attach to tmux session (create if missing)
  stop        send C-c to runner (best-effort interrupt)
  kill        kill tmux session (workspace remains)
  push        push branch to origin and create/update GitHub PR
  verify      run scripts.verify and record results
  merge       verify, confirm, merge PR, and archive workspace
  clean       archive without merging (abandon run)
  completion  generate shell completion scripts

global options:
  --verbose   show detailed error context
  -h, --help      show this help
  -v, --version   show version

run 'agency <command> --help' for command-specific help.
`

const initUsageText = `usage: agency init [options]

create agency.json template and stub scripts in a repo.
defaults to current directory; use --repo to target a different repo.

options:
  --repo <path>    target a specific repo (default: current directory)
  --no-gitignore   do not modify .gitignore
  --force          overwrite existing agency.json
  -h, --help       show this help

examples:
  agency init
  agency init --repo /path/to/repo
  agency init --force
`

const doctorUsageText = `usage: agency doctor [options]

check prerequisites and show resolved paths.
verifies git, tmux, gh, runner command, and scripts are present and configured.
defaults to current directory; use --repo to target a different repo.

options:
  --repo <path>   target a specific repo (default: current directory)
  -h, --help      show this help

examples:
  agency doctor
  agency doctor --repo /path/to/repo
`

const runUsageText = `usage: agency run --name <name> [options]

create workspace, run setup, and start tmux runner session.
defaults to current directory; use --repo to target a different repo.
requires the target repo to have agency.json.
by default, attaches to the tmux session after creation.

options:
  --name <string>     run name (required, 2-40 chars, lowercase alphanumeric with hyphens)
  --repo <path>       target a specific repo (default: current directory)
  --runner <name>     runner name: claude or codex (default: user config defaults.runner)
  --parent <branch>   parent branch (default: current branch)
  --detached          do not attach to tmux session after creation
  -h, --help          show this help

examples:
  agency run --name my-feature
  agency run --name fix-bug-123 --runner claude
  agency run --name refactor-auth --detached
  agency run --name feature-x --repo /path/to/repo
`

const attachUsageText = `usage: agency attach <run> [options]

attach to the tmux session for an existing run.
works from any directory; resolves runs globally.

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --repo <path>   scope name resolution to a specific repo
  -h, --help      show this help

resolution:
  - run_id and prefix resolution is always global
  - name resolution prefers current repo (if inside one)
  - use --repo to disambiguate when names conflict across repos

examples:
  agency attach my-feature
  agency attach 20260110120000-a3f2
  agency attach my-feature --repo /path/to/repo
`

const stopUsageText = `usage: agency stop <run> [options]

send C-c to the runner in the tmux session (best-effort interrupt).
sets needs_attention flag on the run.
works from any directory; resolves runs globally.

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --repo <path>   scope name resolution to a specific repo
  -h, --help      show this help

notes:
  - best-effort only; may not stop the runner if it is in the middle of an operation
  - session remains alive; use 'agency resume --restart' to guarantee a fresh runner

examples:
  agency stop my-feature
  agency stop 20260110120000-a3f2
  agency stop my-feature --repo /path/to/repo
`

const killUsageText = `usage: agency kill <run> [options]

kill the tmux session for a run.
workspace remains intact.
works from any directory; resolves runs globally.

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --repo <path>   scope name resolution to a specific repo
  -h, --help      show this help

examples:
  agency kill my-feature
  agency kill 20260110120000-a3f2
  agency kill my-feature --repo /path/to/repo
`

const resumeUsageText = `usage: agency resume <run> [options]

attach to the tmux session for a run.
if session is missing, creates one and starts the runner.
works from any directory; resolves runs globally.

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --repo <path>   scope name resolution to a specific repo
  --detached      do not attach; return after ensuring session exists
  --restart       kill existing session (if any) and recreate
  --yes           skip confirmation prompt for --restart
  -h, --help      show this help

notes:
  - resume never runs scripts (setup/verify/archive)
  - resume preserves git state; only tmux session changes
  - --restart will lose in-tool history (chat context, etc.)

examples:
  agency resume my-feature                    # attach (create if needed)
  agency resume my-feature --detached         # ensure session exists
  agency resume my-feature --restart          # force fresh runner
  agency resume my-feature --restart --yes    # non-interactive restart
  agency resume my-feature --repo /path/to/repo
`

const pushUsageText = `usage: agency push <run> [options]

push the run branch to origin.
creates/updates GitHub PR in future PRs (slice 3 PR-03).

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --allow-dirty  allow push even if worktree has uncommitted changes
  --force       proceed even if .agency/report.md is missing/empty
  -h, --help    show this help

notes:
  - requires origin to be a github.com remote
  - requires gh to be authenticated
  - does NOT bypass E_EMPTY_DIFF (at least one commit required)
  - fails if worktree has uncommitted changes unless --allow-dirty

examples:
  agency push my-feature               # push branch
  agency push my-feature --force       # push with empty report
  agency push my-feature --allow-dirty # push with dirty worktree
`

const verifyUsageText = `usage: agency verify <run> [options]

run the repo's scripts.verify for a run and record results.
works from any directory; resolves runs globally.

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --repo <path>     scope name resolution to a specific repo
  --timeout <dur>   script timeout override (Go duration format, e.g., '30m')
                    defaults to timeout in agency.json scripts.verify.timeout
  -h, --help        show this help

behavior:
  - writes verify_record.json and verify.log
  - updates run flags (needs_attention on failure)
  - does NOT affect push or merge behavior

examples:
  agency verify my-feature                 # run verify
  agency verify my-feature --timeout 10m
  agency verify my-feature --repo /path/to/repo
`

const mergeUsageText = `usage: agency merge <run> [options]

verify, confirm, merge PR, and archive workspace.
requires cwd to be inside the target repo.
requires an interactive terminal for confirmation.

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --squash           use squash merge strategy (default)
  --merge            use regular merge strategy
  --rebase           use rebase merge strategy
  --no-delete-branch preserve the remote branch after merge (default: delete)
  --allow-dirty      allow merge even if worktree has uncommitted changes
  --force            bypass verify-failed prompt (still runs verify)
  -h, --help         show this help

behavior:
  1. runs prechecks (origin, gh auth, PR exists, mergeable, etc.)
  2. runs scripts.verify (timeout: 30m)
  3. if verify fails: prompts to continue (unless --force)
  4. prompts for typed confirmation (must type 'merge')
  5. merges PR via gh pr merge --delete-branch (unless --no-delete-branch)
  6. archives workspace (runs archive script, kills tmux, deletes worktree)

notes:
  - PR must already exist (run 'agency push' first)
  - --force does NOT bypass: missing PR, non-mergeable PR, gh auth failure
  - at most one of --squash/--merge/--rebase may be set
  - by default, the remote branch is deleted after merge

examples:
  agency merge my-feature                      # squash merge, delete branch
  agency merge my-feature --merge              # regular merge, delete branch
  agency merge my-feature --no-delete-branch   # preserve remote branch
  agency merge my-feature --force              # skip verify-fail prompt
`

const cleanUsageText = `usage: agency clean <run> [options]

archive a run without merging (abandon).
works from any directory; resolves runs globally.
requires an interactive terminal for confirmation.

arguments:
  run           run name, run_id, or unique run_id prefix

behavior:
  - runs scripts.archive (timeout: 5m)
  - kills tmux session if exists
  - deletes worktree
  - retains metadata and logs
  - marks run as abandoned

  with --delete-branch:
  - deletes local git branch
  - deletes remote branch (if pushed)
  - closes PR (if exists)

confirmation:
  you must type 'clean' to confirm the operation.

options:
  --repo <path>     scope name resolution to a specific repo
  --allow-dirty     allow clean even if worktree has uncommitted changes
  --delete-branch   delete local/remote branch and close PR
  -h, --help        show this help

examples:
  agency clean my-feature
  agency clean my-feature --delete-branch
  agency clean my-feature --allow-dirty
  agency clean my-feature --repo /path/to/repo
`

const lsUsageText = `usage: agency ls [options]

list runs and their statuses.
by default, lists runs for the current repo (excludes archived).
if not inside a git repo, lists runs across all repos.

options:
  --repo <path>   scope listing to a specific repo (alternative to CWD scoping)
  --all           include archived runs
  --all-repos     list runs across all repos (ignores current repo scope)
  --json          output as JSON (stable format)
  -h, --help      show this help

examples:
  agency ls                    # list current repo runs
  agency ls --all              # include archived runs
  agency ls --all-repos        # list all repos
  agency ls --json             # machine-readable output
  agency ls --repo /path/to/repo
`

const showUsageText = `usage: agency show <run> [options]

show details for a single run.
resolves globally (works from anywhere, not just inside a repo).

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --json          output as JSON (stable format)
  --path          output only resolved filesystem paths
  --capture       capture tmux scrollback to transcript files (mutating mode)
  -h, --help      show this help

examples:
  agency show my-feature               # show run details by name
  agency show 20260110120000-a3f2      # show by run_id
  agency show my-feature --json        # machine-readable output
  agency show my-feature --path        # print paths only
  agency show my-feature --capture     # capture transcript + show details
`

const openUsageText = `usage: agency open <run> [options]

open a run worktree in your editor.
resolves globally (works from anywhere, not just inside a repo).

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  --editor <name>   editor name (default: user config defaults.editor)
  -h, --help        show this help

examples:
  agency open my-feature
  agency open 20260110120000-a3f2
  agency open my-feature --editor code
`

const pathUsageText = `usage: agency path <run>

output the worktree path for a run (single line, for scripting).
resolves globally (works from anywhere, not just inside a repo).

arguments:
  run           run name, run_id, or unique run_id prefix

options:
  -h, --help    show this help

examples:
  agency path my-feature
  agency path 20260110120000-a3f2

shell integration:
  # add to your .bashrc or .zshrc:
  acd() { cd "$(agency path "$1")" || return 1; }

  # then use:
  acd my-feature
`

const completionUsageText = `usage: agency completion <shell>

generate shell completion scripts.
prints the script to stdout; does not write files.

arguments:
  shell         target shell: bash or zsh

options:
  -h, --help    show this help

installation:

  bash (with bash-completion package):
    agency completion bash > ~/.local/share/bash-completion/completions/agency

  bash (manual):
    agency completion bash > ~/.agency-completion.bash
    echo 'source ~/.agency-completion.bash' >> ~/.bashrc

  zsh (with fpath):
    agency completion zsh > ~/.zsh/completions/_agency
    # ensure ~/.zsh/completions is in fpath before compinit

  zsh (manual):
    agency completion zsh > ~/.agency-completion.zsh
    echo 'source ~/.agency-completion.zsh' >> ~/.zshrc

after installation, restart your shell.

examples:
  agency completion bash
  agency completion zsh
`

// GlobalOpts holds global options parsed before subcommand dispatch.
type GlobalOpts struct {
	Verbose bool
}

// globalOpts stores the parsed global options for access by subcommands.
var globalOpts GlobalOpts

// GetGlobalOpts returns the parsed global options.
func GetGlobalOpts() GlobalOpts {
	return globalOpts
}

// Run parses arguments and dispatches to the appropriate subcommand.
// Returns an error if the command fails; the caller should print the error and exit.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usageText)
		return errors.New(errors.EUsage, "no command specified")
	}

	// Handle global -h/--help and -v/--version BEFORE flag parsing
	// These are special cases that should work even without a subcommand
	if args[0] == "-h" || args[0] == "--help" {
		_, _ = fmt.Fprint(stdout, usageText)
		return nil
	}
	if args[0] == "-v" || args[0] == "--version" {
		_, _ = fmt.Fprintf(stdout, "agency %s\n", version.Version)
		return nil
	}

	// Parse global flags before subcommand
	// Per spec: --verbose is a global flag that works regardless of position before subcommand
	globalFlags := flag.NewFlagSet("agency", flag.ContinueOnError)
	globalFlags.SetOutput(io.Discard)
	verbose := globalFlags.Bool("verbose", false, "show detailed error context")

	// Parse global flags - stop at first non-flag argument
	// Errors are expected when args[0] is a command name, not a flag.
	// We intentionally ignore parse errors here and fall through to normal command handling.
	_ = globalFlags.Parse(args)

	// Store parsed global options
	globalOpts.Verbose = *verbose

	// Get remaining args after global flags
	subArgs := globalFlags.Args()
	if len(subArgs) == 0 {
		_, _ = fmt.Fprint(stderr, usageText)
		return errors.New(errors.EUsage, "no command specified")
	}

	cmd := subArgs[0]
	cmdArgs := subArgs[1:]

	switch cmd {
	case "init":
		return runInit(cmdArgs, stdout, stderr)
	case "doctor":
		return runDoctor(cmdArgs, stdout, stderr)
	case "run":
		return runRun(cmdArgs, stdout, stderr)
	case "ls":
		return runLS(cmdArgs, stdout, stderr)
	case "show":
		return runShow(cmdArgs, stdout, stderr)
	case "path":
		return runPath(cmdArgs, stdout, stderr)
	case "open":
		return runOpen(cmdArgs, stdout, stderr)
	case "attach":
		return runAttach(cmdArgs, stdout, stderr)
	case "stop":
		return runStop(cmdArgs, stdout, stderr)
	case "kill":
		return runKill(cmdArgs, stdout, stderr)
	case "resume":
		return runResume(cmdArgs, stdout, stderr)
	case "push":
		return runPush(cmdArgs, stdout, stderr)
	case "verify":
		return runVerify(cmdArgs, stdout, stderr)
	case "merge":
		return runMerge(cmdArgs, stdout, stderr)
	case "clean":
		return runClean(cmdArgs, stdout, stderr)
	case "completion":
		return runCompletion(cmdArgs, stdout, stderr)
	case "__complete":
		return runComplete(cmdArgs, stdout, stderr)
	default:
		_, _ = fmt.Fprint(stderr, usageText)
		return errors.New(errors.EUsage, fmt.Sprintf("unknown command: %s", cmd))
	}
}

func runInit(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("init", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "target a specific repo")
	noGitignore := flagSet.Bool("no-gitignore", false, "do not modify .gitignore")
	force := flagSet.Bool("force", false, "overwrite existing agency.json")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, initUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.InitOpts{
		RepoPath:    *repoPath,
		NoGitignore: *noGitignore,
		Force:       *force,
	}

	return commands.Init(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "target a specific repo")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, doctorUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.DoctorOpts{
		RepoPath: *repoPath,
	}

	return commands.Doctor(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runRun(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("run", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	name := flagSet.String("name", "", "run name (required)")
	repoPath := flagSet.String("repo", "", "target a specific repo")
	runner := flagSet.String("runner", "", "runner name (claude or codex)")
	parent := flagSet.String("parent", "", "parent branch")
	detached := flagSet.Bool("detached", false, "do not attach to tmux session after creation")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, runUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// --name is required
	if *name == "" {
		_, _ = fmt.Fprint(stderr, runUsageText)
		return errors.New(errors.EUsage, "--name is required")
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.RunOpts{
		Name:     *name,
		RepoPath: *repoPath,
		Runner:   *runner,
		Parent:   *parent,
		Attach:   !*detached,
	}

	return commands.Run(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runLS(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("ls", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "scope listing to a specific repo")
	all := flagSet.Bool("all", false, "include archived runs")
	allRepos := flagSet.Bool("all-repos", false, "list runs across all repos")
	jsonOutput := flagSet.Bool("json", false, "output as JSON")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, lsUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.LSOpts{
		RepoPath: *repoPath,
		All:      *all,
		AllRepos: *allRepos,
		JSON:     *jsonOutput,
	}

	return commands.LS(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runShow(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("show", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	jsonOutput := flagSet.Bool("json", false, "output as JSON")
	pathOutput := flagSet.Bool("path", false, "output only resolved paths")
	capture := flagSet.Bool("capture", false, "capture tmux scrollback to transcript files")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, showUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, showUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.ShowOpts{
		RunID:   runID,
		JSON:    *jsonOutput,
		Path:    *pathOutput,
		Capture: *capture,
		Args:    args,
	}

	return commands.Show(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runOpen(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("open", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	editor := flagSet.String("editor", "", "editor name")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, openUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, openUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.OpenOpts{
		RunID:  runID,
		Editor: *editor,
	}

	return commands.Open(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runPath(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("path", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, pathUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run reference is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, pathUsageText)
		return errors.New(errors.EUsage, "run reference is required")
	}
	runRef := positionalArgs[0]

	ctx := context.Background()

	opts := commands.PathOpts{
		RunRef: runRef,
	}

	return commands.Path(ctx, opts, stdout, stderr)
}

func runAttach(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("attach", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "scope name resolution to a specific repo")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, attachUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, attachUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.AttachOpts{
		RunID:    runID,
		RepoPath: *repoPath,
	}

	err = commands.Attach(ctx, cr, fsys, cwd, opts, stdout, stderr)
	if err != nil {
		// Print helpful details for E_SESSION_NOT_FOUND
		if ae, ok := errors.AsAgencyError(err); ok && ae.Code == errors.ESessionNotFound {
			if ae.Details != nil {
				if suggestion := ae.Details["suggestion"]; suggestion != "" {
					_, _ = fmt.Fprintf(stderr, "\n%s\n", suggestion)
				}
			}
		}
	}
	return err
}

func runStop(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("stop", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "scope name resolution to a specific repo")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, stopUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, stopUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.StopOpts{
		RunID:    runID,
		RepoPath: *repoPath,
	}

	return commands.Stop(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runKill(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("kill", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "scope name resolution to a specific repo")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, killUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, killUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.KillOpts{
		RunID:    runID,
		RepoPath: *repoPath,
	}

	return commands.Kill(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runResume(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("resume", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "scope name resolution to a specific repo")
	detached := flagSet.Bool("detached", false, "do not attach; return after ensuring session exists")
	restart := flagSet.Bool("restart", false, "kill existing session (if any) and recreate")
	yes := flagSet.Bool("yes", false, "skip confirmation prompt for --restart")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, resumeUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, resumeUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.ResumeOpts{
		RunID:    runID,
		RepoPath: *repoPath,
		Detached: *detached,
		Restart:  *restart,
		Yes:      *yes,
	}

	return commands.Resume(ctx, cr, fsys, cwd, opts, os.Stdin, stdout, stderr)
}

func runPush(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("push", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	allowDirty := flagSet.Bool("allow-dirty", false, "allow push even if worktree has uncommitted changes")
	force := flagSet.Bool("force", false, "proceed even if report is missing/empty")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, pushUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, pushUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.PushOpts{
		RunID:      runID,
		Force:      *force,
		AllowDirty: *allowDirty,
	}

	return commands.Push(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runVerify(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("verify", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "scope name resolution to a specific repo")
	timeoutStr := flagSet.String("timeout", "", "script timeout override (Go duration format, e.g., '30m'); defaults to agency.json config")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, verifyUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, verifyUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Parse timeout: if empty, use 0 (service will use config default)
	var timeout time.Duration
	if *timeoutStr != "" {
		var err error
		timeout, err = time.ParseDuration(*timeoutStr)
		if err != nil {
			_, _ = fmt.Fprint(stderr, verifyUsageText)
			return errors.New(errors.EUsage, fmt.Sprintf("invalid timeout: %s", *timeoutStr))
		}
		if timeout <= 0 {
			_, _ = fmt.Fprint(stderr, verifyUsageText)
			return errors.New(errors.EUsage, "timeout must be positive")
		}
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()

	// Set up cancellation context for user SIGINT
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT for cancellation
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		<-sigCh
		cancel()
	}()

	opts := commands.VerifyOpts{
		RunID:    runID,
		RepoPath: *repoPath,
		Timeout:  timeout,
	}

	return commands.Verify(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runMerge(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("merge", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	squash := flagSet.Bool("squash", false, "use squash merge strategy (default)")
	merge := flagSet.Bool("merge", false, "use regular merge strategy")
	rebase := flagSet.Bool("rebase", false, "use rebase merge strategy")
	noDeleteBranch := flagSet.Bool("no-delete-branch", false, "preserve remote branch after merge")
	allowDirty := flagSet.Bool("allow-dirty", false, "allow merge even if worktree has uncommitted changes")
	force := flagSet.Bool("force", false, "bypass verify-failed prompt")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, mergeUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, mergeUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Validate merge strategy flags (at most one)
	strategyCount := 0
	var strategy commands.MergeStrategy
	if *squash {
		strategyCount++
		strategy = commands.MergeStrategySquash
	}
	if *merge {
		strategyCount++
		strategy = commands.MergeStrategyMerge
	}
	if *rebase {
		strategyCount++
		strategy = commands.MergeStrategyRebase
	}

	if strategyCount > 1 {
		return errors.New(errors.EUsage, "at most one of --squash, --merge, --rebase may be specified")
	}
	if strategyCount == 0 {
		strategy = commands.MergeStrategySquash // default
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.ENoRepo, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.MergeOpts{
		RunID:          runID,
		Strategy:       strategy,
		Force:          *force,
		AllowDirty:     *allowDirty,
		NoDeleteBranch: *noDeleteBranch,
	}

	return commands.Merge(ctx, cr, fsys, cwd, opts, os.Stdin, stdout, stderr)
}

func runClean(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("clean", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	repoPath := flagSet.String("repo", "", "scope name resolution to a specific repo")
	allowDirty := flagSet.Bool("allow-dirty", false, "allow clean even if worktree has uncommitted changes")
	deleteBranch := flagSet.Bool("delete-branch", false, "delete local and remote branch, close PR")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, cleanUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		_, _ = fmt.Fprint(stderr, cleanUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.CleanOpts{
		RunID:        runID,
		RepoPath:     *repoPath,
		AllowDirty:   *allowDirty,
		DeleteBranch: *deleteBranch,
	}

	return commands.Clean(ctx, cr, fsys, cwd, opts, os.Stdin, stdout, stderr)
}

func runCompletion(args []string, stdout, stderr io.Writer) error {
	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			_, _ = fmt.Fprint(stdout, completionUsageText)
			return nil
		}
	}

	// Shell is a required positional argument
	if len(args) < 1 {
		_, _ = fmt.Fprint(stderr, completionUsageText)
		return errors.New(errors.EUsage, "shell is required (bash or zsh)")
	}
	shell := args[0]

	ctx := context.Background()
	opts := commands.CompletionOpts{
		Shell: shell,
	}

	return commands.Completion(ctx, opts, stdout, stderr)
}

func runComplete(args []string, stdout, stderr io.Writer) error {
	// __complete is a hidden command for shell completion scripts.
	// Usage: agency __complete <kind> [--all-repos] [--include-archived]
	// Kinds: commands, runs, runners, merge_strategies
	//
	// Output: newline-separated candidates
	// Error handling: silent failure (print nothing, exit 0) unless AGENCY_DEBUG_COMPLETION=1

	flagSet := flag.NewFlagSet("__complete", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	allRepos := flagSet.Bool("all-repos", false, "include runs from all repos")
	includeArchived := flagSet.Bool("include-archived", false, "include archived runs")

	if err := flagSet.Parse(args); err != nil {
		// Silent failure for shell UX
		return nil
	}

	// Kind is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		// Silent failure for shell UX
		return nil
	}
	kind := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		// Silent failure for shell UX
		return nil
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.CompleteOpts{
		Kind:            commands.CompleteKind(kind),
		AllRepos:        *allRepos,
		IncludeArchived: *includeArchived,
	}

	return commands.Complete(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

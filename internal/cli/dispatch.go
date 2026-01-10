// Package cli handles command-line parsing and dispatch for agency.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const usageText = `agency - local-first runner manager for AI coding sessions

usage: agency <command> [options]

commands:
  init        create agency.json template and stub scripts
  doctor      check prerequisites and show resolved paths
  run         create workspace, setup, and start tmux runner session
  ls          list runs and their statuses
  show        show run details
  attach      attach to a tmux session for an existing run

options:
  -h, --help      show this help
  -v, --version   show version

run 'agency <command> --help' for command-specific help.
`

const initUsageText = `usage: agency init [options]

create agency.json template and stub scripts in the current repo.

options:
  --no-gitignore   do not modify .gitignore
  --force          overwrite existing agency.json
  -h, --help       show this help
`

const doctorUsageText = `usage: agency doctor

check prerequisites and show resolved paths.
verifies git, tmux, gh, runner command, and scripts are present and configured.

options:
  -h, --help    show this help
`

const runUsageText = `usage: agency run [options]

create workspace, run setup, and start tmux runner session.
requires cwd to be inside a git repo with agency.json.

options:
  --title <string>    run title (default: untitled-<shortid>)
  --runner <name>     runner name: claude or codex (default: agency.json defaults.runner)
  --parent <branch>   parent branch (default: agency.json defaults.parent_branch)
  --attach            attach to tmux session immediately after creation
  -h, --help          show this help

examples:
  agency run --title "implement feature X" --runner claude
  agency run --attach
  agency run --parent develop
`

const attachUsageText = `usage: agency attach <run_id>

attach to the tmux session for an existing run.
requires cwd to be inside the target repo.

arguments:
  run_id        the run identifier (e.g., 20260110120000-a3f2)

options:
  -h, --help    show this help

examples:
  agency attach 20260110120000-a3f2
`

const lsUsageText = `usage: agency ls [options]

list runs and their statuses.
by default, lists runs for the current repo (excludes archived).
if not inside a git repo, lists runs across all repos.

options:
  --all           include archived runs
  --all-repos     list runs across all repos (ignores current repo scope)
  --json          output as JSON (stable format)
  -h, --help      show this help

examples:
  agency ls                    # list current repo runs
  agency ls --all              # include archived runs
  agency ls --all-repos        # list all repos
  agency ls --json             # machine-readable output
`

const showUsageText = `usage: agency show <run_id> [options]

show details for a single run.
resolves run_id globally (works from anywhere, not just inside a repo).
accepts exact run_id or unique prefix.

arguments:
  run_id        the run identifier or unique prefix

options:
  --json          output as JSON (stable format)
  --path          output only resolved filesystem paths
  -h, --help      show this help

examples:
  agency show 20260110120000-a3f2           # show run details
  agency show 20260110                       # unique prefix resolution
  agency show 20260110120000-a3f2 --json    # machine-readable output
  agency show 20260110120000-a3f2 --path    # print paths only
`

// Run parses arguments and dispatches to the appropriate subcommand.
// Returns an error if the command fails; the caller should print the error and exit.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usageText)
		return errors.New(errors.EUsage, "no command specified")
	}

	cmd := args[0]
	cmdArgs := args[1:]

	// Handle global flags
	if cmd == "-h" || cmd == "--help" {
		fmt.Fprint(stdout, usageText)
		return nil
	}
	if cmd == "-v" || cmd == "--version" {
		fmt.Fprintf(stdout, "agency %s\n", version.Version)
		return nil
	}

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
	case "attach":
		return runAttach(cmdArgs, stdout, stderr)
	default:
		fmt.Fprint(stdout, usageText)
		return errors.New(errors.EUsage, fmt.Sprintf("unknown command: %s", cmd))
	}
}

func runInit(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("init", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	noGitignore := flagSet.Bool("no-gitignore", false, "do not modify .gitignore")
	force := flagSet.Bool("force", false, "overwrite existing agency.json")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, initUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
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

	opts := commands.InitOpts{
		NoGitignore: *noGitignore,
		Force:       *force,
	}

	return commands.Init(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, doctorUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
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

	return commands.Doctor(ctx, cr, fsys, cwd, stdout, stderr)
}

func runRun(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("run", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	title := flagSet.String("title", "", "run title")
	runner := flagSet.String("runner", "", "runner name (claude or codex)")
	parent := flagSet.String("parent", "", "parent branch")
	attach := flagSet.Bool("attach", false, "attach to tmux session immediately")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, runUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
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

	opts := commands.RunOpts{
		Title:  *title,
		Runner: *runner,
		Parent: *parent,
		Attach: *attach,
	}

	return commands.Run(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runLS(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("ls", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	all := flagSet.Bool("all", false, "include archived runs")
	allRepos := flagSet.Bool("all-repos", false, "list runs across all repos")
	jsonOutput := flagSet.Bool("json", false, "output as JSON")

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, lsUsageText)
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

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, showUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		fmt.Fprint(stderr, showUsageText)
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
		RunID: runID,
		JSON:  *jsonOutput,
		Path:  *pathOutput,
	}

	return commands.Show(ctx, cr, fsys, cwd, opts, stdout, stderr)
}

func runAttach(args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("attach", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	// Handle help manually to return nil (exit 0)
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, attachUsageText)
			return nil
		}
	}

	if err := flagSet.Parse(args); err != nil {
		return errors.Wrap(errors.EUsage, "invalid flags", err)
	}

	// run_id is a required positional argument
	positionalArgs := flagSet.Args()
	if len(positionalArgs) < 1 {
		fmt.Fprint(stderr, attachUsageText)
		return errors.New(errors.EUsage, "run_id is required")
	}
	runID := positionalArgs[0]

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.ENoRepo, "failed to get working directory", err)
	}

	// Create real implementations
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()

	opts := commands.AttachOpts{
		RunID: runID,
	}

	err = commands.Attach(ctx, cr, fsys, cwd, opts, stdout, stderr)
	if err != nil {
		// Print helpful details for E_TMUX_SESSION_MISSING
		if ae, ok := errors.AsAgencyError(err); ok && ae.Code == errors.ETmuxSessionMissing {
			if ae.Details != nil {
				fmt.Fprintln(stderr)
				if wp := ae.Details["worktree_path"]; wp != "" {
					fmt.Fprintf(stderr, "worktree_path: %s\n", wp)
				}
				if rc := ae.Details["runner_cmd"]; rc != "" {
					fmt.Fprintf(stderr, "runner_cmd: %s\n", rc)
				}
				if hint := ae.Details["hint"]; hint != "" {
					fmt.Fprintf(stderr, "\nto start the runner manually:\n  %s\n", hint)
				}
				fmt.Fprintln(stderr, "\nnote: resume is not yet implemented (coming in later slice)")
			}
		}
	}
	return err
}

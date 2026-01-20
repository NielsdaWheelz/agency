// Package commands implements CLI command handlers for agency.
// This file implements shell tab completion for bash and zsh.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// CompletionOpts holds options for the completion command.
type CompletionOpts struct {
	Shell  string // "bash" or "zsh"
	Output string // optional output file path; if empty, writes to stdout
}

// Completion generates shell completion scripts.
// If opts.Output is set, writes the script to that file (creating parent dirs);
// otherwise prints to stdout.
func Completion(_ context.Context, opts CompletionOpts, stdout, stderr io.Writer) error {
	var script string
	switch opts.Shell {
	case "bash":
		script = bashCompletionScript
	case "zsh":
		script = zshCompletionScript
	default:
		return errors.New(errors.EUsage, fmt.Sprintf("unsupported shell: %s (supported: bash, zsh)", opts.Shell))
	}

	// If no output file specified, write to stdout
	if opts.Output == "" {
		_, _ = fmt.Fprint(stdout, script)
		return nil
	}

	// Create parent directories if needed
	dir := filepath.Dir(opts.Output)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrap(errors.EInternal, fmt.Sprintf("failed to create directory %s", dir), err)
	}

	// Write to file atomically using temp file + rename
	tmpPath := opts.Output + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(script), 0644); err != nil {
		return errors.Wrap(errors.EInternal, fmt.Sprintf("failed to write %s", opts.Output), err)
	}

	if err := os.Rename(tmpPath, opts.Output); err != nil {
		_ = os.Remove(tmpPath) // cleanup on error
		return errors.Wrap(errors.EInternal, fmt.Sprintf("failed to rename to %s", opts.Output), err)
	}

	return nil
}

// CompleteKind is the type of completion to generate.
type CompleteKind string

const (
	CompleteKindCommands        CompleteKind = "commands"
	CompleteKindRuns            CompleteKind = "runs"
	CompleteKindRunners         CompleteKind = "runners"
	CompleteKindMergeStrategies CompleteKind = "merge_strategies"
)

// CompleteOpts holds options for the __complete command.
type CompleteOpts struct {
	Kind            CompleteKind
	AllRepos        bool // include runs from all repos
	IncludeArchived bool // include archived runs
}

// Complete generates completion candidates for shell scripts.
// Outputs newline-separated candidates. Silent on error (returns nothing).
// This is a hidden command used by completion scripts.
func Complete(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts CompleteOpts, stdout, stderr io.Writer) error {
	debug := os.Getenv("AGENCY_DEBUG_COMPLETION") == "1"

	candidates, err := generateCompletionCandidates(ctx, cr, fsys, cwd, opts, debug, stderr)
	if err != nil {
		if debug {
			_, _ = fmt.Fprintf(stderr, "completion error: %v\n", err)
			return err
		}
		// Silent failure for shell UX
		return nil
	}

	for _, c := range candidates {
		_, _ = fmt.Fprintln(stdout, c)
	}
	return nil
}

// generateCompletionCandidates returns completion candidates based on kind.
func generateCompletionCandidates(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts CompleteOpts, debug bool, stderr io.Writer) ([]string, error) {
	switch opts.Kind {
	case CompleteKindCommands:
		return completeCommands(), nil
	case CompleteKindRuns:
		return completeRuns(ctx, cr, fsys, cwd, opts, debug, stderr)
	case CompleteKindRunners:
		return completeRunners(fsys, debug, stderr)
	case CompleteKindMergeStrategies:
		return completeMergeStrategies(), nil
	default:
		return nil, fmt.Errorf("unknown completion kind: %s", opts.Kind)
	}
}

// completeCommands returns the static list of user-facing top-level commands.
// Excludes hidden/internal commands like __complete.
func completeCommands() []string {
	return []string{
		"attach",
		"clean",
		"completion",
		"doctor",
		"init",
		"kill",
		"ls",
		"merge",
		"open",
		"path",
		"push",
		"resume",
		"run",
		"show",
		"stop",
		"verify",
	}
}

// completeMergeStrategies returns the static list of merge strategy flags.
func completeMergeStrategies() []string {
	return []string{
		"--merge",
		"--rebase",
		"--squash",
	}
}

// completeRunners returns runner names from user config + built-in defaults.
func completeRunners(fsys fs.FS, debug bool, stderr io.Writer) ([]string, error) {
	// Resolve config directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		if debug {
			_, _ = fmt.Fprintf(stderr, "debug: failed to get home dir: %v\n", err)
		}
		// Return built-in defaults on error
		return []string{"claude", "codex"}, nil
	}

	env := completeEnv{}
	dirs := paths.ResolveDirs(env, homeDir)

	// Load user config
	cfg, _, err := config.LoadUserConfig(fsys, dirs.ConfigDir)
	if err != nil {
		if debug {
			_, _ = fmt.Fprintf(stderr, "debug: failed to load user config: %v\n", err)
		}
		// Return built-in defaults on error
		return []string{"claude", "codex"}, nil
	}

	// Collect runner names
	runners := make(map[string]struct{})

	// Add built-in defaults
	runners["claude"] = struct{}{}
	runners["codex"] = struct{}{}

	// Add configured runners
	for name := range cfg.Runners {
		runners[name] = struct{}{}
	}

	// Add default runner if not already present
	if cfg.Defaults.Runner != "" {
		runners[cfg.Defaults.Runner] = struct{}{}
	}

	// Convert to sorted slice
	result := make([]string, 0, len(runners))
	for name := range runners {
		result = append(result, name)
	}
	sort.Strings(result)

	return result, nil
}

// completeRuns returns run candidates (names and run_ids).
// Per spec:
// - Default: current repo only, active (non-archived) runs only
// - Include names only if unique within the candidate set
// - Always include all run_ids
// - Sort by created_at DESC, tie-breaker: run_id DESC
func completeRuns(ctx context.Context, cr exec.CommandRunner, _ fs.FS, cwd string, opts CompleteOpts, debug bool, stderr io.Writer) ([]string, error) {
	// Resolve data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		if debug {
			_, _ = fmt.Fprintf(stderr, "debug: failed to get home dir: %v\n", err)
		}
		return nil, nil
	}

	env := completeEnv{}
	dirs := paths.ResolveDirs(env, homeDir)

	if debug {
		_, _ = fmt.Fprintf(stderr, "debug: data_dir=%s\n", dirs.DataDir)
	}

	var records []store.RunRecord

	if opts.AllRepos {
		// Scan all repos
		records, err = store.ScanAllRuns(dirs.DataDir)
		if err != nil {
			if debug {
				_, _ = fmt.Fprintf(stderr, "debug: scan all runs failed: %v\n", err)
			}
			return nil, nil
		}
	} else {
		// Try to detect current repo
		repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
		if err != nil {
			if debug {
				_, _ = fmt.Fprintf(stderr, "debug: not in repo: %v\n", err)
			}
			// Not in a repo - return empty list (per spec)
			return nil, nil
		}

		// Get origin info for repo identity
		originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
		repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

		if debug {
			_, _ = fmt.Fprintf(stderr, "debug: repo_root=%s repo_id=%s\n", repoRoot.Path, repoIdentity.RepoID)
		}

		// Scan runs for this repo
		records, err = store.ScanRunsForRepo(dirs.DataDir, repoIdentity.RepoID)
		if err != nil {
			if debug {
				_, _ = fmt.Fprintf(stderr, "debug: scan repo runs failed: %v\n", err)
			}
			return nil, nil
		}
	}

	if debug {
		_, _ = fmt.Fprintf(stderr, "debug: found %d records\n", len(records))
	}

	// Filter: exclude archived unless --include-archived
	var filtered []store.RunRecord
	for _, rec := range records {
		if rec.Broken {
			// Skip broken records
			continue
		}

		// Check if archived (worktree deleted or archive.archived_at set)
		isArchived := false
		if rec.Meta != nil && rec.Meta.Archive != nil && rec.Meta.Archive.ArchivedAt != "" {
			isArchived = true
		}

		if isArchived && !opts.IncludeArchived {
			continue
		}

		filtered = append(filtered, rec)
	}

	if debug {
		_, _ = fmt.Fprintf(stderr, "debug: after filter: %d records\n", len(filtered))
	}

	// Sort by created_at DESC, tie-breaker: run_id DESC
	sort.Slice(filtered, func(i, j int) bool {
		iTime := parseCreatedAt(filtered[i].Meta)
		jTime := parseCreatedAt(filtered[j].Meta)

		if !iTime.Equal(jTime) {
			return iTime.After(jTime) // DESC
		}
		// Tie-breaker: run_id DESC (lexicographic)
		return filtered[i].RunID > filtered[j].RunID
	})

	// Build candidates:
	// - Count name occurrences to detect duplicates
	// - Include name only if unique
	// - Always include run_id
	nameCounts := make(map[string]int)
	for _, rec := range filtered {
		if rec.Name != "" {
			nameCounts[rec.Name]++
		}
	}

	var candidates []string
	seenNames := make(map[string]bool)

	for _, rec := range filtered {
		// Always add run_id
		candidates = append(candidates, rec.RunID)

		// Add name only if unique and not already added
		if rec.Name != "" && nameCounts[rec.Name] == 1 && !seenNames[rec.Name] {
			candidates = append(candidates, rec.Name)
			seenNames[rec.Name] = true
		}
	}

	return candidates, nil
}

// parseCreatedAt parses the created_at timestamp from meta.
// Returns zero time if missing or invalid (sorts last per spec).
func parseCreatedAt(meta *store.RunMeta) time.Time {
	if meta == nil || meta.CreatedAt == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, meta.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// completeEnv implements paths.Env using os.Getenv for completion.
type completeEnv struct{}

func (completeEnv) Get(key string) string {
	return os.Getenv(key)
}

// Bash completion script with install instructions as comments.
const bashCompletionScript = `# agency bash completion
#
# Installation (choose one):
#
# Option 1: Using bash-completion package (recommended)
#   agency completion bash > ~/.local/share/bash-completion/completions/agency
#   # Requires: bash-completion package installed
#   # The directory is auto-sourced by bash-completion
#
# Option 2: Manual sourcing
#   agency completion bash > ~/.agency-completion.bash
#   echo 'source ~/.agency-completion.bash' >> ~/.bashrc
#
# After installation, restart your shell or run:
#   source ~/.bashrc

_agency() {
    local cur prev words cword
    _init_completion 2>/dev/null || {
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        words=("${COMP_WORDS[@]}")
        cword=$COMP_CWORD
    }

    # Find the subcommand (first non-flag argument after 'agency')
    local cmd=""
    local i
    for ((i=1; i < cword; i++)); do
        case "${words[i]}" in
            --*) ;;
            -*)  ;;
            *)
                cmd="${words[i]}"
                break
                ;;
        esac
    done

    # No subcommand yet - complete commands
    if [[ -z "$cmd" ]]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=($(compgen -W "--verbose --help --version" -- "$cur"))
        else
            local commands
            commands=$(agency __complete commands 2>/dev/null)
            COMPREPLY=($(compgen -W "$commands" -- "$cur"))
        fi
        return
    fi

    # Subcommand-specific completion
    case "$cmd" in
        attach|clean|kill|merge|open|path|push|resume|show|stop|verify)
            # Commands that take a run reference as first positional arg
            if [[ "$cur" == --* ]]; then
                # Flag completion for merge only
                if [[ "$cmd" == "merge" ]]; then
                    COMPREPLY=($(compgen -W "$(agency __complete merge_strategies 2>/dev/null)" -- "$cur"))
                fi
            else
                # Run reference completion
                local runs
                runs=$(agency __complete runs 2>/dev/null)
                COMPREPLY=($(compgen -W "$runs" -- "$cur"))
            fi
            ;;
        run)
            if [[ "$prev" == "--runner" ]]; then
                local runners
                runners=$(agency __complete runners 2>/dev/null)
                COMPREPLY=($(compgen -W "$runners" -- "$cur"))
            fi
            ;;
        completion)
            COMPREPLY=($(compgen -W "bash zsh" -- "$cur"))
            ;;
    esac
}

complete -F _agency agency
`

// Zsh completion script with install instructions as comments.
const zshCompletionScript = `#compdef agency
# agency zsh completion
#
# Installation (choose one):
#
# Option 1: Using fpath (recommended)
#   agency completion zsh > ~/.zsh/completions/_agency
#   # Ensure ~/.zsh/completions is in your fpath (add to .zshrc before compinit):
#   #   fpath=(~/.zsh/completions $fpath)
#   # Then run compinit:
#   #   autoload -Uz compinit && compinit
#
# Option 2: Manual sourcing
#   agency completion zsh > ~/.agency-completion.zsh
#   echo 'source ~/.agency-completion.zsh' >> ~/.zshrc
#
# After installation, restart your shell or run:
#   source ~/.zshrc

_agency() {
    local -a commands
    local -a runs
    local -a runners
    local -a merge_strategies

    # Get commands list
    commands=(${(f)"$(agency __complete commands 2>/dev/null)"})

    # Context-aware completion
    case "$words[2]" in
        attach|clean|kill|merge|open|path|push|resume|show|stop|verify)
            # Commands that take a run reference
            if [[ "$words[CURRENT]" == --* ]]; then
                if [[ "$words[2]" == "merge" ]]; then
                    merge_strategies=(${(f)"$(agency __complete merge_strategies 2>/dev/null)"})
                    _describe 'merge strategy' merge_strategies
                fi
            else
                runs=(${(f)"$(agency __complete runs 2>/dev/null)"})
                _describe 'run' runs
            fi
            ;;
        run)
            case "$words[CURRENT-1]" in
                --runner)
                    runners=(${(f)"$(agency __complete runners 2>/dev/null)"})
                    _describe 'runner' runners
                    ;;
            esac
            ;;
        completion)
            _describe 'shell' '(bash zsh)'
            ;;
        "")
            # No subcommand yet
            if [[ "$words[CURRENT]" == -* ]]; then
                _arguments \
                    '--verbose[show detailed error context]' \
                    '--help[show help]' \
                    '--version[show version]'
            else
                _describe 'command' commands
            fi
            ;;
        *)
            # Unknown command - fall back to commands
            _describe 'command' commands
            ;;
    esac
}

_agency "$@"
`

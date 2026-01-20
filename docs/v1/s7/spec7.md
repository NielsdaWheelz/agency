# PR Spec: Shell Tab Completion (bash + zsh)

## Goal

Add first-class shell tab completion for the `agency` CLI, covering:
- subcommands
- run references (names + ids)
- enum-like values (runner names, merge strategies)

This PR must not change CLI parsing, command behavior, or storage semantics.

---

## Non-goals (Explicit)

This PR does **not**:
- add interactive TUI features
- add fuzzy search or fzf integration
- add full flag completion for all flags
- modify existing commands’ semantics
- auto-edit shell dotfiles
- require Cobra or any CLI framework migration
- add fish completion (deferred)

---

## User-visible Outcome

After installation:
- macOS (Homebrew): tab completion works automatically after shell restart
- Linux (manual): user can enable completion via one command

Examples:
```bash
agency <TAB>                # shows subcommands
agency show <TAB>           # shows run names / ids
agency attach feat<TAB>     # completes run name
agency run --runner <TAB>   # claude, codex
agency merge x --<TAB>      # --squash, --merge, --rebase (strategy flags only)
```

---

## Scope

### Supported Shells (v1)

* bash
* zsh

### Completion Targets

* **Commands**: static list
* **Run references**: dynamic
* **Runner names**: from config + defaults
* **Merge strategies**: `--squash`, `--merge`, `--rebase`

### Default Completion Policy

* Runs:

  * scope: **current repo only**
  * include: **active (non-archived) runs only**
  * ordering: **most recent first**
  * candidates:

    * unique run names only
    * all run_ids
* Archived runs:

  * excluded by default
* Cross-repo runs:

  * excluded by default

---

## New Commands

### `agency completion <bash|zsh>`

* Prints a shell-specific completion script to **stdout**
* Does not write files
* Does not mutate any state
* Includes brief install instructions as comments at top of script
* Exit code:

  * `0` on success
  * `E_USAGE` on invalid shell

### `agency __complete <kind> [flags]` (hidden)

**Kinds (v1):**

* `commands`
* `runs`
* `runners`
* `merge_strategies`

**Flags (v1):**

* `--all-repos` — include runs from all repos (not used by shell scripts in v1)
* `--include-archived` — include archived runs (not used by shell scripts in v1)

These flags exist for:
* Manual debugging (`agency __complete runs --all-repos`)
* Future shell script enhancements
* Direct human use when troubleshooting

Shell-generated completion scripts use defaults only (current repo, active runs).

**Behavior:**

* Outputs **newline-separated candidates only**
* No formatting, no headers
* No stderr output on normal failure
* Read-only: no locks, no writes, no tmux, no git network, no gh

**Error Handling:**

* If not in repo and kind is `runs`: print nothing, exit 0
* If store unavailable/corrupt: print nothing, exit 0
* Internal errors: print nothing, exit 0

**Debug Mode (`AGENCY_DEBUG_COMPLETION=1`):**

When set:
* Print diagnostic messages to stderr (store path, repo detection, candidate counts, errors)
* Exit non-zero on internal errors (instead of silent exit 0)
* Useful for troubleshooting "completion silently broken" scenarios

When unset (default):
* Silence all output on errors
* Always exit 0 on errors (shell UX requirement)

**Empty/Missing Store:**

Completion must work before `agency doctor` has ever been run:
* `__complete commands` — always works (static list)
* `__complete runners` — returns built-in defaults (`claude`, `codex`)
* `__complete merge_strategies` — always works (static list)
* `__complete runs` — returns empty list (no runs exist yet)

---

## Shell Script Responsibilities

### bash

* Use `complete -F _agency agency`
* Inspect `COMP_WORDS` / `COMP_CWORD`
* Decide which `__complete <kind>` to call based on position
* Fall back silently if `agency __complete` returns nothing

### zsh

* Use `compdef _agency agency`
* Native zsh completion function (no bash emulation)
* Same semantic behavior as bash version

### Flag Completion (v1 Scope)

**Merge strategy flags only:**
- When the current word starts with `--` **and** the active command is `merge`
- Complete with: `--squash`, `--merge`, `--rebase`
- Do **not** complete other merge flags (`--no-delete-branch`, `--allow-dirty`, `--force`)
- Do **not** complete flags for any other command

This is the only flag completion in v1. General flag completion is deferred.

Scripts must **not**:

* assume bash-completion package
* assume nonstandard shell options
* emit errors to stderr during normal operation
* pass `--all-repos` or `--include-archived` to `__complete` (v1 shell scripts use defaults only; these flags exist for manual debugging/future use)

---

## Go-side Responsibilities

### `__complete runs`

* Detect current repo from cwd
* Default: current repo, active runs only
* Apply flags:

  * `--include-archived`
  * `--all-repos`
* Deduplicate run names:

  * Include name only if unique within the final candidate set (after scope/filter application)
  * If two active runs in the current repo have the same name, emit neither name (only their run_ids)
* Always include run_ids
* Sort by `created_at DESC`:
  * Source: `meta.json` `created_at` field (RFC3339 format)
  * Missing/invalid `created_at`: sort last
  * Tie-breaker: `run_id` descending (lexicographic)
* Return candidates as plain strings

### `__complete runners`

* Return:

  * configured runners from user config
  * built-in defaults (`claude`, `codex`)
* Deduplicated, sorted lexicographically

### `__complete commands`

* Static list of user-facing top-level commands
* **Excludes** hidden/internal commands: `__complete`
* **Includes** `completion` (user-facing)

### `__complete merge_strategies`

* Static list:

  * `--squash`
  * `--merge`
  * `--rebase`

---

## Performance Constraints

* Typical completion latency: **<100ms**
* No disk scans outside agency data directory
* No git network operations
* No tmux or gh invocations

**Allowed git invocations:**
* `git rev-parse --show-toplevel` — for repo root detection from cwd
* This is local-only (no network), fast, and required to map cwd → repo_id

---

## Installation UX

### macOS (Homebrew)

* Completion scripts installed to:

  * zsh: `$(brew --prefix)/share/zsh/site-functions/_agency`
  * bash: `$(brew --prefix)/share/bash-completion/completions/agency`
* Requires shell restart
* zsh requires `compinit` (documented in comments)

### Linux / Manual

Generated scripts include install instructions as comments. Two installation approaches:

**bash (with bash-completion package installed):**
```bash
agency completion bash > ~/.local/share/bash-completion/completions/agency
# Requires: bash-completion package, directory auto-sourced
```

**bash (fallback, no bash-completion):**
```bash
agency completion bash > ~/.agency-completion.bash
echo 'source ~/.agency-completion.bash' >> ~/.bashrc
```

**zsh (with fpath configured):**
```bash
agency completion zsh > ~/.zsh/completions/_agency
# Requires: ~/.zsh/completions in fpath, compinit called
```

**zsh (fallback):**
```bash
agency completion zsh > ~/.agency-completion.zsh
echo 'source ~/.agency-completion.zsh' >> ~/.zshrc
```

Generated script comments must document both approaches.

---

## Testing

### Unit Tests

* `__complete runs`:

  * repo-scoped default
  * exclude archived by default
  * include archived with flag
  * unique name filtering
  * ordering by recency

### Script Generation Tests

* `agency completion bash` output:

  * defines `_agency`
  * registers completion
  * calls `agency __complete runs`
* `agency completion zsh` output:

  * defines `_agency`
  * uses `compdef`

No shell integration tests required.

---

## Edge Cases

**Run names matching command names:**
* A run named `show` or `init` is valid and will appear in completion candidates
* Resolution already handles this: positional context determines whether it's a command or run ref
* No special handling needed in completion

**Run names with hyphens:**
* Fully supported (e.g., `feature-auth-v2`)
* Shell word splitting handles this correctly

## Invariants

* Completion is read-only
* Completion never mutates state
* Completion never blocks
* Completion failure is silent
* No changes to existing command semantics

---

## Acceptance Criteria

* Tab completion works for commands and run refs
* Works on macOS with Homebrew install
* Works on Linux with manual install
* No regressions in existing CLI behavior
* Completion remains fast and silent under error conditions

# CLI reference

agency uses [Cobra](https://github.com/spf13/cobra) for command-line parsing. Use `agency --help` or `agency <command> --help` to see all options.

```
agency [command]

global flags:
  -h, --help      help for agency
      --verbose   show detailed error context
  -v, --version   version for agency

legacy commands (v1):
  init        create agency.json template + stub scripts
  doctor      check prerequisites + show paths
  run         create workspace, setup, start tmux, attach
  ls          list runs + statuses
  show        show run details (global)
  path        output worktree path (for scripting, global)
  open        open worktree in editor (global)
  attach      attach to tmux session (global)
  resume      attach to tmux session (create if missing, global)
  stop        send C-c to runner (global)
  kill        kill tmux session (global)
  push        push + create/update PR
  verify      run verify script and record results
  merge       verify, confirm, merge PR, delete branch, archive
  clean       archive without merging (abandon run)
  resolve     show conflict resolution guidance
  completion  generate shell completion scripts (bash, zsh)
  version     print agency version

v2 commands (slice 8+):
  worktree    manage integration worktrees
  agent       manage agent invocations (headed + headless via daemon)
              subcommands: start, ls, show, attach, enter, stop, kill, diff,
                           land, discard, open, path, shell, chat, restart,
                           history, logs, review
  daemon      manage the agency daemon (headless supervision)
              subcommands: start, stop, status, install, uninstall
  checkpoint  manage sandbox checkpoints for headless invocations
  repo        manage repository registry
  watch       full-screen readiness monitoring workspace
```

high-traffic flags use consistent short aliases where available:
- `-r` for `--repo`
- `-j` for `--json`
- `-y` for `--yes`
- `-o` for `--open`

## `agency watch` (v2.1)

opens the full-screen daemon-backed monitoring workspace.

**usage:**
```bash
agency watch [--interval <duration>]
```

**flags:**
- `--interval`: snapshot refresh interval (default: `2s`, min: `250ms`, max: `5s`)

**keyboard shortcuts:**
- `up/down` or `k/j`: move selected invocation
- `home/end` or `g/G`: jump to top/bottom
- `enter`: delegate to canonical `agent enter` for selected invocation
- `o`: delegate to canonical `agent open` for selected invocation
- `p`: sync PR for selected invocation's worktree (via canonical worktree PR sync flow)
- `r`: trigger immediate refresh
- `q`, `esc`, `ctrl+c`: quit and restore prior shell screen state

**behavior:**
- requires an interactive terminal (`E_NOT_INTERACTIVE` otherwise)
- composes state from daemon read APIs (`repos`, paged `worktrees`, paged `invocations`, per-invocation `review`)
- preserves selection by invocation identity across refresh reordering
- surfaces recoverable refresh failures in-session without collapsing the workspace
- action failures are surfaced in-session and keep watch running (including explicit `E_SESSION_ENDED` guidance for ended headed sessions)

## `agency repo` (v2)

manages the repository registry. repos must be registered before creating worktrees or starting agents. repos are auto-registered on first use, but can be managed explicitly.

### `agency repo add`

registers a repository.

**usage:**
```bash
agency repo add [--path <path>] [--json]
```

**flags:**
- `--path`: path to the git repository (default: current directory)
- `--json`: output as JSON

**behavior:**
1. resolves git root from the given path
2. generates a deterministic repo_id from the repo root
3. writes `repo.json` and updates `repo_index.json`
4. idempotent: re-registering an existing repo is a no-op

**output:**
```
Registered repository
  repo_id: abcd1234ef567890
  root:    /path/to/repo
```

### `agency repo ls`

lists registered repositories.

**usage:**
```bash
agency repo ls [--json]
```

**flags:**
- `--json`: output as JSON

**output:**
the first column shows the repo short name (derived from the GitHub repository name). this name can be used as the `--repo` argument for any command that accepts a repo reference. when names are ambiguous across repos, use the full repo key (`owner/repo`) or id instead.

### `agency repo show`

shows details of a registered repository.

**usage:**
```bash
agency repo show <name|repo-key|id|prefix> [--json]
```

**arguments:**
- `name|repo-key|id|prefix`: repository identifier. resolved in order: short name (GitHub repo name) → repo key (`owner/repo`) → exact id → unique id prefix

**flags:**
- `--json`: output as JSON

## `agency worktree` (v2)

manages integration worktrees — stable, human-owned branches that are independent of agent invocations.

### `agency worktree create`

creates a new integration worktree.

**usage:**
```bash
agency worktree create --name <name> [--parent <branch>] [--open] [--editor <name>]
```

**flags:**
- `--name`: worktree name (required, 2-40 chars, lowercase alphanumeric with hyphens)
- `--parent`: parent branch to branch from (default: current branch)
- `--open`: open the worktree in editor after creation
- `--editor`: editor to use (overrides config)

**behavior:**
1. validates name format and uniqueness
2. generates worktree_id
3. creates branch `agency/<name>-<shortid>`
4. creates git worktree at `${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/tree/`
5. writes `.agency/INTEGRATION_MARKER` (prevents runners from executing in integration trees)
6. writes `meta.json` with worktree metadata

**output:**
```
Created integration worktree 'my-feature'
  worktree_id: 20260131120000-a3f2
  branch:      agency/my-feature-a3f2
  path:        /path/to/tree
```

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_PARENT_DIRTY` — working tree has uncommitted changes
- `E_INVALID_NAME` — name does not match validation rules
- `E_NAME_EXISTS` — name already used by an active worktree
- `E_WORKTREE_CREATE_FAILED` — git worktree add failed

### `agency worktree ls`

lists integration worktrees.

**usage:**
```bash
agency worktree ls [--all] [--repo <path>] [--all-repos] [--json] [--watch] [--interval <duration>]
```

**flags:**
- `--all`: include archived worktrees
- `--repo`: path to git repository
- `--all-repos`: list worktrees across all registered repositories
- `--json`: output as JSON
- `--watch`: live-updating ANSI-redraw view (incompatible with `--json`)
- `--interval`: refresh interval for `--watch` (default: 500ms, min: 250ms, max: 5s)

**output:**
```
20260131120000-a3f2  my-feature  agency/my-feature-a3f2
20260131110000-c3d4  bugfix      agency/bugfix-c3d4 [archived]
```

### `agency worktree show`

shows details of an integration worktree.

**usage:**
```bash
agency worktree show <name|id|prefix> [--json]
```

**arguments:**
- `name|id|prefix`: worktree identifier (name, id, or unique prefix)

**flags:**
- `--json`: output as JSON

**output:**
```
worktree_id:   20260131120000-a3f2
name:          my-feature
branch:        agency/my-feature-a3f2
parent_branch: main
state:         present
created_at:    2026-01-31T12:00:00Z
tree_path:     /path/to/tree
```

### `agency worktree path`

outputs the tree path for scripting.

**usage:**
```bash
agency worktree path <name|id|prefix>
```

**example:**
```bash
cd $(agency worktree path my-feature)
```

### `agency worktree open`

opens the worktree in the configured editor.

**usage:**
```bash
agency worktree open <name|id|prefix> [--editor <name>]
```

### `agency worktree shell`

opens a shell in the worktree.

**usage:**
```bash
agency worktree shell <name|id|prefix>
```

spawns `$SHELL` (or `/bin/sh`) with the worktree as the working directory.

### `agency worktree rm`

removes an integration worktree.

**usage:**
```bash
agency worktree rm <name|id|prefix> [--force] [--yes]
```

**flags:**
- `--force`: remove even if worktree has uncommitted changes
- `--yes`: skip interactive confirmation (required in non-interactive mode)

**behavior:**
1. in interactive mode, prompts for confirmation token (`rm`) unless `--yes` is passed
2. in non-interactive mode, fails with `E_CONFIRMATION_REQUIRED` unless `--yes` is passed
3. runs `git worktree remove` (fails if dirty without `--force`)
4. sets `state = archived` in `meta.json`
5. preserves the record directory and metadata

**error codes:**
- `E_WORKTREE_NOT_FOUND` — worktree not found
- `E_DIRTY_WORKTREE` — worktree has uncommitted changes (use `--force`)
- `E_WORKTREE_REMOVE_FAILED` — git worktree remove failed

### `agency worktree pr sync`

pushes a worktree branch and creates/updates the branch-scoped PR.

**usage:**
```bash
agency worktree pr sync <worktree_ref> [--repo <name|id|prefix>] [--allow-dirty] [--force-with-lease] [--json]
```

**flags:**
- `-r, --repo`: repo name, key, id, or prefix
- `--allow-dirty`: allow sync with uncommitted integration worktree changes
- `--force-with-lease`: use `git push --force-with-lease`
- `-j, --json`: machine-readable mutation envelope output

**behavior:**
- resolves worktree context deterministically (name/id/prefix within repo scope)
- enforces dirty-worktree and push policy validation with typed errors/hints
- creates or updates one PR identity per branch and returns stable outcome fields (`branch`, `pr_action`, `pr_url`)
- evaluates reports-v2 canonically (`report.json` authoritative over `report.md`) and returns `report_source` plus diagnostics in json mode
- worktree flow is compatibility-first for report body generation (deterministic fallback body + diagnostics when needed)

### `agency worktree merge`

runs worktree-scoped verify + pull-request merge for the resolved integration branch.

**usage:**
```bash
agency worktree merge <worktree_ref> [--repo <name|id|prefix>] [--squash|--merge|--rebase] [--no-delete-branch] [--yes] [--json]
```

**flags:**
- `-r, --repo`: repo name, key, id, or prefix
- `--squash`: squash merge strategy (default)
- `--merge`: regular merge strategy
- `--rebase`: rebase merge strategy
- `--no-delete-branch`: keep remote branch after merge
- `-y, --yes`: required for non-interactive/scripted runs
- `-j, --json`: machine-readable mutation envelope output

**behavior:**
- resolves worktree context deterministically
- enforces explicit confirmation contract (`--yes` non-interactive, typed token in interactive mode)
- runs verify script with worktree-scoped environment, merges via `gh pr merge`, and writes private logs under worktree state
- evaluates report contract using the same canonical resolver as `worktree pr sync`; success payload includes `report_source` and diagnostics
- emits typed failure codes for prechecks, verify failure, mergeability conflicts, and durability failures

### `agency worktree update`

fetches from origin and rebases the worktree branch onto its configured parent branch.

**usage:**
```bash
agency worktree update <worktree_ref> [--repo <name|id|prefix>] [--json]
```

**flags:**
- `-r, --repo`: repo name, key, id, or prefix
- `-j, --json`: machine-readable mutation envelope output

**behavior:**
- requires a clean worktree; dirty trees fail with typed `E_DIRTY_WORKTREE`
- executes `git fetch origin` followed by `git rebase origin/<parent_branch>`
- on rebase conflicts, attempts `git rebase --abort` and returns typed `E_REBASE_CONFLICT`
- appends started/succeeded/failed lifecycle events to worktree event stream; append failures fail the operation

## `agency agent` (v2)

manages agent invocations — executions of runners inside isolated sandbox worktrees.

### `agency agent start`

starts a new agent invocation with its sandbox worktree.

**usage:**
```bash
agency agent start --worktree <name|id|prefix> [--runner <runner>] [--headless] [--name <name>] [--detached] [--prompt <string>] [--prompt-file <path>] [--model <name>] [--effort <level>] [--runner-arg <arg>]... [--no-include-untracked] [--json]
```

**flags:**
- `--worktree`: integration worktree to run against (required)
- `--runner`: runner id to use (`claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`; legacy aliases: `claude`, `cursor-cli`)
  - default resolution: `config.json defaults.runner` -> built-in fallback `claude-code`
- `--headless`: run in headless mode (non-interactive, via daemon)
- `--name`: optional human-readable label for the invocation (unique among active invocations)
- `--detached`: start but do not attach (headed mode only; no-op for headless)
- `--prompt`: prompt string for headless mode
- `--prompt-file`: path to file containing prompt for headless mode
- `--model`: model override (supported for `claude-code`, `codex`, and `cursor`)
- `--effort`: effort override (supported for `claude-code` and `codex`; `cursor` uses thinking-capable model ids via `--model`)
- `--runner-arg`: additional argument to pass to the runner (repeatable)
- `--no-include-untracked`: exclude untracked files from checkpoint snapshots (headless only)
- `--json`: machine-readable mutation envelope output

runner commands are resolved from user config (`config.runners`) and must be explicitly mapped.
typed model/effort resolution is deterministic:
- model (`claude-code`, `codex`, `cursor`): CLI `--model` -> `config.json defaults.model` -> none.
- effort (`claude-code`, `codex`): CLI `--effort` -> `config.json defaults.effort` -> none.
runner-specific mapping:
- `claude-code`: `--model <value>`, `--effort <value>`
- `codex`: `--model <value>`, `--config model_reasoning_effort=<value>`
- `cursor`: `--model <value>` (use thinking model variants such as `sonnet-4.6-thinking` when needed)

conflicting values between typed flags and `--runner-arg` are rejected with `E_USAGE`.

**behavior (headed mode, default):**
1. resolves integration worktree
2. verifies `INTEGRATION_MARKER` exists (target must be integration worktree)
3. validates invocation name uniqueness if provided
4. generates invocation_id
5. creates sandbox directory
6. captures base_commit from integration branch
7. creates sandbox worktree via `git worktree add -b agency/sandbox-<invocation_id>`
8. writes `.agency/SANDBOX_MARKER`
9. writes invocation `meta.json` with `status=starting`
10. preflight check: verifies no tmux session with this name exists
11. creates tmux session `agency_<invocation_id>` with CWD = sandbox tree, runs runner command directly
12. updates invocation meta with `status=running`, `tmux_session` set
13. attaches to tmux session (unless `--detached`)

**behavior (headless mode):**
1. resolves integration worktree
2. verifies `INTEGRATION_MARKER` exists
3. validates invocation name uniqueness if provided
4. generates invocation_id
5. creates sandbox directory
6. captures base_commit from integration branch
7. creates sandbox worktree
8. writes invocation `meta.json` with `status=starting`, `mode=headless`
9. resolves prompt from `--prompt` or `--prompt-file` using bounded reads (required, max 256KB)
10. ensures daemon is running (autostarts if needed)
11. sends start request to daemon via IPC
12. daemon validates sandbox markers, spawns runner process, streams logs
13. returns immediately (headless is detached by default)

**output (headed):**
```
Started agent invocation
  invocation_id:  20260131120500-b7c9
  runner:         claude-code
  mode:           headed
  worktree:       my-feature (20260131120000-a3f2)
  sandbox_path:   /path/to/sandboxes/20260131120500-b7c9/tree
  tmux_session:   agency_20260131120500-b7c9

Attaching to tmux session... (detach with Ctrl+b, d)
```

**json mutation envelope (`--json`):**
- stable top-level fields: `ok`, `error_code`, `message`, `hint`, `request_id`, `api_version`, `build_version`, `client_request_id`
- command-specific success fields are additive (for example `invocation_id`, `sandbox_path`, `pid`, `pgid`)
- failure responses are emitted to stdout as JSON (`ok=false`) so automation does not need stderr parsing

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_WORKTREE_NOT_FOUND` — integration worktree not found
- `E_WORKTREE_BROKEN` — integration worktree meta.json unreadable
- `E_INTEGRATION_MARKER_MISSING` — target is not an integration worktree
- `E_SANDBOX_PATH_UNSAFE` — sandbox path resolves to integration tree (invariant violation)
- `E_INVOCATION_CREATE_FAILED` — invocation creation failed
- `E_SANDBOX_CREATE_FAILED` — sandbox worktree creation failed
- `E_TMUX_SESSION_EXISTS` — tmux session already exists (leaked session or parallel execution)
- `E_INVOCATION_START_FAILED` — tmux session creation failed
- `E_RUNNER_NOT_CONFIGURED` — runner command not found
- `E_PROMPT_REQUIRED` — headless prompt missing or empty
- `E_PROMPT_TOO_LARGE` — prompt exceeds 256KB bound
- `E_USAGE` — invalid prompt flags (for example both `--prompt` and `--prompt-file`)

### `agency agent ls`

lists agent invocations.

**usage:**
```bash
agency agent ls [--worktree <name|id|prefix>] [--all] [--repo <path>] [--all-repos] [--json] [--watch] [--interval <duration>]
```

**flags:**
- `--worktree`: filter by integration worktree
- `--all`: include finished (landed/discarded) invocations
- `--repo`: path to git repository
- `--all-repos`: list invocations across all registered repositories
- `--json`: output as JSON
- `--watch`: live-updating ANSI-redraw view (incompatible with `--json`)
- `--interval`: refresh interval for `--watch` (default: 500ms, min: 250ms, max: 5s)

**output:**
```
20260131120500-b7c9  claude  headed  starting  (arch-agent)
20260131110000-d4e5  codex   headless  running
```

### `agency agent show`

shows details of an agent invocation.

**usage:**
```bash
agency agent show <invocation_id|prefix> [--json]
```

**arguments:**
- `invocation_id|prefix`: invocation identifier (id or unique prefix)

**flags:**
- `--json`: output as JSON

**output:**
```
invocation_id:          20260131120500-b7c9
name:                   arch-agent
integration_worktree:   20260131120000-a3f2
runner:                 claude
mode:                   headed
status:                 starting
started_at:             2026-01-31T12:05:00Z
base_commit:            789abc...
sandbox_branch:         agency/sandbox-20260131120500-b7c9
sandbox_path:           /path/to/sandboxes/20260131120500-b7c9/tree
sandbox_exists:         true
```

**error codes:**
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_INVOCATION_ID_AMBIGUOUS` — prefix matches multiple invocations
- `E_INVOCATION_BROKEN` — invocation meta.json unreadable

### `agency agent attach`

compatibility alias for `agency agent enter` that attaches to a running headed invocation's tmux session.
prefer `agency agent enter` for canonical invocation navigation.

**usage:**
```bash
agency agent attach <invocation_id|prefix> [-r|--repo <name|id|prefix>]
```

**arguments:**
- `invocation_id|prefix`: invocation identifier (id or unique prefix)

**flags:**
- `-r, --repo`: repo name, key, id, or prefix

**behavior:**
1. performs TTY preflight
2. resolves invocation via daemon-first navigation
3. verifies invocation mode is `headed` (attach not supported for headless)
4. verifies tmux session exists
5. attaches to tmux session (blocks until user detaches)

**output:**
(enters tmux session)

**error codes:**
- `E_NOT_INTERACTIVE` — command requires an interactive terminal
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_AMBIGUOUS` — ref matches multiple invocations
- `E_INVOCATION_INVALID_MODE` — invocation is headless; attach only supports headed
- `E_SESSION_ENDED` — tmux session not found (may have exited)

### `agency agent stop`

sends a graceful stop signal (Ctrl-C / SIGINT) to a running invocation.

**usage:**
```bash
agency agent stop <invocation_id|name|prefix> [--repo <name|id|prefix>] [--json]
```

**arguments:**
- `invocation_id|name|prefix`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--repo`: repo name, key, id, or prefix
- `--json`: machine-readable mutation envelope output

**behavior (headed mode):**
1. resolves invocation
2. sends C-c to the tmux session via `tmux send-keys`
3. updates invocation meta: sets `stop_requested_at` and `flags.needs_attention=true`

**behavior (headless mode):**
1. resolves invocation
2. sends stop request to daemon via IPC
3. daemon sends SIGINT to the runner's process group
4. updates invocation meta: sets `stop_requested_at` and `flags.needs_attention=true`

**note:** this does not guarantee termination — the runner may ignore the signal.
use `agency agent kill` for forceful termination.

**output:**
```
Stop signal sent to invocation 20260131120500-b7c9
Note: The runner may ignore the interrupt. Use 'agency agent kill' to force termination.
```

**error codes:**
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_INVOCATION_ID_AMBIGUOUS` — identifier matches multiple invocations
- `E_DAEMON_NOT_RUNNING` — daemon not running for headless invocation (and no PGID available)

### `agency agent kill`

forcefully terminates a running invocation.

**usage:**
```bash
agency agent kill <invocation_id|name|prefix> [--repo <name|id|prefix>] [--json]
```

**arguments:**
- `invocation_id|name|prefix`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--repo`: repo name, key, id, or prefix
- `--json`: machine-readable mutation envelope output

**behavior (headed mode):**
1. resolves invocation
2. kills the tmux session via `tmux kill-session`
3. updates invocation meta: `status=failed`, `exit_reason=killed`, `finished_at=now`

**behavior (headless mode):**
1. resolves invocation
2. sends kill request to daemon via IPC
3. daemon sends SIGKILL to the runner's process group
4. updates invocation meta: `status=failed`, `exit_reason=killed`, `finished_at=now`

**note:** sandbox is preserved for inspection. The invocation can be resolved by name (if named) or ID/prefix.

**output:**
```
Killed invocation 20260131120500-b7c9
Sandbox preserved at: /path/to/sandboxes/20260131120500-b7c9/tree
```

**error codes:**
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_INVOCATION_ID_AMBIGUOUS` — identifier matches multiple invocations

### `agency agent diff`

shows sandbox changes vs the integration worktree.

**usage:**
```bash
agency agent diff <invocation_id|name|prefix> [--repo <name|id|prefix>] [--json] [--turn <entry_id> | --turn-range <start>..<end>]
```

**arguments:**
- `invocation_id|name|prefix`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--repo`: repo name, key, id, or prefix
- `--json`: machine-readable output
- `--turn`: single timeline entry id to anchor deterministic diff context
- `--turn-range`: inclusive timeline range (`<start_entry_id>..<end_entry_id>`)

**behavior:**
- default mode shows committed diff (`base_commit..sandbox_tip`) and uncommitted sandbox changes
- turn-aware mode maps selected timeline turn(s) to checkpoint-bounded commit ranges
- when turn selectors are used, uncommitted working tree diff is intentionally excluded
- invalid selectors fail with `E_INVALID_ARGUMENT`; missing checkpoint mapping fails with `E_CHECKPOINT_NOT_FOUND`

**examples:**
```bash
agency agent diff 20260131
agency agent diff --repo myrepo my-invocation
agency agent diff --repo abc123 my-invocation
agency agent diff --turn inv_event:2:agency.followup_prompt 20260131
agency agent diff --turn-range stream:4..stream:9 --json 20260131
```

### `agency agent review`

shows canonical review/readiness state for invocation progression.

**usage:**
```bash
agency agent review <invocation_id|name|prefix> [-r|--repo <name|id|prefix>] [-j|--json]
```

**arguments:**
- `invocation_id|name|prefix`: invocation identifier (name, id, or unique prefix)

**flags:**
- `-r, --repo`: repo name, key, id, or prefix
- `-j, --json`: machine-readable output

**behavior:**
- reports deterministic review verdict (`ready` or `blocked`) with typed blocking reasons
- includes explicit `pr_sync_eligible` and invocation-linked navigation commands (`history`, `diff`, `pr sync`)
- in headless strict mode, includes report-contract metadata (`report_source`, `report_diagnostics`) when available
- terminal and json modes share the same truth (no dual semantics)

**examples:**
```bash
agency agent review 20260131
agency agent review --repo myrepo my-invocation
agency agent review --repo abc123 my-invocation
agency agent review --json 20260131
agency agent review -r abc123 -j my-invocation
```

### `agency agent land`

applies sandbox changes back to the integration worktree.

**usage:**
```bash
agency agent land <invocation_id|name|prefix> [--repo <name|id|prefix>] [--apply] [--require-base] [--json]
```

**arguments:**
- `invocation_id|name|prefix`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--repo`: repo name, key, id, or prefix
- `--apply`: apply uncommitted changes as a patch (when sandbox has no commits)
- `--require-base`: fail if integration branch has moved since sandbox was created
- `--json`: machine-readable mutation envelope output

**behavior:**
- **cherry-pick mode (default)**: cherry-picks sandbox commits onto integration HEAD
- **apply mode (--apply)**: applies uncommitted changes as a patch
- **conflicts**: landing aborts and sandbox is preserved for resolution
- **cleanup**: on success, sandbox worktree, branch, and checkpoint refs are removed

### `agency agent discard`

discards a sandbox without landing changes.

**usage:**
```bash
agency agent discard <invocation_id|name|prefix> [--repo <name|id|prefix>] [--json]
```

**arguments:**
- `invocation_id|name|prefix`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--repo`: repo name, key, id, or prefix
- `--json`: machine-readable mutation envelope output

**behavior:**
- stops running invocations (graceful, then forceful after 5s)
- removes sandbox worktree, branch, and checkpoint refs
- preserves invocation record with `landing_status=discarded`

### `agency agent open`

opens the sandbox in the configured editor.

**usage:**
```bash
agency agent open <invocation_id|name|prefix> [-r|--repo <name|id|prefix>] [--editor <name>]
```

resolves invocation via daemon-first navigation kernel. no local store discovery.

**flags:**
- `-r, --repo`: repo name, key, id, or prefix
- `--editor`: editor override (default: configured editor)

**error codes:**
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_AMBIGUOUS` — ref matches multiple invocations (via navigation kernel)
- `E_SANDBOX_MISSING` — sandbox directory no longer exists on disk

### `agency agent path`

prints the daemon-resolved sandbox path for scripting.

**usage:**
```bash
agency agent path <invocation_ref> [-r|--repo <name|id|prefix>]
```

**example:**
```bash
cd $(agency agent path 20260131)
```

resolves invocation via daemon-first navigation kernel. no local store discovery.
`agent path` is a pure path-printing surface — it does not fail if the path no longer exists.

**flags:**
- `-r, --repo`: repo name, key, id, or prefix

### `agency agent shell`

opens a login shell with the working directory set to the sandbox path.

**usage:**
```bash
agency agent shell <invocation_ref>
```

spawns `$SHELL` (or `/bin/sh`) with the daemon-resolved sandbox as the working directory.

**error codes:**
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_AMBIGUOUS` — ref matches multiple invocations
- `E_SANDBOX_MISSING` — sandbox directory no longer exists on disk

### `agency agent enter`

attaches to a running headed invocation's tmux session (canonical interactive navigation).

**usage:**
```bash
agency agent enter <invocation_ref> [-r|--repo <name|id|prefix>]
```

resolves invocation identity/path via daemon-first navigation kernel with TTY preflight.
headed-only: headless invocations are rejected with `E_INVOCATION_INVALID_MODE`.
tmux session name is derived deterministically from `tmux.SessionName(invocation_id)`.

**flags:**
- `-r, --repo`: repo name, key, id, or prefix

**error codes:**
- `E_NOT_INTERACTIVE` — not running in an interactive terminal
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_AMBIGUOUS` — ref matches multiple invocations
- `E_INVOCATION_INVALID_MODE` — invocation is headless; enter only supports headed
- `E_SESSION_ENDED` — tmux session not found

### `agency agent chat`

sends a follow-up prompt to an existing headless invocation without creating a new invocation.

**usage:**
```bash
agency agent chat <invocation_ref> [--repo <name|id|prefix>] [--prompt <text> | --prompt-file <path>] [--json]
```

**arguments:**
- `invocation_ref`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--prompt`: inline follow-up prompt
- `--prompt-file`: read follow-up prompt from file (bounded read, max 256KB)
- `--json`: machine-readable mutation envelope output
- `--repo`: repo name, key, id, or prefix

**json mutation envelope (`--json`):**
- stable top-level fields: `ok`, `error_code`, `message`, `hint`, `request_id`, `api_version`, `build_version`, `client_request_id`
- command-specific success fields are additive (for example `invocation_id`, `timeline_entry_id`, `already_applied`)
- failure responses are emitted to stdout as JSON (`ok=false`) so automation does not need stderr parsing

**behavior:**
1. resolves invocation through daemon-first navigation
2. validates target is running headless invocation
3. sends follow-up prompt with `client_request_id` for idempotent control-plane handling
4. appends a follow-up prompt invocation event (or reuses existing event on duplicate request identity)
5. returns the unified timeline entry identity; no new invocation is created

**error codes:**
- `E_PROMPT_REQUIRED` — prompt missing or empty
- `E_PROMPT_TOO_LARGE` — prompt exceeds 256KB bound
- `E_USAGE` — invalid prompt flags (for example both `--prompt` and `--prompt-file`)
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_INVOCATION_INVALID_MODE` — invocation is not headless
- `E_INVOCATION_NOT_RUNNING` — invocation exists but is not running

### `agency agent restart`

applies a checkpoint and restarts the same headless invocation in one invocation-scoped flow.

**usage:**
```bash
agency agent restart <invocation_ref> (--checkpoint <id> | --history) [--repo <name|id|prefix>] [--model <name>] [--effort <level>] [--runner-arg <arg>]... [--env KEY=VALUE]... [--json]
```

**arguments:**
- `invocation_ref`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--checkpoint`: checkpoint id to restore before restart (explicit/script-safe mode)
- `--history`: open interactive TUI picker over timeline history (groups entries into conversation turns with checkpoint badges)
- `--model`: model override for restarted runner (supported for `claude-code`, `codex`, and `cursor`)
- `--effort`: effort override for restarted runner (supported for `claude-code` and `codex`; `cursor` uses thinking-capable model ids via `--model`)
- `--runner-arg`: additional argument to pass to the restarted runner (repeatable)
- `--env`: explicit env override for restarted runner, format `KEY=VALUE` (repeatable)
- `--json`: machine-readable mutation envelope output
- `--repo`: repo name, key, id, or prefix

**json mutation envelope (`--json`):**
- stable top-level fields: `ok`, `error_code`, `message`, `hint`, `request_id`, `api_version`, `build_version`, `client_request_id`
- command-specific success fields are additive (for example `invocation_id`, `checkpoint_id`, `snapshot_commit`, `restored_at`, `pid`, `pgid`, `log_paths`)
- failure responses are emitted to stdout as JSON (`ok=false`) so automation does not need stderr parsing

**behavior:**
1. resolves invocation through daemon-first navigation
2. validates target is headless
3. if `--history` is used, requires an interactive terminal and opens a full-screen TUI picker (`↑/↓` or `k/j`, `home/g`, `end/G`, `enter` to confirm, `q/esc` to cancel) that groups timeline entries into conversation turns (prompt, assistant + completed tool calls, follow-up) with checkpoint badges, checkpoint trigger metadata, and authoritative changed-path previews
4. each turn carries the latest valid checkpoint at or before it; selecting a turn without a checkpoint returns deterministic error guidance
5. if invocation has a stored custom-env profile, requires explicit replay of all required env keys
6. if running, force-stops current process and waits for terminalization
7. rewinds sandbox branch `HEAD` to the checkpoint's recorded `sandbox_head_sha`, then restores the checkpoint snapshot tree exactly
8. restarts runner under the same `invocation_id` and returns new `pid/pgid`

`agent restart` always reuses the stored original prompt (`invocations/<id>/prompt.txt`). use `agency checkpoint apply` when you want restore-only behavior without replaying the prompt.

**error codes:**
- `E_USAGE` — invalid CLI usage (for example malformed `--env` value, missing selector mode, or conflicting `--checkpoint` + `--history`)
- `E_NOT_INTERACTIVE` — `--history` used outside an interactive terminal
- `E_ABORTED` — interactive selection canceled by user
- `E_INVALID_ARGUMENT` — history/checkpoint selection input is invalid or exceeds bounded picker limits
- `E_INVALID_REQUEST` — restart env replay is incomplete for required keys
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_INVOCATION_INVALID_MODE` — invocation is not headless
- `E_CHECKPOINT_NOT_FOUND` — explicit checkpoint id does not exist, or selected history point has no valid checkpoint mapping
- `E_RUNNER_ARG_CONFLICT` — runner args include reserved flags
- `E_RUNNER_START_FAILED` — runner failed to start after restore

### `agency agent history`

shows a rich transcript of the agent's conversation for an invocation.

for runners with semantic adapters (claude, codex, cursor), the default output is a styled, human-readable transcript showing assistant messages, tool use with inputs, prompts, and results. for other runners, falls back to a sparse timeline format.

**usage:**
```bash
agency agent history <invocation_ref> [--last] [--repo <name|id|prefix>] [--limit <n>] [--cursor <opaque>] [--json]
```

**arguments:**
- `invocation_ref`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--last`: show only the most recent timeline entry (mutually exclusive with `--cursor`)
- `--limit`: maximum entries returned per page (default: 100, required range: 1..500)
- `--cursor`: opaque continuation cursor from prior response
- `--json`: machine-readable output (includes full `content_blocks` in message entries)
- `--repo`: repo name, key, id, or prefix

**output modes:**
- **human** (default): rich transcript with styled headers, tool use blocks, and exit codes. adapters that produce `content_blocks` (claude, codex, cursor) get full rendering; non-adapted runners fall back to sparse one-liners.
- **json** (`--json`): structured output with full `content_blocks` data for programmatic consumption.

**entry coverage:**
- prompt seed context (`prompt_seed`)
- assistant/user messages (`message`) — includes `content_blocks` with structured text, tool_use (name, id, input), and tool_result (tool_use_id, content) blocks when available
- tool activity (`tool_use`)
- follow-up prompts (`followup_prompt`)
- raw-log coverage marker (`raw_log_coverage`)
- invocation/checkpoint lifecycle events (`invocation_event` / `checkpoint_event`)

**pagination model:**
- deterministic keyset cursoring (no offset drift)
- incremental continuation is stable across pages (no duplicate/skip drift)
- invalid `--limit` values fail closed with `E_INVALID_ARGUMENT` (no silent coercion)
- `--last` uses server-side reverse ordering (`order=desc`) with `limit=1`; cursor pagination is not supported with `--last`

### `agency agent logs`

views invocation logs.

**usage:**
```bash
agency agent logs <invocation_id|name|prefix> [--kind <type>] [--follow] [--offset <bytes>]
```

**arguments:**
- `invocation_id|name|prefix`: invocation identifier (name, id, or unique prefix)

**flags:**
- `--kind`: log stream type — `raw` (default), `stderr`, or `stream`
- `--follow`: tail -f style polling for new data (500ms intervals, exit with Ctrl-C)
- `--offset`: byte offset to start reading from

**log kinds:**
- **raw**: verbatim runner stdout (JSONL as emitted by claude/codex) — default
- **stderr**: runner stderr (errors, warnings)
- **stream**: normalized events (written by daemon stream parser, if available)

## `agency daemon` (v2)

manages the agency daemon — the supervisor for headless agent invocations.

### `agency daemon start`

starts the daemon. by default starts as a detached background process. use `--foreground` for service managers or debugging.

**usage:**
```bash
agency daemon start               # background (default)
agency daemon start --foreground   # foreground (for launchd/systemd)
```

**flags:**
- `--foreground`: run in foreground (for service managers or debugging)

**behavior (background, default):**
1. checks if daemon already running via health endpoint → exits 0 if so (idempotent)
2. cleans stale PID/socket if process is dead
3. starts daemon as detached background process (re-execs with `--foreground`)
4. waits up to 10s for health check to pass
5. prints PID, socket path, and instance ID

**behavior (foreground):**
1. checks for existing daemon via PID file
2. if daemon already running: prints message and exits 0 (idempotent)
3. cleans up any stale socket file
4. creates Unix socket at `${AGENCY_DATA_DIR}/agencyd.sock` (permissions 0600)
5. writes PID file to `${AGENCY_DATA_DIR}/agencyd.pid`
6. runs recovery scan (marks orphaned invocations)
7. runs HTTP server loop until SIGINT/SIGTERM
8. on shutdown: flushes pending meta writes, removes socket and PID file

**output:**
```
Agency daemon started (pid 12345)
Socket: /path/to/agencyd.sock
Instance ID: <uuid>
```

### `agency daemon status`

shows daemon status.

**usage:**
```bash
agency daemon status [--json]
```

**flags:**
- `--json`: output as JSON

**behavior:**
1. connects to daemon socket
2. calls `GET /health`
3. displays health information

**output:**
```
Daemon is running
  PID:           12345
  Instance ID:   <uuid>
  API Version:   1
  Build Version: v0.1.0
  Uptime:        3600s
```

**error codes:**
- `E_DAEMON_NOT_RUNNING` — daemon is not running

### `agency daemon stop`

stops the daemon.

**usage:**
```bash
agency daemon stop [--force]
```

**flags:**
- `--force`: terminate all active invocations before stopping

**behavior:**
1. attempts RPC shutdown via `POST /shutdown`
2. if active invocations exist without `--force`: returns `E_DAEMON_BUSY` with list
3. if `--force`: daemon terminates all invocations (SIGINT → wait 5s → SIGKILL)
4. if RPC fails: falls back to PID file, sends SIGTERM (wait 5s → SIGKILL)
5. cleans up stale PID and socket files

**output:**
```
Daemon shutdown initiated
```

**error codes:**
- `E_DAEMON_NOT_RUNNING` — daemon is not running
- `E_DAEMON_BUSY` — active invocations exist (use `--force` to override)

### `agency daemon install`

installs the daemon as an OS-managed service that starts automatically on login.

**usage:**
```bash
agency daemon install
```

**behavior:**
- **macOS**: writes a launchd plist to `~/Library/LaunchAgents/com.agency.daemon.plist` and loads it with `launchctl load -w`
- **Linux**: writes a systemd user unit to `~/.config/systemd/user/agency-daemon.service`, runs `daemon-reload`, `enable`, and `start`
- the service runs `agency daemon start --foreground` and is configured to restart on failure

**output:**
```
Daemon installed as launchd service
  Service file: /Users/alice/Library/LaunchAgents/com.agency.daemon.plist
  Binary:       /usr/local/bin/agency

The daemon will start automatically on login.
```

**error codes:**
- `E_DAEMON_SERVICE_ALREADY_INSTALLED` — service is already installed
- `E_DAEMON_SERVICE_INSTALL_FAILED` — install operation failed
- `E_DAEMON_SERVICE_UNSUPPORTED` — platform not supported (not macOS or Linux)

### `agency daemon uninstall`

removes the daemon OS service.

**usage:**
```bash
agency daemon uninstall
```

**behavior:**
- **macOS**: unloads with `launchctl unload -w` and removes the plist file
- **Linux**: stops, disables, removes the unit file, and runs `daemon-reload`

**output:**
```
Daemon launchd service uninstalled
```

**error codes:**
- `E_DAEMON_SERVICE_NOT_INSTALLED` — service is not installed
- `E_DAEMON_SERVICE_UNINSTALL_FAILED` — uninstall operation failed
- `E_DAEMON_SERVICE_UNSUPPORTED` — platform not supported

## `agency checkpoint` (v2)

manages checkpoints for headless agent invocations. checkpoints are automatic snapshots of sandbox state created by the daemon during execution.

### `agency checkpoint ls`

lists checkpoints for a headless invocation.

**usage:**
```bash
agency checkpoint ls --invocation <name|id|prefix> [--json]
```

**flags:**
- `--invocation`: invocation identifier (required)
- `--json`: output as JSON

**behavior:**
1. resolves invocation (by name, id, or unique prefix)
2. verifies invocation is headless mode
3. loads `checkpoints.json` from sandbox directory
4. displays checkpoint list

**output:**
```
Checkpoints for invocation 20260201120500-b7c9:

ID    Created               Untracked   Head SHA    Diffstat
----  --------------------  ----------  ----------  --------
1     2026-02-01 12:05:30   yes         abc123ef    3 files changed, 25+/10-
2     2026-02-01 12:06:15   yes         def456ab    1 file changed, 5+/2-
3     2026-02-01 12:08:00   no          ghi789cd    2 files changed, 10+/5-
```

**json output:**
```json
{
  "schema_version": "1.0",
  "checkpoints": [
    {
      "id": 1,
      "snapshot_ref": "refs/agency/snapshots/20260201120500-b7c9/1",
      "snapshot_commit": "abc123ef...",
      "sandbox_head_sha": "...",
      "created_at": "2026-02-01T12:05:30Z",
      "includes_untracked": true,
      "diffstat": "+25 -10 in 3 files",
      "tree_sha": "def456..."
    }
  ]
}
```

checkpoints are deduplicated: if the sandbox tree hasn't changed since the last checkpoint (same `tree_sha`), the engine skips creating a new snapshot. this prevents duplicate checkpoints from poll-tick noise.

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_INVOCATION_ID_AMBIGUOUS` — prefix matches multiple invocations
- `E_INVOCATION_INVALID_MODE` — invocation is headed (only headless supported)

### `agency checkpoint apply`

restores a sandbox to a checkpoint state (rollback).

**usage:**
```bash
agency checkpoint apply --invocation <name|id|prefix> <checkpoint_id>
```

**arguments:**
- `checkpoint_id`: the checkpoint ID to restore to (positive integer)

**flags:**
- `--invocation`: invocation identifier (required)

**behavior:**
1. resolves invocation (by name, id, or unique prefix)
2. verifies invocation is headless mode
3. verifies invocation is NOT running (must be stopped/finished)
4. sends apply request to daemon
5. daemon performs rollback: `git reset --hard`, `git clean -fd`, `git read-tree --reset -u <snapshot>`
6. emits `checkpoint_apply_started` and `checkpoint_applied` events

**output:**
```
Restored to checkpoint 3 (commit ghi789cd)
```

**preconditions:**
- invocation must be headless mode
- invocation must NOT be running (use `agency agent stop` or `agency agent kill` first)
- checkpoint must exist in `checkpoints.json`
- daemon must be running

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_INVOCATION_ID_AMBIGUOUS` — prefix matches multiple invocations
- `E_INVOCATION_INVALID_MODE` — invocation is headed (only headless supported)
- `E_INVOCATION_STILL_RUNNING` — invocation is still running; stop or kill first
- `E_CHECKPOINT_NOT_FOUND` — checkpoint_id does not exist or snapshot commit is inaccessible
- `E_ROLLBACK_FAILED` — git reset/clean/checkout failed during restore
- `E_CHECKPOINT_FAILED` — checkpoint subsystem error (e.g., failed to load checkpoints.json)
- `E_DAEMON_NOT_RUNNING` — daemon is not running

all checkpoint errors are typed `AgencyError` objects with stable codes. the daemon handler dispatches on `errors.GetCode()`, not string matching.

**notes:**
- after rollback, start a new invocation to continue work
- checkpoint refs remain valid for future rollback
- rollback overwrites all sandbox files (tracked and untracked)
- original sandbox HEAD is not preserved (use a different checkpoint to go back)
- rollback is restore-only; it does not restart runner execution or replay prompts

**examples:**
```bash
agency checkpoint ls --invocation my-agent           # list checkpoints
agency checkpoint ls --invocation 20260201 --json   # JSON output
agency checkpoint apply --invocation my-agent 3     # restore to checkpoint 3
```

## `agency init`

creates `agency.json` template and stub scripts in the current git repo.

**flags:**
- `--no-gitignore`: do not modify `.gitignore` (by default, `.agency/` is appended)
- `--force`: overwrite existing `agency.json` (scripts are never overwritten)

**files created:**
- `agency.json` — configuration file with defaults
- `CLAUDE.md` — runner protocol file (instructs runners on status reporting)
- `scripts/agency_setup.sh` — stub setup script (exits 0)
- `scripts/agency_verify.sh` — stub verify script (exits 1, must be replaced)
- `scripts/agency_archive.sh` — stub archive script (exits 0)
- `.gitignore` entry for `.agency/` (unless `--no-gitignore`)

**output:**
```
repo_root: /path/to/repo
agency_json: created
scripts_created: scripts/agency_setup.sh, scripts/agency_verify.sh, scripts/agency_archive.sh
gitignore: updated
claude_md: created
```

## `agency completion`

generates shell completion scripts for bash or zsh using Cobra's built-in generators.

**usage:**
```bash
agency completion <shell> [--output <path>]
```

**arguments:**
- `shell`: target shell (`bash` or `zsh`)

**flags:**
- `--output`: write completion script to file instead of stdout

**behavior:**
- prints completion script to stdout (or file with `--output`)
- completions are generated by Cobra (not handwritten)
- provides static subcommand/flag completion out of the box
- dynamic completion for run names will be added in a future release

**examples:**
```bash
agency completion bash > ~/.agency-completion.bash
agency completion zsh > ~/.zsh/completions/_agency
agency completion --output ~/.local/share/bash-completion/completions/agency bash
```

## `agency doctor`

verifies all prerequisites are met for running agency commands.

**checks:**
- repo root discovery via `git rev-parse --show-toplevel`
- `agency.json` exists and is valid
- required tools installed: `git`, `tmux`, `gh`
- `gh` is authenticated (`gh auth status`)
- runner command exists for selected runner id (via explicit `config.runners` mapping)
- scripts exist and are executable

**on success:**
- writes/updates `${AGENCY_DATA_DIR}/repo_index.json`
- writes/updates `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`

**output (stable key: value format):**
```
repo_root: /path/to/repo
agency_data_dir: ~/Library/Application Support/agency
agency_config_dir: ~/Library/Preferences/agency
agency_cache_dir: ~/Library/Caches/agency
repo_key: github:owner/repo
repo_id: abcd1234ef567890
origin_present: true
origin_url: git@github.com:owner/repo.git
origin_host: github.com
github_flow_available: true
git_version: git version 2.40.0
tmux_version: tmux 3.3a
gh_version: gh version 2.40.0 (2024-01-15)
gh_authenticated: true
defaults_parent_branch: main
defaults_runner: claude-code
runner_cmd: claude
script_setup: /path/to/repo/scripts/agency_setup.sh
script_verify: /path/to/repo/scripts/agency_verify.sh
script_archive: /path/to/repo/scripts/agency_archive.sh
status: ok
```

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_NO_AGENCY_JSON` — agency.json not found
- `E_INVALID_AGENCY_JSON` — agency.json validation failed
- `E_GIT_NOT_INSTALLED` — git not found
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_GH_NOT_INSTALLED` — gh CLI not found
- `E_GH_NOT_AUTHENTICATED` — gh not authenticated
- `E_RUNNER_NOT_CONFIGURED` — runner command not found
- `E_SCRIPT_NOT_FOUND` — required script not found
- `E_SCRIPT_NOT_EXECUTABLE` — script is not executable (suggests `chmod +x`)
- `E_PERSIST_FAILED` — failed to write persistence files

## `agency run`

creates an isolated workspace and launches the runner in a tmux session.
by default, attaches to the tmux session after creation.

**usage:**
```bash
agency run --name <name> [--runner <name>] [--parent <branch>] [--repo <path>] [--detached] [--open]
```

**flags:**
- `--name`: run name (required, 2-40 chars, lowercase alphanumeric with hyphens, must start with letter)
- `--runner`: runner name (default: agency.json `defaults.runner`; command must be mapped in `config.runners`)
- `--parent`: parent branch to branch from (default: agency.json `defaults.parent_branch`)
- `--repo`: target a specific repo path instead of current directory
- `--detached`: do not attach to tmux session after creation
- `--open`: open the created workspace and skip auto-attach

**behavior:**
1. validates parent working tree is clean (`git status --porcelain`)
2. creates git worktree + branch under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/`
3. creates `.agency/`, `.agency/out/`, `.agency/tmp/`, `.agency/state/` directories
4. creates `.agency/INSTRUCTIONS.md` with runner guidance (overwritten on every run)
5. creates `.agency/report.md` with template (name as heading, used for PR body when complete)
6. runs `scripts.setup` with injected environment variables (timeout: 10 minutes)
7. creates tmux session `agency_<run_id>` running the runner command
8. writes `meta.json` with run metadata
9. attaches to tmux session (unless `--detached` or `--open`)

**success output (with `--detached`):**
```
run_id: 20260110120000-a3f2
name: feature-x
runner: claude-code
parent: main
branch: agency/feature-x-a3f2
worktree: ~/Library/Application Support/agency/repos/abc123/worktrees/20260110120000-a3f2
tmux: agency_20260110120000-a3f2
next: agency attach feature-x
```

note: the `next:` line is shown whenever auto-attach is skipped (`--detached` or `--open`). with `--open`, output also includes `open_status: opened|failed` and open dispatch failure is warning-only (workspace creation still succeeds).

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_NO_AGENCY_JSON` — agency.json not found
- `E_INVALID_AGENCY_JSON` — agency.json validation failed
- `E_PARENT_DIRTY` — parent working tree has uncommitted changes
- `E_EMPTY_REPO` — repository has no commits
- `E_PARENT_BRANCH_NOT_FOUND` — specified parent branch does not exist locally
- `E_WORKTREE_CREATE_FAILED` — git worktree add failed
- `E_SCRIPT_FAILED` — setup script exited non-zero
- `E_SCRIPT_TIMEOUT` — setup script timed out (>10 minutes)
- `E_TMUX_FAILED` — tmux session creation failed
- `E_TMUX_ATTACH_FAILED` — tmux attach failed

**on failure:**

if the run fails after worktree creation, the error output includes:
- `run_id`
- `worktree` path (for inspection)
- `setup_log` path (if setup failed)

the worktree and metadata are retained for debugging; use `agency clean <id>` to remove.

## `agency ls`

lists runs and their statuses.

**usage:**
```bash
agency ls [--all] [--all-repos] [--json]
```

**flags:**
- `--all`: include archived runs (worktree deleted)
- `--all-repos`: list runs across all repos (ignores current repo scope)
- `--json`: output as JSON (stable format)

**default behavior:**
- if **inside a git repo**: lists runs for that repo only, excluding archived
- if **outside any git repo**: lists runs across all repos, excluding archived

**human output columns:**
- `RUN_ID`: full run identifier
- `NAME`: run name (truncated to 50 chars; `<broken>` for corrupt meta; `<untitled>` for empty)
- `STATUS`: derived status (e.g., "active", "idle", "ready for review", "merged (archived)")
- `SUMMARY`: runner-reported summary (truncated to 40 chars; shows stall duration for stalled runs; `-` if unavailable)
- `PR`: PR number if exists (e.g., "#123")

**example output:**
```
RUN_ID              NAME            STATUS            SUMMARY                    PR
20260119-a3f2       auth-fix        needs input       Which auth library?        #123
20260118-c5d2       bug-fix         stalled           (no activity for 45m)      -
20260118-e7f3       feature-x       working           Implementing validation    -
```

**empty state:**
- inside repo without `--all`: `no active runs (use --all to include archived)`
- inside repo with `--all`: `no runs found`
- outside repo / `--all-repos`: `no runs found`

**status values** (in precedence order):
- `broken`: meta.json is unreadable/invalid
- `merged`: PR merged
- `abandoned`: explicitly abandoned
- `failed`: setup script failed
- `needs attention`: verify failed, PR not mergeable, or stop requested
- `ready for review`: runner reports work complete
- `needs input`: runner waiting for user answer
- `blocked`: runner cannot proceed
- `working`: runner actively making progress
- `stalled`: no status update for 15+ minutes (tmux active)
- `active`: tmux session exists (fallback when no runner status)
- `idle`: no tmux session (fallback)
- `(archived)` suffix: worktree no longer exists

**json output:**
```json
{
  "schema_version": "1.0",
  "data": [
    {
      "run_id": "20260110120000-a3f2",
      "repo_id": "abc123",
      "repo_key": "github:owner/repo",
      "origin_url": "git@github.com:owner/repo.git",
      "name": "feature-x",
      "runner": "claude",
      "created_at": "2026-01-10T12:00:00Z",
      "last_push_at": "2026-01-10T14:00:00Z",
      "tmux_active": true,
      "worktree_present": true,
      "archived": false,
      "pr_number": 123,
      "pr_url": "https://github.com/owner/repo/pull/123",
      "derived_status": "ready for review",
      "summary": "Implementing user authentication",
      "broken": false
    }
  ]
}
```

**sorting:**
- newest `created_at` first
- broken runs (null `created_at`) sort last
- tie-breaker: `run_id` ascending

**examples:**
```bash
agency ls                    # list current repo runs
agency ls --all              # include archived runs
agency ls --all-repos        # list all repos
agency ls --all-repos --all  # everything
agency ls --json             # machine-readable output
agency ls --json | jq '.data[].run_id'
```

## `agency show`

shows detailed information about a single run.

**usage:**
```bash
agency show <run_id> [--json] [--path] [--capture]
```

**arguments:**
- `run_id`: the run identifier (exact) or unique prefix

**flags:**
- `--json`: output as JSON (stable format)
- `--path`: output only resolved filesystem paths
- `--capture`: capture tmux scrollback to transcript files (mutating mode)

**behavior:**
- resolves run_id globally (works from anywhere, not just inside a repo)
- accepts exact run_id or unique prefix for convenience
- displays rich metadata, derived status, and paths

**id resolution:**
- exact match wins if found
- if no exact match, checks for unique prefix match
- multiple matches: fails with `E_RUN_ID_AMBIGUOUS` and lists candidates
- no matches: fails with `E_RUN_NOT_FOUND`

**human output:**
```
run: 20260110120000-a3f2
name: feature-x
repo: abc123
runner: claude
parent: main
branch: agency/feature-x-a3f2
worktree: ~/Library/Application Support/agency/repos/abc123/worktrees/20260110120000-a3f2

tmux: agency_20260110120000-a3f2
pr: https://github.com/owner/repo/pull/123 (#123)
last_push_at: 2026-01-10T14:00:00Z
last_report_sync_at: 2026-01-10T14:00:00Z
report_hash: abc123def456...
status: ready for review

runner_status:
  status: needs_input
  updated: 5m ago
  summary: Implementing OAuth but need clarification
  questions:
    - Which OAuth provider should I use?
    - Should sessions persist across restarts?
```

note: there is a blank line between `worktree:` and `tmux:`.

when PR is missing: `pr: none (#-)`
when timestamps are missing: `last_push_at: none`
runner_status section only appears when `.agency/state/runner_status.json` exists and is valid.

**json output:**
```json
{
  "schema_version": "1.0",
  "data": {
    "meta": { /* raw meta.json */ },
    "repo_id": "abc123",
    "repo_key": "github:owner/repo",
    "origin_url": "git@github.com:owner/repo.git",
    "archived": false,
    "derived": {
      "derived_status": "active",
      "tmux_active": true,
      "worktree_present": true,
      "report": { "exists": true, "bytes": 256, "path": "..." },
      "logs": { "setup_log_path": "...", "verify_log_path": "...", "archive_log_path": "..." },
      "runner_status": {
        "status": "needs_input",
        "updated_at": "2026-01-10T14:00:00Z",
        "summary": "Implementing OAuth but need clarification",
        "questions": ["Which OAuth provider?"],
        "blockers": [],
        "how_to_test": "",
        "risks": []
      }
    },
    "paths": {
      "repo_root": "/path/to/repo",
      "worktree_root": "/path/to/worktree",
      "run_dir": "/path/to/run",
      "events_path": "/path/to/events.jsonl",
      "transcript_path": "/path/to/transcript.txt"
    },
    "broken": false
  }
}
```

**path output:**
```
repo_root: /path/to/repo
worktree_root: /path/to/worktree
run_dir: /path/to/run
logs_dir: /path/to/run/logs
events_path: /path/to/run/events.jsonl
transcript_path: /path/to/run/transcript.txt
report_path: /path/to/worktree/.agency/report.md
```

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_RUN_ID_AMBIGUOUS` — prefix matches multiple runs (lists candidates)
- `E_RUN_BROKEN` — run exists but meta.json is unreadable/invalid

**broken run handling:**
- `ls` shows broken runs with `<broken>` title and `broken` status
- `show` targeting a broken run fails with `E_RUN_BROKEN`
- `--json` still outputs envelope with `broken=true` and `meta=null`
- `--path` outputs best-effort paths and exits non-zero

**`--capture` behavior:**
- takes repo lock (mutating mode)
- emits `cmd_start` and `cmd_end` events to `events.jsonl`
- captures full tmux scrollback from the session's primary pane
- strips ANSI escape codes from captured text
- rotates `transcript.txt` to `transcript.prev.txt` (single backup)
- writes new `transcript.txt` atomically
- if session is missing: warns and continues without transcript
- capture failures never block `show` output

**transcript files:**
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.txt`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.prev.txt`

**events file:**
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
- append-only JSONL format
- each line contains: `schema_version`, `timestamp`, `repo_id`, `run_id`, `event`, `data`

**examples:**
```bash
agency show my-feature           # show run details
agency show my-feature --json    # machine-readable output
agency show my-feature --path    # print paths only
agency show my-feature --capture # capture transcript + show
agency show my-feature --json | jq '.data.derived.derived_status'
```

## `agency path`

compatibility alias for `agency agent path`.
prints daemon-resolved sandbox path as a single line.

**usage:**
```bash
agency path <invocation_ref> [-r|--repo <name|id|prefix>]
```

**arguments:**
- `invocation_ref`: invocation id, name, or unique prefix

**flags:**
- `-r, --repo`: repo name, key, id, or prefix

**behavior:**
- resolves invocation via daemon-first navigation
- prints `sandbox_path` exactly (single line + newline)
- pure path output surface (does not check path existence)

**error codes:**
- `E_NO_REPO_CONTEXT` — no repo context and no `--repo` provided
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_AMBIGUOUS` — ref matches multiple invocations

## `agency open`

opens a run or invocation worktree in your editor.

resolves the reference as an invocation first (daemon-first), then
falls back to legacy run resolution if no invocation matches.

**usage:**
```bash
agency open <ref> [-r|--repo <name|id|prefix>] [--editor <name>]
```

**arguments:**
- `ref`: run or invocation id, name, or unique prefix

**flags:**
- `-r, --repo`: repo name, key, id, or prefix (invocation resolution only)
- `--editor`: editor override (default: configured editor)

**error codes:**
- `E_NO_REPO_CONTEXT` — no repo context and no `--repo` provided
- `E_INVOCATION_NOT_FOUND` — invocation not found (triggers fallback to run resolution)
- `E_RUN_NOT_FOUND` — ref matches neither invocation nor run
- `E_AMBIGUOUS` — ref matches multiple invocations
- `E_SANDBOX_MISSING` — sandbox directory no longer exists on disk

## `agency attach`

compatibility alias for `agency agent attach` (which itself aliases canonical `agency agent enter`).
attaches to a running headed invocation tmux session.

**usage:**
```bash
agency attach <invocation_ref> [-r|--repo <name|id|prefix>]
```

**arguments:**
- `invocation_ref`: invocation id, name, or unique prefix

**flags:**
- `-r, --repo`: repo name, key, id, or prefix

**behavior:**
1. performs TTY preflight
2. resolves invocation via daemon-first navigation
3. validates invocation mode is headed
4. validates tmux session exists
5. attaches to tmux session

**error codes:**
- `E_NOT_INTERACTIVE` — command requires an interactive terminal
- `E_NO_REPO_CONTEXT` — no repo context and no `--repo` provided
- `E_INVOCATION_NOT_FOUND` — invocation not found
- `E_AMBIGUOUS` — ref matches multiple invocations
- `E_INVOCATION_INVALID_MODE` — invocation is headless
- `E_SESSION_ENDED` — tmux session not found

## `agency stop`

sends C-c to the runner in the tmux session (best-effort interrupt).

**usage:**
```bash
agency stop <run_id>
```

**arguments:**
- `run_id`: the run identifier or name

**behavior:**
- if session exists: sends C-c to the primary pane, sets `needs_attention` flag, appends `stop` event
- if session missing: prints `no session for <id>` to stderr and exits 0 (no-op)

**notes:**
- best-effort only; does not guarantee the runner stops
- session remains alive; use `agency resume --restart` to guarantee a fresh runner
- does not mutate meta or events if session is missing

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_TMUX_FAILED` — tmux send-keys failed
- `E_PERSIST_FAILED` — failed to write event

## `agency kill`

kills the tmux session for a run. Workspace remains intact.

**usage:**
```bash
agency kill <run_id>
```

**arguments:**
- `run_id`: the run identifier or name

**behavior:**
- if session exists: kills the tmux session, appends `kill_session` event
- if session missing: prints `no session for <id>` to stderr and exits 0 (no-op)

**notes:**
- does not delete the worktree (use `agency clean <id>` for that)
- does not set any flags on the run
- does not append events if session is missing

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_TMUX_FAILED` — tmux kill-session failed
- `E_PERSIST_FAILED` — failed to write event

## `agency resume`

attaches to the tmux session for a run. If session is missing, creates one and starts the runner.

**usage:**
```bash
agency resume <run_id> [--detached] [--restart] [--yes]
```

**arguments:**
- `run_id`: the run identifier or name

**flags:**
- `--detached`: do not attach; return after ensuring session exists
- `--restart`: kill existing session (if any) and recreate
- `--yes`: skip confirmation prompt for `--restart`

**behavior:**
- if session exists (no `--restart`): attaches to session (unless `--detached`)
- if session missing: creates new tmux session with cwd in worktree, starts runner, then attaches (unless `--detached`)
- if `--restart`: prompts for confirmation (unless `--yes` or non-interactive), kills session if exists, creates new session

**locking:**
- resume acquires repo lock **only** when creating or restarting a session
- uses double-check pattern: check session existence, acquire lock, re-check under lock

**notes:**
- resume **never** runs scripts (setup/verify/archive)
- resume **never** touches git (worktree state preserved)
- `--restart` will lose in-tool history (chat context, etc.) but git state is unchanged
- archived runs cannot be resumed (`E_WORKTREE_MISSING`)

**output (detached mode):**
```
ok: session agency_<run_id> ready
```

**confirmation prompt (restart with existing session):**
```
restart session? in-tool history will be lost (git state unchanged) [y/N]:
```

**events:**
- `resume_attach`: session existed, attached
- `resume_create`: session missing, created new session
- `resume_restart`: `--restart` used, killed and recreated session
- `resume_failed`: worktree missing (archived or corrupted)

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_CONFIRMATION_REQUIRED` — `--restart` attempted in non-interactive mode without `--yes`
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_TMUX_FAILED` — tmux operation failed
- `E_RUNNER_NOT_CONFIGURED` — runner command not found

**examples:**
```bash
agency resume my-feature               # attach (create if needed)
agency resume my-feature --detached    # ensure session exists
agency resume my-feature --restart     # force fresh runner (prompts)
agency resume my-feature --restart --yes  # non-interactive restart
```

## `agency push`

pushes the run branch to origin and creates/updates a GitHub PR.

**usage:**
```bash
agency push <run_id> [--allow-dirty] [--force] [--force-with-lease]
```

**arguments:**
- `run_id`: the run identifier (exact or unique prefix)

**flags:**
- `--allow-dirty`: proceed even if worktree has uncommitted changes
- `--force`: retained for compatibility (no-op for report checks)
- `--force-with-lease`: use `git push --force-with-lease` when branch history was rewritten

**preflight checks (in order):**
1. resolve run_id and load metadata
2. verify worktree exists on disk
3. acquire repo lock (mutating command)
4. fail if worktree has uncommitted changes (unless `--allow-dirty`)
5. verify `origin` remote exists
6. verify origin host is exactly `github.com`
7. read report via bounded input contract (max 256 KiB), then decide PR body source (warnings/degrade only)
8. verify `gh auth status` succeeds

**git operations (after preflight passes):**
1. `git fetch origin` (non-destructive)
2. resolve parent ref (local branch preferred, else `origin/<parent_branch>`)
3. compute commits ahead via `git rev-list --count <parent_ref>..<branch>`
4. refuse if ahead == 0 (`--force` does NOT bypass this)
5. `git push -u origin <branch>` (no force push)

**pr operations (after git push succeeds):**
1. look up existing PR:
   - first by stored `pr_number` in meta.json
   - fallback to `gh pr view --head <branch>`
2. if PR exists but not OPEN (CLOSED or MERGED): fail with `E_PR_NOT_OPEN`
3. if no PR exists: create via `gh pr create`
   - title: `[agency] <run_name>`
   - body: canonical reports-v2 body when valid (`.agency/report.json` takes precedence over `.agency/report.md`), otherwise auto-generated PR body
4. sync PR body:
   - compute sha256 hash of the body file used
   - if hash unchanged from `last_report_hash`: skip sync
   - else: update PR body via `gh pr edit --body-file`

**success output:**
```
pr: https://github.com/owner/repo/pull/123
```

**metadata persistence:**
- updates `meta.json` with:
  - `last_push_at` timestamp
  - `pr_number` and `pr_url`
  - `last_report_sync_at` and `last_report_hash` (when report synced)
- appends events to `events.jsonl`:
  - `push_started`, `git_fetch_finished`, `git_push_finished`
  - `pr_created` (if created)
  - `pr_body_synced` (if body updated)
  - `push_finished` (on success)
  - `push_failed` (on failure)

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_RUN_ID_AMBIGUOUS` — prefix matches multiple runs
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_DIRTY_WORKTREE` — worktree has uncommitted changes without `--allow-dirty`
- `E_NO_ORIGIN` — no origin remote configured
- `E_UNSUPPORTED_ORIGIN_HOST` — origin is not github.com
- `E_GH_NOT_INSTALLED` — gh CLI not found
- `E_GH_NOT_AUTHENTICATED` — gh not authenticated
- `E_PARENT_NOT_FOUND` — parent branch not found locally or on origin
- `E_EMPTY_DIFF` — no commits ahead of parent branch
- `E_GIT_PUSH_FAILED` — git push failed
- `E_GH_PR_CREATE_FAILED` — gh pr create failed
- `E_GH_PR_EDIT_FAILED` — gh pr edit failed
- `E_GH_PR_VIEW_FAILED` — gh pr view failed after create (retries exhausted)
- `E_PR_NOT_OPEN` — PR exists but is not OPEN (CLOSED or MERGED)

**notes:**
- all git/gh subprocesses run with non-interactive environment:
  - `GIT_TERMINAL_PROMPT=0`
  - `GH_PROMPT_DISABLED=1`
  - `CI=1`
- PR creation uses `--body-file` to preserve markdown formatting
- PR title is NOT updated after creation (v1)
- push remains compatibility-first for report contract outcomes (warnings + deterministic fallback body, no progression block)
- when both report artifacts exist, `.agency/report.json` is authoritative and `.agency/report.md` is only used for diagnostics/conflict detection
- report/body processing is bounded for safety:
  - report reads capped at 256 KiB
  - fallback generation caps commit/file sections (10 commits, 20 files) with deterministic truncation indicators
- auto-generated PR bodies include commit subjects, bounded diffstat/files, and meta
- `--force` does NOT bypass `E_EMPTY_DIFF` (must have commits)
- `--allow-dirty` prints a warning and dirty context
- `--force-with-lease` uses `git push --force-with-lease` for safe force push after rebase

**non-fast-forward handling:**

when push fails due to non-fast-forward (e.g., after rebasing), agency detects this and prints a helpful hint:

```
error_code: E_GIT_PUSH_FAILED
push rejected (non-fast-forward)

hint: branch was rebased or amended; retry with:
  agency push <ref> --force-with-lease
```

**examples:**
```bash
agency push my-feature                 # push branch + create/update PR
agency push my-feature --allow-dirty   # push with dirty worktree
agency push my-feature --force-with-lease  # force push after rebase
```

## `agency verify`

runs the repo's `scripts.verify` for a run and records deterministic verification evidence.

**usage:**
```bash
agency verify <run_id> [--timeout <dur>]
```

**arguments:**
- `run_id`: the run identifier (exact or unique prefix)

**flags:**
- `--timeout`: script timeout override (Go duration format like `10m`, `90s`); defaults to `agency.json` configured timeout

**behavior:**
1. resolve run_id globally (works from anywhere, not just inside a repo)
2. validate workspace exists (not archived)
3. acquire repo lock for the duration of verification
4. run `scripts.verify` with L0 environment variables (timeout from config)
5. read optional `.agency/out/verify.json` structured output
6. write canonical `verify_record.json` with full evidence
7. update `meta.json` with `last_verify_at` and `flags.needs_attention`
8. append `verify_started` and `verify_finished` events to `events.jsonl`

**verify_record.json:**

canonical evidence record written to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json`:
- `schema_version`: always `"1.0"`
- `repo_id`, `run_id`: identifiers
- `script_path`: exact script string from agency.json
- `started_at`, `finished_at`: RFC3339Nano timestamps
- `duration_ms`, `timeout_ms`: timing info
- `exit_code`: integer or null (null if signal-terminated)
- `signal`: signal name if terminated (e.g., `"SIGKILL"`)
- `timed_out`, `cancelled`: boolean flags (mutually exclusive)
- `ok`: final result after precedence rules
- `summary`: human-readable summary
- `error`: internal errors only (not script failures)

**ok derivation precedence:**
1. if `timed_out` or `cancelled` => `ok=false`
2. else if `exit_code` is null => `ok=false`
3. else if `exit_code != 0` => `ok=false`
4. else if `verify.json` valid => `ok = verify.json.ok`
5. else => `ok=true`

**needs_attention rules:**
- verify ok clears `needs_attention` **only if** reason was `verify_failed`
- verify fail sets `needs_attention=true` with reason `verify_failed`
- verify ok does **not** clear attention for other reasons (e.g., `stop_requested`)

**success output:**
```
ok verify my-feature record=/path/to/verify_record.json log=/path/to/verify.log
```

**failure output:**
```
E_SCRIPT_FAILED: verify failed (exit 1) record=/path/to/verify_record.json log=/path/to/verify.log
```

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_RUN_ID_AMBIGUOUS` — prefix matches multiple runs
- `E_WORKSPACE_ARCHIVED` — run exists but worktree missing or archived
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_SCRIPT_FAILED` — verify script exited non-zero
- `E_SCRIPT_TIMEOUT` — verify script timed out

**notes:**
- does **not** affect `agency push` behavior (push does not run verify)
- does **not** require being in the repo directory
- logs are written to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log`
- logs are overwritten per verify run (not appended)
- user cancellation (Ctrl-C) is recorded as `cancelled=true`

**examples:**
```bash
agency verify my-feature                # run verify with configured timeout
agency verify my-feature --timeout 10m  # custom timeout
```

## `agency merge`

verifies, confirms, merges a GitHub PR, and archives the workspace.
requires cwd to be inside the target repo.
interactive mode prompts for confirmation by default; non-interactive mode requires `--yes`.

**usage:**
```bash
agency merge <run_id> [--squash|--merge|--rebase] [--no-delete-branch] [--force] [--yes]
```

**arguments:**
- `run_id`: the run identifier or name

**flags:**
- `--squash`: use squash merge strategy (default)
- `--merge`: use regular merge strategy
- `--rebase`: use rebase merge strategy
- `--no-delete-branch`: preserve the remote branch after merge (default: delete)
- `--force`: bypass verify-failed prompt (still runs verify, still records failure)
- `--yes`: bypass interactive prompts (required in non-interactive mode)

**behavior:**
1. runs prechecks:
   - run exists, worktree present
   - origin remote exists and is github.com
   - gh is authenticated
   - PR exists (must run `agency push` first)
   - PR is open, not a draft
   - PR is mergeable (not conflicting)
   - local head matches origin (up-to-date)
2. runs `scripts.verify` (timeout: 30 minutes)
3. if verify fails and no `--force` and no `--yes`: prompts to continue (`[y/N]`)
4. prompts for typed confirmation (must type `merge`) unless `--yes` is passed
5. merges PR via `gh pr merge --delete-branch` (deletes remote branch by default)
6. archives workspace (runs archive script, kills tmux, deletes worktree)

**confirmation prompts (interactive mode without `--yes`):**
```
verify failed. continue anyway? [y/N]
confirm: type 'merge' to proceed:
```

**success output:**
```
merged: my-feature
pr: https://github.com/owner/repo/pull/123
log: /path/to/logs/archive.log
```

**events:**
- `merge_started`, `merge_prechecks_passed`
- `verify_started`, `verify_finished`
- `verify_continue_prompted`, `verify_continue_accepted|rejected` (if verify failed)
- `merge_confirm_prompted`, `merge_confirmed`
- `gh_merge_started`, `gh_merge_finished`
- `archive_started`, `archive_finished|archive_failed`
- `merge_finished`

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_CONFIRMATION_REQUIRED` — non-interactive merge requires `--yes`
- `E_NO_ORIGIN` — no origin remote configured
- `E_UNSUPPORTED_ORIGIN_HOST` — origin is not github.com
- `E_GH_NOT_AUTHENTICATED` — gh not authenticated
- `E_GH_REPO_PARSE_FAILED` — failed to parse owner/repo from origin URL
- `E_NO_PR` — no PR exists for the run (run `agency push` first)
- `E_GH_PR_VIEW_FAILED` — gh pr view failed or returned invalid schema
- `E_PR_NOT_OPEN` — PR is CLOSED or already MERGED
- `E_PR_DRAFT` — PR is a draft
- `E_PR_MISMATCH` — PR head branch doesn't match expected branch
- `E_PR_NOT_MERGEABLE` — PR has conflicts
- `E_PR_MERGEABILITY_UNKNOWN` — GitHub couldn't determine mergeability
- `E_GIT_FETCH_FAILED` — git fetch failed
- `E_REMOTE_OUT_OF_DATE` — local head differs from origin (run `agency push`)
- `E_SCRIPT_FAILED` — verify script exited non-zero
- `E_SCRIPT_TIMEOUT` — verify script timed out
- `E_ABORTED` — user declined confirmation or typed wrong token
- `E_GH_PR_MERGE_FAILED` — gh pr merge failed
- `E_PERSIST_FAILED` — failed to persist merge log
- `E_ARCHIVE_FAILED` — archive step failed

**notes:**
- `--force` does NOT bypass: missing PR, non-mergeable PR, gh auth failure, remote out-of-date
- `--yes` bypasses interactive prompts only; it does not bypass prechecks or verify execution
- at most one of `--squash`/`--merge`/`--rebase` may be specified
- if already merged (idempotent): skips verify/mergeability checks, still requires confirmation unless `--yes`, then archives workspace
- PR must exist before merge; agency does NOT call `push` implicitly
- gh merge output is captured to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/merge.log`
- merge-log persistence is correctness-critical: write failures return typed `E_PERSIST_FAILED`
- merge logs are persisted with private permissions (`0700` directory, `0600` file)
- post-merge confirmation: agency verifies PR reached `MERGED` state with retries (250ms, 750ms, 1500ms backoff)
- by default, the remote branch is deleted after merge; use `--no-delete-branch` to preserve it

**merge conflict handling:**

when merge fails due to conflicts, agency provides an actionable resolution path:

```
error_code: E_PR_NOT_MERGEABLE
PR #93 has conflicts with main and cannot be merged.

pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2
worktree: /path/to/worktree

next:

1. agency open feature-x
2. git fetch origin
3. git rebase origin/main
4. resolve conflicts, then:
   git add -A && git rebase --continue
5. agency push feature-x --force-with-lease
6. agency merge feature-x

alt: cd "/path/to/worktree"
```

**examples:**
```bash
agency merge my-feature                       # squash merge, delete branch (default)
agency merge my-feature --merge               # regular merge, delete branch
agency merge my-feature --rebase              # rebase merge, delete branch
agency merge my-feature --no-delete-branch    # squash merge, preserve branch
agency merge my-feature --force               # skip verify-fail prompt
agency merge my-feature --yes                 # non-interactive/script-safe confirmation
```

## `agency resolve`

shows conflict resolution guidance for a run.
provides step-by-step instructions to resolve merge conflicts via rebase.
read-only: makes no git changes, does not require repo lock.

**usage:**
```bash
agency resolve <run_id>
```

**arguments:**
- `run_id`: the run identifier (name, exact run_id, or unique prefix)

**behavior:**
- if worktree present: prints action card to stdout, exits 0
- if worktree missing: prints partial guidance to stderr, exits with `E_WORKTREE_MISSING`

**output (worktree present):**
```
pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2
worktree: /path/to/worktree

next:

1. agency open feature-x
2. git fetch origin
3. git rebase origin/main
4. resolve conflicts, then:
   git add -A && git rebase --continue
5. agency push feature-x --force-with-lease
6. agency merge feature-x

alt: cd "/path/to/worktree"
```

**output (worktree missing):**
```
error_code: E_WORKTREE_MISSING
worktree archived or missing

pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2

hint: worktree no longer exists; resolve conflicts via GitHub web UI or restore locally
```

**examples:**
```bash
agency resolve my-feature
```

## `agency clean`

archives a run without merging (abandons the run).
requires cwd to be inside the target repo.
interactive mode prompts for confirmation by default; non-interactive mode requires `--yes`.

**usage:**
```bash
agency clean <run_id> [--yes]
```

**arguments:**
- `run_id`: the run identifier or name

**flags:**
- `--yes`: bypass interactive confirmation (required in non-interactive mode)

**behavior:**
1. acquires repo lock
2. prompts for confirmation (must type `clean`) unless `--yes` is passed
3. runs `scripts.archive` (timeout: 5 minutes)
4. kills tmux session if exists
5. deletes worktree (git worktree remove, fallback to safe rm -rf)
6. retains metadata and logs in `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/`
7. marks run as abandoned (`flags.abandoned=true`, `archive.archived_at` set)

**confirmation prompt (interactive mode without `--yes`):**
```
confirm: type 'clean' to proceed:
```

**success output:**
```
cleaned: my-feature
log: /path/to/logs/archive.log
```

**archive failure handling:**
- archive is best-effort: all steps are attempted even if earlier steps fail
- if any step fails: returns `E_ARCHIVE_FAILED` but does not set `archive.archived_at`
- worktree deletion fallback (rm -rf) is only allowed if path is under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/`

**idempotency:**
- if run is already archived: prints `already archived` and exits 0

**events:**
- `clean_started`, `archive_started`
- `archive_finished` (on success) or `archive_failed` (on any failure)
- `clean_finished`

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_RUN_NOT_FOUND` — run not found
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_CONFIRMATION_REQUIRED` — non-interactive clean requires `--yes`
- `E_ABORTED` — user declined confirmation or typed wrong token
- `E_ARCHIVE_FAILED` — archive step failed (script, tmux, or delete failure)

**notes:**
- does **not** merge any PR (use `agency merge` for that)
- does **not** delete git branches (local or remote)
- worktree and tmux session are deleted; metadata and logs are retained

**examples:**
```bash
agency clean my-feature    # archive without merging
agency clean my-feature --yes  # non-interactive/script-safe confirmation
```

## error output

agency uses structured error output with stable error codes.

### default error format

```
error_code: E_...
<one-line message>

<context (key: value pairs)>

hint: <actionable guidance>
```

example (verify failure):

```
error_code: E_SCRIPT_FAILED
verify failed (exit 1)

script: scripts/agency_verify.sh
exit_code: 1
duration: 12.3s
log: /path/to/verify.log
record: /path/to/verify_record.json

output (last 20 lines):
  npm ERR! Test failed
  npm ERR! code ELIFECYCLE
  ...

hint: fix the failing tests and run: agency verify my-feature
```

### `--verbose` mode

use `agency --verbose <command>` to see additional context:

- more context keys displayed
- longer output tails (up to 100 lines)
- extra details section with all remaining metadata

```bash
agency --verbose push my-feature
agency --verbose merge my-feature
```

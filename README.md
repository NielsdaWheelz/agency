# agency

local-first AI coding agent manager for Mac and Linux. creates isolated git workspaces, launches configured AI runners in tmux, tracks progress with automatic checkpoints, and lands changes back via GitHub PRs.

## installation

### macos (homebrew)

```bash
brew install --cask NielsdaWheelz/tap/agency
```

### linux

```bash
curl -LO https://github.com/NielsdaWheelz/agency/releases/download/v0.1.0/agency_0.1.0_linux_amd64.tar.gz
tar xzf agency_0.1.0_linux_amd64.tar.gz
mkdir -p ~/.local/bin && mv agency ~/.local/bin/
```

### from source

```bash
go install github.com/NielsdaWheelz/agency/cmd/agency@latest
```

## prerequisites

`git`, `tmux`, `gh` (authenticated), and at least one supported runner executable on `PATH`.

Run `agency config init` first. It writes a working version `3` `config.json` under your agency config dir.
For the full schema and setup rules, see [docs/configuration.md](docs/configuration.md).
For paths and precedence, see [docs/environment.md](docs/environment.md).

Short working `config.json` example:

```json
{
  "version": 3,
  "defaults": {
    "runner": "claude-code",
    "editor": "code",
    "base_branch": "main"
  },
  "runner_defaults": {
    "claude-code": {
      "model": "claude-opus-4-7[1m]",
      "effort": "max",
      "permission_mode": "bypassPermissions"
    }
  },
  "runners": {
    "claude-code": "claude"
  },
  "editors": {
    "code": "code"
  }
}
```

## quick start

`agency init` writes per-repo agency config and scripts under `$AGENCY_CONFIG_DIR` by default, so setup/verify/archive scripts do not need to be committed to the repo.
Use `agency init --repo-config` only when you want shareable `agency.json` and scripts in the repo.

```bash
agency config init
agency repo add /path/to/myrepo
agency init --path /path/to/myrepo
agency worktree create my-feature --repo <repo-ref> --base main
agency context use my-feature --repo <repo-ref>
agency agent start --headless --prompt "Fix the auth bug"
agency agent <invocation-ref> land --apply
```

`agency repo add [path]` uses a positional path. Omit it only when your current directory is already inside the repo you want to register.
`agency init` and `agency doctor` use `--path <checkout-path>` when you are not already in the target repo.
`agency context` shows the active repo/worktree. Use `agency context use <worktree-ref> --repo <repo-ref>` to set it and `agency context unset` to clear it.
`worktree create` and `agent start` accept optional `--repo` selectors from any cwd; when omitted, they resolve the repo from the active context first and then from the current directory.
`agency repo <repo-ref>`, `agency worktree <worktree-ref>`, and `agency agent <invocation-ref>` are the default show forms. Collection verbs remain explicit: `agency repo ls`, `agency worktree ls`, and `agency agent ls`.
`agency r`, `agency wt`, and `agency ag` are supported noun aliases for `repo`, `worktree`, and `agent`, but the long forms remain the primary documented CLI surface.
`--repo` accepts a repo name, key, id, or unique prefix from `agency repo ls`.
`agency worktree create <name>` uses a positional name and defaults omitted `--base` to the current branch of the selected checkout.
`agency agent start` takes no positional worktree argument. Use `--worktree <worktree-ref>` when you want an explicit override from any cwd.
If `--worktree` is omitted, `agency agent start` resolves the worktree from the active context first and then falls back to the current integration worktree only when cwd is already inside one.
Worktree name and id-prefix lookup only consider present worktrees; archived worktrees must be addressed by exact `worktree_id`.
`agent start` uses agency config precedence for repo-scoped runner defaults: explicit `--agency-config`, repo-shared `<canonical-repo-root>/agency.json`, then per-repo config under `$AGENCY_CONFIG_DIR`.
`agency agent start` defaults to headed mode. Use `--headless` for daemon-backed runs that require `--prompt` or `--prompt-file`.
For `claude-code`, Agency owns Claude `model`, `effort`, and `permission_mode`. Set `permission_mode` in user `config.json` only; use `runner_defaults` or `agency agent start --model/--effort` for Claude model and effort.

headed (interactive tmux):

```bash
agency agent start --worktree my-feature --repo <repo-ref>
agency agent start --worktree my-feature --repo <repo-ref> --detached --model opus[1m] --effort max
agency agent <invocation-ref> attach
```

For `claude-code`, headed mode launches interactive Claude in tmux and applies Agency-owned Claude settings without the print/stream-json startup path.

headless (fire-and-forget):

```bash
agency context use my-feature --repo <repo-ref>
agency agent start --headless --prompt "Fix the auth bug"
agency agent start --worktree my-feature --repo <repo-ref> --headless --prompt "Fix auth edge cases" --model claude-opus-4-7[1m] --effort max
agency agent <invocation-ref> history                 # interactive invocation history/transcript/logs UI (same runtime; tty only)
agency agent <invocation-ref> history --json          # machine-readable timeline output
agency agent <invocation-ref> history logs --follow   # raw invocation logs
agency agent <invocation-ref> followup --prompt "continue with edge-case tests"
agency agent <invocation-ref> check                   # canonical invocation state + reason
agency agent <invocation-ref> diff --turn <entry>    # changes for a turn or range
agency agent <invocation-ref> diff --turn-range <start>..<end>
agency agent <invocation-ref> land --apply           # land sandbox into integration worktree
agency worktree <worktree-ref> pr sync               # push branch + create/update PR
agency worktree <worktree-ref> pr merge --yes        # verify, merge, and archive worktree PR
agency worktree <worktree-ref> rebase                # rebase worktree branch onto origin/<base_branch>
agency agent <invocation-ref> restore --checkpoint 3
agency agent <invocation-ref> restore --turn <entry>
```

For `claude-code`, headless mode runs through the daemon in print/stream-json mode. Agency owns the Claude startup flags, applies configured `model` and `effort`, and controls the effective permission behavior.

short alias parity for high-traffic s6 navigation/progression surfaces:
- `worktree create`: `-r/--repo`
- `agent start`: `-r/--repo`, `--worktree`
- `agent <ref> check`: `-r/--repo`, `-j/--json`
- `agent <ref> path|open|attach`: `-r/--repo`

`agency watch` and `agency agent <invocation-ref> history` open different pages of the same Bubble Tea runtime.
That runtime exposes workspace, history, transcript, and logs pages over the same daemon-backed read model.
`agency agent <invocation-ref> history` is the canonical inspection surface for invocation turns, checkpoints, transcripts, and logs.
`agency agent <invocation-ref> attach` stays a thin tmux handoff for running headed invocations; it is not a parallel inspection workflow.
For headed interactive logs, prefer the live terminal output; use history/transcript/logs pages for daemon-backed inspection and replay.
Invocation state uses one canonical vocabulary: `starting`, `running`, `waiting`, `stopping`, `succeeded`, `failed`.
`waiting` covers both done-and-idle and waiting-for-user cases.
`blocked` is not a user-facing state.
`agency agent <invocation-ref> restore` restores sandbox state only; it does not rerun the original prompt.
use `--checkpoint` for explicit/scripted restore and `--turn` when selecting a restorable turn from history output.
after a restore, use `agency agent <invocation-ref> followup` to continue from the restored state.
runner config details live in [docs/configuration.md](docs/configuration.md).

non-interactive destructive flows require explicit confirmation via `--yes`:

```bash
agency worktree <worktree-ref> rm --yes
agency worktree <worktree-ref> pr merge --yes
agency repo <repo-ref> rm --yes
```

automation-friendly mutation json:

```bash
agency agent start --worktree my-feature --repo <repo-ref> --headless --prompt "fix bug" --json
agency agent <invocation-ref> stop --json
agency agent <invocation-ref> kill --json
agency agent <invocation-ref> land --json
agency worktree <worktree-ref> pr sync --json
agency worktree <worktree-ref> pr merge --yes --json
agency worktree <worktree-ref> rebase --json
agency agent <invocation-ref> discard --json
agency agent <invocation-ref> followup --prompt "continue" --json
agency agent <invocation-ref> recreate --json
agency agent <invocation-ref> restore --checkpoint 3 --json
agency repo add /abs/path/to/repo --json
agency repo <repo-ref> rm --yes --json
```

all mutation `--json` responses use a stable envelope with deterministic fields:
`ok`, `error_code`, `message`, `hint`, `request_id`, `api_version`, `build_version`, `client_request_id`.
success payloads include additive command-specific fields (for example `timeline_entry_id` for `followup`,
and `checkpoint_id`/`snapshot_commit`/`restored_at` for `restore`).

for daemon-backed mutations, `request_id` is daemon-issued and mirrors the daemon response header `X-Request-ID` for correlation.
daemon mutation request bodies are strict JSON: unknown fields and trailing/multi-object payloads are rejected with typed `E_INVALID_ARGUMENT` errors.

## how it works

```
Repo ──► Worktree ──► Agent Invocation ──► Sandbox
(yours)  (stable       (one run of         (isolated copy
         branch)       configured runner)    agent works in)
```

you register a repo, create worktrees (isolated branches), start agents inside sandboxed copies of those branches, then land the agent's changes back. a background daemon supervises everything — auto-starts on first use.

invocation mutation flows (follow-up prompts, checkpoint lifecycle, rollback apply, headed recreate, land/discard) are recorded in one daemon-owned append-only event log with deterministic per-invocation sequencing.
for headless runs, stdout capture is safety-bounded: `raw.jsonl` is preserved verbatim, oversized lines emit `parse_error` in `stream.jsonl`, and processing continues with subsequent valid lines.
the only semantic runner contract is `.agency/state/runner_status.json`.
it uses one canonical `state`; the CLI and terminal UI should not layer separate semantic, display, or readiness statuses on top of it.
the runner writes `running`, `waiting`, `succeeded`, or `failed`.
`waiting` covers both turn-complete idle and waiting-for-user cases.
unknown provider events should degrade to diagnostics in transcript/log inspection, not to a fake top-level invocation state.

## documentation

- **[docs/index.md](docs/index.md)** — documentation map and ownership rules
- **[docs/configuration.md](docs/configuration.md)** — config setup and version `3` schemas
- **[docs/codebase.md](docs/codebase.md)** — package layout and architecture boundaries
- **[docs/daemon.md](docs/daemon.md)** — daemon lifecycle, ownership, and mutation rules
- **[docs/environment.md](docs/environment.md)** — config paths, overrides, and precedence
- **[docs/git-worktrees.md](docs/git-worktrees.md)** — repo, integration worktree, invocation, and sandbox model
- **[docs/overrides.md](docs/overrides.md)** — explicit CLI and config override rules
- **[docs/persistence.md](docs/persistence.md)** — on-disk schemas, atomic writes, and permissions
- **[docs/testing.md](docs/testing.md)** — testing standards, layers, fixtures, and e2e rules
- **[docs/modules/index.md](docs/modules/index.md)** — subsystem-owned docs

## license

MIT

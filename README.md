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

`git`, `tmux`, `gh` (authenticated), plus explicit runner mappings in user config.

runner commands must be configured in `config.json` under your agency config dir
(`$AGENCY_CONFIG_DIR/config.json`; defaults to `~/Library/Preferences/agency/config.json` on macOS and `~/.config/agency/config.json` on Linux):

```json
{
  "version": 1,
  "defaults": {
    "runner": "claude-code",
    "editor": "code",
    "model": "opus",
    "effort": "high"
  },
  "runners": {
    "claude-code": "claude",
    "codex": "codex"
  }
}
```

supported canonical runner ids: `claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`.

## quick start

```bash
cd myrepo
agency repo add                              # register this repo
agency worktree create --name my-feature     # create an isolated branch
agency agent start --worktree my-feature     # headed start requires an interactive terminal; use --detached to skip attach
# Ctrl+b, d to detach from tmux
agency watch                                 # full-screen readiness workspace (interactive tty; enter/o/p actions)
agency agent ls                              # concise invocation list
agency agent land <invocation-id> --apply    # land changes back to worktree
```

headless (fire-and-forget):

```bash
agency agent start --worktree my-feature --headless --prompt "Fix the auth bug"
agency agent start --worktree my-feature --headless --prompt "Fix auth edge cases" --model opus --effort high
agency agent logs <invocation-id> --follow
agency agent chat <invocation-id> --prompt "continue with edge-case tests"
agency agent history <invocation-id> --limit 50   # limit must be 1..500
agency agent history <invocation-id> --last        # show latest meaningful turn/activity
agency agent review <invocation-id>               # review verdict + blocking reasons
agency agent diff <invocation-id> --turn <entry> # turn-anchored diff context
agency agent land <invocation-id> --apply         # land sandbox into integration worktree
agency worktree pr sync <worktree-ref>            # push branch + create/update PR
agency worktree pr merge <worktree-ref> --yes     # verify, merge, and archive worktree PR
agency worktree rebase <worktree-ref>             # rebase worktree branch onto origin/<parent_branch>
agency agent checkpoint ls <invocation-id>
agency agent restart <invocation-id> --checkpoint 3 --env FAKE_RUNNER_MODE=sleep
agency agent restart <invocation-id> --checkpoint 3 --model opus --effort high
agency agent restart <invocation-id> --history     # interactive history selector (tty only)
```

short alias parity for high-traffic s6 navigation/progression surfaces:
- `agent review`: `-r/--repo`, `-j/--json`
- `agent path|open|enter`: `-r/--repo`

if the original headless start used custom env keys, `agent restart` requires explicitly replaying those keys via `--env KEY=VALUE`.
for non-interactive/scripted use, prefer `--checkpoint`; `--history` is interactive.
`agent restart` replays the invocation's stored original prompt; use `agency agent checkpoint apply` when you want restore-only rollback without restarting prompt execution.
`agent restart --history` shows checkpoint-aware turn summaries, completed tool calls, and authoritative changed-file previews from checkpoint-to-checkpoint git diffs.
typed model/effort knobs are supported for `claude-code`, `codex`, and `cursor`.
for `claude-code` and `codex`, `--model` and `--effort` apply.
for `cursor`, use `--model` only (choose a thinking-capable model id when needed, for example `sonnet-4.6-thinking`).
for other runners, keep using `--runner-arg`.

non-interactive destructive flows require explicit confirmation via `--yes`:

```bash
agency worktree rm <name|id|prefix> --yes
agency worktree pr merge <worktree-ref> --yes
agency repo rm <repo-ref> --yes
```

automation-friendly mutation json:

```bash
agency agent start --worktree my-feature --headless --prompt "fix bug" --json
agency agent stop <invocation-id> --json
agency agent kill <invocation-id> --json
agency agent land <invocation-id> --json
agency worktree pr sync <worktree-ref> --json
agency worktree pr merge <worktree-ref> --yes --json
agency worktree rebase <worktree-ref> --json
agency agent discard <invocation-id> --json
agency agent chat <invocation-id> --prompt "continue" --json
agency agent restart <invocation-id> --checkpoint 3 --json
agency repo add --json
agency repo rm <repo-ref> --yes --json
```

all mutation `--json` responses use a stable envelope with deterministic fields:
`ok`, `error_code`, `message`, `hint`, `request_id`, `api_version`, `build_version`, `client_request_id`.
success payloads include additive command-specific fields (for example `timeline_entry_id` for `chat`,
and `checkpoint_id`/`snapshot_commit`/`restored_at` for `restart`).
for `worktree pr sync` and `worktree pr merge`, additive report fields include
`report_source` and `report_diagnostics`.

for daemon-backed mutations, `request_id` is daemon-issued and mirrors the daemon response header `X-Request-ID` for correlation.
daemon mutation request bodies are strict JSON: unknown fields and trailing/multi-object payloads are rejected with typed `E_INVALID_ARGUMENT` errors.

## how it works

```
Repo ──► Worktree ──► Agent Invocation ──► Sandbox
(yours)  (stable       (one run of         (isolated copy
         branch)       configured runner)    agent works in)
```

you register a repo, create worktrees (isolated branches), start agents inside sandboxed copies of those branches, then land the agent's changes back. a background daemon supervises everything — auto-starts on first use.

invocation mutation flows (follow-up prompts, checkpoint lifecycle, rollback apply, land/discard) are recorded in one daemon-owned append-only event log with deterministic per-invocation sequencing.
for headless runs, stdout capture is safety-bounded: `raw.jsonl` is preserved verbatim, oversized lines emit `parse_error` in `stream.jsonl`, and processing continues with subsequent valid lines.
reports-v2 progression is mode-aware: headless `review`/`pr sync`/`pr merge` is strict and typed; headed flows stay progression-capable with explicit diagnostics and deterministic fallback behavior.

## documentation

- **[docs/index.md](docs/index.md)** — documentation map and ownership rules
- **[docs/codebase.md](docs/codebase.md)** — package layout and architecture boundaries
- **[docs/daemon.md](docs/daemon.md)** — daemon lifecycle, ownership, and mutation rules
- **[docs/git-worktrees.md](docs/git-worktrees.md)** — repo, integration worktree, invocation, and sandbox model
- **[docs/persistence.md](docs/persistence.md)** — on-disk schemas, atomic writes, and permissions
- **[docs/testing.md](docs/testing.md)** — testing standards, layers, fixtures, and e2e rules
- **[docs/modules/index.md](docs/modules/index.md)** — subsystem-owned docs

## license

MIT

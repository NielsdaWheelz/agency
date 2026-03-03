# agency

local-first AI coding agent manager for Mac and Linux. creates isolated git workspaces, launches configured AI runners in tmux, tracks progress with automatic checkpoints, and lands changes back via GitHub PRs.

## installation

### macos (homebrew)

```bash
brew install NielsdaWheelz/tap/agency
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
    "editor": "code"
  },
  "runners": {
    "claude-code": "claude",
    "codex": "codex"
  }
}
```

supported canonical runner ids: `claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`.
legacy aliases are accepted: `claude` -> `claude-code`, `cursor-cli` -> `cursor`.

## quick start

```bash
cd myrepo
agency repo add                              # register this repo
agency worktree create --name my-feature     # create an isolated branch
agency agent start --worktree my-feature     # launch claude-code in a tmux session
# Ctrl+b, d to detach from tmux
agency agent ls --watch                      # monitor your agents
agency agent land <invocation-id> --apply    # land changes back to worktree
```

headless (fire-and-forget):

```bash
agency agent start --worktree my-feature --headless --prompt "Fix the auth bug"
agency agent logs <invocation-id> --follow
agency agent chat <invocation-id> --prompt "continue with edge-case tests"
agency agent history <invocation-id> --limit 50   # limit must be 1..500
agency agent review <invocation-id>               # review verdict + blocking reasons
agency agent diff <invocation-id> --turn <entry> # turn-anchored diff context
agency agent land <invocation-id> --apply         # land sandbox into integration worktree
agency agent pr sync <invocation-id>              # push branch + create/update PR
agency agent merge <invocation-id> --yes          # verify + merge invocation PR
agency checkpoint ls --invocation <invocation-id>
agency agent restart <invocation-id> --checkpoint 3 --env FAKE_RUNNER_MODE=sleep
agency agent restart <invocation-id> --history     # interactive history selector (tty only)
```

if the original headless start used custom env keys, `agent restart` requires explicitly replaying those keys via `--env KEY=VALUE`.
for non-interactive/scripted use, prefer `--checkpoint`; `--history` is interactive.

automation-friendly mutation json:

```bash
agency agent start --worktree my-feature --headless --prompt "fix bug" --json
agency agent stop <invocation-id> --json
agency agent kill <invocation-id> --json
agency agent land <invocation-id> --json
agency agent pr sync <invocation-id> --json
agency agent merge <invocation-id> --yes --json
agency agent discard <invocation-id> --json
agency agent chat <invocation-id> --prompt "continue" --json
agency agent restart <invocation-id> --checkpoint 3 --json
```

all mutation `--json` responses use a stable envelope with deterministic fields:
`ok`, `error_code`, `message`, `hint`, `request_id`, `api_version`, `build_version`, `client_request_id`.
success payloads include additive command-specific fields (for example `timeline_entry_id` for `chat`,
and `checkpoint_id`/`snapshot_commit`/`restored_at` for `restart`).
for `agent pr sync` and `agent merge`, additive report fields include
`report_source`, `report_fallback_used`, and `report_diagnostics`.

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
legacy compatibility commands (`agency push` / `agency merge`) are retained, but their report/body handling and merge-log persistence follow the same bounded-input + durable-write safety posture as canonical v2.1 flows.
reports-v2 progression is mode-aware: headless `review`/`pr sync`/`merge` is strict and typed; headed/compatibility paths stay progression-capable with explicit diagnostics and deterministic fallback behavior.

## documentation

- **[getting started](docs/getting-started.md)** — full walkthrough from zero to merged PR
- **[concepts](docs/concepts.md)** — repos, worktrees, agents, sandboxes, landing, checkpoints
- **[CLI reference](docs/cli.md)** — every command and flag
- **[configuration](docs/configuration.md)** — agency.json, environment variables, shell completion
- **[architecture](docs/architecture.md)** — daemon internals, stream parsing, data model
- **[v2.1 docs](docs/v2.1/README.md)** — consolidated product scope, parity matrix, release gates, roadmap
- **[contributing](docs/contributing.md)** — build, test, lint, project structure

## license

MIT

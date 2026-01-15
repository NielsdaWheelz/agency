# Agency S5: PR Roadmap — Verify Recording (`agency verify`)

goal: deliver `agency verify <id> [--timeout <dur>]` with deterministic evidence recording, flags updates, and test coverage, without changing push/merge behavior.

constraints (global):
- no changes to `agency push` (no implicit verify)
- no PR checks parsing
- no tmux involvement for verify (verify runs as a normal subprocess)
- no new storage backends (json files under `${AGENCY_DATA_DIR}`)
- scripts are non-interactive; stdin is `/dev/null`
- repo lock is required for `verify`
- mac/linux only; kill *process group* on timeout/cancel

---

## PR 5.1 — verify runner core (process + record + precedence)

### goal
implement the core verify execution engine and canonical `verify_record.json` writing, including timeout + cancellation semantics and ok derivation precedence.

### scope
- subprocess runner for `scripts.verify`:
  - `cwd = worktree`
  - stdin `/dev/null`
  - env injection (existing L0 contract)
  - capture stdout/stderr to `${...}/logs/verify.log` (truncate/overwrite per invocation)
- timeout handling:
  - default 30m (configurable via caller)
  - on timeout: SIGINT to process group, wait 3s, then SIGKILL to process group
  - record `timed_out=true`, `ok=false`
- cancellation handling (user ctrl-c):
  - forward SIGINT to process group, wait 3s, then SIGKILL to process group
  - record `cancelled=true`, `ok=false`
- structured output consumption (read-only):
  - read `<worktree>/.agency/out/verify.json` if present
  - “valid enough” rules: require `schema_version` + `ok`; tolerate missing `summary`/`data`
  - if invalid: treat as absent for ok derivation, but still record `verify_json_path`
- ok derivation precedence (v1, locked):
  1. if `timed_out` or `cancelled` => `ok=false`
  2. else if `exit_code` is null => `ok=false`
  3. else if `exit_code != 0` => `ok=false`
  4. else if `verify.json` valid => `ok = verify.json.ok`
  5. else => `ok=true`
- write `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json` atomically
- `verify_record.json.summary` derivation:
  - prefer `verify.json.summary` if present
  - else generic (“verify succeeded” / “verify failed (exit N)” / “verify timed out” / “verify cancelled”)
- warnings/errors:
  - do **not** pollute `summary`
  - record any internal issues in `verify_record.json.error` (string) and/or `signal`

### public surface area
none (no CLI command yet). internal packages/helpers allowed.

### files (expected)
- NEW: internal verify runner package (e.g. `internal/verify/`)
- NEW/UPDATED: schema types for `verify_record.json`
- NEW: unit tests (table-driven) for:
  - ok derivation precedence
  - verify.json validation + parsing behavior
  - summary derivation

### acceptance (demo)
- run the verify runner in unit tests using a temp script that:
  - exits 0
  - exits 1
  - writes verify.json ok=false then exits 0
  - sleeps past timeout

### guardrails
- do not touch tmux code
- do not update `meta.json` in this PR
- do not add new commands

---

## PR 5.2 — meta + flags + events integration (+ new error code)

### goal
wire verify results into `meta.json` and `events.jsonl` deterministically, with correct needs-attention semantics and new archived-workspace error.

### scope
- add error code:
  - `E_WORKSPACE_ARCHIVED` for “run exists but worktree missing/archived; cannot verify”
- `agency verify` plumbing helpers (still not CLI):
  - load run meta, locate worktree, fail `E_WORKSPACE_ARCHIVED` if missing
  - acquire repo lock (existing lock behavior), fail `E_REPO_LOCKED` if held and not stale
- update `meta.json` atomically on verify completion:
  - set `last_verify_at`
  - if verify ok:
    - clear `flags.needs_attention` **only if** `flags.needs_attention_reason == "verify_failed"`
    - clearing means: `needs_attention=false` and `needs_attention_reason=""` (empty string) (or omit field if your meta writer supports omission; pick one and be consistent)
  - if verify failed/timed_out/cancelled:
    - set `flags.needs_attention=true`
    - set `flags.needs_attention_reason="verify_failed"`
- append to `events.jsonl` best-effort:
  - `verify_started` (includes timeout_ms, log_path)
  - `verify_finished` (includes ok, exit_code, timed_out, cancelled, duration_ms, verify_json_path)
  - if append fails: continue; store message in `verify_record.json.error` (not `summary`)
- ensure `verify_record.json` is written even if events append fails

### public surface area
- NEW error code: `E_WORKSPACE_ARCHIVED`
- additive meta fields if not already present:
  - `last_verify_at`
  - `flags.needs_attention_reason`

### tests
- unit:
  - needs_attention update rules (clear only when reason == verify_failed)
  - repo lock stale detection behavior (if not already covered)
- integration (no tmux):
  - create temp git repo + `agency.json`
  - create real git worktree for a fake run and a matching `meta.json`
  - run verify pipeline (script exits 0/1/timeout) and assert:
    - `verify_record.json` exists and ok matches
    - `meta.json` updated correctly
    - `events.jsonl` appended with the two events
    - `logs/verify.log` exists and overwritten

### guardrails
- do not introduce `agency verify` CLI yet
- do not change `agency ls` status derivation beyond what’s required to surface needs_attention (if already exists, leave it)

---

## PR 5.3 — CLI command `agency verify` + UX output

### goal
ship the user-facing `agency verify <id> [--timeout <dur>]` command wired to the S5 pipeline, with stable exit codes and minimal, predictable output.

### scope
- add command:
  - `agency verify <id> [--timeout <dur>]`
  - parse timeout using Go duration format; default 30m
- behavior:
  - validate run exists; fail `E_RUN_NOT_FOUND`
  - validate workspace present; fail `E_WORKSPACE_ARCHIVED`
  - acquire repo lock; fail `E_REPO_LOCKED`
  - emit events; run verify; write record; update meta
- stdout/stderr UX contract (v1):
  - on success: one-line `verify ok` + paths (record + log)
  - on failure/timeout/cancel: one-line `verify failed` + paths; exit non-zero
  - do not print full logs; point to `verify.log`

### public surface area
- NEW command: `agency verify`
- no new flags beyond `--timeout`

### tests
- CLI integration test (optional if repo already has command tests):
  - invoke `agency verify` against temp repo/run and assert exit codes

### guardrails
- do not change `push` or `merge` behavior
- do not add any other commands

---

## rollout / validation checklist (human)
1. create a run (S1) with a verify script that can pass/fail.
2. `agency verify <id>`:
   - confirm `verify_record.json` written and `verify.log` overwritten
   - confirm `agency show <id>` includes last_verify_at + flags (if show already supports it)
3. force a failure and confirm:
   - `needs_attention=true`, reason `verify_failed`
4. set `needs_attention` reason to something else (manually edit meta for now) and confirm verify success does **not** clear it.
5. archive the workspace (or delete worktree dir) and confirm `E_WORKSPACE_ARCHIVED`.

---

## commands (for CI / local)
- unit/integration: `go test ./...`

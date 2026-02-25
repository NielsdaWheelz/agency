# Slice S2 - Daemon Read Convergence + Sandbox Navigation Spec

Last updated: 2026-02-25
Status: draft
Upstream slice: `docs/v2.1/slice-roadmap.md` (Slice S2)

## 1. Goal & Scope

**Goal**: finish daemon-first read architecture and detached/fleet navigation basics.

**In Scope**:
- Daemon-first read routing for v2 `agent` and `worktree` CLI read surfaces, with local read fallback allowed only at daemon bootstrap/health boundaries.
- Normative read contracts for invocation/worktree list and get flows used by CLI list/filter/status/navigation commands.
- Detached/fleet navigation basics for direct path/open/shell/select usage, including scriptable output expectations and selection semantics.
- Command-surface consolidation policy for S2 invocation navigation: canonical `agent` verbs with compatibility aliases preserved for v2.1.
- Explicit routing and fallback invariants that downstream slices (S3+) must preserve.

**Out of Scope**:
- Headless chat control plane and restart-from-checkpoint conversational flows (-> S3).
- Runner capability modeling and mutation `--json` parity (-> S4).
- Invocation-scoped review/PR/merge command family (-> S5).
- Reports v2 and broad CLI ergonomics cleanup (-> S6).
- GUI/TUI parity work and any relaxation of sandbox-first safety model (v2.1 out of scope).

---

## 2. Domain Models

### ReadSurface

| Field | Type | Constraints |
|---|---|---|
| `surface_id` | enum | `agent_ls`, `agent_show`, `agent_path`, `agent_open`, `agent_shell`, `agent_enter`, `agent_attach`, `worktree_ls`, `worktree_show`, `worktree_path`, `worktree_open`, `worktree_shell`, `legacy_path`, `legacy_open`, `legacy_attach`, `legacy_resume` |
| `entity_kind` | enum | `invocation` or `worktree` |
| `operation_kind` | enum | `list`, `get`, `navigate`, or `inspect` |
| `daemon_endpoints` | []string | non-empty; each entry must be a canonical daemon read endpoint path |
| `bootstrap_fallback_allowed` | bool | true only for bootstrap/health boundary handling, not normal read resolution |
| `cli_local_store_read_allowed` | bool | must be false for `list` and `get`; temporary exception rows must be listed in section 9 |

### DaemonReadEnvelope

Canonical read response envelope for S2 read endpoints.

| Field | Type | Constraints |
|---|---|---|
| `ok` | bool | true on success, false on error |
| `api_version` | int | must match CLI-supported daemon API version |
| `build_version` | string | non-empty on daemon responses |
| `git_sha` | string | non-empty on daemon responses |
| `request_id` | string | non-empty; stable for request correlation |
| `data` | object | present when `ok=true`; shape depends on endpoint |
| `error_code` | string | present when `ok=false` |
| `message` | string | present when `ok=false` |
| `hint` | string | optional |
| `details` | object | optional, endpoint-specific |

### WorktreeNavigationView

S2 uses the daemon `WorktreeDTO` as the canonical worktree read shape.

| Field | Type | Constraints |
|---|---|---|
| `worktree_id` | string | non-empty; unique within repo |
| `name` | string | non-empty; human-facing identifier |
| `repo_id` | string | non-empty |
| `branch` | string | non-empty |
| `parent_branch` | string | non-empty |
| `tree_path` | absolute path string | non-empty; authoritative path for `worktree path/open/shell` |
| `state` | enum | `present` or `archived` |
| `created_at` | RFC3339 UTC string | non-empty |
| `last_used_at` | RFC3339 UTC string | optional |

### InvocationNavigationView

S2 uses the daemon `InvocationDTO` as the canonical invocation read shape.

| Field | Type | Constraints |
|---|---|---|
| `invocation_id` | string | non-empty; unique within repo |
| `invocation_name` | string | optional |
| `worktree_id` | string | non-empty |
| `repo_id` | string | non-empty |
| `runner` | string | non-empty |
| `mode` | enum | `headed` or `headless` |
| `started_at` | RFC3339 UTC string | non-empty |
| `finished_at` | RFC3339 UTC string | optional |
| `last_output_at` | RFC3339 UTC string | optional |
| `status` | enum | `starting`, `running`, `finished`, or `failed` |
| `exit_reason` | enum | `exited`, `killed`, `stopped`, `start_failed`, or `unknown` |
| `exit_code` | *int | optional |
| `semantic_status` | enum | optional; headless semantic status |
| `landing_status` | enum | optional; `pending`, `landed`, or `discarded` |
| `display_status` | string | daemon-derived; non-empty for list rendering |
| `attention_flags` | []string | daemon-derived; may be empty |
| `sort_key` | int | daemon-derived ordering key |
| `sandbox_path` | absolute path string | non-empty; authoritative path for invocation navigation/open flows |
| `logs_dir` | absolute path string | optional |

### ListQuery

| Field | Type | Constraints |
|---|---|---|
| `repo_id` | string | optional |
| `worktree_ref` | string | optional; only for invocation listing |
| `state` | enum | worktrees: `present|archived|all`; invocations: `active|finished|all`; unknown values must be rejected with `E_INVALID_ARGUMENT` |
| `mode` | enum | invocations only: `headed|headless|all`; unknown values must be rejected with `E_INVALID_ARGUMENT` |
| `limit` | int | default `100`, max `500` |
| `cursor` | opaque string | optional; daemon-generated pagination cursor |

### InvalidQueryArgumentDetails

Structured error details for invalid list-filter enum inputs.

| Field | Type | Constraints |
|---|---|---|
| `param` | string | non-empty; query parameter name (`state` or `mode`) |
| `value` | string | non-empty; rejected input value |
| `allowed_values` | []string | non-empty; canonical accepted values for the parameter |

### NavigationSelection

Represents a CLI-selected target used by path/open/shell/enter flows.

| Field | Type | Constraints |
|---|---|---|
| `selector_source` | enum | `explicit_ref`, `list_row`, or `machine_ref` |
| `target_kind` | enum | `invocation` or `worktree` |
| `ref` | string | non-empty CLI/user-provided selector |
| `repo_scope` | enum | `cwd_repo`, `repo_flag`, or `all_repos` |
| `resolved_repo_id` | string | required unless `repo_scope=all_repos` and selection unresolved |
| `resolved_id` | string | required on successful resolution |
| `resolved_path` | absolute path string | required for `path|open|shell` actions |

### NavigationCommandIntent

| Field | Type | Constraints |
|---|---|---|
| `command_family` | enum | `agent`, `worktree`, or `legacy` |
| `verb` | enum | `ls`, `show`, `path`, `open`, `shell`, `attach`, `enter`, `select`, `resume`, `restart` (`select` may be a workflow action rather than a dedicated CLI verb) |
| `interactive` | bool | true for `attach`, `enter`, `shell` |
| `requires_tty` | bool | true only for interactive attach/enter operations |
| `requires_mutation` | bool | false for S2 read/navigation resolution; true only for explicit compatibility mutation paths (for example legacy `resume` create/restart behavior) or future restart semantics |

### InvocationNavigationVerbPolicy

Defines the canonical and compatibility command surface for S2 invocation navigation.

| Field | Type | Constraints |
|---|---|---|
| `canonical_family` | enum | must be `agent` |
| `canonical_verbs` | []enum | S2 canonical invocation navigation verbs are `path`, `open`, `shell`, `enter`; `restart` is reserved for S3 semantic expansion |
| `compatibility_aliases` | []AliasRule | may include legacy top-level and existing `agent` aliases for v2.1 compatibility |
| `shared_resolution_contract` | string | must reference the S2 daemon-first CLI navigation resolution contract in section 4 |

### AliasRule

| Field | Type | Constraints |
|---|---|---|
| `source_command` | string | non-empty; existing CLI command path (for example `agent attach` or top-level `resume`) |
| `target_command` | string | non-empty; canonical `agent` command path |
| `alias_scope` | enum | `compatibility`, `deprecated_compatibility`, or `headed_only_compatibility` |
| `behavior_equivalence` | bool | true only when resolution and dispatch semantics are identical after daemon-first resolution |
| `notes` | string | optional; must call out headed/headless or checkpoint semantic limitations when relevant |

---

## 3. State Machines

### CLI Read Routing Lifecycle

States:
- `resolve_repo_context`
- `ensure_daemon`
- `daemon_read`
- `bootstrap_fallback`
- `render_or_dispatch`
- `failed`

Legal transitions:
1. `resolve_repo_context -> ensure_daemon`
2. `ensure_daemon -> daemon_read`
3. `ensure_daemon -> bootstrap_fallback`
4. `daemon_read -> render_or_dispatch`
5. `bootstrap_fallback -> render_or_dispatch`
6. `resolve_repo_context -> failed`
7. `ensure_daemon -> failed`
8. `daemon_read -> failed`
9. `bootstrap_fallback -> failed`

Illegal transitions:
1. `resolve_repo_context -> render_or_dispatch` (skips daemon/fallback routing)
2. `ensure_daemon -> render_or_dispatch` (skips read resolution)
3. `daemon_read -> bootstrap_fallback` after a successful daemon response
4. `bootstrap_fallback -> daemon_read` within the same command execution

Guard conditions:
1. `ensure_daemon -> bootstrap_fallback` is allowed only for daemon bootstrap/health boundary handling.
2. `daemon_read -> render_or_dispatch` requires a successful daemon response envelope with `ok=true`.
3. `render_or_dispatch` for path/open/shell/attach/enter commands must use daemon-derived IDs/paths from `InvocationNavigationView` or `WorktreeNavigationView`.
4. Normal command execution must not perform local store scans for entity discovery after entering `ensure_daemon`.
5. Compatibility aliases must transition through the same `daemon_read` and `render_or_dispatch` states as their canonical `agent` targets.

### Navigation Selection Lifecycle

States:
- `unselected`
- `selected`
- `resolved`
- `dispatched`
- `selection_error`

Legal transitions:
1. `unselected -> selected`
2. `selected -> resolved`
3. `selected -> selection_error`
4. `resolved -> dispatched`
5. `resolved -> selection_error`

Illegal transitions:
1. `unselected -> resolved`
2. `unselected -> dispatched`
3. `selected -> dispatched` (must resolve first)

Guard conditions:
1. `selected -> resolved` requires daemon resolution of the provided ref (or explicit list row payload already sourced from daemon list output).
2. `resolved -> dispatched` for `open|shell|path` requires non-empty `resolved_path`.
3. `resolved -> dispatched` for `attach|enter` requires invocation target resolution and TTY preflight for interactive attach behavior.
4. `selection_error` from ambiguous refs must preserve candidate data when available.

---

## 4. API Contracts

S2 adopts the existing daemon read endpoints below as the canonical source for v2 read and navigation resolution. CLI handlers may render or dispatch local editor/shell/tmux actions after daemon resolution, but they must not re-resolve entity identity via local store scans.

### GET /worktrees

**request**:
- query params:
  - `repo_id` (optional string)
  - `state` (optional string; default `present`; accepted values for S2 contract: `present|archived|all`)
  - `limit` (optional int; default `100`; max `500`)
  - `cursor` (optional opaque string)

**response 200**:
```json
{
  "ok": true,
  "api_version": 1,
  "build_version": "x.y.z",
  "git_sha": "abcdef0",
  "request_id": "req-123",
  "data": {
    "worktrees": [
      {
        "worktree_id": "wt-1",
        "name": "alpha",
        "repo_id": "r1",
        "branch": "agency/alpha",
        "parent_branch": "main",
        "tree_path": "/abs/path",
        "state": "present",
        "created_at": "2026-02-25T00:00:00Z",
        "last_used_at": "2026-02-25T00:10:00Z"
      }
    ],
    "next_cursor": "opaque"
  }
}
```

**errors**:
- `E_METHOD_NOT_ALLOWED` (405): request method is not `GET`.
- `E_INVALID_ARGUMENT` (400): `state` is not one of `present|archived|all`; response details must include `param`, `value`, and `allowed_values`.
- `E_INTERNAL` (500): daemon cannot enumerate repo IDs or read backing state for the request.

### GET /worktrees/{ref}

**request**:
- path param: `ref` (worktree name, id, or unique prefix)
- query params:
  - `repo_id` (optional string; required in CLI when repo scope is single-repo and ambiguity must be prevented)

**response 200**:
```json
{
  "ok": true,
  "api_version": 1,
  "build_version": "x.y.z",
  "git_sha": "abcdef0",
  "request_id": "req-123",
  "data": {
    "worktree_id": "wt-1",
    "name": "alpha",
    "repo_id": "r1",
    "branch": "agency/alpha",
    "parent_branch": "main",
    "tree_path": "/abs/path",
    "state": "present",
    "created_at": "2026-02-25T00:00:00Z"
  }
}
```

**errors**:
- `E_METHOD_NOT_ALLOWED` (405): request method is not `GET`.
- `E_WORKTREE_NOT_FOUND` (404): no worktree matches `ref` within the requested scope.
- `E_WORKTREE_ID_AMBIGUOUS` (409): `ref` matches multiple worktrees; `details.candidates` should be present when available.

### GET /invocations

**request**:
- query params:
  - `repo_id` (optional string)
  - `worktree_ref` (optional string; daemon resolves worktree ref for filtering)
  - `state` (optional string; default `all`; accepted values for S2 contract: `active|finished|all`)
  - `mode` (optional string; default `all`; accepted values for S2 contract: `headed|headless|all`)
  - `limit` (optional int; default `100`; max `500`)
  - `cursor` (optional opaque string)

**response 200**:
```json
{
  "ok": true,
  "api_version": 1,
  "build_version": "x.y.z",
  "git_sha": "abcdef0",
  "request_id": "req-123",
  "data": {
    "invocations": [
      {
        "invocation_id": "inv-1",
        "invocation_name": "alpha-run",
        "worktree_id": "wt-1",
        "repo_id": "r1",
        "runner": "codex",
        "mode": "headless",
        "started_at": "2026-02-25T00:00:00Z",
        "status": "running",
        "exit_reason": "unknown",
        "display_status": "working",
        "attention_flags": [],
        "sort_key": 60,
        "sandbox_path": "/abs/path",
        "logs_dir": "/abs/logs"
      }
    ],
    "next_cursor": "opaque"
  }
}
```

**errors**:
- `E_METHOD_NOT_ALLOWED` (405): request method is not `GET`.
- `E_INVALID_ARGUMENT` (400): `state` or `mode` is not in the accepted enum set; response details must include `param`, `value`, and `allowed_values`.
- `E_INTERNAL` (500): daemon cannot enumerate repo IDs or read backing state for the request.

### GET /invocations/{ref}

**request**:
- path param: `ref` (invocation id, name, or unique prefix)
- query params:
  - `repo_id` (optional string; recommended in CLI single-repo resolution paths)

**response 200**:
```json
{
  "ok": true,
  "api_version": 1,
  "build_version": "x.y.z",
  "git_sha": "abcdef0",
  "request_id": "req-123",
  "data": {
    "invocation_id": "inv-1",
    "worktree_id": "wt-1",
    "repo_id": "r1",
    "runner": "codex",
    "mode": "headed",
    "started_at": "2026-02-25T00:00:00Z",
    "status": "running",
    "exit_reason": "unknown",
    "display_status": "running",
    "attention_flags": [],
    "sort_key": 70,
    "sandbox_path": "/abs/path"
  }
}
```

**errors**:
- `E_METHOD_NOT_ALLOWED` (405): request method is not `GET`.
- `E_INVOCATION_NOT_FOUND` (404): no invocation matches `ref` within the requested scope.
- `E_INVOCATION_ID_AMBIGUOUS` (409): `ref` matches multiple invocations; `details.candidates` should be present when available.

### CLI Navigation Resolution Contract (normative behavior)

This contract is implementation-surface agnostic but mandatory for S2 CLI handlers.

**request**:
```json
{
  "command_family": "worktree",
  "verb": "open",
  "ref": "alpha",
  "repo_scope": "cwd_repo"
}
```

**response 200**:
```json
{
  "ok": true,
  "target_kind": "worktree",
  "resolved_repo_id": "r1",
  "resolved_id": "wt-1",
  "resolved_path": "/abs/path",
  "resolution_source": "daemon_get_worktree"
}
```

**errors**:
- `E_DAEMON_CONNECTION_FAILED` (503): daemon cannot be reached and request is outside bootstrap/health fallback boundary.
- `E_DAEMON_INCOMPATIBLE` (409): CLI/daemon API versions are incompatible.
- `E_NO_REPO_CONTEXT` (400): command requires repo scope and none can be determined.
- `E_AMBIGUOUS` (409): repo selection or target selection is ambiguous across repos.
- `E_WORKTREE_NOT_FOUND` (404): worktree target does not exist.
- `E_INVOCATION_NOT_FOUND` (404): invocation target does not exist.

### CLI Invocation Navigation Command Policy (normative behavior)

S2 command-surface policy for invocation navigation:
- Canonical family: `agency agent`
- Canonical S2 invocation navigation verbs: `path`, `open`, `shell`, `enter`
- `agent attach` remains supported in v2.1 as a compatibility alias to `agent enter` for headed attach semantics
- Legacy top-level `path`, `open`, `attach`, and `resume` remain supported in v2.1 as compatibility adapters
- All aliases/adapters must delegate to the same daemon-first navigation resolution contract before local editor/shell/tmux dispatch
- `agent restart` is reserved for S3 canonical checkpoint-aware semantics; S2 must not assign conflicting semantics to the canonical verb
- S2 does not require a dedicated `select` CLI verb; the roadmap’s `select` acceptance may be satisfied by deterministic list-row/script-driven target selection that feeds canonical path/open/shell/enter commands

Compatibility constraints:
- If S2 introduces `agent restart` before S3, it must be a clearly labeled compatibility alias with headed-only/non-checkpoint semantics and must not redefine the S3 checkpoint-aware contract.
- Legacy `resume --restart` remains a compatibility restart path in v2.1 and must be documented as headed-only/non-checkpoint behavior.
- Compatibility aliases must preserve script-safe behavior (exit codes and target-resolution determinism) while deprecation messaging remains non-breaking.

---

## 5. Error Codes

| code | http | meaning |
|---|---:|---|
| `E_METHOD_NOT_ALLOWED` | 405 | Wrong HTTP method for a daemon read endpoint. |
| `E_INTERNAL` | 500 | Daemon read handler cannot complete the request due to internal error. |
| `E_WORKTREE_NOT_FOUND` | 404 | No worktree matches the requested ref in scope. |
| `E_WORKTREE_ID_AMBIGUOUS` | 409 | Worktree ref matches multiple candidates. |
| `E_INVOCATION_NOT_FOUND` | 404 | No invocation matches the requested ref in scope. |
| `E_INVOCATION_ID_AMBIGUOUS` | 409 | Invocation ref matches multiple candidates. |
| `E_DAEMON_NOT_RUNNING` | 503 | Daemon is unavailable before a read can be routed. |
| `E_DAEMON_START_FAILED` | 503 | CLI attempted daemon bootstrap and startup failed. |
| `E_DAEMON_CONNECTION_FAILED` | 503 | CLI could not connect to daemon for canonical read resolution. |
| `E_DAEMON_INCOMPATIBLE` | 409 | CLI and daemon API versions do not match. |
| `E_NO_REPO_CONTEXT` | 400 | Command requires repo context but neither cwd nor flags resolve one. |
| `E_AMBIGUOUS` | 409 | Cross-repo resolution yields multiple candidates and command cannot proceed deterministically. |
| `E_INVALID_ARGUMENT` | 400 | Caller provided invalid read/list query params (including unknown `state`/`mode`) or invalid list/watch adjunct arguments. |
| `E_NOT_INTERACTIVE` | 400 | Interactive navigation command invoked without a TTY. |
| `E_INVOCATION_INVALID_MODE` | 409 | Requested interactive action is not valid for the invocation mode. |

---

## 6. Invariants

1. Every S2 `agent`/`worktree` list/show read resolves via daemon APIs and renders daemon DTO data without local store re-derivation.
2. Every S2 path/open/shell navigation command must obtain the authoritative target path from a daemon read contract before local editor/shell dispatch.
3. CLI bootstrap/health fallback is the only allowed local-read exception path for S2 read routing.
4. `--json` list/show outputs for S2 read surfaces must be direct serializations of daemon DTO payloads (field names unchanged).
5. List pagination defaults and limits are stable across worktree and invocation list endpoints: default `100`, max `500`.
6. Ambiguous target resolution must fail explicitly and preserve candidate information when the daemon provides candidates.
7. Interactive attach/enter flows must perform TTY preflight before tmux attach dispatch.
8. Fleet list/filter flows must remain scriptable: human output may be formatted, but selection identity must always be resolvable back to daemon IDs/refs.
9. S2 must not require CLI command handlers to scan the local store filesystem to discover invocations/worktrees for read or navigation target resolution.
10. Canonical invocation navigation behavior must live under `agent`, while any v2.1 compatibility aliases are thin wrappers over the same daemon-first resolution path and explicitly documented dispatch behavior.
11. `agent restart` canonical semantics must remain unassigned in S2 unless they are fully compatible with the S3 checkpoint-aware contract.
12. Invalid list-filter enum values (`state`, `mode`) must fail closed with `E_INVALID_ARGUMENT` and must not silently broaden or default the result set.

---

## 7. Acceptance Scenarios

### scenario: daemon-of-record invocation list
- **given**: multiple invocations exist in the selected repo
- **when**: the user runs `agent ls` with or without filters
- **then**: the CLI resolves repo scope, calls daemon `GET /invocations`, and renders daemon DTO fields without local store discovery

### scenario: daemon-of-record invocation show
- **given**: an invocation id or unique prefix
- **when**: the user runs `agent show <ref>`
- **then**: the CLI resolves the invocation via daemon `GET /invocations/{ref}` and prints daemon-owned status/path fields

### scenario: daemon-of-record worktree list and show
- **given**: one or more integration worktrees in repo scope
- **when**: the user runs `worktree ls` or `worktree show <ref>`
- **then**: the CLI resolves and renders from daemon `GET /worktrees` or `GET /worktrees/{ref}` only

### scenario: worktree path/open/shell use daemon path resolution
- **given**: a valid worktree ref in repo scope
- **when**: the user runs `worktree path`, `worktree open`, or `worktree shell`
- **then**: the authoritative `tree_path` is sourced from daemon worktree read data before local editor/shell execution

### scenario: invocation navigation open uses daemon invocation resolution
- **given**: a valid invocation ref in repo scope
- **when**: the user runs `agent open <ref>`
- **then**: the authoritative `sandbox_path` is sourced from daemon invocation read data before local editor execution

### scenario: canonical invocation navigation under agent family
- **given**: a valid invocation target and repo scope
- **when**: the user runs `agent path`, `agent open`, `agent shell`, or `agent enter`
- **then**: the command resolves the invocation/path via daemon-first S2 navigation resolution and dispatches without local store discovery

### scenario: compatibility alias delegates to canonical path
- **given**: a v2.1 compatibility command (`agent attach` or legacy top-level `path/open/attach/resume`)
- **when**: the user invokes the command with a valid target
- **then**: the command delegates to the same daemon-first resolution contract and preserves deterministic exit behavior

### scenario: fleet filter on worktree ref remains deterministic
- **given**: invocations across multiple worktrees
- **when**: the user runs `agent ls --worktree <ref>`
- **then**: the daemon resolves the worktree filter and returns either a filtered list or an empty list for unresolved refs without silently widening the result set

### scenario: invalid list filter is rejected
- **given**: a caller provides an unsupported `state` or `mode` value to a daemon-backed list flow
- **when**: the request is evaluated by the daemon read contract
- **then**: the daemon returns `400 E_INVALID_ARGUMENT` with structured details and does not coerce the value to a broader default

### scenario: ambiguous selection fails with candidates
- **given**: a ref that matches multiple invocations or worktrees in scope
- **when**: the user runs a single-target read or navigation command
- **then**: the command fails with an ambiguous error and preserves candidate details for deterministic retry

### scenario: daemon unavailable outside bootstrap boundary
- **given**: the daemon is unreachable after repo context resolution
- **when**: the user runs an S2 read or navigation resolution command
- **then**: the command fails with daemon-unavailable semantics and does not silently scan local store state

### scenario: interactive navigation enforces tty preflight
- **given**: a non-interactive session
- **when**: the user runs an interactive attach/enter navigation command
- **then**: the command fails before dispatch with a TTY-required error and a recovery hint

### scenario: list outputs remain scriptable at fleet scale
- **given**: many invocations/worktrees and repeated list/filter commands
- **when**: the user uses list output to select and navigate to a target
- **then**: the identifiers/paths emitted by S2 flows remain stable enough to drive direct path/open/select usage

---

## 8. Traceability Map

| l1 acceptance item | spec section(s) |
|---|---|
| v2 `agent`/`worktree` reads resolve through daemon APIs (except bootstrap/health fallback) | 1, 2, 3, 4, 5, 6, 7 |
| navigation/list/filter flows support direct path/shell/open/select usage | 1, 2, 3, 4, 6, 7 |
| invocation navigation converges toward canonical `agent` command family without breaking existing workflows during v2.1 | 1, 2, 3, 4, 6, 7 |
| S2 remains limited to daemon-read convergence + navigation basics (not chat/restart-from-checkpoint/review) | 1, 6, 9 |
| detached/fleet navigation ergonomics support many worktrees/invocations | 2, 4, 6, 7 |

---

## 9. Unresolved Questions + Temporary Defaults (must be empty before freeze)

| question | temporary default behavior | owner | due |
|---|---|---|---|

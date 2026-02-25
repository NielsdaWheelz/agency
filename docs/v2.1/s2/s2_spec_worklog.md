# S2 Daemon Read Convergence + Sandbox Navigation Spec - Worklog

Last updated: 2026-02-25
Status: draft

## Cluster 1: Scope boundaries and acceptance decomposition

### Evidence log

| Date | Source | Evidence | Relevance |
|---|---|---|---|
| 2026-02-25 | `docs/v2.1/slice-roadmap.md:48` | S2 goal is daemon-first read architecture + detached/fleet navigation basics. | Defines slice contract goal. |
| 2026-02-25 | `docs/v2.1/slice-roadmap.md:50` | S2 outcome removes local store scans from CLI read handlers and improves multi-target navigation. | Defines architecture and UX target. |
| 2026-02-25 | `docs/v2.1/slice-roadmap.md:52` | S2 acceptance requires daemon `agent`/`worktree` reads plus direct path/shell/open/select flows. | Primary L1 acceptance target. |
| 2026-02-25 | `docs/v2.1/slice-roadmap.md:54` | S3 owns chat control plane + restart-from-checkpoint continuity. | Prevents S2 scope leakage. |
| 2026-02-25 | `docs/v2.1/product-brief.md:20` | v2.1 goal makes daemon APIs read/write authority for `agent` + `worktree` surfaces. | Product-level authority requirement. |
| 2026-02-25 | `docs/v2.1/product-brief.md:30` | Local read fallback is allowed only for daemon bootstrap/health boundaries. | Defines S2 fallback exception boundary. |
| 2026-02-25 | `docs/v2.1/product-brief.md:32` | Product scope names `agent path`, `agent shell`, `agent enter`, `agent restart`. | Creates command-surface migration decision for S2. |
| 2026-02-25 | `docs/v2.1/product-brief.md:36` | Fleet operations require list/filter/sort/status plus fast enter/detach loops with scriptable outputs. | Drives list/filter/navigation invariants. |
| 2026-02-25 | `docs/v2.1/product-brief.md:54` | v2.1 must keep sandbox-first safety model. | Constrains navigation command design. |
| 2026-02-25 | `docs/sdlc/L2-slice-spec.md:29` | L2 phase 1 requires writing the skeleton first. | Determines drafting sequence. |
| 2026-02-25 | `docs/sdlc/L2-slice-spec.md:33` | L2 phase 2 requires contract clusters by behavior surface. | Drives cluster layout in this worklog. |
| 2026-02-25 | `docs/sdlc/L2-slice-spec.md:38` | L2 micro-loop requires minimal facts, forced questions, immediate spec patching, and companion logs. | Justifies iterative drafting style. |
| 2026-02-25 | `docs/sdlc/L2-slice-spec.md:134` | Unresolved/default table must be empty before freeze. | Defines freeze gate for open S2 decisions. |

## Cluster 2: Existing daemon read contracts and CLI read coverage

### Evidence log

| Date | Source | Evidence | Relevance |
|---|---|---|---|
| 2026-02-25 | `internal/daemon/read_types.go:9` | Daemon read API uses a common envelope (`ok`, `api_version`, `request_id`, `data`, error fields). | Canonical envelope for S2 API contract. |
| 2026-02-25 | `internal/daemon/read_types.go:37` | `WorktreeDTO` includes `tree_path`, state, branch metadata, timestamps. | Provides authoritative path and display fields for worktree navigation. |
| 2026-02-25 | `internal/daemon/read_types.go:53` | `InvocationDTO` includes `sandbox_path`, status, display status, attention flags, sort key. | Provides authoritative invocation navigation and fleet display fields. |
| 2026-02-25 | `internal/daemon/read_handlers.go:70` | Daemon serves `GET /worktrees` list endpoint with sorting/pagination. | Core S2 worktree list read contract. |
| 2026-02-25 | `internal/daemon/read_handlers.go:129` | Daemon serves `GET /worktrees/{ref}` and returns ambiguous candidate details on conflict. | Core S2 worktree single-target read contract. |
| 2026-02-25 | `internal/daemon/read_handlers.go:169` | Daemon serves `GET /invocations` list endpoint with filtering and pagination. | Core S2 invocation list read contract. |
| 2026-02-25 | `internal/daemon/read_handlers.go:263` | Daemon serves `GET /invocations/{ref}` and surfaces ambiguous/not-found errors. | Core S2 invocation single-target read contract. |
| 2026-02-25 | `internal/daemon/read_handlers.go:876` | List parsers default to `limit=100` and cap at `500`; worktree state defaults `present`, invocation state/mode defaults `all`. | Exact query default/bounds contract. |
| 2026-02-25 | `internal/daemon/read_handlers.go:1037` | Pagination cursors are daemon-generated opaque base64 payloads with deterministic sort boundaries. | Defines pagination stability contract for list flows. |
| 2026-02-25 | `internal/daemon/read_handlers.go:885` | Current parser accepts arbitrary `state` strings and stores them without validation. | Identifies current implementation leniency vs desired strict enum contract. |
| 2026-02-25 | `internal/daemon/read_handlers.go:914` | Current parser accepts arbitrary invocation `state` and `mode` strings without validation. | Identifies current implementation leniency vs desired strict enum contract. |
| 2026-02-25 | `internal/daemon/read_handlers.go:993` | `matchesWorktreeState` defaults unknown filters to present-state behavior instead of erroring. | Shows silent coercion risk for automation. |
| 2026-02-25 | `internal/daemon/read_handlers.go:1004` | `matchesInvocationState` defaults unknown filters to match-all behavior. | Shows silent broadening risk for automation. |
| 2026-02-25 | `internal/daemon/read_handlers.go:1022` | `matchesInvocationMode` defaults unknown filters to match-all behavior. | Shows silent broadening risk for automation. |
| 2026-02-25 | `internal/daemonclient/client.go:540` | CLI daemon client exposes `ListWorktrees` with request envelope decode and typed DTO return. | Confirms current client surface for S2 reuse. |
| 2026-02-25 | `internal/daemonclient/client.go:600` | CLI daemon client exposes `GetWorktree` for single worktree resolution. | Supports daemon-first worktree path/open/shell resolution. |
| 2026-02-25 | `internal/daemonclient/client.go:660` | CLI daemon client exposes `ListInvocations` for fleet list/filter. | Supports daemon-first invocation list/filter flows. |
| 2026-02-25 | `internal/daemonclient/client.go:726` | CLI daemon client exposes `GetInvocation` for single invocation resolution. | Supports daemon-first invocation navigation resolution. |

## Cluster 3: Remaining local-store read/navigation gaps in CLI handlers

### Evidence log

| Date | Source | Evidence | Relevance |
|---|---|---|---|
| 2026-02-25 | `internal/commands/agent.go:336` | `AgentLS` resolves repo via daemon and calls daemon `ListInvocations`, rendering daemon DTOs directly. | Confirms some S2 read convergence is already complete. |
| 2026-02-25 | `internal/commands/agent.go:449` | `AgentShow` calls daemon `GetInvocation` and renders daemon DTO fields. | Confirms single-invocation read is daemon-first already. |
| 2026-02-25 | `internal/commands/worktree.go:150` | `WorktreeLS` calls daemon `ListWorktrees` and renders daemon DTOs. | Confirms worktree list read convergence is complete. |
| 2026-02-25 | `internal/commands/worktree.go:271` | `WorktreeShow` calls daemon `GetWorktree` and renders daemon DTOs. | Confirms worktree show read convergence is complete. |
| 2026-02-25 | `internal/commands/worktree.go:346` | `WorktreePath` already uses daemon `GetWorktree` and outputs `tree_path`. | Confirms path command is close to S2 target. |
| 2026-02-25 | `internal/commands/worktree.go:424` | `WorktreeOpen` still resolves worktree via local integration-worktree service for `tree_path`. | Identifies S2 gap: local store re-resolution after daemon repo resolution. |
| 2026-02-25 | `internal/commands/worktree.go:509` | `WorktreeShell` still resolves worktree via local integration-worktree service for `tree_path`. | Identifies S2 gap: local store re-resolution after daemon repo resolution. |
| 2026-02-25 | `internal/commands/agent.go:608` | `AgentAttach` resolves invocation via local invocation service after daemon repo resolution. | Identifies S2 gap in invocation navigation resolution. |
| 2026-02-25 | `internal/commands/agent.go:1109` | `AgentOpen` resolves invocation via local invocation service for sandbox path. | Identifies S2 gap in invocation open/navigation resolution. |

## Cluster 4: Command surface overlap and migration risk

### Evidence log

| Date | Source | Evidence | Relevance |
|---|---|---|---|
| 2026-02-25 | `internal/cli/cobra/agent.go:27` | `agent` command currently includes `ls/show/attach/open/...` but not `path/shell/enter/restart`. | Confirms product-vs-current CLI naming mismatch. |
| 2026-02-25 | `internal/cli/cobra/worktree.go:25` | `worktree` command already includes `path/open/shell`. | S2 can reuse mature worktree navigation verbs. |
| 2026-02-25 | `internal/cli/cobra/cmd_path.go:11` | Legacy top-level `path` command exists for run-oriented scripting. | Creates overlap with product-scope `agent path`. |
| 2026-02-25 | `internal/cli/cobra/cmd_open.go:15` | Legacy top-level `open` command exists for run-oriented editor open flow. | Creates overlap with `agent open` and future `agent enter` UX. |
| 2026-02-25 | `internal/cli/cobra/cmd_resume.go:15` | Legacy top-level `resume` includes `--restart` behavior. | Closest existing surface to product-scope `agent enter/restart`. |
| 2026-02-25 | `internal/commands/path.go:23` | Legacy `Path` resolves runs via local global resolver and local store metadata. | Confirms non-daemon legacy behavior that conflicts with S2 target if reused as-is. |
| 2026-02-25 | `internal/commands/open.go:35` | Legacy `Open` resolves runs/worktree path via local store and filesystem checks. | Confirms non-daemon legacy behavior. |
| 2026-02-25 | `internal/commands/attach.go:30` | Legacy `Attach` resolves runs globally and attaches to tmux session directly. | Relevant to `agent enter` alias strategy. |
| 2026-02-25 | `internal/commands/resume.go:50` | Legacy `Resume` provides attach/create/restart tmux semantics with lock/confirm flow. | Relevant to `agent restart` and `agent enter` migration path. |

### Decisions captured

- D-001 resolved: canonical invocation navigation is the `agent` family (`path`, `open`, `shell`, `enter`) with v2.1 compatibility aliases (`agent attach` and legacy top-level `path/open/attach/resume`) delegating to the same daemon-first resolver; canonical `agent restart` remains reserved for S3 checkpoint-aware semantics.
- D-006 resolved: unknown list-filter enum values (`state`, `mode`) are rejected by the S2 daemon read contract with `400 E_INVALID_ARGUMENT` and structured details; current lenient matcher behavior is implementation drift to close in S2 PRs.

## Cluster status

- Cluster 1 drafted in `s2_spec.md`.
- Cluster 2 drafted in `s2_spec.md`.
- Cluster 3 drafted in `s2_spec.md` (with identified convergence gaps).
- Cluster 4 drafted in `s2_spec.md` (command-surface policy resolved; alias/deprecation behavior now normative).
- Hardening passes completed:
  - completeness pass vs S2 acceptance wording and approved command-surface policy.
  - consistency pass (models, api contracts, errors, invariants, acceptance scenarios).
  - traceability pass after final acceptance scenario updates.
  - boundary cleanup (no L3/L4 implementation directives in contract sections).
  - ambiguity cleanup (unresolved/default table cleared for freeze readiness).

### Remaining implementation drift to address in S2 PRs

- Daemon list handlers currently coerce unknown `state`/`mode` values instead of returning `E_INVALID_ARGUMENT`; implementation must be brought up to the S2 contract before slice completion.
- Several navigation commands still re-resolve entities through local store services after daemon repo resolution and must be converted to daemon-first resolution (`worktree open/shell`, `agent attach/open`).

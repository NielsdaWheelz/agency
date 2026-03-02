# Slice S5: Invocation-Centric Review + PR + Merge — Spec

## Goal

Move review/PR/merge operations under invocation scope.

## Acceptance Criteria

### canonical command set is invocation-scoped under `agent`
- **given**: a user runs review/PR/merge progression from CLI
- **when**: they use `agent review <invocation_ref>`, `agent pr sync <invocation_ref>`, and `agent merge <invocation_ref>`
- **then**: these are the canonical v2.1 surfaces; each command requires `invocation_ref`, supports `--repo` and `--json`, and enforces deterministic validation for command-specific flags (merge strategy exclusivity, confirmation mode, dirty-worktree policy, and force-with-lease policy)

### review surface returns deterministic progression state
- **given**: an invocation in any lifecycle state
- **when**: the user runs `agent review` in human or `--json` mode
- **then**: output includes one deterministic readiness verdict with typed blocking reasons and navigation hints, including whether PR progression is currently allowed from that invocation context

### PR sync is invocation-addressed but branch-scoped
- **given**: an invocation resolves to an integration worktree and review prerequisites are satisfied
- **when**: the user runs `agent pr sync`
- **then**: the system pushes the integration branch and creates or updates the corresponding PR deterministically, returning stable machine-readable PR identity/outcome fields; if prerequisites fail (for example unresolved landing/readiness), the command returns typed deterministic errors with hints

### PR body/report processing is bounded
- **given**: report content, git history, or changed-file lists are very large
- **when**: PR body completeness checks and fallback body generation run
- **then**: reads and generated sections are bounded by contract limits, over-limit behavior is deterministic, and automation never depends on unbounded in-memory reads

### merge flow is invocation-scoped and scriptable
- **given**: an invocation has an open, mergeable PR and merge prerequisites pass
- **when**: the user runs `agent merge` (interactive or non-interactive)
- **then**: verify, confirmation policy, merge execution, and post-merge lifecycle actions execute as one invocation-scoped flow with deterministic result contracts; non-interactive automation has an explicit confirmation path and does not require ad hoc TTY scraping

### merge log durability and permissions are contract-enforced
- **given**: merge execution emits output
- **when**: merge log persistence is attempted
- **then**: log-write failures are surfaced as typed operation failures (not ignored), and successful log artifacts use private permissions aligned with v2.1 safety expectations

### verify environment semantics are correct for merge path
- **given**: merge verify execution prepares runtime environment
- **when**: environment variables are assembled
- **then**: repository-root and workspace-root semantics are unambiguous and deterministic (`repo root` is the actual repository root; workspace root remains the target integration worktree)

### daemon contracts back invocation-scoped review/pr/merge
- **given**: agent review/pr/merge commands execute
- **when**: command handlers resolve refs and perform read/mutation operations
- **then**: behavior runs through canonical daemon contracts with stable response/error envelopes (including typed `error_code`, message, hint, and request correlation), not ad hoc local-store orchestration in CLI handlers

### end-to-end coverage proves workflow integrity
- **given**: CI e2e coverage for PR lifecycle flows
- **when**: invocation progression executes review -> PR sync -> merge on a real repo/remote
- **then**: happy path and key failure paths (not ready, missing/closed PR, mergeability failure, confirmation failure, bounded-input rejection, log-write failure) are asserted with deterministic outcomes

## Key Decisions

**Engine is progression-state evaluation keyed by invocation + integration worktree**: S5 treats invocation as the user-facing selector while PR/merge eligibility is computed from invocation lifecycle state, landing status, and integration branch state in one deterministic review progression model.

**PR identity is branch-scoped, not per-invocation object identity**: invocation selects the target context, but PR create/update/lookup is anchored to the integration worktree branch so repeated invocation cycles on the same branch do not fork incompatible PR lineage.

**Daemon owns invocation-scoped review/pr/merge behavior**: canonical S5 read/mutation behavior is daemon-mediated to preserve v2.1 authority-model invariants and prevent CLI/daemon drift in validation, errors, and JSON contracts.

**Machine-readable outputs are first-class contracts**: `--json` outputs for review/pr/merge are stable, typed, and script-safe; human formatting is a rendering layer and never the automation contract.

**Safety hardening is part of product behavior, not optional tech debt**: bounded report/PR-body processing, merge-log write-failure visibility, and private log permissions are required S5 behavior to keep lifecycle automation safe under real repository scale.

**Legacy run-scoped `push`/`merge` are compatibility paths only**: S5 defines invocation-scoped `agent` commands as canonical v2.1 behavior; any legacy commands may remain for backward compatibility but must not redefine S5 semantics.

## Out of Scope

- Reports v2 artifact/strictness transition and global CLI ergonomics cleanup (including broad `--yes` standardization) (-> Slice S6)
- Full-screen watch/TUI or GUI workflow expansion (-> Slice S7 / deferred)
- Merge queue orchestration or policy-driven auto-fix loops from review comments
- Non-GitHub-host expansion as part of S5 contract definition

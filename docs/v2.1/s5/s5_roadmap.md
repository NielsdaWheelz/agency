# Slice S5: Invocation-Centric Review + PR + Merge — PR Roadmap

### PR-01: daemon-backed invocation review + PR sync (`agent review`, `agent pr sync`)
- **goal**: establish canonical invocation-scoped review and PR sync flows with deterministic human/JSON contracts.
- **builds on**: Slice S4 merged state (`agent checks` readiness model, daemon API envelopes, runner-capability and mutation JSON baselines).
- **acceptance**:
  - `agency agent review <invocation_ref>` (human and `--json`) returns one deterministic readiness verdict with typed blocking reasons, navigation hints, and explicit PR progression eligibility for that invocation context.
  - `agency agent pr sync <invocation_ref>` resolves invocation -> integration worktree deterministically, pushes the branch, and creates/updates one branch-scoped PR identity with stable machine-readable identity/outcome fields.
  - command validation is deterministic and typed for repo resolution, dirty-worktree policy, and force-with-lease policy.
  - report/body processing for PR sync is bounded by contract limits; over-limit reads or generation paths fail or degrade deterministically (no unbounded in-memory reads).
  - canonical read/mutation behavior for these surfaces is daemon-mediated, not ad hoc CLI-local run-store orchestration.
- **non-goals**: no invocation-scoped merge execution yet; no removal of legacy `push`/`merge` compatibility commands.

### PR-02: invocation-scoped merge contract + durability + workflow e2e (planned after PR-01 merges)
- **goal**: complete invocation-scoped review -> PR -> merge progression with script-safe confirmation semantics and merge-path hardening.
- **builds on**: PR-01.
- **acceptance**:
  - `agency agent merge <invocation_ref>` supports interactive and non-interactive confirmation paths with deterministic validation for merge strategy exclusivity and confirmation mode; non-interactive confirmation is standardized on `--yes`.
  - merge prechecks, verify execution, merge execution, and post-merge lifecycle actions run as one invocation-scoped flow with stable `--json` result/error contracts.
  - merge-log persistence failures surface as typed operation failures; successful merge logs use private permissions aligned with v2.1 safety expectations.
  - verify environment assembly uses deterministic and correct root semantics (`repo root` = actual repository root; workspace root = integration worktree target).
  - end-to-end coverage asserts happy path and key failure paths (not ready, missing/closed PR, mergeability failure, confirmation failure, bounded-input rejection, log-write failure) with deterministic outcomes.
- **non-goals**: no reports-v2 migration or broad CLI ergonomics standardization beyond S5 canonical confirmation behavior (Slice S6).

### Compatibility policy (applies across S5 PRs)
- `agent review` is canonical for invocation readiness/progression assessment.
- `agent checks` is removed from the S5 command surface; compatibility may exist only in internal code paths during migration.

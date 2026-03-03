# Agency v2.1 Constitution

Last updated: 2026-03-03
Status: active
Owners: `@nnandal` + `Codex`

## 1. Vision

### Problem
Agency already has strong local isolation and daemon primitives, but v2 UX is fragmented for detached/headless execution and invocation-centric review/merge. Users cannot reliably run one continuous `chat -> review -> PR -> merge -> archive` workflow from an invocation context.

### Solution
v2.1 delivers functional Conductor parity at the daemon + CLI layer while preserving Agency's sandbox-first safety model and deterministic machine-readable contracts.

### Scope (v2.1)
1. Daemon-owned read/write authority for v2 `agent` + `worktree` surfaces (local fallback only at bootstrap/health boundaries).
2. Invocation navigation and re-entry command surfaces (`agent path`, `agent shell`, `agent enter`, `agent restart`).
3. Headless chat control plane with transcript visibility, follow-up prompting, and checkpoint restart continuity.
4. Fleet-scale invocation/worktree operations (list/filter/sort/status/select flows).
5. Invocation-scoped review/PR/merge command family with stable `--json` output contracts.
6. Runner capability model for `claude-code`, `codex`, `amp`, `opencode`, `cursor`, and `droid`, including raw-log fallback where semantic adapters are unavailable.
7. Mutation-command JSON parity for v2 `agent` surfaces.
8. Reports v2 transition (`report.json` optional artifact with deterministic precedence, markdown compatibility retained).
9. Interactive history-driven checkpoint restore (arrow-key terminal navigation) for headless invocations.
10. Durable package boundaries for release-gating and contract enforcement (no temporary slice-scoped namespaces).

### Non-Scope (v2.1)
1. No GUI parity with Conductor desktop/web.
2. No full-screen TUI requirement for baseline parity (terminal-first checks/readiness is enough for v2.1).
3. No merge queue orchestration.
4. No autonomous policy-driven auto-fix loop from review comments.
5. No relaxation of sandbox-first safety model.
6. No full migration from markdown issue stubs to GitHub issue APIs in v2.1 (markdown remains compatibility source).

## 2. Core Abstractions

| Concept | Definition |
|---|---|
| **Repository Context** | The current repo boundary where daemon-managed worktree/invocation state is resolved. |
| **Integration Worktree** | The durable isolated branch workspace used for unit-of-work lifecycle. |
| **Invocation** | A runner execution context bound to a worktree, with detached/headed/headless continuity semantics. |
| **Checkpoint** | A restore point for invocation state used by restart and history navigation flows. |
| **Runner Capability** | A declared capability set that determines runner behavior/contracts independent of runner name allowlists. |
| **Gate Item** | A release-blocking issue artifact with closure, evidence, and transition policy. |
| **Gate Set** | Canonical Gate A/B membership used to determine release readiness and drift invariants. |

## 3. Architecture

### Components

```text
CLI (agent/worktree commands)
  |
  | daemon API (canonical read/write authority)
  v
Daemon control plane
  |
  | orchestrates
  v
Invocation + runner processes (sandboxed) ----> logs/events/checkpoints
  |
  | integrates with
  v
Issue + release-gate evaluation services
```

### Trust Model
- CLI is a client; daemon contracts are authoritative for v2 lifecycle read/write behavior.
- Runner processes execute under sandbox constraints and must not escape sandbox-first boundaries.
- Release readiness must be deterministically derived from canonical gate membership + issue evidence.
- Human approval remains required for merge/release decisions even when automation surfaces are available.

## 4. Hard Constraints

| Constraint | Value |
|---|---|
| Safety model | Sandbox-first execution is mandatory and non-negotiable. |
| Authority model | Daemon is canonical read/write source for v2 `agent` + `worktree` lifecycle surfaces. |
| Delivery model | CLI-first parity is required; GUI/full TUI is optional/deferred. |
| Runner target set | `claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid` must share one capability-driven model. |
| Output contracts | Automation-facing mutation flows must support stable `--json` responses. |
| Report contract | `report.json` is authoritative when present; markdown remains deterministic compatibility fallback. |
| Confirmation contract | `--yes` is the canonical non-interactive primitive for destructive/irreversible confirmation flows. |
| Release policy | Gate A/B closure + parity baseline + contract/test compliance must all be satisfied before v2.1 RC. |

## 5. Conventions

### Command Surface
- Invocation-scoped lifecycle, chat, review, PR, and merge behaviors are expressed under `agent` command family.
- Compatibility aliases may exist but must not redefine canonical semantics.
- High-traffic additive short aliases are canonicalized as: `-r/--repo`, `-j/--json`, `-y/--yes`, `-o/--open`.
- Non-interactive destructive/irreversible confirmation flows must use `--yes` with deterministic confirmation-required failures when omitted.

### Contract Discipline
- Behavior-changing daemon endpoints require `docs/contracts/*` updates.
- Error and event semantics for critical mutation paths must be deterministic and test asserted.
- Reports-v2 progression surfaces resolve through one canonical report model with mode-aware strictness (headless fail-closed; headed/compatibility fallback with explicit diagnostics).

### Evidence and Closure
- Gate-item closure requires implementation references and test evidence recorded in issue artifacts.
- Drift validation compares canonical gate membership against canonical issue-map membership coverage.

### Documentation Layering
- This file is the v2.1 L0 source of truth.
- `roadmap.md` is L1 sequencing.
- `docs/v2.1/s*/s*_spec.md` files are L2 contracts per slice.

## 6. Invariants

1. Daemon APIs are the read/write source of truth for v2 `agent` and `worktree` command behavior.
2. Detached/headless invocation continuity must survive detach/re-entry cycles without creating a new invocation.
3. Checkpoint restore + restart must be available as a single invocation-scoped flow.
4. History-driven checkpoint selection must be deterministic and scriptable alternatives must remain available.
5. Runner support is capability-driven, not hardcoded by runner-name allowlist checks.
6. Invocation-scoped review/PR/merge surfaces must expose deterministic machine-readable outputs.
7. Gate A and Gate B membership is canonical only from this constitution's Gate A/B sections.
8. Gate drift validation must fail when any canonical gate issue is missing from or duplicated in the Issue Map section.
9. Gate A/B closure status must be reproducible from issue artifacts and declared evidence.
10. Sandbox-first boundaries must not be bypassed by parity features.
11. v2.1 scope must not expand into GUI/full TUI requirements.
12. Code + tests are the proof gate; docs are the direction gate.
13. Reports-v2 precedence is deterministic: `report.json` takes priority when present; markdown remains compatibility input.
14. Headless progression paths (`review`/`pr sync`/`merge`) fail closed on report contract violations; headed/compatibility paths fail open with explicit diagnostics.
15. Destructive/irreversible non-interactive flows must use the `--yes` confirmation contract.

## 7. Release Policy

### Gate Taxonomy
- **Gate A**: P0 safety closure; must be zero-open before release candidate.
- **Gate B**: parity-critical P1 closure; must be complete before release candidate.
- **Gate C**: parity baseline acceptance behaviors.
- **Gate D**: contract and test compliance requirements.

### Gate C: parity baseline acceptance
1. Headless invocation supports detached transcript visibility (prompts/messages/tool-use/logs) and follow-up prompt flow.
2. Users can enter/detach/re-enter invocation context repeatedly without resetting invocation continuity.
3. Restart-from-checkpoint exists as a single invocation command path.
4. Invocation-centric PR/review/merge command family exists with stable `--json` contracts.
5. Runner capability model replaces hardcoded `claude|codex` gates.
6. Runner targets `claude-code`, `codex`, `amp`, `opencode`, `cursor`, and `droid` are supported through one capability-driven invocation model.
7. Checkpoint restore supports explicit checkpoint selection and interactive history-based selection.
8. Daemon APIs remain read/write authority for v2 `agent` + `worktree` surfaces.
9. Fleet workflows support efficient list/filter/status/selection over many worktrees/invocations.
10. Reports-v2 progression resolves through one canonical model with deterministic `report.json` precedence and markdown compatibility.
11. High-traffic confirmation/flag ergonomics are standardized with `--yes` and canonical short aliases (`-r`, `-j`, `-y`, `-o`).

### Gate D: contract and test compliance
1. `docs/contracts/*` is updated for new daemon endpoints or data-contract changes.
2. Behavior-changing decisions ship with unit/integration tests.
3. Event + error-code behavior is asserted for critical mutation flows.
4. End-to-end coverage exists for invocation-centric review/PR/merge happy path.

## 8. L1 Slice Ordering

The canonical v2.1 sequencing is maintained in `roadmap.md`.

## Gate A

1. `docs/issues/events-p0-event-system-hardening.md`
2. `docs/issues/store-p0-08-remove-paths-use-raw-osremoveall-without-safety-checks.md`
3. `docs/issues/daemon-p0-08-unsafe-deletes-in-landing.md`

## Gate B

1. `docs/issues/cli-p1-01-all-commands-drop-cmdcontext.md`
2. `docs/issues/core-p1-01-cli-ignores-cancellation-and-timeouts-at-the-boundary.md`
3. `docs/issues/runtime-p1-runtime-dirs-single-source-of-truth.md`
4. `docs/issues/daemon-p1-04-request-decoding-is-too-permissive.md`
5. `docs/issues/daemon-p1-20-legacy-headless-endpoint-lacks-modern-validation.md`
6. `docs/issues/daemon-p1-21-clientrequestid-is-not-validated-as-uuid.md`
7. `docs/issues/cli-p1-12-unbounded-prompt-file-reads.md`
8. `docs/issues/daemon-p1-15-headless-runner-inherits-stdin.md`
9. `docs/issues/daemon-p1-23-headless-runner-env-lacks-required-defaults.md`
10. `docs/issues/events-p1-remove-ad-hoc-event-writers.md`
11. `docs/issues/events-p1-enforce-required-events-in-flows.md`
12. `docs/issues/daemon-p1-13-stream-parser-drops-write-errors.md`
13. `docs/issues/daemon-p1-14-stream-parser-can-allocate-unbounded-memory-on-huge.md`
14. `docs/issues/stream-p1-01-normalized-event-seq-is-not-persisted.md`
15. `docs/issues/checkpoint-p1-01-event-sequence-is-not-monotonic-across-daemon-restarts.md`
16. `docs/issues/checkpoint-p1-02-checkpoint-apply-emits-seq1-unconditionally.md`
17. `docs/issues/core-p1-tighten-file-permissions.md`
18. `docs/issues/exec-p1-deterministic-env-merge.md`
19. `docs/issues/spec-p1-07-runner-capability-target-set-claude-code-codex-amp-opencode-cursor-droid.md`
20. `docs/issues/spec-p1-08-daemon-read-write-authority-for-v2-agent-and-worktree-surfaces.md`
21. `docs/issues/spec-p1-09-detached-chat-transcript-and-session-reentry-contract.md`
22. `docs/issues/spec-p1-10-fleet-management-for-many-worktrees-and-invocations.md`
23. `docs/issues/checkpoint-p1-10-interactive-history-selector-for-checkpoint-revert.md`
24. `docs/issues/spec-p1-11-invocation-scoped-review-pr-merge-command-contracts.md`

## Issue Map

### S1 Platform Hardening Gates

1. `docs/issues/events-p0-event-system-hardening.md`
2. `docs/issues/store-p0-08-remove-paths-use-raw-osremoveall-without-safety-checks.md`
3. `docs/issues/daemon-p0-08-unsafe-deletes-in-landing.md`
4. `docs/issues/cli-p1-01-all-commands-drop-cmdcontext.md`
5. `docs/issues/core-p1-01-cli-ignores-cancellation-and-timeouts-at-the-boundary.md`
6. `docs/issues/runtime-p1-runtime-dirs-single-source-of-truth.md`
7. `docs/issues/daemon-p1-04-request-decoding-is-too-permissive.md`
8. `docs/issues/daemon-p1-20-legacy-headless-endpoint-lacks-modern-validation.md`
9. `docs/issues/daemon-p1-21-clientrequestid-is-not-validated-as-uuid.md`
10. `docs/issues/core-p1-tighten-file-permissions.md`
11. `docs/issues/events-p1-remove-ad-hoc-event-writers.md`
12. `docs/issues/events-p1-enforce-required-events-in-flows.md`

### S2 Daemon Read Convergence + Sandbox Navigation

1. `docs/issues/cli-p2-13-worktree-hardcodes-the-daemon-socket-path.md`
2. `docs/issues/cli-p2-02-multiple-commands-ignore-injected-ctxstdoutstderr.md`
3. `docs/issues/cli-p2-03-widespread-direct-os-usage-despite-fsfs-being-passed.md`
4. `docs/issues/ids-p2-02-ambiguous-worktreeinvocation-errors-are-weak.md`
5. `docs/issues/spec-p1-08-daemon-read-write-authority-for-v2-agent-and-worktree-surfaces.md`
6. `docs/issues/spec-p1-10-fleet-management-for-many-worktrees-and-invocations.md`

### S3 Chat Control Plane + Restart-From-Checkpoint

1. `docs/issues/cli-p1-12-unbounded-prompt-file-reads.md`
2. `docs/issues/daemon-p1-15-headless-runner-inherits-stdin.md`
3. `docs/issues/daemon-p1-23-headless-runner-env-lacks-required-defaults.md`
4. `docs/issues/daemon-p2-19-no-size-limits-for-promptslog-writes.md`
5. `docs/issues/checkpoint-p1-01-event-sequence-is-not-monotonic-across-daemon-restarts.md`
6. `docs/issues/checkpoint-p1-02-checkpoint-apply-emits-seq1-unconditionally.md`
7. `docs/issues/checkpoint-p1-10-interactive-history-selector-for-checkpoint-revert.md`
8. `docs/issues/spec-p1-09-detached-chat-transcript-and-session-reentry-contract.md`

### S4 Runner Capability Model + Agent Mutation JSON

1. `docs/issues/exec-p1-deterministic-env-merge.md`
2. `docs/issues/daemon-p1-13-stream-parser-drops-write-errors.md`
3. `docs/issues/daemon-p1-14-stream-parser-can-allocate-unbounded-memory-on-huge.md`
4. `docs/issues/stream-p1-01-normalized-event-seq-is-not-persisted.md`
5. `docs/issues/stream-p1-02-writes-ignore-errors.md`
6. `docs/issues/spec-p1-07-runner-capability-target-set-claude-code-codex-amp-opencode-cursor-droid.md`

### S5 Invocation-Centric Review + PR + Merge

1. `docs/issues/spec-p1-11-invocation-scoped-review-pr-merge-command-contracts.md`
2. `docs/issues/spec-p2-09-e2e-tests-for-pr-flows.md`
3. `docs/issues/merge-p1-04-merge-log-writes-ignore-errors-and-use-0644.md`
4. `docs/issues/merge-p1-05-buildverifyenvformerge-sets-agencyreporoot-to-worktree-path.md`
5. `docs/issues/push-p2-04-fallback-pr-body-generation-is-unbounded.md`
6. `docs/issues/push-p2-06-report-parsing-reads-unbounded-content.md`

### S6 Reports v2 + CLI Ergonomics Cleanup

1. `docs/issues/product-p3-cli-ergonomics-backlog.md`
2. `docs/issues/cli-p2-15-pr-fallback-generation-is-unbounded.md`
3. `docs/issues/spec-p2-10-reports-v2-json-and-markdown-compat-contract.md`

### S7 Checks-First Watch Seed (Stretch)

1. `docs/issues/spec-p3-07-product-direction-tui-optional-vs-essential.md`
2. `docs/issues/spec-p3-08-tmux-lifecycle-when-runner-exits.md`

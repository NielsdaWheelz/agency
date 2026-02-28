# Slice S3 - PR Roadmap

Last updated: 2026-02-28
Status: draft
Upstream spec: `docs/v2.1/s3/s3_spec.md`

## 0. Contract inventory

| cluster id | l2 cluster (normative surface) |
|---|---|
| C1 | Unified invocation timeline read contract (`typed timeline entries`, `cursor pagination`, one ordering model for prompts/messages/tool-use/raw-log coverage) |
| C2 | Follow-up prompt continuation contract (invocation-scoped write path, idempotent retry behavior, continuity across detach/re-entry cycles) |
| C3 | Prompt/log safety envelope (bounded prompt input paths, bounded transcript/log reads, deterministic validation failures) |
| C4 | Canonical checkpoint restart contract (checkpoint apply + runner restart as one invocation-scoped command path, one result shape, non-interactive execution defaults) |
| C5 | Interactive history selector contract (arrow-key terminal selection, deterministic history-point to checkpoint mapping, scriptable fallback) |
| C6 | Turn-aware diff and checks-first terminal contract (durable turn identity joins timeline/checkpoints/diff, invocation-linked readiness surface with human and JSON output) |

## 1. Dependency graph

```text
PR-01 Unified Invocation Timeline + Durable Ordering
  |
  v
PR-02 Follow-up Prompt Control Plane + Safety Bounds
  |
  v
PR-03 Canonical Agent Restart-From-Checkpoint Flow
  | \
  |  \
  v   v
PR-04 Interactive History Selector + Deterministic Mapping   PR-05 Turn-Aware Diff + Checks-First Readiness Surface
```

## 2. Ownership matrix

| contract cluster (from l2) | owning pr |
|---|---|
| C1: one typed timeline contract for detached transcript/history reads with stable cursoring and durable monotonic ordering across daemon lifecycle boundaries and checkpoint apply operations | PR-01 |
| C2: follow-up prompt write path on existing invocation context, timeline insertion semantics, idempotent retry handling, and detach/re-entry continuity behavior | PR-02 |
| C3: bounded prompt and transcript/log payload handling with deterministic validation errors for out-of-contract sizes and limits | PR-02 |
| C4: integrated checkpoint restore + restart execution as one canonical `agent` flow, including deterministic non-interactive runner launch defaults for restart execution | PR-03 |
| C5: interactive terminal history selection with deterministic mapping from selected timeline point to checkpoint restore outcome, while preserving explicit non-interactive checkpoint flags | PR-04 |
| C6: durable turn identity mapping from timeline to diff context and checks-first terminal readiness surface with machine-readable parity | PR-05 |

## 3. Acceptance coverage map

| l2 acceptance scenario | primary owner pr | supporting pr(s) |
|---|---|---|
| detached transcript timeline is available | PR-01 | none |
| follow-up prompt continues the same invocation | PR-02 | PR-01 |
| prompt and log safety limits are enforced | PR-02 | PR-01 |
| repeated detach and re-entry preserves continuity | PR-02 | PR-01 |
| explicit checkpoint restart is one command path | PR-03 | PR-02 |
| interactive history selector restores deterministically | PR-04 | PR-03 |
| timeline ordering survives daemon restarts and checkpoint apply | PR-01 | PR-03 |
| headless restart execution uses non-interactive process defaults | PR-03 | none |
| turn-aware diff context is available from chat history | PR-05 | PR-01, PR-03 |
| checks-first readiness surface is available in terminal | PR-05 | PR-02 |

## 4. PRs

### PR-01: Unified Invocation Timeline + Durable Ordering
- **Goal**: establish one daemon-owned timeline contract for detached transcript/history reads with stable cursor navigation and durable ordering guarantees.
- **Dependencies**: none.
- **Acceptance**:
  - Transcript/history reads return one ordered timeline that includes prompt seed context, assistant/user messages, tool-use activity, and raw-log coverage markers.
  - Timeline entries are typed and available in both human and `--json` command paths without diverging ordering semantics.
  - Cursor pagination is stable for incremental reads and supports deterministic continuation.
  - Timeline ordering remains monotonic across daemon restarts and checkpoint-apply boundaries.
- **Non-goals**:
  - No follow-up prompt write-path behavior.
  - No checkpoint restore/restart mutation flow.

### PR-02: Follow-up Prompt Control Plane + Safety Bounds
- **Goal**: add invocation-scoped follow-up prompting and enforce S3 prompt/log size contracts with deterministic validation behavior.
- **Dependencies**: PR-01.
- **Acceptance**:
  - Follow-up prompt submission writes to the existing invocation context (not a new invocation) and appears in timeline order.
  - Follow-up prompt writes support idempotent retry semantics and deterministic duplicate handling.
  - Prompt inputs from direct flags and file-backed sources enforce bounded read behavior and deterministic over-limit errors.
  - Transcript/log read requests enforce S3 read-limit bounds with deterministic invalid-argument responses.
  - Repeated detach and re-entry cycles preserve invocation continuity for read and follow-up prompt flows.
- **Non-goals**:
  - No integrated checkpoint restore + restart flow.
  - No interactive history selector UX.

### PR-03: Canonical Agent Restart-From-Checkpoint Flow
- **Goal**: deliver one invocation-scoped `agent` restart path that executes checkpoint restore and headless runner restart as one contract.
- **Dependencies**: PR-02.
- **Acceptance**:
  - Explicit checkpoint selection executes restore + restart in one command flow with one response contract.
  - Canonical restart behavior is invocation-scoped under `agent` and does not require manual multi-command sequencing.
  - Restart execution preserves invocation continuity state for subsequent timeline reads and follow-up prompts.
  - Restarted headless process launch uses deterministic non-interactive defaults (no interactive stdin expectations and required automation-safe environment defaults).
- **Non-goals**:
  - No interactive history selector path.
  - No turn-aware diff or checks-first read surface delivery.

### PR-04: Interactive History Selector + Deterministic Mapping
- **Goal**: provide arrow-key terminal history selection that deterministically resolves to checkpoint restore + restart outcomes.
- **Dependencies**: PR-03.
- **Acceptance**:
  - Interactive history selection supports arrow-key navigation over invocation timeline/checkpoint history in terminal mode.
  - Selected history points map to checkpoint decisions via deterministic and auditable rules.
  - Selector-driven restore invokes the same canonical restart contract as explicit checkpoint selection.
  - Scripted/non-interactive usage remains supported through explicit checkpoint flags and machine-readable output.
- **Non-goals**:
  - No full-screen watch shell or multi-pane TUI chrome.
  - No review/PR/merge command-family expansion.

### PR-05: Turn-Aware Diff + Checks-First Readiness Surface
- **Goal**: expose turn-anchored diff context and a checks-focused terminal readiness surface using the S3 timeline identity model.
- **Dependencies**: PR-03.
- **Acceptance**:
  - Turn identifiers are durable join keys between timeline entries and diff context requests.
  - Diff context queries for selected turn or turn range resolve deterministically and remain stable across detach/re-entry.
  - A checks-first terminal surface reports readiness state, blocking reasons, and invocation-linked navigation context in human and `--json` modes.
  - Checks/readiness behavior is terminal-first and scriptable without requiring full workspace UI chrome.
- **Non-goals**:
  - No invocation-scoped PR/review/merge mutation family (S5 scope).
  - No broad mutation-command `--json` parity outside S3 chat/restart/checks surfaces.

## 5. L3 hardening checks

1. Ownership completeness: C1-C6 each have exactly one owning PR.
2. Ordering correctness: no PR depends on behavior from an unmerged PR; timeline/order contracts land before follow-up/restart/diff/checks consumers.
3. Acceptance completeness: every S3 L2 acceptance scenario has one primary owner.
4. Scope purity: roadmap content avoids file paths, function signatures, and test-case detail.

## 6. Deferred hardening PR candidates (post-core S3)

### PR-06: Unified Invocation Event Writer + Cross-Surface Sequencing
- **Goal**: consolidate invocation event appends under one daemon-owned writer contract so event sequencing remains deterministic when multiple mutation surfaces write concurrently.
- **Dependencies**: PR-03.
- **Acceptance**:
  - Follow-up prompts, checkpoint engine events, checkpoint-apply events, and landing/discard lifecycle events all use one invocation-event append path.
  - Sequence allocation is invocation-scoped and monotonic under concurrent writes from different producers.
  - Retry/idempotency semantics remain deterministic for follow-up prompt writes (`client_request_id`) after writer unification.
  - Event append failure behavior is explicit and consistent across all producers (no silent best-effort divergence by surface).
- **Non-goals**:
  - No new user-facing CLI command family.
  - No change to timeline entry taxonomy or cursor contract semantics.

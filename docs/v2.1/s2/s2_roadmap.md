# Slice S2 - PR Roadmap

Last updated: 2026-02-25
Status: draft
Upstream spec: `docs/v2.1/s2/s2_spec.md`

## 0. Contract inventory

| cluster id | l2 cluster (normative surface) |
|---|---|
| C1 | Daemon read API contract hardening (`Domain Models`: `DaemonReadEnvelope`, `ListQuery`, `InvalidQueryArgumentDetails`; `API Contracts`: `GET /worktrees`, `GET /worktrees/{ref}`, `GET /invocations`, `GET /invocations/{ref}` envelope/error/pagination contract alignment with strict list-filter validation) |
| C2 | Shared CLI read-routing and navigation selection kernel (`Domain Models`: `NavigationSelection`, `NavigationCommandIntent`; `State Machines`: CLI Read Routing Lifecycle, Navigation Selection Lifecycle; `API Contracts`: CLI Navigation Resolution Contract; shared ambiguity/daemon-unavailable error behavior) |
| C3 | Worktree command-surface convergence (`ReadSurface` worktree rows; daemon-first `worktree ls/show/path/open/shell` behavior; worktree-target dispatch adoption of the shared kernel; worktree-side scriptable selection identity rendering) |
| C4 | Canonical agent read + invocation navigation convergence (`ReadSurface` agent rows; daemon-first `agent ls/show/path/open/shell/enter`; canonical invocation navigation dispatch via shared kernel) |
| C5 | Compatibility adapter convergence + command-policy enforcement (`Domain Models`: `InvocationNavigationVerbPolicy`, `AliasRule`; `API Contracts`: CLI Invocation Navigation Command Policy compatibility constraints for `agent attach` and legacy top-level `path/open/attach/resume`, including documented compatibility dispatch behavior for legacy `resume`; deprecation-safe deterministic exit behavior) |

## 1. Dependency graph

```text
PR-01 Daemon Read API Contract Hardening
  |
  v
PR-02 Shared CLI Navigation Resolution Kernel
  | \
  |  \
  v   v
PR-03 Worktree Read + Navigation Convergence   PR-04 Canonical Agent Read + Invocation Navigation Convergence
  \                                           /
   \                                         /
    v                                       v
      PR-05 Compatibility Adapters + Command-Policy Enforcement
```

## 2. Ownership matrix

| contract cluster (from l2) | owning pr |
|---|---|
| C1: Daemon read envelope + list/get endpoint contract alignment for S2 read surfaces, including strict enum validation (`state`, `mode`), structured invalid-argument details, and stable pagination bounds/cursor semantics (`Domain Models`, `API Contracts`, `Error Codes`) | PR-01 |
| C2: Shared CLI read-routing lifecycle, bootstrap-only fallback boundary, daemon-first navigation selection/resolution lifecycle, and cross-surface ambiguity/daemon-unavailable/TTY preflight behavior (`State Machines`, `CLI Navigation Resolution Contract`, `Invariants`) | PR-02 |
| C3: Worktree command-family convergence onto daemon-first reads/navigation (`worktree ls/show/path/open/shell`) using the shared kernel without post-daemon local store re-resolution (`ReadSurface`, `Invariants`, worktree-related acceptance scenarios) | PR-03 |
| C4: Canonical `agent` read + invocation navigation convergence (`agent ls/show/path/open/shell/enter`) using daemon-first reads/navigation and canonical command-family semantics (`ReadSurface`, `CLI Invocation Navigation Command Policy`, `Invariants`) | PR-04 |
| C5: Compatibility adapter behavior and deprecation-safe command-policy enforcement for `agent attach` and legacy top-level `path/open/attach/resume`, routing target resolution through canonical/shared daemon-first resolution where applicable while preserving documented compatibility dispatch behavior and deterministic exits (`InvocationNavigationVerbPolicy`, `AliasRule`, `CLI Invocation Navigation Command Policy`, `Invariants`) | PR-05 |

## 3. Acceptance coverage map

| l2 acceptance scenario | primary owner pr | supporting pr(s) |
|---|---|---|
| scenario: daemon-of-record invocation list | PR-04 | PR-01 |
| scenario: daemon-of-record invocation show | PR-04 | PR-01 |
| scenario: daemon-of-record worktree list and show | PR-03 | PR-01 |
| scenario: worktree path/open/shell use daemon path resolution | PR-03 | PR-02 |
| scenario: invocation navigation open uses daemon invocation resolution | PR-04 | PR-02 |
| scenario: canonical invocation navigation under agent family | PR-04 | PR-02 |
| scenario: compatibility alias delegates to canonical path | PR-05 | PR-02, PR-03, PR-04 |
| scenario: fleet filter on worktree ref remains deterministic | PR-01 | PR-04 |
| scenario: invalid list filter is rejected | PR-01 | none |
| scenario: ambiguous selection fails with candidates | PR-02 | PR-03, PR-04 |
| scenario: daemon unavailable outside bootstrap boundary | PR-02 | PR-03, PR-04 |
| scenario: interactive navigation enforces tty preflight | PR-02 | PR-04, PR-05 |
| scenario: list outputs remain scriptable at fleet scale | PR-02 | PR-01, PR-03, PR-04 |

## 4. PRs

### PR-01: Daemon Read API Contract Hardening
- **Goal**: align S2 daemon read endpoints with the L2 read contract so list/show consumers receive deterministic envelopes, list-filter validation, and stable pagination/error behavior.
- **Dependencies**: none.
- **Acceptance**:
  - S2 daemon read endpoints used by `agent`/`worktree` surfaces conform to the documented read envelope and endpoint error semantics.
  - List endpoints fail closed on unsupported `state`/`mode` values with `E_INVALID_ARGUMENT` and structured details (`param`, `value`, `allowed_values`).
  - List pagination defaults and limits remain stable (`100` default, `500` max) with opaque cursor continuity preserved.
  - Worktree-ref filtering for invocation lists remains deterministic (filtered results or empty results, no silent widening).
- **Non-goals**:
  - No CLI command-surface migration onto daemon-first navigation resolution.
  - No compatibility alias/deprecation behavior changes.

### PR-02: Shared CLI Navigation Resolution Kernel
- **Goal**: establish one shared daemon-first CLI read-routing and target-resolution kernel that all S2 navigation and single-target read commands consume.
- **Dependencies**: PR-01.
- **Acceptance**:
  - CLI read routing enforces the S2 lifecycle ordering (repo resolution -> daemon/read routing -> render/dispatch) with bootstrap-only fallback boundaries and explicit repo-context failure semantics (no local-scan bypass).
  - Navigation selection/resolution behavior is shared across worktree and invocation flows, including explicit ambiguous-target failure with candidate preservation.
  - Daemon-unavailable and daemon-incompatible failures outside bootstrap/health boundaries fail explicitly without local store discovery fallback.
  - Interactive attach/enter navigation performs TTY preflight before dispatch, and cross-surface selection identity remains stable for script-driven follow-on navigation.
  - S2 `select` acceptance is satisfied by deterministic list-row/script-driven selection inputs that feed canonical path/open/shell/enter flows (no dedicated `select` verb required in S2).
- **Non-goals**:
  - No command-family-specific UX migration for `worktree` or canonical `agent` verbs.
  - No compatibility alias surface rollout (`agent attach`, legacy top-level adapters).

### PR-03: Worktree Read + Navigation Convergence
- **Goal**: converge the `worktree` command family onto daemon-first reads and shared navigation resolution without post-daemon local re-resolution.
- **Dependencies**: PR-01, PR-02.
- **Acceptance**:
  - `worktree ls` and `worktree show` satisfy S2 daemon-of-record read behavior and render daemon-owned fields.
  - `worktree path`, `worktree open`, and `worktree shell` resolve authoritative `tree_path` via daemon-first shared resolution before local dispatch.
  - Worktree command behavior preserves deterministic target identity/output needed for script-driven selection and follow-on commands.
- **Non-goals**:
  - No canonical `agent` invocation navigation rollout.
  - No legacy top-level alias compatibility rewiring.

### PR-04: Canonical Agent Read + Invocation Navigation Convergence
- **Goal**: converge canonical `agency agent` read and invocation navigation surfaces onto daemon-first reads and the shared navigation kernel.
- **Dependencies**: PR-01, PR-02.
- **Acceptance**:
  - `agent ls` and `agent show` satisfy S2 daemon-of-record read behavior and preserve daemon-owned rendering/scriptability expectations.
  - Canonical `agent path`, `agent open`, `agent shell`, and `agent enter` resolve invocation identity/path through the shared daemon-first navigation contract before local dispatch.
  - Canonical `agent` invocation navigation behavior aligns with S2 command-family policy while preserving deterministic target selection semantics for fleet workflows.
  - Canonical invocation navigation enforces invocation-mode validity for interactive actions and preserves S2 invalid-mode error semantics (`E_INVOCATION_INVALID_MODE`) for unsupported mode/action combinations.
- **Non-goals**:
  - No compatibility alias rollout for `agent attach` or legacy top-level `path/open/attach/resume`.
  - No canonical `agent restart` semantic assignment (reserved for S3).

### PR-05: Compatibility Adapters + Command-Policy Enforcement
- **Goal**: preserve v2.1 compatibility command surfaces while enforcing S2 command-policy boundaries and routing compatibility target resolution through canonical/shared daemon-first resolution where applicable.
- **Dependencies**: PR-03, PR-04.
- **Acceptance**:
  - `agent attach` and legacy top-level `path`, `open`, `attach`, and `resume` use shared daemon-first target resolution where applicable, while preserving explicitly documented compatibility dispatch behavior (including legacy `resume` compatibility semantics).
  - Compatibility adapters preserve deterministic exit behavior and target-resolution determinism while allowing non-breaking deprecation messaging.
  - Compatibility restart behavior remains explicitly compatibility-scoped/headed-only and does not assign conflicting canonical `agent restart` semantics before S3.
- **Non-goals**:
  - No checkpoint-aware restart semantics or chat continuation behavior (S3 scope).
  - No new canonical command-family expansion beyond S2 policy.

## 5. L3 hardening checks

1. Ownership completeness: C1-C5 each have exactly one owning PR.
2. Ordering correctness: no PR depends on behavior from an unmerged PR; shared kernel lands before worktree/agent convergence and compatibility adapters.
3. Acceptance completeness: every S2 L2 acceptance scenario appears in the coverage map with one primary owner.
4. Scope purity: roadmap content avoids file paths, function signatures, and test-case detail.

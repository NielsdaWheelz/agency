# S2 Daemon Read Convergence + Sandbox Navigation Roadmap - Worklog

Last updated: 2026-02-25
Status: draft

## Evidence log

| Date | Source | Evidence | Relevance |
|---|---|---|---|
| 2026-02-25 | `docs/sdlc/L3-pr-roadmap.md:26` | L3 requires extracting contract clusters from L2 before PR entries. | Drives contract-inventory-first drafting sequence. |
| 2026-02-25 | `docs/sdlc/L3-pr-roadmap.md:41` | One cluster must map to exactly one owner PR. | Enforces non-overlapping S2 PR boundaries. |
| 2026-02-25 | `docs/sdlc/L3-pr-roadmap.md:47` | Dependency graph must respect data-first and invariant-first ordering. | Constrains sequencing of daemon contract hardening vs command migration PRs. |
| 2026-02-25 | `docs/sdlc/L3-pr-roadmap.md:64` | Every L2 acceptance scenario must be covered in the L3 roadmap. | Requires full S2 acceptance coverage map. |
| 2026-02-25 | `docs/v2.1/slice-roadmap.md:48` | S2 goal is daemon read convergence + detached/fleet navigation basics. | Defines slice decomposition scope. |
| 2026-02-25 | `docs/v2.1/slice-roadmap.md:52` | S2 acceptance requires daemon-backed `agent`/`worktree` reads and direct path/shell/open/select navigation flows. | Anchors roadmap acceptance coverage. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:27` | L2 domain models split read DTO/query surfaces from navigation selection and alias-policy surfaces. | Supports separate daemon-contract vs navigation-kernel vs compatibility clusters. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:169` | L2 defines shared CLI read-routing and selection state machines. | Indicates a cross-command kernel seam that may deserve its own PR. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:236` | L2 locks daemon list/get API contracts and strict invalid-filter semantics. | Supports early daemon contract hardening PR. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:395` | L2 defines a shared CLI navigation resolution contract used by both worktree and agent surfaces. | Creates ownership-boundary decision for L3 split vs bundling. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:429` | L2 locks canonical `agent` invocation navigation family and compatibility alias policy. | Constrains sequencing of canonical agent verbs before compatibility adapters. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:469` | L2 invariants prohibit post-daemon local store scans and require bootstrap-only fallback. | Requires explicit ownership of invariant enforcement in roadmap clusters. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:486` | L2 lists 13 acceptance scenarios spanning daemon reads, navigation, alias compatibility, errors, TTY, and scriptability. | Defines coverage-map rows. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec_decisions.md:10` | D-001 fixes command-surface policy (canonical `agent`, aliases remain compatibility). | Prevents roadmap from reopening command namespace decisions. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec_decisions.md:15` | D-006 fixes strict enum validation (`E_INVALID_ARGUMENT`) for `state`/`mode`. | Forces daemon filter validation hardening into S2 roadmap. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec_worklog.md:103` | Current daemon list handlers still coerce unknown `state`/`mode`. | Identifies concrete implementation drift for a daemon-contract PR. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec_worklog.md:104` | `worktree open/shell` and `agent attach/open` still re-resolve via local store after daemon repo resolution. | Identifies command-migration gaps for navigation convergence PRs. |

## Current decomposition notes

- Draft contract inventory for S2 L3 is staged in `docs/v2.1/s2/s2_roadmap.md`.
- D-001 resolved: isolate the shared CLI navigation kernel as PR-02 before command-family convergence PRs to avoid ownership overlap and hidden dependencies.
- Current draft decomposition uses five PRs: daemon read API hardening, shared CLI navigation kernel, worktree convergence, canonical agent convergence, and compatibility adapters/policy enforcement.

## Hardening pass notes

1. Ownership completeness: C1-C5 each map to exactly one PR owner in `s2_roadmap.md`.
2. Ordering correctness: PR-01 (daemon contract hardening) precedes PR-02 (shared kernel); PR-03/PR-04 depend on PR-02; PR-05 depends on PR-03 and PR-04 to avoid alias-before-canonical rollout.
3. Acceptance completeness: all 13 S2 L2 acceptance scenarios appear in the coverage map with one primary owner and explicit supports where needed.
4. Scope purity: roadmap content remains at contract/ownership granularity (no file paths, signatures, or test names).

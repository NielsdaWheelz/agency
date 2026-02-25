# pr-02 worklog: shared cli navigation resolution kernel

Last updated: 2026-02-25
Status: draft
Upstream l2: `docs/v2.1/s2/s2_spec.md`
Upstream l3: `docs/v2.1/s2/s2_roadmap.md` (PR-02)

## purpose
- capture code-fact evidence used to scope PR-02 L4.
- record drift relevant to PR-02 owned contract cluster (`C2`).
- log hardening pass outcomes and any cross-PR boundary cleanup.

## evidence log

| date | source | finding | relevance |
|---|---|---|---|
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap.md:76` | PR-02 is the shared CLI navigation resolution kernel. | Primary L3 PR ownership scope. |
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap_ownership.md:11` | PR-02 owns shared cross-surface routing/selection/fallback/TTY semantics only. | Boundary guard against PR-03/PR-04/PR-05 scope smuggling. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:169` | CLI Read Routing Lifecycle defines legal/illegal transitions and bootstrap-only fallback guard. | PR-02 state-machine contract basis. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:203` | Navigation Selection Lifecycle defines select/resolve/dispatch/error guards including ambiguity candidate preservation and TTY preflight. | PR-02 state-machine contract basis. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:395` | CLI Navigation Resolution Contract requires explicit daemon and ambiguity error semantics. | PR-02 API contract basis. |
| 2026-02-25 | `internal/commands/repo.go:49` | `ResolveRepoViaClient` already centralizes daemon-aware repo context resolution. | PR-02 should reuse for `resolve_repo_context` state rather than duplicate. |
| 2026-02-25 | `internal/commands/worktree.go:426` | `worktree open` resolves repo via daemon, then re-resolves target via local store service. | Known S2 convergence drift; PR-02 defines shared replacement kernel used by PR-03. |
| 2026-02-25 | `internal/commands/worktree.go:511` | `worktree shell` follows same local re-resolution pattern after daemon repo resolution. | Same PR-02/PR-03 seam. |
| 2026-02-25 | `internal/commands/agent.go:608` | `agent attach` resolves repo via daemon, then re-resolves invocation via local store service. | Same PR-02/PR-04/PR-05 seam. |
| 2026-02-25 | `internal/commands/agent.go:1109` | `agent open` resolves repo via daemon, then re-resolves invocation via local store service. | Same PR-02/PR-04 seam. |
| 2026-02-25 | `internal/commands/agent.go:562` | `agent attach` performs command-local TTY preflight (`E_NOT_INTERACTIVE`) before dispatch. | PR-02 should centralize TTY preflight semantics in shared kernel. |
| 2026-02-25 | `internal/daemon/read_types.go:21` | daemon read `APIResponse` supports arbitrary `details` payloads (`interface{}`). | Daemon can already transport ambiguity candidate arrays. |
| 2026-02-25 | `internal/daemon/read_types.go:24` | `AmbiguousDetails` provides structured candidate list shape. | PR-02 ambiguity candidate preservation depends on client transport. |
| 2026-02-25 | `internal/daemonclient/client.go:624` | `GetWorktree` converts daemon API error to code+message only; drops `hint`/`details`. | Blocks straightforward PR-02 candidate preservation without extra transport. |
| 2026-02-25 | `internal/daemonclient/client.go:750` | `GetInvocation` has same error detail loss behavior. | Same PR-02 blocker. |
| 2026-02-25 | `internal/errors/errors.go:216` | `AgencyError.Details` is `map[string]string`, not generic/typed JSON. | Constrains direct passthrough of daemon `details.candidates` arrays. |

## drift notes (PR-02 relevant)
- Shared navigation semantics are duplicated/inconsistent across command handlers today (repo resolution shared, target resolution and TTY preflight not shared).
- Daemon read ambiguity details exist but are not preserved through `daemonclient` read methods consumed by CLI code.

## open decisions encountered
- none (all material PR-02 drafting decisions resolved before L4 freeze pass).

## hardening pass status
- skeleton pass: complete (L4 sections created).
- acceptance-cluster micro-loop: complete (D-001 through D-006 resolved; fallback-boundary positive-path test exactness finalized).
- boundary cleanup: complete (PR-03/PR-04/PR-05 surfaces remain explicit non-goals).
- traceability completeness: complete (all PR-02 L3 acceptance bullets mapped to deliverables/tests).
- open-questions/defaults freeze check: complete (table cleared; no temporary defaults remain).

## decisions resolved during drafting
- D-001 resolved: PR-02 will use a narrow, typed daemonclient read-API error passthrough path for kernel consumers to preserve daemon `hint` and structured `details` (including ambiguity candidates) without broadening `AgencyError` or changing unrelated daemonclient call-site behavior.
- D-002 resolved: PR-02 navigation-resolution ambiguity failures normalize to `E_AMBIGUOUS`; direct daemon read endpoint ambiguity codes remain entity-specific unless a consumer explicitly opts into the navigation-kernel contract.
- D-003 resolved: PR-02 machine/script selection uses a structured repo-scoped selector contract at the kernel boundary (and daemon DTO IDs from JSON/list-row sources), with no new opaque machine token grammar introduced in S2.
- D-004 resolved: PR-02 shared TTY preflight must return `E_NOT_INTERACTIVE` with a non-empty generic recovery hint; downstream command surfaces may override/append wording without weakening kernel semantics.
- D-005 resolved: PR-02 encodes bootstrap-only fallback eligibility as an explicit kernel policy/input (fallback disabled by default for normal navigation/read intents), with explicit fallback callback injection required for boundary-eligible routing.
- D-006 resolved: PR-02 includes one kernel-level positive fallback-boundary test using a synthetic boundary-eligible routing policy/input and injected fallback callback, without pulling bootstrap/health command migrations into PR-02.

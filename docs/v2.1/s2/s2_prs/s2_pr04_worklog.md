# pr-04 worklog: canonical agent read + invocation navigation convergence

Last updated: 2026-02-25
Status: draft
Upstream l2: `docs/v2.1/s2/s2_spec.md`
Upstream l3: `docs/v2.1/s2/s2_roadmap.md` (PR-04)

## purpose
- capture code-fact evidence used to scope PR-04 L4.
- record current canonical `agent` convergence drift relative to PR-04-owned cluster `C4`.
- log drafting progress, boundary checks, and unresolved PR-04 decisions.

## evidence log

| date | source | finding | relevance |
|---|---|---|---|
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap.md:100` | PR-04 is the canonical `agent` read + invocation navigation convergence PR. | Primary L3 PR ownership scope. |
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap_ownership.md:13` | PR-04 owns canonical `agent` surface adoption only; no compatibility alias rollout. | Boundary guard against PR-05 scope smuggling. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:429` | Canonical S2 invocation navigation verbs are `agent path/open/shell/enter`; `agent attach` remains a compatibility alias. | PR-04 command-surface policy basis. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:488`, `docs/v2.1/s2/s2_spec.md:493` | `agent ls/show` must be daemon-of-record reads in S2. | PR-04 primary acceptance basis (read side). |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:508`, `docs/v2.1/s2/s2_spec.md:513` | `agent open` and canonical `agent path/open/shell/enter` must use daemon-first invocation resolution before dispatch. | PR-04 primary acceptance basis (navigation side). |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:476` | Ambiguous target resolution must preserve candidate information when daemon provides candidates. | Drives D-001 on `agent show` rich read path. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:477` | Interactive attach/enter flows must perform TTY preflight before tmux attach dispatch. | PR-04 must consume PR-02 TTY semantics on canonical `agent enter`. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:479` | S2 CLI handlers must not scan local store filesystem for read/navigation target resolution. | PR-04 must remove local invocation re-resolution for canonical navigation surfaces. |
| 2026-02-25 | `internal/commands/agent.go:314` | `AgentLS` already uses daemon invocation list endpoint and daemon DTO rendering. | PR-04 read-side baseline; mostly regression coverage and scriptability assertions. |
| 2026-02-25 | `internal/commands/agent.go:452` and `internal/commands/agent.go:481` | `AgentShow` already uses daemon get endpoint, but via non-rich `GetInvocation`. | Direct D-001 seam for ambiguity candidate preservation. |
| 2026-02-25 | `internal/commands/agent.go:556` and `internal/commands/agent.go:608` | `AgentAttach` still resolves invocation locally via `invocation.NewService(...).Resolve(...)` after daemon repo resolution. | Current compatibility-surface implementation; PR-04 must avoid alias rollout but can reuse logic via internal helper extraction if neutral. |
| 2026-02-25 | `internal/commands/agent.go:1080` and `internal/commands/agent.go:1109` | `AgentOpen` still re-resolves invocation locally after daemon repo resolution. | Direct PR-04 drift to close on canonical navigation surface. |
| 2026-02-25 | `internal/commands/agent.go:628` | `AgentAttach` enforces invalid-mode (`headless` -> `E_INVOCATION_INVALID_MODE`) via local meta. | PR-04 must preserve semantics for canonical `agent enter` while moving to daemon-first resolution. |
| 2026-02-25 | `internal/commands/agent.go:556` and `internal/commands/error_codes_test.go:24` | `AgentAttach` has command-local TTY preflight and error-code coverage. | PR-04 can reuse seams for canonical `agent enter` tests without re-testing PR-02 internals. |
| 2026-02-25 | `internal/daemonclient/client.go:825` | `GetInvocation` drops daemon read error `hint/details`. | Candidate detail loss risk for ambiguous `agent show`. |
| 2026-02-25 | `internal/daemonclient/client.go:870` | `GetInvocationRich` preserves daemon read error `hint/details`. | Candidate-preserving direct-read option for PR-04 `agent show`. |
| 2026-02-25 | `internal/commands/navigation_kernel.go:161` | PR-02 shared kernel enforces daemon-first routing and no-local-discovery target resolution. | PR-04 must consume this seam for canonical invocation navigation. |
| 2026-02-25 | `internal/commands/navigation_kernel.go:235` | PR-02 kernel normalizes invocation/worktree navigation ambiguity to `E_AMBIGUOUS`. | PR-04 navigation ambiguity code behavior must align with kernel semantics. |
| 2026-02-25 | `internal/cli/cobra/agent.go:46`-`internal/cli/cobra/agent.go:57` | Current `agent` cobra command lacks canonical `path`, `shell`, and `enter` subcommands. | PR-04 must add canonical subcommands under `agent` family. |
| 2026-02-25 | `internal/commands/worktree.go:354` and `internal/commands/worktree.go:521` | PR-03 worktree convergence now uses PR-02 kernel with local `buildNavDeps` helper pattern. | Reusable implementation pattern for PR-04 canonical agent navigation wiring. |
| 2026-02-25 | `internal/commands/worktree_test.go:3`, `internal/commands/worktree_test.go:128` | PR-03 established daemon-backed env/shim test pattern for dispatch/cwd assertions. | Reusable test strategy for PR-04 `agent open/shell` dispatch assertions. |
| 2026-02-25 | `internal/daemon/read_types.go:61`, `internal/daemon/read_types.go:67`, `internal/daemon/read_types.go:91` | Invocation DTO includes `invocation_id`, `mode`, and `sandbox_path` for daemon-first navigation. | Supports canonical `agent path/open/shell/enter` resolution without local target discovery for identity/path. |
| 2026-02-25 | `internal/daemon/read_types.go:61`-`internal/daemon/read_types.go:93` | Invocation DTO does not expose `tmux_session`. | PR-04 D-002 decision seam for canonical `agent enter` tmux session sourcing. |
| 2026-02-25 | `internal/commands/agent.go:641`, `internal/commands/agent.go:645` | Existing `AgentAttach` computes `tmux.SessionName(invocationID)` as fallback when `Meta.TmuxSession` is empty. | Evidence that deterministic tmux session derivation is an established code path. |
| 2026-02-25 | `internal/tmux/capture.go:93` | `tmux.SessionName(runID)` defines canonical session naming (`agency_<run_id>`). | Supports daemon-first `agent enter` session derivation without local meta reads. |
| 2026-02-25 | `internal/commands/agent.go:671`, `internal/commands/agent.go:676` | `AgentAttach` uses direct `realTmuxAttach` for attach dispatch (stdio-coupled), bypassing `tmux.Client.Attach`. | PR-04 D-003 seam: canonical `agent enter` needs testable attach dispatch without losing real interactive behavior. |
| 2026-02-25 | `internal/tmux/client_exec.go:72`, `internal/exec/runner.go:113`, `internal/exec/runner.go:129` | `tmux.ExecClient.Attach` uses `exec.CommandRunner`, which is non-interactive/captured I/O. | Confirms `tmuxClient.Attach` is not a safe production replacement for real interactive attach semantics in PR-04. |
| 2026-02-25 | `internal/testutil/fake_tmux.go:55`, `internal/testutil/fake_tmux.go:97` | `FakeTmuxClient` records `AttachCalls`. | Supports a narrow option-scoped attach-dispatch seam for canonical `agent enter` tests. |
| 2026-02-25 | `internal/commands/agent.go:1109`, `internal/commands/agent.go:1119`, `internal/commands/agent.go:1130` | `AgentOpen` currently returns `E_INVOCATION_BROKEN` from local invocation meta resolution and `E_SANDBOX_MISSING` from local sandbox existence checks. | PR-04 D-004 seam for canonical navigation target-resolution vs local runtime error semantics. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:421`, `docs/v2.1/s2/s2_spec.md:427` | L2 invocation navigation contract error set does not include `E_INVOCATION_BROKEN`. | Supports D-004 recommendation to remove local broken-target semantics from canonical navigation target resolution. |
| 2026-02-25 | `internal/commands/agent.go:1130` | `AgentOpen` has an existing explicit `E_SANDBOX_MISSING` runtime semantic. | PR-04 D-005 seam: preserve and daemonize this runtime behavior (and decide canonical `agent shell` parity). |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:508`, `docs/v2.1/s2/s2_spec.md:511` | S2 acceptance requires daemon-sourced `sandbox_path` before local editor execution. | Supports explicit post-resolution runtime checks using daemon-resolved path data. |
| 2026-02-25 | `internal/commands/navigation_kernel.go:234`, `docs/v2.1/s2/s2_prs/s2_pr02.md:85` | PR-02 kernel normalizes navigation ambiguity to `E_AMBIGUOUS` with candidate preservation. | PR-04 D-006 seam: canonical `agent` navigation ambiguity code behavior after kernel adoption. |
| 2026-02-25 | `internal/commands/agent.go:1109`, `internal/invocation/service.go:427` | Current canonical-adjacent `AgentOpen` can surface `E_INVOCATION_ID_AMBIGUOUS` via local invocation resolution. | Confirms D-006 is a script-visible behavior change on canonical `agent open` migration. |
| 2026-02-25 | `docs/v2.1/s2/s2_prs/s2_pr04.md:184`, `docs/v2.1/s2/s2_prs/s2_pr04.md:190`, `docs/v2.1/s2/s2_prs/s2_pr04.md:197`, `docs/v2.1/s2/s2_prs/s2_pr04.md:230` | Draft currently includes ambiguity regressions for `agent path/open/enter` and shell-specific non-ambiguity coverage (`no-local-resolve`, sandbox-missing) but no `agent shell` ambiguity test. | PR-04 D-007 seam: explicit test-scope decision needed for ambiguity coverage breadth after D-006. |
| 2026-02-25 | `docs/v2.1/s2/s2_prs/s2_pr03_decisions.md:183` | PR-03 used representative ambiguity surface regressions (non-dispatch + one dispatch) rather than duplicating every migrated surface. | Precedent for a disciplined PR-04 ambiguity test-scope decision. |

## drift notes (PR-04 relevant)
- `agent ls` already meets most daemon-of-record read behavior and daemon DTO rendering expectations.
- `agent show` already reads from daemon but may not preserve ambiguity candidate details because it uses non-rich daemonclient read path.
- canonical `agent path`, `agent shell`, and `agent enter` command surfaces do not exist yet.
- canonical `agent open` still violates S2 no-post-daemon-local-resolution invariant by re-resolving invocation targets via local store service.
- `agent attach` contains compatibility behavior and local target-resolution logic; PR-04 must avoid compatibility rollout while implementing canonical `agent enter`.

## open decisions encountered
- none currently open.

## hardening pass status
- skeleton pass: complete (L4 sections created in `s2_pr04.md`).
- acceptance-cluster micro-loop: complete (cluster 1 `agent ls/show` read behavior updated; cluster 2 `agent enter` session/dispatch seams D-002/D-003 resolved; cluster 2/3 canonical navigation error semantics D-004 resolved; cluster 2/3 runtime sandbox-missing behavior D-005 resolved; cluster 2/3 navigation ambiguity code split D-006 resolved; ambiguity surface-test breadth decision D-007 resolved).
- boundary cleanup: complete (final review pass completed; no scope leaks or unresolved placeholders remain).
- traceability completeness: complete for all PR-04 L3 bullets; decision-driven test refinements incorporated through D-007.
- open-questions/defaults freeze check: complete (no open questions/defaults remain).

## decisions resolved during drafting
- D-001 resolved: PR-04 will adopt `daemonclient.GetInvocationRich` for `agent show` so ambiguous single-target direct-read failures preserve daemon-provided candidate details while retaining direct read endpoint ambiguity semantics (`E_INVOCATION_ID_AMBIGUOUS`).
- D-002 resolved: PR-04 canonical `agent enter` will derive tmux session name deterministically from daemon-resolved `invocation_id` via `tmux.SessionName(invocationID)` rather than reintroducing local invocation target discovery.
- D-003 resolved: PR-04 canonical `agent enter` will use a narrow option/helper-scoped attach-dispatch seam (defaulting to `realTmuxAttach`) so tests can assert positive dispatch without changing production interactive attach semantics.
- D-004 resolved: PR-04 canonical `agent path/open/shell/enter` will drop local `E_INVOCATION_BROKEN` target-resolution branches and align target-resolution errors to daemon-first navigation/read semantics, while allowing local runtime checks on daemon-resolved data (for example `E_SANDBOX_MISSING`, tmux runtime/session errors).
- D-005 resolved: PR-04 will preserve explicit local runtime `E_SANDBOX_MISSING` on canonical `agent open` and `agent shell` using daemon-resolved `sandbox_path`; canonical `agent path` remains a pure path-printing surface without local existence gating.
- D-006 resolved: PR-04 canonical `agent path/open/shell/enter` will surface navigation ambiguity as PR-02 kernel `E_AMBIGUOUS` (with machine-readable candidate preservation) and will not translate back to `E_INVOCATION_ID_AMBIGUOUS`; direct `agent show` remains entity-specific per D-001.
- D-007 resolved: PR-04 will require exactly three ambiguity surface regressions (`agent path`, `agent open`, `agent enter`) and will omit a dedicated `agent shell` ambiguity/no-dispatch regression unless implementation reveals a distinct shell ambiguity path.

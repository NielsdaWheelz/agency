# v2.1 Conductor Parity Matrix

Last updated: 2026-02-16
Status: active

Legend:
- `current`: `done`, `partial`, `missing`
- `target`: `required-v2.1`, `stretch-v2.1`, `deferred-post-v2.1`

| Capability | Conductor signal | Agency current | v2.1 target | Notes |
|---|---|---|---|---|
| Isolated workspace/branch per unit of work | workflow + first workspace docs | done | required-v2.1 | Already core to integration worktree + sandbox model. |
| Detached headless execution with full raw/semantic logs | workflow + checkpoints docs | partial | required-v2.1 | `agent logs` exists; chat continuity and restart flow missing. |
| Continue conversation after detach (send follow-up prompt) | slash commands + workflow | missing | required-v2.1 | Requires chat write endpoint + CLI command family. |
| Restart from checkpoint in one flow | checkpoints docs | partial | required-v2.1 | `checkpoint apply` exists, but integrated restart UX is missing. |
| Turn-aware checkpoint semantics (code + conversation) | checkpoints docs | missing | stretch-v2.1 | v2.1 minimum can ship checkpoint+restart with explicit conversation limits. |
| Invocation-centric review surface | diff viewer docs + changelog review features | partial | required-v2.1 | `agent diff` exists; no explicit `agent review` contract yet. |
| Turn-to-diff mapping | changelog: chat-aware diffing | missing | stretch-v2.1 | Requires durable conversation turn IDs and diff linkage. |
| Checks-first merge readiness surface | todos docs + changelog checks tab | missing | stretch-v2.1 | Seed can be CLI/minimal watch; full visual center deferred. |
| Invocation-scoped PR actions | workflow + faq prompt actions | missing | required-v2.1 | Add `agent pr create/status/open` baseline. |
| Invocation-scoped merge action + status | workflow + changelog merge hardening | missing | required-v2.1 | Add `agent merge` with policy/result contract and JSON mode. |
| Archive with recoverable history context | workflow docs | partial | stretch-v2.1 | Lifecycle exists; chat-aware restore semantics are incomplete. |
| Runner portability beyond claude/codex | n/a (Agency-specific differentiator) | missing | required-v2.1 | Replace allowlists with capability model + fallback parser behavior. |
| Merge gating by todos/policy | todos docs | missing | stretch-v2.1 | v2.1 baseline should support checks/review gates first. |
| Sandbox-first execution model | faq notes unsandboxed model | done | required-v2.1 | Intentional non-parity: keep sandbox safety invariant. |

## Source references

- https://docs.conductor.build/workflow
- https://docs.conductor.build/core/diff-viewer
- https://docs.conductor.build/core/todos
- https://docs.conductor.build/core/checkpoints
- https://docs.conductor.build/core/slash-commands
- https://docs.conductor.build/first-workspace
- https://docs.conductor.build/faq
- https://www.conductor.build/changelog

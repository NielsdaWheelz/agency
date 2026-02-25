# S2 Daemon Read Convergence + Sandbox Navigation Spec - Decisions

Last updated: 2026-02-25
Status: draft

## Decision Ledger

| ID | Question | Decision | Rationale | Fallback/Default | Owner | Due |
|---|---|---|---|---|---|---|
| D-001 | What is the canonical S2 command surface for invocation navigation (`agent path/shell/enter/restart`) versus existing top-level `path/open/resume` and current `agent attach/open`? | Canonicalize invocation navigation under `agency agent` (`path`, `open`, `shell`, `enter`) in S2; keep `agent attach` and legacy top-level `path/open/attach/resume` as compatibility adapters over the same daemon-first resolver; reserve canonical `agent restart` semantics for S3 checkpoint-aware behavior. | Matches product direction (`agent` family) while preventing script breakage and duplicate resolution logic during v2.1 migration. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-002 | What boundary does "daemon-first read architecture" apply to in S2? | Apply the requirement to CLI command-handler entity discovery and read resolution, not daemon internals. | L1 outcome text targets CLI handlers no longer depending on local store scans; daemon may still scan local store as implementation detail while providing canonical APIs. | none | `@nnandal` + `Codex` | fixed in L2 draft |
| D-003 | Should S2 invent new navigation DTOs or reuse current daemon read DTOs for path/open/shell/attach resolution? | Reuse existing `WorktreeDTO` and `InvocationDTO` as the canonical navigation resolution shapes. | They already carry authoritative `tree_path` / `sandbox_path`, status, and display fields; reuse minimizes contract drift and accelerates convergence. | none | `@nnandal` + `Codex` | fixed in L2 draft |
| D-004 | What is the S2 contract for pagination defaults and bounds on fleet list flows? | Preserve current daemon list defaults (`limit=100`) and max (`500`) for both worktree and invocation list endpoints. | Existing daemon and client implementations already share these bounds; making them explicit stabilizes scripting and future UI layers. | none | `@nnandal` + `Codex` | fixed in L2 draft |
| D-005 | How should S2 treat daemon bootstrap/health fallback exceptions? | Allow fallback only at daemon bootstrap/health boundaries and require explicit failure outside that boundary. | Matches product brief scope and prevents silent local-store regressions during normal reads. | none | `@nnandal` + `Codex` | fixed in L2 draft |
| D-006 | Should S2 tighten unknown list filter value handling (`state`, `mode`) now? | Yes. S2 daemon read contracts must reject unknown enum values for `state`/`mode` with `400 E_INVALID_ARGUMENT` and structured details (`param`, `value`, `allowed_values`). | Silent widening/defaulting is unsafe for automation and conflicts with S2 fleet-scale scriptability/determinism goals. | none | `@nnandal` + `Codex` | fixed in L2 |

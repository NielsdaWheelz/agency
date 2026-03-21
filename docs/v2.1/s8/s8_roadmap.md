# Slice S8: Invocation History and Runner Log Convergence - PR Roadmap

Last updated: 2026-03-18
Status: complete (implementation and validation corpus closure complete)
Upstream spec: `docs/v2.1/s8/s8_spec.md`
Supporting context: `docs/v2.1/s8/s8_context.md`
Manual validation follow-up: `docs/v2.1/s8/s8_manual_validation_20260319.md`

Current state: invocation history, restart-from-history, checkpoint listing, turn-aware diff, invocation list/watch, show, review, and raw log reads already exist, but they do not share one durable capture model or one derived turn/activity model. Sandbox-owned raw logs can disappear after cleanup, current Codex and Cursor adapters are behind current runner output, and multiple surfaces derive turn/activity truth independently. This roadmap is intentionally fuller than a normal L3 plan because this slice will span multiple sessions and needs a durable reference plan.

Phase progress snapshot:
- PR-01: done
- PR-02: done
- PR-03: done
- PR-04: done
- PR-05: done
- PR-06: done

Closure gate (satisfied 2026-03-18):
- Completed direct runner scenarios `D01`, `D02`, `D03`, `D04` across Claude/Codex/Cursor.
- Completed agency-managed scenarios `A01`, `A02`, `A03`, `A04` across Claude/Codex/Cursor.
- Capture snapshot and artifact inventory are tracked in `docs/v2.1/s8/s8_fixture_capture_20260312.md`.

Post-slice runner follow-up briefs:
- `docs/v2.1/s8/s8_prs/s8_pr07.md` — Codex manual closure and output fidelity
- `docs/v2.1/s8/s8_prs/s8_pr08.md` — Cursor manual closure and follow-up parity
- `docs/v2.1/s8/s8_prs/s8_pr09.md` — Claude follow-up completion and status correctness

### PR-01: invocation-owned raw capture and replay baseline
- **goal**: make supported runner raw stdout and stderr durable under invocation ownership and establish a fixture capture protocol that future converter work can replay safely.
- **builds on**: S7 merged baseline and current daemon-owned invocation lifecycle flows.
- **acceptance**:
  - landed and discarded Claude, Codex, and Cursor invocations retain their original raw stdout and stderr without depending on a surviving sandbox directory.
  - raw log read behavior continues to work for supported invocations after lifecycle cleanup.
  - preserved raw output is suitable as replay input for future converter updates rather than being coupled to the current parser shape.
  - a documented fixture-capture protocol exists for collecting current real runner outputs across the supported runner set.
- **non-goals**: no broad UI redesign; no cross-surface turn/activity convergence yet.

### PR-02: replayable Claude, Codex, and Cursor converters
- **goal**: update supported runner converters so current outputs normalize into one canonical runner event model and unknown output fails safely.
- **builds on**: PR-01 durable replay inputs and fixture protocol.
- **acceptance**:
  - current Claude, Codex, and Cursor outputs replay into canonical normalized events for messages, prompts, tool activity, tool results, file edits, usage, and errors.
  - unknown, malformed, or oversized runner output is preserved and surfaced with parse or truncation diagnostics instead of silently disappearing or being mislabeled.
  - Codex and Cursor current output shapes that were previously missed by the repo are covered by regression fixtures.
  - converter behavior is runner-specific at the boundary but runner-neutral for downstream consumers.
- **non-goals**: no migration of user-facing surfaces beyond compatibility fixes required to consume the new normalized model.

### PR-03: canonical turns and history or restart convergence
- **goal**: make `agent history` and `agent restart --history` read from the same derived turn model with concise default human rendering.
- **builds on**: PR-02 merged canonical runner event model.
- **acceptance**:
  - `agent history` and `agent restart --history` agree on turn boundaries, assistant or user semantics, checkpoint association, and latest meaningful turn behavior.
  - default human history rendering is concise for large tool payloads while preserving a raw or machine-readable escape hatch.
  - `agent history --last` resolves to the latest meaningful activity or turn rather than an arbitrary low-level timeline entry.
  - misleading prompt-as-tool-result behavior is eliminated for supported runner outputs.
- **non-goals**: no migration of watch, list, show, review, or diff surfaces yet.

### PR-04: checkpoint and turn-selector convergence
- **goal**: make checkpoint history, restart mapping, and turn-aware diff consume the same checkpoint and turn-selector truth.
- **builds on**: PR-03 merged turn model.
- **acceptance**:
  - checkpoint listing and restart-from-history use the same checkpoint association rules and latest-checkpoint mapping.
  - turn-aware diff selectors resolve against the same derived turn model used by history and restart.
  - navigation output that points users at history or diff stays behaviorally aligned with the canonical turn IDs exposed elsewhere.
  - users do not see conflicting checkpoint or selector interpretations across history, restart, and diff flows.
- **non-goals**: no broad watch or list UI restyling.

### PR-05: live activity surface convergence
- **goal**: make invocation list, watch, show, and review surfaces reuse one latest-activity and status-summary model.
- **builds on**: PR-04 merged turn and checkpoint convergence.
- **acceptance**:
  - `agent ls`, `agent ls --watch`, `agency watch`, `agent show`, and `agent review` or `checks` agree on latest activity, display status, summary text, and navigation context for the same invocation.
  - watch and list surfaces remain additive human views and do not create a new authority path for invocation truth.
  - latest activity shown in fleet-style surfaces is derived from the same projection model as invocation-local history.
  - raw log reading remains available as a separate debug lane rather than becoming the default list or watch presentation.
- **non-goals**: no final visual or thematic redesign of the TUI family.

### PR-06: shared human renderer and TUI cleanup
- **goal**: finish the slice by consolidating human-readable rendering and TUI presentation around the shared projections, with concise defaults and explicit raw expansion.
- **builds on**: PR-05 merged shared activity projections.
- **acceptance**:
  - invocation-facing history and activity surfaces use shared human-readable rendering primitives instead of bespoke formatting paths.
  - full raw tool payloads are hidden behind raw, json, or explicit expansion behavior rather than dumped by default.
  - the interactive history and watch experiences use the same projection language and style conventions instead of presenting unrelated visual vocabularies.
  - stylistic improvements do not change the underlying machine-readable or raw-debug contracts.
- **non-goals**: no new runner families; no GUI or web dashboard scope.

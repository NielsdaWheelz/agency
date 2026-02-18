# v2.1 Release Gates

Last updated: 2026-02-18
Status: active

This document defines mandatory hardening gates for v2.1.
Product parity work does not override these gates.
For S1, gate membership is sourced from local `docs/issues/*.md` artifacts.
Post-S1 direction is GitHub-issue-native tracking, with local markdown treated as compatibility input only.

## Gate A: P0 safety closure (must be zero open)

1. `docs/issues/events-p0-event-system-hardening.md`
2. `docs/issues/store-p0-08-remove-paths-use-raw-osremoveall-without-safety-checks.md`
3. `docs/issues/daemon-p0-08-unsafe-deletes-in-landing.md`

## Gate B: parity-critical P1 closure (must be complete before release candidate)

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
19. `docs/issues/spec-p1-07-runner-capability-target-set-claude-code-codex-amp-opencode-cursor-cli-droid.md`
20. `docs/issues/spec-p1-08-daemon-read-write-authority-for-v2-agent-and-worktree-surfaces.md`
21. `docs/issues/spec-p1-09-detached-chat-transcript-and-session-reentry-contract.md`
22. `docs/issues/spec-p1-10-fleet-management-for-many-worktrees-and-invocations.md`
23. `docs/issues/checkpoint-p1-10-interactive-history-selector-for-checkpoint-revert.md`
24. `docs/issues/spec-p1-11-invocation-scoped-review-pr-merge-command-contracts.md`

## Gate C: parity baseline acceptance

1. Headless invocation supports detached transcript visibility (prompts/messages/tool-use/logs) and follow-up prompt flow.
2. Users can enter/detach/re-enter invocation context repeatedly without resetting invocation continuity.
3. Restart-from-checkpoint flow exists as a single invocation command path.
4. Invocation-centric PR/review/merge command family exists with stable `--json`.
5. Runner capability model replaces hardcoded `claude|codex` gates.
6. Runner targets `claude-code`, `codex`, `amp`, `opencode`, `cursor-cli`, and `droid` are supported via one capability-driven invocation model.
7. Checkpoint restore supports both explicit checkpoint selection and interactive history-based selection with arrow-key terminal navigation.
8. Daemon APIs are the read/write source of truth for v2 `agent` + `worktree` command surfaces.
9. Fleet workflows support efficient list/filter/status/selection over many worktrees/invocations.

## Gate D: contract and test compliance

1. `docs/contracts/*` updated for any new daemon endpoint or data contract.
2. New behavior-changing decisions have unit/integration tests.
3. Event and error-code behavior is asserted for critical mutation paths.
4. End-to-end coverage exists for invocation-centric review/PR/merge happy path.

## Exit criteria summary

v2.1 release candidate requires all gates A-D to be satisfied.

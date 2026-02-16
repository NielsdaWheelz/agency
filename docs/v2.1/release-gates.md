# v2.1 Release Gates

Last updated: 2026-02-16
Status: active

This document defines mandatory hardening gates for v2.1.
Product parity work does not override these gates.

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

## Gate C: parity baseline acceptance

1. Headless invocation supports detached log visibility and follow-up prompt flow.
2. Restart-from-checkpoint flow exists as a single invocation command path.
3. Invocation-centric PR/review/merge command family exists with stable `--json`.
4. Runner capability model replaces hardcoded `claude|codex` gates.

## Gate D: contract and test compliance

1. `docs/contracts/*` updated for any new daemon endpoint or data contract.
2. New behavior-changing decisions have unit/integration tests.
3. Event and error-code behavior is asserted for critical mutation paths.
4. End-to-end coverage exists for invocation-centric review/PR/merge happy path.

## Exit criteria summary

v2.1 release candidate requires all gates A-D to be satisfied.

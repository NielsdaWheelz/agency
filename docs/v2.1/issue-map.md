# v2.1 Issue Map

Last updated: 2026-02-18
Status: active

This map links v2.1 slices to issue stubs for execution tracking.
It is intentionally selective for v2.1 scope and release-gate relevance.
S1 uses markdown issue stubs as compatibility source; long-term tracking direction is GitHub-issue-native.

## S1 Platform Hardening Gates

1. `docs/issues/events-p0-event-system-hardening.md`
2. `docs/issues/store-p0-08-remove-paths-use-raw-osremoveall-without-safety-checks.md`
3. `docs/issues/daemon-p0-08-unsafe-deletes-in-landing.md`
4. `docs/issues/cli-p1-01-all-commands-drop-cmdcontext.md`
5. `docs/issues/core-p1-01-cli-ignores-cancellation-and-timeouts-at-the-boundary.md`
6. `docs/issues/runtime-p1-runtime-dirs-single-source-of-truth.md`
7. `docs/issues/daemon-p1-04-request-decoding-is-too-permissive.md`
8. `docs/issues/daemon-p1-20-legacy-headless-endpoint-lacks-modern-validation.md`
9. `docs/issues/daemon-p1-21-clientrequestid-is-not-validated-as-uuid.md`
10. `docs/issues/core-p1-tighten-file-permissions.md`
11. `docs/issues/events-p1-remove-ad-hoc-event-writers.md`
12. `docs/issues/events-p1-enforce-required-events-in-flows.md`

## S2 Daemon Read Convergence + Sandbox Navigation

1. `docs/issues/cli-p2-13-worktree-hardcodes-the-daemon-socket-path.md`
2. `docs/issues/cli-p2-02-multiple-commands-ignore-injected-ctxstdoutstderr.md`
3. `docs/issues/cli-p2-03-widespread-direct-os-usage-despite-fsfs-being-passed.md`
4. `docs/issues/ids-p2-02-ambiguous-worktreeinvocation-errors-are-weak.md`
5. `docs/issues/spec-p1-08-daemon-read-write-authority-for-v2-agent-and-worktree-surfaces.md`
6. `docs/issues/spec-p1-10-fleet-management-for-many-worktrees-and-invocations.md`

## S3 Chat Control Plane + Restart-From-Checkpoint

1. `docs/issues/cli-p1-12-unbounded-prompt-file-reads.md`
2. `docs/issues/daemon-p1-15-headless-runner-inherits-stdin.md`
3. `docs/issues/daemon-p1-23-headless-runner-env-lacks-required-defaults.md`
4. `docs/issues/daemon-p2-19-no-size-limits-for-promptslog-writes.md`
5. `docs/issues/checkpoint-p1-01-event-sequence-is-not-monotonic-across-daemon-restarts.md`
6. `docs/issues/checkpoint-p1-02-checkpoint-apply-emits-seq1-unconditionally.md`
7. `docs/issues/checkpoint-p1-10-interactive-history-selector-for-checkpoint-revert.md`
8. `docs/issues/spec-p1-09-detached-chat-transcript-and-session-reentry-contract.md`

## S4 Runner Capability Model + Agent Mutation JSON

1. `docs/issues/exec-p1-deterministic-env-merge.md`
2. `docs/issues/daemon-p1-13-stream-parser-drops-write-errors.md`
3. `docs/issues/daemon-p1-14-stream-parser-can-allocate-unbounded-memory-on-huge.md`
4. `docs/issues/stream-p1-01-normalized-event-seq-is-not-persisted.md`
5. `docs/issues/stream-p1-02-writes-ignore-errors.md`
6. `docs/issues/spec-p1-07-runner-capability-target-set-claude-code-codex-amp-opencode-cursor-cli-droid.md`

## S5 Invocation-Centric Review + PR + Merge

1. `docs/issues/spec-p1-11-invocation-scoped-review-pr-merge-command-contracts.md`
2. `docs/issues/spec-p2-09-e2e-tests-for-pr-flows.md`
3. `docs/issues/merge-p1-04-merge-log-writes-ignore-errors-and-use-0644.md`
4. `docs/issues/merge-p1-05-buildverifyenvformerge-sets-agencyreporoot-to-worktree-path.md`
5. `docs/issues/push-p2-04-fallback-pr-body-generation-is-unbounded.md`
6. `docs/issues/push-p2-06-report-parsing-reads-unbounded-content.md`

## S6 Reports v2 + CLI Ergonomics Cleanup

1. `docs/issues/product-p3-cli-ergonomics-backlog.md`
2. `docs/issues/cli-p2-15-pr-fallback-generation-is-unbounded.md`
3. `docs/issues/spec-p2-10-reports-v2-json-and-markdown-compat-contract.md`

## S7 Checks-First Watch Seed (Stretch)

1. `docs/issues/spec-p3-07-product-direction-tui-optional-vs-essential.md`
2. `docs/issues/spec-p3-08-tmux-lifecycle-when-runner-exits.md`

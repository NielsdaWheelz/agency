# v2.1 Product Docs

Last updated: 2026-02-17
Status: active
Owners: `@nnandal` + `Codex`
Source of truth: this directory

## Purpose

This directory is the consolidated product documentation set for v2.1.
It replaces split planning across ad-hoc notes and keeps one canonical place
for direction, parity scope, release gates, and slice sequencing.

## Release boundaries

1. No GUI in v2.1.
2. No full-screen TUI in v2.1 (checks-first seed can be CLI/minimal terminal).
3. Preserve sandbox-first safety invariants.
4. Daemon remains the read/write authority for invocation/worktree lifecycle.

## Canonical docs

- `product-brief.md` - product outcomes, scope, non-scope, and parity definition.
- `parity-matrix.md` - Conductor capability map, current state, and v2.1 targets.
- `release-gates.md` - mandatory P0/P1 hardening gates and ship criteria.
- `slice-roadmap.md` - ordered v2.1 slices and dependency DAG.
- `issue-map.md` - mapping from slices to issue stubs for execution tracking.

## Active slice artifacts

- `s1/s1_spec.md` - L2 contract for Slice S1.
- `s1/s1_spec_worklog.md` - evidence log for Slice S1 L2 drafting.
- `s1/s1_spec_decisions.md` - decision ledger for Slice S1 L2 drafting.
- `s1/s1_roadmap.md` - L3 PR roadmap for Slice S1.
- `s1/s1_roadmap_ownership.md` - ownership ledger for Slice S1 L3 decomposition.
- `s1/s1_roadmap_worklog.md` - evidence log for Slice S1 L3 drafting.
- `s1/s1_roadmap_decisions.md` - decision ledger for Slice S1 L3 drafting.
- `s1/s1_prs/s1_pr01.md` - L4 contract for Slice S1 PR-01.
- `s1/s1_prs/s1_pr01_worklog.md` - evidence log for Slice S1 PR-01 L4 drafting.
- `s1/s1_prs/s1_pr01_decisions.md` - decision ledger for Slice S1 PR-01 L4 drafting.
- `s1/s1_prs/s1_pr02.md` - L4 contract for Slice S1 PR-02.
- `s1/s1_prs/s1_pr02_worklog.md` - evidence log for Slice S1 PR-02 L4 drafting.
- `s1/s1_prs/s1_pr02_decisions.md` - decision ledger for Slice S1 PR-02 L4 drafting.

## Legacy doc status

- `../v2.1-build-plan.md` is retained as a compatibility pointer.
- `../v2.1-notes.md` is retained as a research archive and evidence log.

## Update protocol

1. Product behavior changes update `product-brief.md` and `parity-matrix.md`.
2. Release-blocking triage changes update `release-gates.md`.
3. Sequencing changes update `slice-roadmap.md` and `issue-map.md`.
4. Any API/schema contract changes must also update `docs/contracts/*`.

# v2.1 Product Docs

Last updated: 2026-02-16
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

## Legacy doc status

- `../v2.1-build-plan.md` is retained as a compatibility pointer.
- `../v2.1-notes.md` is retained as a research archive and evidence log.

## Update protocol

1. Product behavior changes update `product-brief.md` and `parity-matrix.md`.
2. Release-blocking triage changes update `release-gates.md`.
3. Sequencing changes update `slice-roadmap.md` and `issue-map.md`.
4. Any API/schema contract changes must also update `docs/contracts/*`.

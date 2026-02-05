# architecture

this document defines the long-term architecture contract. it is normative. when in doubt, update this doc before shipping.

## goals

- keep the system local-first, single-user, and deterministic
- make metadata and events the single source of truth
- keep process supervision correct and testable
- make contracts explicit and enforceable

## non-goals

- multi-user isolation
- windows support
- zero data loss on power loss

## core invariants

- all persisted state is owned by `internal/store` and its contract files.
- all external process execution goes through `internal/exec`.
- the daemon is the only supervisor for headless invocations.
- metadata and events are append-only or atomic writes; no partial writes.
- schema_version is required and validated on every read.
- environment merges are deterministic and stable (sorted keys).

## components

- cli layer: `cmd/agency` + `internal/commands` for orchestration only.
- daemon: `internal/daemon` supervises headless processes and owns lifecycle mutations.
- daemon client: `internal/daemonclient` for http-over-unix-socket control plane.
- store: `internal/store` owns persistence and atomic writes.
- fs: `internal/fs` owns safe filesystem operations.
- exec: `internal/exec` owns process execution, streaming, and process groups.
- git: `internal/git` wraps git operations.
- services: `internal/runservice`, `internal/invocation`, `internal/worktree`, `internal/integrationworktree`.
- contracts: `docs/contracts/*` define all file and api schemas.

## data model

- repo registry: `repo_index.json` and `repo.json` are the canonical repo registry.
- run records: `runs/<run_id>/meta.json` + `events.jsonl`.
- invocation records: `invocations/<id>/meta.json` + `events.jsonl` + logs.
- worktree records: `integration_worktrees/<id>/meta.json`.
- runner status: `.agency/state/runner_status.json` for runner-to-agency state.

## control plane rules

- cli commands call services; services call store.
- daemon api is the only contract for headless control.
- legacy daemon endpoints are deprecated and must not gain features.

## extension points

- runners: add new runner types by extending runner command resolution and status parsing.
- contracts: new fields require schema bump, validation, and tests.

## stubs

- observability spec (structured logs, trace ids)
- multi-repo orchestration (if ever needed)

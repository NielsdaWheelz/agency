# [p1][events][tech-debt] remove ad hoc event writers (landing/checkpoint)

labels: `p1`, `type:tech-debt`, `area:events`

## summary
remove ad hoc event writers (landing/checkpoint)

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - landing events bypass the events subsystem
  - checkpoint apply events are best-effort and bypass the events subsystem
  - checkpoint engine events are best-effort and bypass the events subsystem
  - event schema is ad hoc and not the shared events contract
  - events are best-effort and unlocked
- details:
  - `landing/service.go` hand-rolls JSONL without repo/run ids, ignores errors, and skips file locking. unify on `events.AppendEvent`.
  -
  - `daemon/checkpoint/apply.go` writes JSONL directly with `os.OpenFile` and ignores errors.
  -
  - `daemon/checkpoint/engine.go` appends JSONL directly, ignores errors, and writes with 0644.
  -
  - emits `{event, data}` records, not standard event kind, and ignores errors.
  -
  - `appendEvent` ignores errors and does no file locking; JSONL can corrupt under concurrency.
  -

## acceptance criteria
- [ ] define minimal fix + tests


# [p0][events][tech-debt] event system hardening (atomic, locked, validated)

labels: `p0`, `type:tech-debt`, `area:events`

## summary
event system hardening (atomic, locked, validated)

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - append is best-effort by design
  - no file locking
  - event logging is not concurrency-safe
  - permissive file permissions
  - schema_version is not enforced on read
  - events.jsonl is also written with 0644 and parent dirs 0755
- details:
  - `events.AppendEvent` comment says ignore errors, but we now require events in critical flows.
  -
  - concurrent writers can interleave JSONL; must lock or serialize per run.
  -
  - `events.AppendEvent` appends without file locking; concurrent commands can interleave JSONL and corrupt logs.
  -
  - creates dirs 0755 and files 0644; should be 0700/0600 for private run data.
  -
  - if events are contractual, add validation + tooling to detect corrupt events.
  -
  - `events.AppendEvent` should respect the same private permissions.
  -

## acceptance criteria
- [ ] define minimal fix + tests


# backlog policy

this document defines issue triage and prioritization.

## labels

- priority: `p0`, `p1`, `p2`, `p3`
- type: `bug`, `design`, `tech-debt`, `security`, `docs`
- area: subsystem owner (daemon, store, commands, etc.)

## priorities

- p0: correctness or safety break; blocks releases.
- p1: high-impact correctness or user-facing regressions.
- p2: important but non-blocking.
- p3: opportunistic or cleanup.

## rules

1. every issue must have a priority and area.
2. p0/p1 require clear acceptance criteria and tests.
3. close issues only when acceptance criteria are met.
4. keep `docs/issues.md` in sync with issue stubs.

## references

- `docs/issue_process.md`

## stubs

- service-level objectives for response and fix times

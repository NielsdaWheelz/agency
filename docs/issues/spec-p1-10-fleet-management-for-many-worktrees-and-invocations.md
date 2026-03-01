# [p1][spec][design] fleet management for many worktrees and invocations

labels: `p1`, `type:design`, `area:spec`

## summary
define scalable list/filter/navigation workflows so users can observe and
manage large numbers of worktrees and invocations efficiently.

## context
- section: v2.1 parity baseline
- source: docs/v2.1/constitution.md
- details:
  - parity expectations include fast navigation across many concurrent agents.
  - command UX must support selection and entry without requiring manual path spelunking.
  - outputs must remain scriptable for automation and operator dashboards.

## acceptance criteria
- [ ] define canonical list/filter/sort selectors for worktrees and invocations
- [ ] define bulk-friendly status views (state, runner, last activity, readiness signals)
- [ ] define command path to enter invocation context and detach safely
- [ ] add tests for large result sets, pagination/cursors, and selection determinism

# [p2][render][tech-debt] writer errors are swallowed

labels: `p2`, `type:tech-debt`, `area:render`

## summary
writer errors are swallowed

## context
- section: Audit: Render
- source: docs/issues.md
- details:
  - `render.WriteShowHuman`, `render.WriteConflictCard`, and related helpers discard `fmt.Fprintf` errors and return nil, so broken pipes look like success. return errors or use a checked writer helper.

## acceptance criteria
- [ ] define minimal fix + tests


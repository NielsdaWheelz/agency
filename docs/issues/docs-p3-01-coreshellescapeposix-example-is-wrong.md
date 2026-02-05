# [p3][docs][tech-debt] core.ShellEscapePosix example is wrong

labels: `p3`, `type:tech-debt`, `area:docs`

## summary
core.ShellEscapePosix example is wrong

## context
- section: Audit: Comments
- source: docs/issues.md
- details:
  - the empty-string example shows a non-ASCII quote character and doesn’t match the actual return value `''`. this is small, but it’s still slop.

## acceptance criteria
- [ ] define minimal fix + tests


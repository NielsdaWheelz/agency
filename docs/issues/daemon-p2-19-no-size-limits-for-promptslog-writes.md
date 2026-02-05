# [p2][daemon][tech-debt] no size limits for prompts/log writes

labels: `p2`, `type:tech-debt`, `area:daemon`

## summary
no size limits for prompts/log writes

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - headless start writes prompt directly to disk with no max size; add limits to prevent huge files.

## acceptance criteria
- [ ] define minimal fix + tests


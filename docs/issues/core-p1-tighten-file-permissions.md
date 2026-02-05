# [p1][core][tech-debt] tighten file permissions for private data

labels: `p1`, `type:tech-debt`, `area:core`

## summary
tighten file permissions for private data

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - permissions are inconsistent and too open
  - permissions are too open
  - logs are world-readable
  - log files are created as 0644
  - pidfile is created with 0644
  - user config permissions are too open
- details:
  - `.agency/` dirs and logs are created with `0755/0644`; use `0700/0600` for private run data.
  -
  - verify logs and record dirs are `0755`, log is `0644`; use `0700/0600`.
  -
  - `archive.log` uses 0644; use 0600 + 0700 dirs for private run data.
  -
  - raw/stderr/stream logs are world-readable; enforce 0600.
  -
  - `daemon/server.go` writes pid files world-readable; use 0600 and `fs.FS`.
  -
  - `agency init` creates `config.json` with 0644 and config dir 0755. for a user-only tool, make it 0600/0700 by default.
  -

## acceptance criteria
- [ ] define minimal fix + tests


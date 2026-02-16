# [p1][spec][design] daemon read/write authority for v2 agent and worktree surfaces

labels: `p1`, `type:design`, `area:spec`

## summary
make daemon APIs the canonical read and mutation authority for v2 `agent` and
`worktree` command surfaces.

## context
- section: v2.1 parity baseline
- source: docs/v2.1/product-brief.md + docs/v2.1/parity-matrix.md
- details:
  - v2.1 direction requires daemon-centric lifecycle control, not split
    direct-store reads in CLI command handlers.
  - all user-visible state for invocation/worktree flows should resolve through
    daemon DTOs and contracts.
  - CLI may keep minimal bootstrap fallback only for daemon startup/health path.

## acceptance criteria
- [ ] define which `agent`/`worktree` commands are daemon-backed for reads and writes
- [ ] remove direct local store scans from v2 command handlers where daemon API exists
- [ ] codify bootstrap fallback boundaries (allowed vs forbidden local reads)
- [ ] add tests proving daemon responses are source of truth for v2 command outputs

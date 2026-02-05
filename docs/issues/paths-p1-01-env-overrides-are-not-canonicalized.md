# [p1][paths][tech-debt] env overrides are not canonicalized

labels: `p1`, `type:tech-debt`, `area:paths`

## summary
env overrides are not canonicalized

## context
- section: Audit: Paths
- source: docs/issues.md
- details:
  - `paths.ResolveDirs` accepts relative and symlinked paths; later containment checks and equality comparisons can be wrong. enforce absolute + clean + EvalSymlinks or reject non-absolute.

## acceptance criteria
- [ ] define minimal fix + tests


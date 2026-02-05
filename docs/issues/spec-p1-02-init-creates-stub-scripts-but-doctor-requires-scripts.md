# [p1][spec][design] Init creates stub scripts but doctor requires scripts exist + executable

labels: `p1`, `type:design`, `area:spec`

## summary
Init creates stub scripts but doctor requires scripts exist + executable

## context
- section: Open Issues / Notes
- source: docs/issues.md
- details:
  - Init semantics say “scripts are never overwritten,” and agency.json overwriting requires `--force`. Edge case: user runs init once, gets stub verify exiting 1, then doctor fails forever until they edit it. That’s intended, but the doctor error must be unmissable: “verify script is a stub and exits 1; replace it.” Also require scripts be relative to repo root in validation: reject absolute paths and `..` path traversal in v1 to avoid path injection.

## acceptance criteria
- [ ] define minimal fix + tests


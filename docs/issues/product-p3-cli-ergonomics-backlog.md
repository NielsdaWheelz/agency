# [p3][product][enhancement] cli ergonomics backlog

labels: `p3`, `type:enhancement`, `area:product`

## summary
cli ergonomics backlog

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - Switch to Cobra from Go stdlib
  - Reports should be JSON, simpler, not required, and omit "how to test" or other basics
  - Cleaner flags and confirmation. Add --yes to skip confirmation
  - Short flags and easier commands
  - Add a flag to run --open (open in IDE right away)
  - approved direction: canonical short aliases are `-r/--repo`, `-j/--json`, `-y/--yes`, `-o/--open` (additive compatibility-preserving)
  - approved direction: `--yes` standardization covers high-impact destructive/irreversible confirmation paths (`agent merge`, `clean`, `resume --restart`, `worktree rm`)
  - approved direction: open-on-create applies to canonical creation plus compatibility run/create flows

## acceptance criteria
- [ ] standardize non-interactive confirmation policy with `--yes` across approved high-impact confirmation paths with deterministic `E_CONFIRMATION_REQUIRED` behavior when omitted.
- [ ] normalize high-traffic flags to canonical long names plus additive short aliases `-r`, `-j`, `-y`, `-o` without semantic drift.
- [ ] add open-on-create ergonomics to canonical creation and compatibility run/create flows with deterministic behavior in interactive and scriptable contexts.
- [ ] keep legacy spellings/paths as compatibility aliases only (no behavior redefinition).
- [ ] update command help/docs and add tests for human and `--json`/automation-facing ergonomics behavior.

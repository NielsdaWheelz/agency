# [p3][spec][design] TMUX lifecycle when runner exits

labels: `p3`, `type:design`, `area:spec`

## summary
TMUX lifecycle when runner exits

## context
- section: Open Issues / Notes
- source: docs/issues.md
- details:
  - Should we be kicked out of tmux when the runner exits? Currently `attach` fails when the runner is dead. At minimum, error messaging should be clearer. Consider whether tmux should stay open so users can work in the terminal or re-open the runner later without a full `resume`.

## acceptance criteria
- [ ] define minimal fix + tests


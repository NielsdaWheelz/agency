# [p3][spec][design] Headless mode: --headless

labels: `p3`, `type:design`, `area:spec`

## summary
Headless mode: --headless

## context
- section: Open Issues / Notes
- source: docs/issues.md
- details:
  - Example: `claude -p "Find and fix the bug in auth.py" --allowedTools "Read,Edit,Bash"` and `codex exec`. This requires larger changes: attach a text prompt (e.g. `--prompt "fix bug"`) and log all outputs. See v1.5.

## acceptance criteria
- [ ] define minimal fix + tests


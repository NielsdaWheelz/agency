# [p2][spec][design] Push step 1 git fetch origin can be slow and may prompt for creds

labels: `p2`, `type:design`, `area:spec`

## summary
Push step 1 git fetch origin can be slow and may prompt for creds

## context
- section: Open Issues / Notes
- source: docs/issues.md
- details:
  - Fetch can hang if the remote needs authentication. You can’t eliminate this, but add timeouts for git/gh commands (not just scripts) and document “git must be configured for non-interactive auth (ssh agent, credential helper).”

## acceptance criteria
- [ ] define minimal fix + tests


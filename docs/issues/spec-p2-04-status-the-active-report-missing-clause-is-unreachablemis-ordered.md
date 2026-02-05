# [p2][spec][design] Status: the “active (report missing)” clause is unreachable/mis-ordered

labels: `p2`, `type:design`, `area:spec`

## summary
Status: the “active (report missing)” clause is unreachable/mis-ordered

## context
- section: Open Issues / Notes
- source: docs/issues.md
- details:
  - You check “PR exists and last_push_at and report exists” => ready, else if active and PR open => “active (report missing),” but you don’t define “PR open” vs “PR exists” consistently and you don’t store PR state in meta. In v1, don’t assert open/closed without calling `gh pr view`. Options: display “pr: yes” without open/closed, or define that `ls` may call `gh pr view` (slow) and cache it. Recommendation: don’t hit network in `ls` by default; show only meta fields and add `agency ls --fresh` later. Update display logic to only use meta fields: if `pr_url` present, show “(pr)” indicator, not “open.” `E_PR_NOT_OPEN` can exist for merge-time when you query gh.

## acceptance criteria
- [ ] define minimal fix + tests


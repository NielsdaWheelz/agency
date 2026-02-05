# [p1][git][enhancement] support github enterprise and ssh remotes end-to-end

labels: `p1`, `type:enhancement`, `area:git`

## summary
support github enterprise and ssh remotes end-to-end

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - support github enterprise and ssh remotes
  - ParseOriginHost rejects valid hosts without dots and ignores ssh://
  - repo_key for github repos ignores host
  - github.com is hard-coded in multiple flows
  - github.com-only gate blocks enterprise/ssh
  - PR URL regex is hard-coded to github.com
  - gh host is not passed through
  - github.com-only gate blocks enterprise/ssh
  - gh host is not passed through
  - github.com-only gate blocks enterprise/ssh
  - PR close uses ParseGitHubOwnerRepo which only supports github.com
  - gh host is not passed through
- details:
  - parse ssh/https/ssh://, accept non-github.com hosts, and plumb host into gh usage.
  -
  - that breaks GitHub Enterprise or internal hosts. either support or explicitly error early.
  -
  - for enterprise, `github:owner/repo` collides across hosts. include host in repo_key, e.g. `github:<host>/<owner>/<repo>`.
  -
  - `push`, `merge`, and `clean` reject non-github.com hosts; `ParseGitHubOwnerRepo` and PR URL regex also assume github.com.
  -
  - `ParseOriginHost` + `ParseGitHubOwnerRepo` + PR URL regex reject non-github.com.
  -
  - fails on GH enterprise; must parse host-aware URLs.
  -
  - for enterprise, set `GH_HOST` or equivalent in env; otherwise gh hits github.com.
  -
  - `parseOriginHost` + `ParseGitHubOwnerRepo` enforce github.com.
  -
  - for enterprise, set `GH_HOST` or equivalent in env; otherwise gh hits github.com.
  -
  - branch deletion and PR close are skipped for enterprise hosts.
  -
  - enterprise PRs will never be closed.
  -
  - for enterprise, set `GH_HOST` or equivalent in env; otherwise gh hits github.com.
  -

## acceptance criteria
- [ ] define minimal fix + tests


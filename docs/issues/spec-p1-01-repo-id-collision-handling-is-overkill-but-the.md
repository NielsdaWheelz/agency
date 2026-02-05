# [p1][spec][design] Repo ID collision handling is overkill, but the real collision is repo moves + path-key

labels: `p1`, `type:design`, `area:spec`

## summary
Repo ID collision handling is overkill, but the real collision is repo moves + path-key

## context
- section: Open Issues / Notes
- source: docs/issues.md
- details:
  - You already track multiple paths, but the fallback key uses `sha256(abs_path)`, so moving a repo generates a new `repo_key` and thus a new `repo_id`. That “loses history.” If accepted for v1, call out the limitation: “path-based repo identity is not stable across moves; moving a non-github repo will be treated as a new repo.”

## acceptance criteria
- [ ] define minimal fix + tests


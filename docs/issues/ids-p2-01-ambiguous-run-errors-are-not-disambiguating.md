# [p2][ids][tech-debt] ambiguous run errors are not disambiguating

labels: `p2`, `type:tech-debt`, `area:ids`

## summary
ambiguous run errors are not disambiguating

## context
- section: Audit: IDs
- source: docs/issues.md
- details:
  - `ids.ErrAmbiguous.Error` prints only run_ids; when the same run_id exists in multiple repos, the message is useless. include repo_id or name.

## acceptance criteria
- [ ] define minimal fix + tests


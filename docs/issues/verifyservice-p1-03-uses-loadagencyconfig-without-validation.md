# [p1][verifyservice][tech-debt] uses LoadAgencyConfig without validation

labels: `p1`, `type:tech-debt`, `area:verifyservice`

## summary
uses LoadAgencyConfig without validation

## context
- section: Audit: Verifyservice
- source: docs/issues.md
- details:
  - missing `scripts.verify.path` can slip through; should use `LoadAndValidate`.

## acceptance criteria
- [ ] define minimal fix + tests


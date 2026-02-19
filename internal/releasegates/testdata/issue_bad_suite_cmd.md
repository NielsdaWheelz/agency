# [p0][events][tech-debt] event hardening

labels: `p0`, `type:tech-debt`, `area:events`
state: closed

## summary
event hardening

## acceptance criteria
- [x] done

## closure evidence

```json
{
  "implemented_refs": ["pr:101"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/test.md",
      "command": "go test ./internal/events/...",
      "scope": "targeted",
      "result": "pass",
      "artifact_ref": "ci:build-100",
      "recorded_at": "2026-02-15T10:00:00Z"
    }
  ],
  "suite_test_refs": [
    {
      "issue_path": "docs/issues/test.md",
      "command": "npm test",
      "scope": "suite",
      "result": "pass",
      "artifact_ref": "ci:build-101",
      "recorded_at": "2026-02-15T11:00:00Z"
    }
  ]
}
```

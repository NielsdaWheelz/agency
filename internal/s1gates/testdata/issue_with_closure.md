# [p0][events][tech-debt] event system hardening

labels: `p0`, `type:tech-debt`, `area:events`
state: closed

## summary
event system hardening

## acceptance criteria
- [x] atomic event writes
- [x] file locking on event append
- [x] private permissions (0700/0600)

## closure evidence

```json
{
  "implemented_refs": ["pr:101", "commit:abc123"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/events-p0-event-system-hardening.md",
      "command": "go test ./internal/events/...",
      "scope": "targeted",
      "result": "pass",
      "artifact_ref": "ci:build-100",
      "recorded_at": "2026-02-15T10:00:00Z"
    }
  ],
  "suite_test_refs": [
    {
      "issue_path": "docs/issues/events-p0-event-system-hardening.md",
      "command": "go test ./...",
      "scope": "suite",
      "result": "pass",
      "artifact_ref": "ci:build-101",
      "recorded_at": "2026-02-15T11:00:00Z"
    }
  ]
}
```

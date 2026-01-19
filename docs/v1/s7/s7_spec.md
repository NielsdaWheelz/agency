# Slice 7: Runner Status Contract & Watchdog

Replace heuristic-based status detection with a deterministic, artifact-first system where runners communicate state via `.agency/state/runner_status.json`.

---

## Goals

1. Semantic status: `working`, `needs input`, `blocked`, `ready for review` instead of vague `idle`
2. Runner contract: Runners update `.agency/state/runner_status.json` at milestones
3. System prompt: `CLAUDE.md` created on `init` with runner instructions
4. Stall detection: Flag runs with no status updates for 15+ minutes
5. Actionable output: `ls` shows summary, `show` shows questions/blockers

## Non-Goals

- Modifying runner binaries
- Real-time output streaming
- Automatic intervention on stall

---

## Add

### `CLAUDE.md` (repo root, on init)

```markdown
# Agency Runner Protocol

Update `.agency/state/runner_status.json` at milestones:

| Status | When | Required Fields |
|--------|------|-----------------|
| `working` | Actively making progress | `summary` |
| `needs_input` | Waiting for user answer | `summary`, `questions[]` |
| `blocked` | Cannot proceed | `summary`, `blockers[]` |
| `ready_for_review` | Work complete | `summary`, `how_to_test` |

Schema:
```json
{
  "schema_version": "1.0",
  "status": "working",
  "updated_at": "2026-01-19T12:00:00Z",
  "summary": "Implementing user authentication",
  "questions": [],
  "blockers": [],
  "how_to_test": "",
  "risks": []
}
```

Before `ready_for_review`, update `.agency/report.md` with summary, decisions, testing instructions, and risks.
```

### `.agency/state/runner_status.json` (worktree, on run)

Initial content written by agency:

```json
{
  "schema_version": "1.0",
  "status": "working",
  "updated_at": "<now>",
  "summary": "Starting work",
  "questions": [],
  "blockers": [],
  "how_to_test": "",
  "risks": []
}
```

### `internal/runnerstatus/runnerstatus.go`

```go
package runnerstatus

type Status string

const (
    StatusWorking        Status = "working"
    StatusNeedsInput     Status = "needs_input"
    StatusBlocked        Status = "blocked"
    StatusReadyForReview Status = "ready_for_review"
)

type RunnerStatus struct {
    SchemaVersion string   `json:"schema_version"`
    Status        Status   `json:"status"`
    UpdatedAt     string   `json:"updated_at"`
    Summary       string   `json:"summary"`
    Questions     []string `json:"questions"`
    Blockers      []string `json:"blockers"`
    HowToTest     string   `json:"how_to_test"`
    Risks         []string `json:"risks"`
}

func Load(worktreePath string) (*RunnerStatus, error)  // nil, nil if missing
func (s *RunnerStatus) Validate() error
func (s *RunnerStatus) Age() time.Duration
func StatusPath(worktreePath string) string
```

### `internal/watchdog/watchdog.go`

```go
package watchdog

const DefaultStallThreshold = 15 * time.Minute

type ActivitySignals struct {
    StatusFileModTime *time.Time
    TmuxSessionExists bool
}

type StallResult struct {
    IsStalled       bool
    StalledDuration time.Duration
}

func CheckStall(signals ActivitySignals, threshold time.Duration) StallResult
```

### `internal/scaffold/claude_md.go`

```go
package scaffold

const ClaudeMDTemplate = `# Agency Runner Protocol...`

func WriteClaudeMD(repoRoot string) (created bool, err error)
```

---

## Change

### `internal/status/derive.go`

New `Snapshot` fields:

```go
type Snapshot struct {
    TmuxActive      bool
    WorktreePresent bool
    RunnerStatus    *runnerstatus.RunnerStatus // nil if missing/invalid
    StallResult     *watchdog.StallResult
}
```

New precedence:

```
1. broken           → meta.json unreadable
2. merged           → archive.merged_at set
3. abandoned        → flags.abandoned set
4. failed           → flags.setup_failed set
5. needs attention  → flags.needs_attention set
6. ready for review → runner_status.status == "ready_for_review"
7. needs input      → runner_status.status == "needs_input"
8. blocked          → runner_status.status == "blocked"
9. working          → runner_status.status == "working"
10. stalled         → watchdog.IsStalled && tmux exists
11. active          → tmux exists (fallback)
12. idle            → no tmux (fallback)
```

### `internal/worktree/scaffold.go`

- Create `.agency/state/` directory
- Write initial `runner_status.json`

### `internal/commands/init.go`

- Call `WriteClaudeMD()`
- Output: `claude_md: created` or `claude_md: exists`

### `internal/commands/ls.go`

- Load runner_status.json for each run
- Stat file mtime, call `CheckStall()`
- Pass to `Derive()`

New output:

```
RUN_ID              NAME            STATUS            SUMMARY                    PR
20260119-a3f2       auth-fix        needs input       Which auth library?        #123
20260118-c5d2       bug-fix         stalled           (no activity for 45m)      -
20260118-e7f3       feature-x       working           Implementing validation    -
```

### `internal/commands/show.go`

New section:

```
runner_status:
  status: needs_input
  updated: 5m ago
  summary: Implementing OAuth but need clarification
  questions:
    - Which OAuth provider should I use?
    - Should sessions persist across restarts?
```

### `internal/render/ls.go`

- Add SUMMARY column
- Truncate to 40 chars

---

## Remove

| Item | Location |
|------|----------|
| `StatusReadyForReview` (old) | `internal/status/status.go` |
| `StatusActiveReportMissing` | `internal/status/status.go` |
| `StatusActivePR` | `internal/status/status.go` |
| `StatusIdlePR` | `internal/status/status.go` |
| `ReportNonemptyThresholdBytes` | `internal/status/derive.go` |
| `ReportBytes` field | `Snapshot` struct |
| Report-size logic in `Derive()` | `internal/status/derive.go` |

---

## Breaking Changes

| Change | Impact |
|--------|--------|
| Status strings change | `active (pr)`, `idle (pr)` gone; `ready for review` now runner-reported |
| `ls` output format | New SUMMARY column |
| `show` output format | New `runner_status:` section |
| `init` creates `CLAUDE.md` | New file in repo (if not exists) |

---

## Acceptance Criteria

- [ ] `agency init` creates `CLAUDE.md` (doesn't overwrite existing)
- [ ] `agency run` creates `.agency/state/runner_status.json` with status `working`
- [ ] `agency ls` shows runner-reported status when file exists
- [ ] `agency ls` shows `stalled` when no status update for 15+ minutes and tmux exists
- [ ] `agency ls` falls back to `active`/`idle` when no status file
- [ ] `agency show` displays questions/blockers/how_to_test
- [ ] Invalid status file doesn't crash (fallback to tmux detection)

---

## Tests

**Unit** (`internal/runnerstatus/`, `internal/watchdog/`, `internal/status/`):
- Load valid/missing/invalid status file
- Validate required fields per status
- Stall detection at threshold boundary
- Derive with each runner status value
- Derive fallback when no status file
- Terminal/failure states override runner status

**Integration** (`internal/commands/`):
- init creates CLAUDE.md
- run creates state directory and status file
- ls shows summary column
- show displays status details

**E2E**:
- Full lifecycle: init → run → update status file → ls/show reflect changes

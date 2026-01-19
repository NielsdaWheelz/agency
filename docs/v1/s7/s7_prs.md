# Slice 7: PR Breakdown

## PR 7.1: Runner status contract

- Add `internal/runnerstatus/` (types, Load, Validate)
- Add `internal/watchdog/` (CheckStall)
- Add `internal/scaffold/claude_md.go` (template)
- Update `internal/status/derive.go` (new precedence, remove old statuses)
- Update `internal/worktree/scaffold.go` (create `.agency/state/`, write initial status)
- Update `internal/commands/init.go` (create CLAUDE.md)
- Unit tests for new packages, update existing status tests

**Acceptance**: `agency init` creates CLAUDE.md, `agency run` creates status file, status derivation uses it.

---

## PR 7.2: Enhanced ls/show output

- Update `internal/commands/ls.go` (load status, check stall)
- Update `internal/commands/show.go` (display status details)
- Update `internal/render/ls.go` (SUMMARY column)
- Integration tests

**Acceptance**: `ls` shows SUMMARY column, `show` displays questions/blockers/how_to_test.

---

## PR 7.3: Documentation

- Update `docs/v1/constitution.md`
- Update `README.md`
- E2E test

**Acceptance**: Docs reflect new status model, E2E passes.

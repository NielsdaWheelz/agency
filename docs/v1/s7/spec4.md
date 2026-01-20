# S7 Spec 4: Runner Instructions + Report Completeness Gate

This spec introduces runner guidance via a tool-owned instructions file and enforces report completeness at push time.

---

## 1) `.agency/INSTRUCTIONS.md`

### properties

- tool-owned (never committed)
- overwritten on each run
- short, imperative, checklist-style
- no repo-specific commands (fmt/test/etc.)
- runner-agnostic (do not reference runner-specific features)

### required contents (normative)

- make incremental, focused commits
- keep commits buildable
- update `.agency/report.md` before finishing
- fill in at least `summary` and `how to test`
- record status in `.agency/state/runner_status.json` if supported

this file is advisory only. no correctness depends on it.

### creation semantics

- **when**: during `agency run`, after worktree and `.agency/` directories are created, before tmux runner starts
- **where**: `<worktree>/.agency/INSTRUCTIONS.md` (worktree-local, not global)
- **overwrite**: unconditionally overwrite on every run (no "if exists" check)
- **mode**: 0644
- **encoding**: UTF-8, unix line endings (LF)

---

## 2) update report template to reference instructions

modify the report template written at run creation:

- insert exactly one new line under the title:
  > "runner: read `.agency/INSTRUCTIONS.md` before starting."

this is a template change; the heading structure and other sections remain the same.

---

## 3) define report completeness contract

### definition

a report is considered **complete** if:
- the `## summary` section contains ≥1 non-whitespace character
- the `## how to test` section contains ≥1 non-whitespace character

all other sections are optional.

### parsing rules

**heading detection:**
- match lines starting with exactly `##` followed by whitespace and title text
- `###` and deeper headings are ignored for section boundaries

**title normalization** (applied before matching):
- lowercase
- trim leading/trailing whitespace
- collapse multiple consecutive spaces to single space
- strip trailing punctuation (`:`, `.`, `-`)

**accepted aliases:**
| canonical name | also accepts |
|----------------|--------------|
| `summary` | `overview` |
| `how to test` | `how-to-test`, `testing`, `tests` |

**content boundaries:**
- section content = everything from the heading line until the next `##` or `#` heading
- `###` subheadings are included in the parent section's content

**edge cases:**
- duplicate headings: first occurrence wins
- headings inside fenced code blocks (`` ``` ``): ignored (not treated as section boundaries)

---

## 4) enforce report completeness on `agency push`

### behavior

- if report file is missing:
  - abort push
  - return error `E_REPORT_INVALID` (existing error code)
  - hint: "report file not found at `<path>`"

- if report is incomplete (file exists but missing required sections):
  - abort push
  - return error `E_REPORT_INCOMPLETE`
  - list missing sections explicitly
  - print worktree path and suggest `agency open <id>` for quick editing
  - hint: "fill required sections or re-run with --force"

- allow bypass with `--force`

### output format

per constitution section 16.5, error output:
```
error_code: E_REPORT_INCOMPLETE
report: <worktree>/.agency/report.md
missing: summary, how to test
hint: fill required sections or use --force
hint: agency open <run_id>
```

exits non-zero.

### notes

- this is a hard gate for push only
- merge behavior is unchanged
- `--force` bypasses completeness check but not missing file check

---

## error handling

### error codes

| code | when | notes |
|------|------|-------|
| `E_REPORT_INVALID` | report file missing | existing code, reused |
| `E_REPORT_INCOMPLETE` | report exists but missing required sections | new code |

`E_REPORT_INCOMPLETE` must be added to constitution error codes (section 16.5).

---

## files to touch

**allowed**
- `internal/worktree/` (instruction file generation)
- `internal/worktree/` or `internal/scaffold/` (report template writer)
- `internal/commands/push.go` (validation logic)
- `internal/report/` (new package for parsing/validation)
- `internal/errors/codes.go` (new error code)
- unit tests

**explicitly forbidden**
- agency.json schema
- verify / setup / archive script behavior
- merge command
- runner invocation logic
- tmux handling
- storage schemas (`meta.json`, `repo.json`, etc.)

---

## tests

### unit tests (required)

**report completeness detection:**
1. empty template → incomplete
2. summary filled only → incomplete
3. how-to-test filled only → incomplete
4. both filled → complete
5. whitespace-only content → incomplete
6. reordered sections → handled correctly
7. alias headings (`## testing`, `## overview`) → recognized
8. headings inside fenced code blocks → ignored (not false positives)
9. case variations (`## Summary`, `## SUMMARY`) → normalized correctly
10. trailing punctuation (`## summary:`) → stripped, matches

**push gating logic:**
1. missing report file → fails with `E_REPORT_INVALID`
2. incomplete report → fails with `E_REPORT_INCOMPLETE`
3. incomplete report + `--force` → allowed
4. complete report → allowed

tests must not depend on git, tmux, or gh.

---

## acceptance criteria

- `.agency/INSTRUCTIONS.md` is created on every run (overwritten unconditionally)
- report template references instructions
- push fails on missing report with `E_REPORT_INVALID`
- push fails on incomplete report with `E_REPORT_INCOMPLETE` unless `--force`
- error messages include report path, missing sections, and `agency open` hint
- heading parsing handles aliases, normalization, and code fences
- no assumptions about repo tooling are introduced
- behavior is identical for claude and codex runners

---

## rationale

correctness is enforced mechanically (verify scripts, gates).
runner behavior is improved through visible guidance, not trust.
this preserves runner-agnosticism while raising baseline quality.

the heading alias approach (light flexibility) reduces user friction without adding significant complexity. strict-only matching would cause failures for reasonable variations like `## Testing` or `## Summary:` that users naturally write.

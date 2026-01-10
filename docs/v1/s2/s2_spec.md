# agency s2: observability (ls/show + transcript capture + events)

## goal
make agency runs inspectable and debuggable: list runs fast, show exact state, and capture tmux transcripts deterministically without network calls.

## scope
in-scope (v1):
- `agency ls` with sane defaults + `--all` + `--all-repos` + `--json`
- `agency show <id>` with rich details + `--path` + `--json` + `--capture`
- derive display status from local evidence only (no `gh` calls)
- per-run append-only `events.jsonl` (commands that take the repo lock emit; read-only commands without mutating flags do not)
- script logs already exist from s1; this slice standardizes how `show` surfaces them
- repo move handling (best-effort): use `repo_index.json` to map repo_key -> seen paths and pick an existing path deterministically
  - path selection preference order (for printing paths and default scope only; never affects run discovery or run_id resolution):
    1. current cwd repo root if it matches repo_key (when in repo)
    2. most recently seen path in repo_index that still exists
    3. any existing path from seen_paths
  - if none exist: `repo_root = null` and show warns “repo not found on disk”
  - do not try to resolve repo_root for archived runs beyond detecting missing worktree; show whatever path is in meta
  - cwd repo root match only applies when repo_key is available (via repo.json)

out-of-scope (explicit):
- any `gh` refresh during `ls`/`show` (`show --refresh` deferred)
- runner pid inspection (v1 `active` == tmux session exists)
- live streaming transcript recording
- auto-repair/adopt orphaned worktrees
- interactive agency tui
- no new top-level commands beyond l0; new flags permitted where specified

## public surface area added/changed

### commands / flags

#### `agency ls`
new flags:
- `--all` : include archived runs for the selected repo(s)
- `--all-repos` : list runs across repos
- `--json` : machine output (stable)

behavior tweaks (locked):
- if cwd is inside a git repo: default scope = that repo, exclude archived
- if cwd is not inside a git repo: default scope = `--all-repos`, exclude archived
- sorting: newest `created_at` first

#### `agency show <id>`
new flags:
- `--json` : machine output (stable)
- `--capture` : capture tmux scrollback into transcript files (best-effort), then show (mutating mode; takes repo lock and emits events)
existing:
- `--path` : print only resolved filesystem paths (repo root, worktree, global run dir, logs, report)

#### id resolution
- `show`, `attach`, `resume`, `stop`, `kill`, `push`, `merge`, `clean` accept:
  - exact `run_id` OR a unique prefix of `run_id`
- collisions are errors (see error codes)
- `agency show <id>` resolves by global scan and works inside or outside a repo

### outputs (human)
`ls` row fields (human mode):
- `run_id` (full)
- `title` (truncate to 50 chars; indicate truncation)
- `runner`
- `created_at` (relative ok, but `--json` must include absolute)
- `status` (derived string)
- `pr` (short: `#123` or empty; full url shown in `show`)

`show` (human mode):
- core meta fields (run_id/title/runner/created_at/branch/parent/worktree path/tmux session)
- pr fields if present
- workspace presence (worktree exists?)
- report presence + size
- last script outcomes (setup/verify/archive): exit code, duration, last lines pointer, log path
- derived status
- repo identity details (repo_key/repo_id/origin url if known)
note: `show` is read-only unless `--capture` is specified

### outputs (json)
`--json` outputs must be stable and versioned:
- top-level: `{ "schema_version": "1.0", "data": ... }`
- `ls --json`: list of run summaries (see persistence section for fields)
- `show --json`: full run record (meta + derived fields + resolved paths)

## files created/modified

### created/modified (global)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
  - created lazily on first emitted event
- no new global indexes introduced; run discovery is by scanning filesystem

### read (global)
- `${AGENCY_DATA_DIR}/repo_index.json`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/*` (script logs from s1)

### workspace-local (read)
- `<worktree>/.agency/report.md`
- `<worktree>/.agency/out/*.json` (optional script outputs)

### transcript capture (write)
on `agency show <id> --capture`:
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.txt` (overwrite)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.prev.txt` (single backup; best-effort rotate)

## new error codes
add (public contract):
- `E_RUN_ID_AMBIGUOUS` — id prefix matches >1 run; output candidates
- `E_RUN_BROKEN` — run exists but `meta.json` is unreadable/invalid; show path + recovery hint
  - only applies to commands targeting a specific run (not `ls`)

exit code expectations:
- `ls` exits 0 unless catastrophic (e.g., data dir unreadable)
- `show` exits non-zero on `E_RUN_BROKEN` even with `--json` (include `broken=true` in output)

## behaviors (given/when/then)

### run discovery + scope
- given: cwd inside repo A
  - when: `agency ls`
  - then: list runs whose `repo_id` matches repo A, excluding archived (worktree deleted) unless `--all`
- given: cwd not inside any git repo
  - when: `agency ls`
  - then: list runs across all repos, excluding archived unless `--all`

### discovery mechanism
- given: `${AGENCY_DATA_DIR}/repos/*/runs/*/meta.json`
  - when: `agency ls --all-repos`
  - then: agency scans those directories, parses meta where possible, and lists runs
  - and: unreadable/invalid meta results in a “broken run” row (human) and `broken=true` in json output
  - and: broken rows derive `run_id` from the run directory name; set `title="<broken>"`, `runner=null`, `created_at=null`
  - and: `E_RUN_BROKEN` only applies to commands targeting a specific run (show/attach/push/etc.); `ls` never throws it

### id resolution
- given: two runs `20260110-a3f2` and `20260110-a3ff`
  - when: `agency show 20260110-a3f`
  - then: fail `E_RUN_ID_AMBIGUOUS` and print both run_ids
- given: one run `20260110-a3f2`
  - when: `agency show 20260110-a3`
  - then: resolves to that run

### status derivation (local-only)
- given: a run with `meta.pr_number` set, `last_push_at` set, report exists and size >= 64 bytes
  - when: `agency ls`
  - then: display includes `ready for review` (unless overridden by needs_attention/setup_failed/etc.)
- given: `flags.setup_failed=true`
  - then: display status is `failed` (even if tmux exists)
- given: `flags.needs_attention=true`
  - then: display status is `needs attention` (even if ready_for_review predicates true)
- given: `archive.merged_at` present (or terminal outcome recorded as merged)
  - then: display status is `merged` (archived suffix if worktree missing)
- given: worktree missing but meta exists
  - then: status shows `(archived)` suffix and presence=archived
status precedence (highest wins):
- terminal outcome `merged|abandoned` always wins
- `setup_failed` (for open runs)
- `needs_attention`
- `ready_for_review`
- else: active/idle variants as currently defined
terminal outcome source:
- merged/abandoned outcomes come only from meta fields written by agency (no gh calls in s2)
report size heuristic:
- constant threshold in v1: 64 bytes
- report is "empty" if missing or bytes < 64
- `report_nonempty` is true if report exists and bytes >= 64
 - template-only reports may exceed 64 bytes; false positives are accepted in v1
ready_for_review predicates:
- `pr_number` present
- `last_push_at` present
- `report_nonempty` (heuristic; can be false-positive/false-negative)
- outcome is open

### transcript capture
- given: tmux session exists for run
  - when: `agency show <id> --capture`
  - then: agency captures full scrollback from the primary pane, strips ANSI escape codes after capture, writes transcript.txt, rotates previous to transcript.prev.txt
- given: tmux session missing
  - when: `agency show <id> --capture`
  - then: show still succeeds, but prints warning “no tmux session; transcript not captured”
capture target:
- the tmux session has exactly one window/pane
- capture via `tmux capture-pane -p -S - -t <target>`
- failure to run tmux command does not fail `show`; warn only

## persistence

### `ls --json` schema (v1)
`{ "schema_version": "1.0", "data": [RunSummary...] }`

RunSummary fields:
- `run_id`
- `repo_id`
- `repo_key`
- `origin_url` (nullable)
- `title`
- `runner`
- `created_at` (rfc3339)
- `last_push_at` (nullable rfc3339)
- `tmux_active` (bool; session exists)
- `worktree_present` (bool; path exists)
- `archived` (bool)
- `pr_number` (nullable)
- `pr_url` (nullable)
- `derived_status` (string)
- `broken` (bool; true if meta unreadable)
repo identity join:
- `repo_key`/`origin_url` are populated by loading `repos/<repo_id>/repo.json`
- if repo.json is missing or corrupt, set `repo_key=null` and `origin_url=null` and continue (do not mark run broken unless meta is broken)
 - same join behavior applies to `show --json`

### `show --json` schema (v1)
`{ "schema_version": "1.0", "data": RunDetail }`

RunDetail fields:
- `meta` (raw parsed meta.json object)
- `repo_id`
- `repo_key`
- `origin_url` (nullable)
- `archived` (bool)
- `derived`:
  - `derived_status` (string)
  - `tmux_active` (bool)
  - `worktree_present` (bool)
  - `report`:
    - `exists` (bool)
    - `bytes` (int)
    - `path` (string)
  - `logs`:
    - `setup_log_path`
    - `verify_log_path`
    - `archive_log_path`
- `paths`:
  - `repo_root`
  - `worktree_root`
  - `run_dir`
  - `events_path`
  - `transcript_path`
- `broken` (bool)

### events.jsonl (introduced here)
location: `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`

schema (each line):
```json
{
  "schema_version": "1.0",
  "timestamp": "rfc3339",
  "repo_id": "...",
  "run_id": "...",
  "event": "cmd_start|cmd_end|script_start|script_end",
  "data": {}
}

emission rules:
	•	any command that takes the repo lock emits `cmd_start`/`cmd_end`
	•	`resume` only takes the lock if it will create/kill a session (detect before locking)
	•	read-only commands without mutating flags (ls, show, attach, doctor) do not emit
	•	script events always emitted when setup/verify/archive run
	•	`show --capture` is mutating: it takes the repo lock and emits `cmd_start`/`cmd_end`

minimum data:
	•	for cmd_start: { "cmd": "...", "args": ["..."] }
	•	for cmd_end: { "cmd": "...", "exit_code": 0, "duration_ms": 123, "error_code": "E_..."? }
	•	for script_*: { "script": "setup|verify|archive", "exit_code": 0, "duration_ms": 123, "timed_out": false }
	•	stop/kill do not emit in s2 (best-effort commands without repo lock)

tests

manual (demo)
	•	create 2 runs, verify ls lists only current repo runs
	•	cd /tmp (outside repo): agency ls lists across repos
	•	create two runs with same prefix collision and confirm E_RUN_ID_AMBIGUOUS
	•	agency show <id> --capture writes transcript files when tmux session exists

automated (minimum)

unit tests (table-driven):
	•	run_id prefix resolution: 0/1/many
	•	ambiguous prefix resolution across repos (`--all-repos` case)
	•	status derivation matrix from synthetic meta + filesystem existence mocks
	•	origin parsing is not required for s2, but repo_key -> repo_id mapping should be stable (reuse s0 helpers)
	•	transcript capture: verify tmux command string/args and ANSI stripping function (pure)

light integration test:
	•	create temp ${AGENCY_DATA_DIR} structure with fake repos/runs/meta.json
	•	run agency ls --all-repos --all --json and assert expected run_ids appear
	•	include one corrupt meta.json and assert broken=true and process exit 0

guardrails
	•	do not change agency.json schema in this slice
	•	do not introduce gh network calls in ls/show
	•	do not add runner pid inspection
	•	do not introduce new indexes beyond scanning existing directory layout
	•	do not mutate meta.json during read-only commands (no “last_seen_at” writes)
	•	do not modify repo_index.json during ls/show

rollout notes
	•	events.jsonl will start appearing for new mutating operations post-s2; existing runs may lack events.jsonl until next mutation.
	•	transcript capture is best-effort; failures must not block show.

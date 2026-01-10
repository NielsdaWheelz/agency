# agency slice 01 — PR-08 report: tmux session creation + `agency attach`

## summary

implemented the `StartTmux` pipeline step and the `agency attach <run_id>` CLI command. the pipeline now creates a detached tmux session running the configured runner after a successful setup, and users can attach to it via the new attach command.

### changes made

1. **`internal/runservice/service.go`**:
   - replaced the stub `StartTmux` implementation with a real tmux session creation
   - added `TmuxSessionPrefix` constant (`agency_`)
   - implemented session collision detection via `tmux has-session`
   - implemented session creation via `tmux new-session -d -s <session> -- sh -lc '<pane_cmd>'`
   - added meta.json update for `tmux_session_name` on success
   - added `flags.tmux_failed` flag setting on failure
   - added guard to prevent tmux creation when setup has failed

2. **`internal/commands/attach.go`** (new file):
   - implemented `Attach` command that:
     - resolves repo root and repo identity
     - loads run metadata
     - verifies tmux session exists
     - attaches to the session interactively

3. **`internal/cli/dispatch.go`**:
   - added `attach` command to CLI dispatcher
   - added usage text and help for attach command
   - added special handling for `E_TMUX_SESSION_MISSING` to print helpful details (worktree path, runner cmd, manual command hint)

4. **`internal/runservice/service_test.go`**:
   - replaced `TestService_StartTmux_NotImplemented` with real implementation tests:
     - `TestService_StartTmux_Success`: verifies successful tmux creation
     - `TestService_StartTmux_SetupFailed`: verifies tmux is not started when setup fails
     - `TestService_StartTmux_SessionExists`: verifies collision detection works

5. **`README.md`**:
   - updated slice 1 progress to mark PR-08 as complete
   - added documentation for `agency attach` command

---

## problems encountered

### 1. tmux colon-to-underscore conversion

**problem**: the spec requires session names in the format `agency:<run_id>`. however, tmux uses colons as special syntax for `session:window.pane` targeting. when you create a session with a colon in the name, tmux automatically converts it to an underscore.

**symptoms**:
- `tmux new-session -d -s 'agency:foo'` creates a session named `agency_foo`
- `tmux has-session -t 'agency:foo'` tries to find window `foo` in session `agency`, not session `agency:foo`
- this caused all session checks to fail, leading to "session exists" errors when trying to create new sessions

### 2. test session cleanup

**problem**: integration tests that create real tmux sessions need proper cleanup. stale sessions from previous test runs could cause collision errors.

**symptoms**:
- tests would fail with "duplicate session" errors when run multiple times

---

## solutions implemented

### 1. session name format change

**solution**: changed the session name prefix from `agency:` to `agency_` (underscore instead of colon).

**rationale**: 
- tmux's internal handling converts colons anyway
- using underscore directly is cleaner and avoids tmux's special syntax interpretation
- all operations (has-session, new-session, attach, kill-session) work correctly with underscores

**implementation**:
- defined `TmuxSessionPrefix = "agency_"` in `runservice/service.go`
- all session operations use this prefix consistently
- updated tests to use the underscore format

### 2. proper test cleanup

**solution**: tests now explicitly clean up tmux sessions after completion using `defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()`.

---

## decisions made

1. **session name format**: used `agency_<run_id>` instead of `agency:<run_id>` due to tmux's special handling of colons. this is a deviation from the spec (see below).

2. **guard against setup failure**: the `StartTmux` step checks if `flags.setup_failed` is set in meta.json before attempting to create a tmux session. if setup failed, it returns `E_TMUX_FAILED` with a clear message.

3. **collision detection first**: before creating a session, we first check if one already exists with `tmux has-session`. this allows us to return a specific error code (`E_TMUX_SESSION_EXISTS`) rather than failing generically.

4. **session existence check in attach**: the attach command verifies that the tmux session actually exists before attempting to attach. this handles cases where the session was killed externally (e.g., system reboot).

5. **interactive attach**: used `os/exec` directly for the attach command rather than the `CommandRunner` interface because `tmux attach` requires proper terminal handling (stdin/stdout/stderr).

---

## deviations from spec

### session name format

**spec says**: `agency:<run_id>`
**implementation uses**: `agency_<run_id>`

**reason**: tmux interprets colons as `session:window.pane` syntax separators. a session name with a colon gets converted to underscore internally by tmux, but operations using the original colon-format name fail because tmux tries to parse them as session:window references.

**impact**: this is a cosmetic change. the session naming is still unique and consistent. users will see `agency_20260110120000-a3f2` in `tmux ls` output instead of `agency:20260110120000-a3f2`.

### no E_RUN_REPO_MISMATCH error

**spec mentions**: `E_RUN_REPO_MISMATCH` for cross-repo attach scenarios
**implementation**: simplified to just `E_RUN_NOT_FOUND`

**reason**: the v1 spec notes "no cross-repo attach in v1; repo-scoped attach only". since attach requires being in the correct repo, and we look up the run under the current repo's `repo_id`, a run from a different repo simply won't be found. implementing explicit cross-repo detection would require scanning all repos, which is out of scope for this PR.

---

## how to run commands

### build

```bash
cd /path/to/agency
go build -o agency ./cmd/agency
```

### run tests

```bash
go test ./...
```

### test tmux functionality manually

```bash
# create a test repo
mkdir /tmp/agency-test && cd /tmp/agency-test
git init
git config user.email "test@example.com"
git config user.name "Test"
echo "# test" > README.md
git add -A && git commit -m "init"

# create agency.json with sh as runner (safe for testing)
cat > agency.json <<'EOF'
{
  "version": 1,
  "defaults": {
    "parent_branch": "main",
    "runner": "sh"
  },
  "scripts": {
    "setup": "scripts/agency_setup.sh",
    "verify": "scripts/agency_verify.sh",
    "archive": "scripts/agency_archive.sh"
  },
  "runners": {
    "sh": "sh"
  }
}
EOF

# create scripts
mkdir -p scripts
echo -e '#!/bin/bash\nexit 0' > scripts/agency_setup.sh
echo -e '#!/bin/bash\nexit 0' > scripts/agency_verify.sh
echo -e '#!/bin/bash\nexit 0' > scripts/agency_archive.sh
chmod +x scripts/*.sh

git add -A && git commit -m "add agency config"

# run doctor to verify setup
agency doctor

# note: agency run is not wired yet (PR-09), so manual pipeline testing required
```

### check/verify new functionality

```bash
# list tmux sessions (should see agency_<run_id> sessions)
tmux ls

# attach to a session manually (for testing)
tmux attach -t agency_<run_id>

# detach from session: press Ctrl-b then d

# kill a session (cleanup)
tmux kill-session -t agency_<run_id>

# use the attach command (once a run exists)
agency attach <run_id>
```

---

## branch name and commit message

**branch name**: `pr08/tmux-session-attach`

**commit message**:

```
feat(s1): implement tmux session creation and attach command (PR-08)

Implement StartTmux pipeline step and agency attach CLI command for
slice 1. This enables running the configured runner (claude/codex) in
a detached tmux session after successful setup.

Changes:
- Replace stub StartTmux with real tmux session creation
- Add session collision detection via tmux has-session
- Create sessions with: tmux new-session -d -s <session> -- sh -lc '<cmd>'
- Update meta.json with tmux_session_name on success
- Set flags.tmux_failed on tmux creation failure
- Add guard to prevent tmux start when setup has failed
- Implement agency attach <run_id> command:
  - Resolve repo root and compute repo_id
  - Load run metadata and verify session exists
  - Attach interactively using tmux attach -t
  - Handle missing session with actionable error message
- Add comprehensive tests for tmux functionality

Deviation from spec:
- Session names use underscore (agency_<run_id>) instead of colon
  (agency:<run_id>) because tmux interprets colons as session:window
  syntax separators and converts them to underscores internally.

Tests: go test ./... passes
```

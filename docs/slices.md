# agency — slice roadmap

this document defines the ordered delivery of user-visible capabilities.
it is about sequencing and dependency, not detailed design.

## slice invariants

- slices add capabilities; they do not redefine earlier ones
- later slices may assume earlier slices are correct
- no slice may require speculative functionality from a future slice

a slice represents a coherent increment that delivers new value and can be built, tested, and reasoned about largely independently.

---

## milestone: v1 — parallel run orchestration

v1 is complete when a user can safely run multiple ai-driven code changes in parallel, inspect them, and merge approved results without corrupting their repo.

v1 consists of slices s0–s2.

---

## s0 — run lifecycle foundation

**goal**

establish the minimal, correct machinery for creating, executing, and cleaning up isolated runs.

**user-visible capability**

a user can:
- start a run in a git repo
- have it execute in an isolated worktree
- see that it is running
- attach to a run's live or completed runner session
- stop or let it finish
- cleanly remove all resources afterward

**scope**

- git worktree creation and teardown
- run id generation
- tmux session per run
- tmux attach/switch-client support per run
- launching an external runner inside the worktree
- passing a prompt and optional read-only file references to the runner
- capturing logs
- basic run state tracking (queued / running / completed / failed / killed)
- explicit cleanup via `agency rm`; no implicit deletion

**explicit non-goals**

- rich status visualization
- merge workflows
- chat systems
- multi-run navigation
- environment bootstrapping beyond the repo itself

**dependencies**

none

**risk / unknowns**

- tmux integration details
- reliable cleanup on failure or interruption

**success criteria**

- multiple runs can execute concurrently
- killing one run never affects another
- worktrees are cleaned up deterministically via explicit `agency rm`
- run completion is determined by runner process exit status

---

## s1 — run inspection and status visualization

**goal**

make runs observable and navigable from the cli.

**user-visible capability**

a user can:
- list all runs
- see each run’s state
- inspect logs
- select a run as the current focus for subsequent commands
- open a run’s worktree in their editor
- view commit history produced by the runner

**scope**

- `agency ls` (summary view)
- `agency show <run_id>` (detailed view)
- log viewing / tailing
- opening worktree paths
- basic commit chain inspection

**explicit non-goals**

- interactive tui dashboards
- filtering/search beyond basics
- cross-run comparisons
- annotations or comments

**dependencies**

- s0

**success criteria**

- a user can quickly understand what is running, what finished, and what failed
- no need to dig through hidden directories to debug a run

---

## s2 — approval, merge, and cleanup

**goal**

complete the v1 loop with explicit human-controlled merge.

**user-visible capability**

a user can:
- review a run’s diff
- explicitly approve it
- merge it into the base branch
- explicitly clean up run resources after merge

**scope**

- `agency diff <run_id>`
- `agency merge <run_id>`
- merge conflict handling (fail + report, not auto-resolve)
- post-merge cleanup (worktree, tmux session, metadata)

**explicit non-goals**

- automatic merges
- automatic rebases, fixups, or history rewriting
- policy engines
- multi-approver workflows
- git host (github) integration beyond local git

**dependencies**

- s0
- s1

**success criteria**

- merges are safe and explicit
- no merged run leaves behind dangling resources
- failed merges are visible and recoverable

---

## milestone: v2 — planning and coordination (placeholder)

details to be defined after v1 stabilization.

---

## post-v1 (intentionally deferred)

these slices are explicitly **not part of v1** and must not leak into v1 work.

- conversations and editable chat history
- structured product/spec state
- multi-actor councils
- provider orchestration
- remote execution / phone clients
- permissions, policies, or auto-approval
- gui or ide-native integrations

these will be planned only after v1 is complete and stable.

---

## ordering summary

s0 → s1 → s2

cleanup means: git worktree removed, tmux session terminated, local metadata marked terminal.

no slice may assume functionality from a later slice.

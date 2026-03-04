# Slice S7: Full-Screen Watch/TUI Seed (Stretch) — Spec

## Goal

Provide a full-screen watch/TUI shell that builds on S3 checks-first terminal contracts.

## Acceptance Criteria

### full-screen watch shell exists as a canonical terminal surface
- **given**: a user is in an interactive terminal with repo context
- **when**: they run `agency watch`
- **then**: Agency opens a full-screen terminal workspace for live monitoring, supports keyboard navigation, and exits cleanly back to the prior shell state

### watch state is composed from daemon-owned read contracts
- **given**: repositories contain many integration worktrees and invocations
- **when**: watch refreshes its workspace view
- **then**: displayed worktree/invocation state comes from canonical daemon read surfaces (not direct CLI store scanning), preserving daemon-first authority and existing sort/filter semantics

### checks-first readiness in watch reuses canonical review truth
- **given**: a user selects an invocation in watch
- **when**: they open readiness/check details
- **then**: watch shows the same readiness verdict, blocking reasons, report diagnostics, and navigation context defined by canonical invocation review behavior, without redefining readiness semantics

### merge-readiness monitoring is visible without leaving the workspace
- **given**: selected invocations have mixed readiness states for progression
- **when**: watch renders invocation detail and summary rows
- **then**: users can distinguish ready vs blocked invocation progression state and see why an invocation is blocked for review/PR/merge flows

### ended headed sessions are handled explicitly and non-destructively
- **given**: a headed invocation whose tmux session has ended
- **when**: a user attempts to enter/attach from watch
- **then**: watch surfaces deterministic session-ended guidance and keeps the watch workspace running instead of collapsing the overall watch session

### watch remains additive to scriptable CLI parity
- **given**: automation uses existing CLI-first machine contracts
- **when**: S7 is delivered
- **then**: existing `agent ... --json` behavior remains the scriptable contract surface, and watch is a human interactive wrapper rather than a replacement contract

### non-interactive usage fails deterministically
- **given**: watch is invoked without an interactive terminal
- **when**: command startup validation runs
- **then**: the command fails with deterministic interactive-required behavior and an actionable recovery hint

## Key Decisions

**TUI is additive and optional, not the baseline product contract**: S7 resolves the direction ambiguity by treating full-screen watch as a thin wrapper on top of existing CLI/daemon behavior. CLI-first parity remains the release baseline for v2.1.

**Engine is snapshot composition over existing contracts**: watch composes workspace state from existing daemon read models for worktrees, invocations, and invocation review/readiness, using periodic refresh rather than introducing a new authority path.

**Checks/readiness truth is owned by canonical review semantics**: watch must render the same deterministic readiness/blocking model already used by invocation review and progression flows; watch cannot introduce a parallel readiness taxonomy.

**Headed tmux lifecycle remains runner-bound**: when runner exit ends the tmux session, S7 does not keep the tmux session alive; watch handles this as a first-class session-ended state with clear next actions.

**Action behavior is delegation-first**: any interactive actions exposed by watch must delegate to canonical agent/worktree behavior and preserve existing confirmation/error contracts, including non-interactive safety expectations.

## Out of Scope

- New daemon readiness or blocking-reason logic beyond existing review/check contracts
- New daemon event-stream infrastructure as a required dependency for S7 seed delivery
- Redefining tmux lifecycle to keep headed sessions alive after runner exit
- Replacing CLI-first scriptable contracts with watch-specific machine interfaces
- GUI/web dashboard scope or full desktop parity
- Merge-queue orchestration or autonomous policy-driven progression beyond current invocation flows

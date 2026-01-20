# Professional Software Development: Documentation Hierarchy

This document defines the gold-standard approach to structuring software projects through layered documentation. Each layer constrains the layer below it, narrowing the solution space until implementation becomes unambiguous.

---

## Foundational Concepts

### What Is Software?

Software is a machine that transforms inputs into outputs. Every program, no matter how complex, is:

```
INPUT → [transformation] → OUTPUT
```

Everything in between is just *how* that transformation happens.

### Why Documents?

Documents are blueprints. They exist so that:
1. **You** don't forget what you're building
2. **Others** (including AI) can help without misunderstanding
3. **Future you** can understand why decisions were made
4. **Changes** don't accidentally break things

### The Document Hierarchy

```
L0: CONSTITUTION (rarely changes)
    ↓ constrains
L1: SLICE ROADMAP (order of work)
    ↓ constrains
L2: SLICE SPECS (contracts per feature)
    ↓ constrains
L3: PR SPECS (exact work units)
    ↓ constrains
L4: CODE (the actual implementation)
```

Each level **narrows the possibilities** for the level below.

### The Visual Funnel

```
┌─────────────────────────────────────────────────────────────┐
│                     L0: CONSTITUTION                        │
│  "We're building a REST API + React SPA. PostgreSQL."      │
│  Constrains: Language, architecture, deployment model       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
        ┌─────────────────────────────────────────┐
        │           L1: SLICE ROADMAP             │
        │  "Auth first, then CRUD, then Search"   │
        │  Constrains: Order, dependencies        │
        └─────────────────────────────────────────┘
                              │
                              ▼
              ┌───────────────────────────────┐
              │        L2: SLICE SPEC         │
              │  "bookmarks table, POST/GET,  │
              │   pagination, error codes"    │
              │  Constrains: Schemas, APIs,   │
              │  interfaces, invariants       │
              └───────────────────────────────┘
                              │
                              ▼
                    ┌───────────────────┐
                    │    L3: PR SPEC    │
                    │  "Add Bookmark    │
                    │   model with      │
                    │   these tests"    │
                    │  Constrains:      │
                    │  Exact code       │
                    └───────────────────┘
                              │
                              ▼
                         ┌────────┐
                         │  CODE  │
                         └────────┘
```

---

## L0: Constitution (also called "Charter" or "System Design")

**Purpose**: Prevent project drift. Lock in irreversible decisions.

**Analogy**: A country's constitution. It doesn't tell you what laws to pass - it tells you what kinds of laws are allowed. It's very hard to change.

**Contains**:
- Goals and explicit non-goals (what we refuse to build)
- System boundaries (what talks to what)
- Trust model (what is trusted vs untrusted)
- Core abstractions (the 3-5 fundamental concepts)
- Irreversible technology choices (language, database, deployment model)
- Cross-cutting conventions (error handling style, logging format, testing patterns)

**Does NOT contain**:
- Specific endpoints
- Database table schemas
- UI flows
- Implementation details

**Decision test**: If this changes, does most of the codebase need to change?

### The Sections of a Gold-Standard Constitution

1. Vision (The "What" and "Why")
- Problem: What pain does this solve? (1-2 sentences)
- Solution: What is this thing? (1-2 sentences)
- Scope: What's included in v1?
- Non-scope: What's explicitly excluded? (Critical - prevents drift)

2. Core Abstractions
- The 3-7 fundamental concepts that everything else builds on
- These become your ubiquitous language - everyone uses these exact terms
- Example for Agency: Run, Workspace, Runner, Session, Repo

3. Architecture
- Components (what are the major pieces?)
- Responsibilities (what does each piece own?)
- Communication (how do pieces talk to each other?)
- Trust boundaries (what trusts what?)

4. Hard Constraints
- Technology choices that cannot change (language, database, etc.)
- Deployment model (local, cloud, hybrid)
- Security model (who can do what)

5. Conventions
- Naming patterns
- Error handling style
- Logging format
- Testing patterns
- File/folder structure rules

6. Invariants
- Rules that must NEVER be violated, system-wide
- These are your "laws of physics"
- Example: "A run cannot be in state 'running' without an active tmux session"

### What Makes a Constitution Good vs Bad

**Bad constitution**:
```
We're building a tool to help developers. It will be fast and reliable.
We'll use modern best practices.
```
This constrains nothing. An engineer could build anything and claim it follows this.

**Good constitution**:
```
Problem: AI coding sessions create messy git state and are hard to track.

Solution: A local CLI that creates isolated worktrees for each AI session,
manages their lifecycle, and handles PR creation/merge.

Non-scope (v1):
- No cloud/remote features
- No sandboxing or containers
- No multi-repo coordination
- No automatic PR approval

Architecture:
- CLI binary (agency) - stateless, handles user commands
- Daemon binary (agencyd) - owns all state, single writer to SQLite
- Communication: Unix domain socket, JSON messages

Conventions:
- All errors: E_CATEGORY_NAME (e.g., E_RUN_NOT_FOUND)
- All timestamps: Unix milliseconds
- All IDs: ULIDs
- CLI always supports --json for machine output
```
This constrains heavily. An engineer cannot deviate without explicitly violating the document.

### The "Non-Scope" Section is the Most Important

Most constitutions fail because they don't say what they're NOT building.

Why non-scope matters:
1. Prevents scope creep ("but wouldn't it be nice if...")
2. Stops AI from hallucinating features
3. Forces hard prioritization decisions upfront
4. Makes "no" easy to say later ("it's in the non-scope")

Good non-scope examples:
- "No web UI in v1"
- "No Windows support in v1"
- "No automatic conflict resolution"
- "No integration with CI/CD systems"
- "No user accounts or authentication"

Each of these is a feature someone will ask for. Having them in non-scope means you've already decided.

### Invariants: Your System's Laws of Physics

Invariants are rules that must never be violated, no matter what.

Good invariants are:
- Testable (you can write code to check them)
- Universal (apply everywhere, not just one feature)
- Protective (violating them would cause serious bugs)

Examples:
- "A run in state 'completed' must have a non-null completed_at timestamp"
- "A workspace directory must not exist for a run in state 'archived'"
- "The daemon is the only process that writes to the database"
- "All API responses include an error_code field on failure"

Why invariants matter:
When debugging, you can check invariants first. If one is violated, you know exactly what category of bug you're looking at.

---

## L1: Slice Roadmap (also called "Milestone Plan" or "Delivery Sequence")

**Purpose**: Order the work. Define dependencies.

**Analogy**: A construction schedule. "Foundation before walls. Walls before roof. Electrical before drywall."

**Contains**:
- Slices (chunks of user-visible value)
- Dependencies between slices
- Acceptance criteria per slice (how do we know it's done?)
- Risk spikes (unknowns we need to investigate early)

**Does NOT contain**:
- How to implement each slice
- Database schemas
- API designs

**Decision test**: If this changes, does the timeline change more than the code?

### What Is a Slice?

A slice is a vertical chunk of user-visible value that can be delivered independently.

Analogy: Slicing a cake vertically, not horizontally.

Horizontal (bad): "First build all the database tables. Then build all the APIs. Then build all the UI."
- Problem: Nothing works until everything works. No feedback until the end.

Vertical (good): "First build login (DB + API + UI). Then build posting (DB + API + UI). Then build search."
- Benefit: Each slice is usable. You get feedback early. You can ship incrementally.

### What Goes in a Roadmap

For each slice:

1. Name and Goal (one sentence)
  - "Slice 0: Bootstrap - User can initialize a repo and verify prerequisites"
2. Outcome (what's true when this slice is done?)
  - "User can run agency init and agency doctor successfully"
3. Dependencies (what must exist first?)
  - "None - this is the foundation"
  - or "Requires Slice 0 (config loading) and Slice 1 (run creation)"
4. Acceptance Criteria (how do we know it's done?)
  - "Running agency doctor checks for git, gh, tmux and reports status"
  - "Running agency init creates agency.json in repo root"
5. Risk/Unknowns (optional - things we need to investigate)
  - "Unknown: How to detect tmux version compatibility"

### What Does NOT Go in a Roadmap

- Database schemas
- API specifications
- Function signatures
- Implementation details
- File structures

The roadmap is purely about ordering and dependencies. All technical details belong in L2 (Slice Spec).

### The Dependency Graph

A good roadmap forms a DAG (Directed Acyclic Graph).

Slice 0: Bootstrap
    │
    ▼
Slice 1: Run ──────────┐
    │                  │
    ▼                  ▼
Slice 2: Observe    Slice 3: Push
    │                  │
    └────────┬─────────┘
              ▼
        Slice 4: Merge

This tells you:
- What can be parallelized (Slice 2 and 3 can be built simultaneously)
- What blocks what (can't build Slice 4 until both 2 and 3 are done)
- Where to start (Slice 0 has no dependencies)

### Slicing Strategies

How do you decide what goes in each slice?

Strategy 1: User Journey
Follow a user through the product. Each major milestone is a slice.
- Slice 0: User can set up
- Slice 1: User can start a session
- Slice 2: User can see what's running
- Slice 3: User can publish their work
- Slice 4: User can finish and clean up

Strategy 2: Risk-First
Put the scariest, most uncertain things early. If they fail, you fail fast.
- "We're not sure if the tmux integration will work - do that in Slice 1"

Strategy 3: Value-First
Put the most valuable features early. Ship value to users ASAP.
- "Users care most about starting sessions - that's Slice 1"

Usually you combine all three: Valuable + risky things go first. Easy + low-value things go last.

### Good vs Bad Roadmap

**Bad roadmap**:
```
Phase 1: Backend
Phase 2: Frontend
Phase 3: Testing
Phase 4: Polish
```

**Problems**:
- Horizontal, not vertical (nothing works until Phase 3)
- No acceptance criteria
- No dependencies specified
- "Polish" is not a slice, it's procrastination

**Good roadmap**:
```
Slice 0: Bootstrap
  Goal: User can initialize repo and verify prerequisites
  Outcome: `agency init` and `agency doctor` work
  Dependencies: None
  Acceptance:
    - `agency init` creates agency.json
    - `agency doctor` checks git, gh, tmux, reports pass/fail

Slice 1: Run
  Goal: User can create and enter an AI coding session
  Outcome: `agency run` creates worktree, starts tmux, launches runner
  Dependencies: Slice 0 (config must load)
  Acceptance:
    - `agency run` creates new worktree from parent branch
    - tmux session starts with runner inside
    - `agency attach` connects to session
```

### The "Walking Skeleton" Pattern

A powerful slicing technique: Build a walking skeleton first.

A walking skeleton is the thinnest possible end-to-end path through your system.

For Agency:
- Slice 0 creates config
- Slice 1 creates a worktree, starts tmux, runs a fake runner that just echoes "hello"
- Slice 3 pushes and creates a PR

This skeleton doesn't do anything useful, but it proves all the pieces connect. Then you flesh out each piece.

---

## L2: Slice Spec (also called "Feature Contract" or "Module Spec")

**Purpose**: Define what multiple PRs must agree on. Prevent parallel work from colliding.

**Analogy**: The electrical blueprint for one room. It shows where every outlet goes, what voltage, what wire gauge. Any electrician can wire it without asking questions.

**Contains**:
- Exact API contracts (request/response shapes)
- Exact database schema for tables touched by this slice
- State machines (what states exist, what transitions are legal)
- Error codes (what can go wrong, and how we report it)
- Invariants (rules that must never be violated)
- Acceptance scenarios (given X, when Y, then Z)

**Does NOT contain**:
- Internal helper function signatures
- Which files to create
- Implementation choices (like "use library X")

**Decision test**: If this changes, would multiple PRs break?

### Why Does L2 Exist?

Imagine two engineers working on the same slice:
- Engineer A builds the API endpoint
- Engineer B builds the database queries

If they don't agree on:
- What the table columns are called
- What the request/response shapes are
- What errors can occur
- What states are valid

...their code won't fit together.

L2 is the agreement. It's the contract that says "we will meet at this exact interface."

### The Sections of a Gold-Standard Slice Spec

1. Goal & Scope
Goal: Enable users to create and manage AI coding sessions.

In Scope:
- Creating runs with worktrees
- Starting tmux sessions
- Launching runners
- Tracking run state

Out of Scope:
- GitHub integration (that's Slice 3)
- Viewing run history (that's Slice 2)

2. Domain Models

The exact data structures, with every field specified.

Run:
  id: TEXT (ULID, primary key)
  repo_id: TEXT (foreign key)
  state: TEXT (enum: queued | running | completed | failed | killed)
  title: TEXT (nullable)
  parent_branch: TEXT (not null)
  runner: TEXT (not null, e.g., "claude-code")
  created_at: INTEGER (unix ms, not null)
  started_at: INTEGER (unix ms, nullable)
  completed_at: INTEGER (unix ms, nullable)
  failure_reason: TEXT (nullable)

This is exact. Not "a run has some metadata." Every field, every type, every constraint.

3. State Machine

If your domain has states, specify every valid transition.

Run State Machine:

  ┌─────────┐
  │ queued  │
  └────┬────┘
        │ start()
        ▼
  ┌─────────┐
  │ running │
  └────┬────┘
        │
        ├── complete() ──► completed
        │
        ├── fail() ──────► failed
        │
        └── kill() ──────► killed

Illegal transitions:
- completed → running (cannot restart finished run)
- failed → completed (cannot succeed after failure)
- queued → completed (must go through running)

4. API Contracts

Every endpoint, fully specified.

POST /runs
  Auth: None (local daemon)

  Request:
    {
      "repo_path": string (required, absolute path),
      "title": string (optional),
      "runner": string (optional, defaults to config),
      "parent_branch": string (optional, defaults to current)
    }

  Response 200:
    {
      "run_id": string (ULID),
      "worktree_path": string,
      "state": "queued"
    }

  Response 400 (E_INVALID_REQUEST):
    { "error_code": "E_INVALID_REQUEST", "message": string }

  Response 404 (E_REPO_NOT_FOUND):
    { "error_code": "E_REPO_NOT_FOUND", "message": string }

  Response 409 (E_REPO_LOCKED):
    { "error_code": "E_REPO_LOCKED", "message": string }

5. Error Codes

Every error that can occur in this slice.

E_REPO_NOT_FOUND: repo_path does not exist or is not a git repo
E_NO_AGENCY_JSON: repo has no agency.json config file
E_INVALID_CONFIG: agency.json failed validation
E_PARENT_DIRTY: parent branch has uncommitted changes
E_WORKTREE_EXISTS: worktree already exists for this run
E_TMUX_FAILED: failed to create tmux session
E_RUNNER_FAILED: runner process exited unexpectedly

6. Invariants

Rules that must always hold within this slice.

- A run in state 'running' MUST have a non-null started_at
- A run in state 'running' MUST have an active tmux session
- A run in state 'completed' MUST have a non-null completed_at
- A run in state 'failed' MUST have a non-null failure_reason
- A worktree directory MUST NOT exist for a run in state 'killed'

7. Acceptance Scenarios

Concrete test cases at the slice level.

```
Scenario: Successful run creation
  Given: A valid repo with agency.json
  And: Parent branch is clean
  When: User calls `agency run --title "fix bug"`
  Then: A new run is created in state 'queued'
  And: A worktree is created at ~/.agency/worktrees/{repo}/{run_id}
  And: A tmux session is started
  And: The runner is launched inside the tmux session
  And: Run transitions to state 'running'

Scenario: Run creation with dirty parent
  Given: A valid repo with agency.json
  And: Parent branch has uncommitted changes
  When: User calls `agency run`
  Then: Error E_PARENT_DIRTY is returned
  And: No run is created
  And: No worktree is created
```

### What Does NOT Go in a Slice Spec

- Implementation details ("use library X")
- File paths ("put this in src/services/run.rs")
- Internal helper functions
- Performance optimizations
- Anything only one PR needs to know

### The "Multiple PRs" Test

For every item in your slice spec, ask:

"Do multiple PRs need to agree on this?"

- Table schema → Yes, the PR that creates the table and the PR that queries it must agree → Include
- Function signature for public API → Yes, caller and callee must agree → Include
- Internal helper function → No, only one file uses it → Exclude
- Error code → Yes, thrower and catcher must agree → Include
- Variable name inside a function → No → Exclude

### Common Slice Spec Mistakes

Mistake 1: Duplicating the Constitution
Don't restate "we use JSON for APIs." That's in L0. Just follow it.

Mistake 2: Over-specifying implementation
Don't say "use a HashMap for caching." That's an implementation detail. Say "lookups must be O(1)" if performance matters.

Mistake 3: Under-specifying errors
"Returns an error on failure" is useless. What error? What code? When exactly?

Mistake 4: Vague state machines
"The run can be in various states" is not a spec. List every state and every legal transition.

Mistake 5: Missing invariants
If you don't write invariants, you'll discover them as bugs later.

---

## L3: PR Spec (also called "Task Spec" or "Work Unit")

**Purpose**: Make one PR trivially reviewable and low-risk.

**Analogy**: A single work order. "Install outlet #3 at position (x,y). Use 12-gauge wire. Connect to circuit B. Test with multimeter."

**Contains**:
- Goal (one sentence)
- Exact public surface being added (function signature, endpoint, table column)
- Acceptance tests (specific inputs → expected outputs)
- Constraints (what files may be touched)
- Non-goals (what this PR explicitly does NOT do)

**Does NOT contain**:
- Restating the whole slice spec
- Architecture decisions
- Long explanations

**Decision test**: If this changes, does it only invalidate this branch?

### Why Not Just Write Code?

Because without a PR spec:
- You might build more than needed (scope creep)
- You might build less than needed (incomplete)
- You might build the wrong thing (misunderstanding)
- An AI will hallucinate features
- Reviewers don't know what to check

A PR spec is a contract with yourself (or with an AI) about exactly what this PR delivers.

### The Sections of a Gold-Standard PR Spec

1. Goal (one sentence)

Goal: Add the `parse_config` function that loads and validates agency.json.

Not two things. Not vague. One specific deliverable.

2. Context (what already exists)

Context:
- Config schema is defined in s0_spec.md
- Error types E_NO_AGENCY_JSON and E_INVALID_CONFIG exist in errors.rs
- Paths module exists with `config_path()` function

This tells the implementer what they can use.

3. Deliverables (exact outputs)

Deliverables:
- New file: crates/agency-core/src/config.rs
- New function: pub fn parse_config(path: &Path) -> Result<Config, ConfigError>
- New struct: pub struct Config { ... } matching schema in s0_spec.md
- New enum: pub enum ConfigError { NotFound, Invalid(String) }
- Re-export from lib.rs

Exact files. Exact function signatures. Exact types.

4. Acceptance Tests (exact inputs → outputs)

Tests (in crates/agency-core/tests/config_test.rs):

Test: parse_config_valid
  Input: valid agency.json with all required fields
  Output: Ok(Config { version: "1", ... })

Test: parse_config_missing_file
  Input: path to non-existent file
  Output: Err(ConfigError::NotFound)

Test: parse_config_invalid_json
  Input: file containing "not json"
  Output: Err(ConfigError::Invalid(_))

Test: parse_config_missing_required_field
  Input: agency.json without "version" field
  Output: Err(ConfigError::Invalid("missing field: version"))

These are exact. Not "test that it works." Exact inputs, exact outputs.

5. Non-Goals (what this PR does NOT do)

Non-goals:
- Does NOT implement config file creation (that's PR-03)
- Does NOT implement config migration (out of scope for v1)
- Does NOT validate runner configurations (that's PR-04)
- Does NOT watch for config changes (not needed)

This is critical. It tells the implementer where to stop.

6. Constraints (rules to follow)

Constraints:
- Only modify files in crates/agency-core/
- Do not add new dependencies
- Follow existing error pattern (see errors.rs)
- No panics - all errors must be Result

This limits what the implementer can do.

### The Art of Scoping a PR

How big should a PR be?

Rules of thumb:
- Reviewable in 15 minutes (under 400 lines changed)
- One logical unit (not "add X and also refactor Y")
- Independently testable (can write tests without other PRs)
- Independently deployable (ideally - won't break if merged alone)

Too big:
PR: Implement the run command
This is a whole slice, not a PR.

Too small:
PR: Add import statement for serde
This isn't meaningful on its own.

Just right:
PR: Add Run struct and state transition methods
PR: Add runs table schema and migrations
PR: Add create_run service function
PR: Add POST /runs endpoint

Each is one logical unit. Each is testable. Each builds on the previous.

### PR Dependency Chains

PRs within a slice often form a chain:

PR-01: Add domain types (Run, RunState, etc.)
    │
    ▼
PR-02: Add database schema and queries
    │
    ▼
PR-03: Add service layer (create_run, get_run, etc.)
    │
    ▼
PR-04: Add API endpoints (POST /runs, GET /runs/:id)
    │
    ▼
PR-05: Add CLI commands (agency run, agency show)

This is a layered approach:
1. Types (no dependencies)
2. Storage (depends on types)
3. Business logic (depends on storage)
4. API (depends on business logic)
5. UI/CLI (depends on API)

Each layer only depends on the layer above it.

### PR Specs for AI (Claude)

When writing a PR spec for an AI to implement, add:

Explicit boundaries:
DO:
- Create the files listed in Deliverables
- Implement the exact function signatures shown
- Write the exact tests specified

DO NOT:
- Create additional helper functions beyond what's specified
- Add error handling beyond what's specified
- Refactor existing code
- Add documentation beyond basic doc comments
- Implement anything marked as non-goal

Example of existing code (if relevant):
Reference - existing error pattern in errors.rs:

#[derive(Debug, thiserror::Error)]
pub enum CoreError {
    #[error("E_NO_REPO: {0}")]
    NoRepo(String),
}

Follow this exact pattern for ConfigError.

Completion checklist:
Checklist (all must be true before PR is complete):
- [ ] All tests pass
- [ ] No compiler warnings
- [ ] cargo fmt has been run
- [ ] cargo clippy shows no errors
- [ ] Only files in Deliverables were modified

### Common PR Spec Mistakes

Mistake 1: Vague deliverables
Bad: "Add config parsing functionality"
Good: "Add pub fn parse_config(path: &Path) -> Result<Config, ConfigError>"

Mistake 2: Missing test cases
Bad: "Add tests"
Good: "Test: input X → output Y; Test: input A → error B"

Mistake 3: No non-goals
Without non-goals, scope creeps. The implementer adds "helpful" features you didn't want.

Mistake 4: Depending on unmerged PRs
Bad: "Uses the Config type from PR-02" (PR-02 not merged yet)
Good: "Uses the Config type from config.rs" (already exists)

PR specs should reference what exists, not what's planned.

Mistake 5: Multiple unrelated changes
Bad: "Add config parsing and also fix the logging format"
Good: "Add config parsing" (logging fix is a separate PR)

### The Complete PR Spec Template

```markdown
# PR-02: Config Parsing

## Goal
Add the `parse_config` function that loads and validates agency.json.

## Context
- Config schema defined in slice_spec.md section 4.2
- Error types defined in slice_spec.md section 5.1
- Path resolution exists in `crates/agency-core/src/paths.rs`

## Dependencies
- PR-01 must be merged (provides base types)

---

## Files to Create

### crates/agency-core/src/config.rs

```rust
// This file handles loading and parsing agency.json configuration.

use std::path::Path;
use serde::Deserialize;
use crate::errors::ConfigError;

/// Runtime configuration loaded from agency.json
#[derive(Debug, Clone, Deserialize)]
pub struct Config {
    /// Schema version, must be "1"
    pub version: String,

    /// Default runner to use when --runner not specified
    #[serde(default)]
    pub default_runner: Option<String>,

    /// Available runner configurations
    #[serde(default)]
    pub runners: Vec<RunnerConfig>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RunnerConfig {
    /// Unique name for this runner (e.g., "claude-code")
    pub name: String,

    /// Command to execute
    pub command: String,

    /// Arguments to pass to command
    #[serde(default)]
    pub args: Vec<String>,
}

/// Parse and validate agency.json from the given path.
///
/// # Arguments
/// * `path` - Absolute path to agency.json file
///
/// # Returns
/// * `Ok(Config)` - Successfully parsed and validated config
/// * `Err(ConfigError::NotFound)` - File does not exist
/// * `Err(ConfigError::ReadError)` - File exists but cannot be read
/// * `Err(ConfigError::ParseError)` - File is not valid JSON
/// * `Err(ConfigError::ValidationError)` - JSON is valid but fails validation
///
/// # Validation Rules
/// 1. `version` must equal "1" (string, not integer)
/// 2. If `default_runner` is set, it must match a runner name in `runners`
/// 3. Each runner must have non-empty `name` and `command`
/// 4. Runner names must be unique
pub fn parse_config(path: &Path) -> Result<Config, ConfigError> {
    // Implementation goes here
}

---
Files to Modify

crates/agency-core/src/lib.rs

Add:
pub mod config;
pub use config::{Config, RunnerConfig, parse_config};

crates/agency-core/src/errors.rs

Add to existing ConfigError enum:
#[derive(Debug, thiserror::Error)]
pub enum ConfigError {
    #[error("E_CONFIG_NOT_FOUND: config file not found at {0}")]
    NotFound(String),

    #[error("E_CONFIG_READ_ERROR: failed to read config: {0}")]
    ReadError(String),

    #[error("E_CONFIG_PARSE_ERROR: invalid JSON: {0}")]
    ParseError(String),

    #[error("E_CONFIG_VALIDATION_ERROR: {0}")]
    ValidationError(String),
}

---
Function Specifications

parse_config

Signature:
pub fn parse_config(path: &Path) -> Result<Config, ConfigError>

Input:
| Parameter | Type  | Constraints                                  |
|-----------|-------|----------------------------------------------|
| path      | &Path | Must be absolute path. May or may not exist. |

Output:
| Condition                     | Return                                                                         |
|-------------------------------|--------------------------------------------------------------------------------|
| File doesn't exist            | Err(ConfigError::NotFound(path.display().to_string()))                         |
| File can't be read            | Err(ConfigError::ReadError(io_error.to_string()))                              |
| Invalid JSON syntax           | Err(ConfigError::ParseError(serde_error.to_string()))                          |
| version != "1"                | Err(ConfigError::ValidationError("version must be \"1\""))                     |
| default_runner not in runners | Err(ConfigError::ValidationError("default_runner \"X\" not found in runners")) |
| runner has empty name         | Err(ConfigError::ValidationError("runner name cannot be empty"))               |
| runner has empty command      | Err(ConfigError::ValidationError("runner command cannot be empty"))            |
| duplicate runner name         | Err(ConfigError::ValidationError("duplicate runner name: X"))                  |
| All validation passes         | Ok(Config { ... })                                                             |

Internal Logic (pseudocode):
1. Check if path exists → NotFound if not
2. Read file contents → ReadError if fails
3. Parse JSON into Config → ParseError if fails
4. Validate version == "1" → ValidationError if not
5. If default_runner is Some:
    - Check it exists in runners list → ValidationError if not
6. For each runner:
    - Check name is non-empty → ValidationError if empty
    - Check command is non-empty → ValidationError if empty
7. Check runner names are unique → ValidationError if duplicate
8. Return Ok(config)

---
Test Specifications

File: crates/agency-core/tests/config_test.rs

Test fixtures (create in tests/fixtures/):

valid_minimal.json:
{
  "version": "1"
}

valid_full.json:
{
  "version": "1",
  "default_runner": "claude",
  "runners": [
    {"name": "claude", "command": "claude-code", "args": ["--yes"]},
    {"name": "codex", "command": "codex", "args": []}
  ]
}

invalid_json.json:
not valid json {{{

wrong_version.json:
{
  "version": "2"
}

missing_version.json:
{
  "runners": []
}

bad_default_runner.json:
{
  "version": "1",
  "default_runner": "nonexistent",
  "runners": []
}

empty_runner_name.json:
{
  "version": "1",
  "runners": [{"name": "", "command": "test"}]
}

duplicate_runner.json:
{
  "version": "1",
  "runners": [
    {"name": "dupe", "command": "a"},
    {"name": "dupe", "command": "b"}
  ]
}

Test cases:

| Test Name                     | Input                   | Expected Output                                                                     |
|-------------------------------|-------------------------|-------------------------------------------------------------------------------------|
| test_parse_valid_minimal      | valid_minimal.json      | Ok(Config { version: "1", default_runner: None, runners: [] })                      |
| test_parse_valid_full         | valid_full.json         | Ok(Config { version: "1", default_runner: Some("claude"), runners: [..2 items..] }) |
| test_parse_not_found          | nonexistent.json        | Err(ConfigError::NotFound(_))                                                       |
| test_parse_invalid_json       | invalid_json.json       | Err(ConfigError::ParseError(_))                                                     |
| test_parse_wrong_version      | wrong_version.json      | Err(ConfigError::ValidationError(_)) containing "version"                           |
| test_parse_missing_version    | missing_version.json    | Err(ConfigError::ParseError(_)) (serde requires it)                                 |
| test_parse_bad_default_runner | bad_default_runner.json | Err(ConfigError::ValidationError(_)) containing "not found"                         |
| test_parse_empty_runner_name  | empty_runner_name.json  | Err(ConfigError::ValidationError(_)) containing "empty"                             |
| test_parse_duplicate_runner   | duplicate_runner.json   | Err(ConfigError::ValidationError(_)) containing "duplicate"                         |

---
Non-Goals (DO NOT IMPLEMENT)

- Config file creation or init command (PR-04)
- Config file watching or hot-reload (not in v1)
- Environment variable substitution in config (not in v1)
- Config migration between versions (not in v1)
- Runner validation beyond name/command non-empty (PR-05)
- Default values beyond what serde provides (keep it simple)

---
Constraints

- Only modify/create files listed above
- No new dependencies beyond serde, thiserror (already in Cargo.toml)
- No panics - all errors return Result
- No unwrap() except in tests
- No println! or dbg! (use tracing if logging needed, but probably not needed)
- Follow existing code style in the crate

---
Completion Checklist

- All files created/modified as specified
- All test fixtures created
- All 9 tests pass
- cargo build succeeds with no warnings
- cargo test passes
- cargo fmt produces no changes
- cargo clippy produces no warnings
- No files modified beyond those listed
```

---

### The Principle: Constrain the Generation Space

Notice what this PR spec does:

| Aspect | How It's Constrained |
|--------|---------------------|
| **What files exist** | Exact file paths listed |
| **What types exist** | Exact struct/enum definitions with all fields |
| **What functions exist** | Exact signatures with full doc comments |
| **What the function does** | Input → Output table, no ambiguity |
| **What logic to use** | Pseudocode showing exact steps |
| **What tests exist** | Exact test names, inputs, outputs |
| **What NOT to do** | Explicit non-goals |
| **What rules to follow** | Explicit constraints |
| **When it's done** | Explicit checklist |

An AI or junior reading this has **no room to improvise**. They can only produce what you specified.

### When to Use Full vs Light Specification

| Implementer | Specification Level |
|-------------|---------------------|
| Senior engineer you trust | Light (goal, deliverables, non-goals) |
| Junior engineer | Medium (add function signatures, test cases) |
| AI (Claude, etc.) | Full (everything above) |
| Yourself in 6 months | Medium to Full (you'll forget context) |

### The Critical Insight - Negative Space

Here's something most beginners miss:

What you explicitly exclude is as important as what you include.

Every document should answer:
1. What is this responsible for? (positive scope)
2. What is this NOT responsible for? (negative scope)

Why? Because without negative scope:
- Engineers invent features you didn't ask for
- AI hallucinates behavior
- Scope creeps invisibly
- When something breaks, nobody knows whose fault it is

Example of good negative scope:
"This subsystem handles authentication. It does NOT handle authorization (that's handled by the permissions subsystem)."

Now if there's a bug where users can access resources they shouldn't, you know immediately: it's not an auth bug, it's a permissions bug.

---

## Quick Reference

### Summary Table

| Level | Contains | Does NOT Contain | Changes When |
|-------|----------|------------------|--------------|
| **L0: Constitution** | Language, architecture, conventions, boundaries, non-goals | Schemas, APIs, file structure | Almost never |
| **L1: Roadmap** | Slice order, dependencies, milestones | How to build each slice | Priorities shift |
| **L2: Slice Spec** | Exact schemas, APIs, state machines, errors, invariants | Implementation details, file names | Learning during slice |
| **L3: PR Spec** | Exact functions, files, tests, constraints | Anything outside this PR | Never (deleted after merge) |

### Scope vs Content

| Level | Scope | Content Type |
|-------|-------|--------------|
| L0 | Whole system | Conventions, boundaries, architecture |
| L1 | Whole system | Ordering and dependencies (NO technical details) |
| L2 | One feature | Technical contracts (schemas, APIs, errors) |
| L3 | One PR | Exact implementation details |

### The Ownership Test

Who would need to approve a change?

| Level | Who Approves Changes? |
|-------|----------------------|
| Constitution | Whole team / tech lead (major discussion) |
| Slice Roadmap | Product + tech lead |
| Slice Spec | Engineers on that slice |
| PR Spec | The individual engineer |

### The Blast Radius Test

If you make a mistake at each level, how much do you redo?

| Level | Blast Radius of a Mistake |
|-------|--------------------------|
| Constitution | Entire project |
| Slice Roadmap | Timeline, possibly multiple slices |
| Slice Spec | Multiple PRs within the slice |
| PR Spec | One branch |

### Decision Tests

- **L0 vs L2**: Does this affect *all* features? → L0. Does this affect *one* feature? → L2.
- **L2 vs L3**: Do multiple PRs need to agree on this? → L2. Is this internal to one PR? → L3.
- **L1 is special**: It spans the whole system but contains *zero* technical specifications. Only "what order?" and "what depends on what?"

---

## Constraining the Generation Space (For LLM-Assisted Development)

### The Fundamental Principle

An LLM is a completion engine. Given context, it generates the most probable continuation.

P(output | context)

Your job is to craft context such that the only probable output is exactly what you want.

Too little context → LLM hallucinates, invents, guesses
Too much context → LLM gets confused, contradicts itself, loses focus
Wrong context → LLM produces confidently wrong output

The goal: Minimum viable context that uniquely determines the correct output.

### The Information Hierarchy

At each layer, you're constraining the layer below:

L0 (Constitution)     → Constrains all LLM interactions on this project
L1 (Roadmap)          → Constrains which work happens when
L2 (Slice Spec)       → Constrains all PRs in this slice
L3 (PR Spec)          → Constrains this specific generation
L4 (Prompt)           → The actual context window sent to the LLM

Key insight: Each layer is a compression of the layers above.

The LLM never sees L0, L1, L2 directly. It sees L4 (the prompt), which summarizes and references the relevant parts of L0-L3.

### The Prompt Architecture

A prompt to an LLM has this structure:

┌─────────────────────────────────────────────────┐
│  IDENTITY: Who are you? What's your role?       │
├─────────────────────────────────────────────────┤
│  CONTEXT: What already exists? What are the     │
│  rules? What decisions are already made?        │
├─────────────────────────────────────────────────┤
│  TASK: What specifically should you produce?    │
├─────────────────────────────────────────────────┤
│  CONSTRAINTS: What must you NOT do?             │
├─────────────────────────────────────────────────┤
│  FORMAT: What shape should the output take?     │
├─────────────────────────────────────────────────┤
│  EXAMPLES: What does good output look like?     │
└─────────────────────────────────────────────────┘

Each section narrows the generation space.

### How Each Section Constrains

IDENTITY narrows: Who is speaking?
Bad:  "You are a helpful assistant."
Good: "You are a TypeScript backend engineer building a REST API.
        You write clean, type-safe code. You never use 'any'.
        You handle all errors explicitly with proper HTTP status codes."

CONTEXT narrows: What's the world state?
Bad:  "We're building a bookmark app."
Good: "You are working in a Node.js/Express project with this structure:
        src/
          models/        # TypeScript interfaces and DB mappers
          services/      # Business logic
          routes/        # Express route handlers
          middleware/    # Auth, validation, error handling

        The following types already exist in src/models/bookmark.model.ts:
        [paste exact type definitions]

        The following error types exist in src/errors/index.ts:
        [paste exact error definitions]

        Authentication middleware extracts userId from JWT:
        [paste middleware signature]"

TASK narrows: What's the output?
Bad:  "Add bookmark creation."
Good: "Create the file src/services/bookmark.service.ts containing:
        1. Function `createBookmark(input: CreateBookmarkInput): Promise<Bookmark>` that:
          - Validates URL is http/https
          - Checks for duplicate URL for this user
          - Inserts into database
          - Returns the created bookmark with generated ID
        2. Throws BookmarkError.DUPLICATE_URL if URL already exists
        3. Throws BookmarkError.INVALID_URL if URL is malformed"

CONSTRAINTS narrows: What's forbidden?
"DO NOT:
  - Add any functions not listed above
  - Create any files not listed above
  - Add dependencies to Cargo.toml
  - Use .unwrap() or .expect() outside of tests
  - Add features for 'later' or 'future use'
  - Implement validation beyond what's specified
  - Add logging, metrics, or tracing
  - Refactor existing code"

FORMAT narrows: What shape?
"Output format:
  - Provide the complete file contents for each file
  - Use ```rust code blocks
  - Do not include explanatory text between files
  - Do not summarize what you did
  - Just output the code"

EXAMPLES narrows: What does good look like?
"Example of existing error pattern to follow:

#[derive(Debug, thiserror::Error)]
pub enum CoreError {
    #[error("E_NO_REPO: {0}")]
    NoRepo(String),
}

Your ConfigError should follow this exact pattern."

### The Layer-by-Layer Application

L0 (Constitution) → System Prompt / Persistent Context

How you use it:
- Extract the relevant conventions and constraints
- Include them in every prompt as "project rules"
- Never make the LLM read the whole constitution

Project Rules (from Constitution):
- Language: Rust, edition 2021
- All errors use pattern E_CATEGORY_NAME
- All timestamps are Unix milliseconds
- No panics in production code
- CLI supports --json flag on all commands

L1 (Roadmap) → Not directly sent to LLM

The roadmap is for humans. The LLM doesn't need to know "Slice 2 comes after Slice 1." It just needs to know what to build right now.

L2 (Slice Spec) → Reference Material

How you use it:
- Extract the exact schemas, APIs, state machines relevant to this PR
- Paste them verbatim (don't paraphrase - LLMs do better with exact text)
- Say "implement according to this spec"

From Slice Spec (implement exactly):

Run State Machine:
  queued → running (on start)
  running → completed (on success)
  running → failed (on error)
  running → killed (on kill signal)

Table Schema:
  runs (
    id TEXT PRIMARY KEY,
    state TEXT NOT NULL CHECK(state IN ('queued','running','completed','failed','killed')),
    created_at INTEGER NOT NULL,
    ...
  )

L3 (PR Spec) → The Core of the Prompt

This is the main content. Paste the full PR spec or its key sections.

L4 (Prompt) → The Assembled Context

The prompt is the assembly of relevant pieces from all layers:

[Identity]
You are a Rust backend engineer working on the Agency project.

[Project Rules - from L0]
- All errors use E_CATEGORY_NAME pattern
- No unwrap() in production code
- ...

[Current State - from codebase]
These files already exist:
- crates/agency-core/src/lib.rs
- crates/agency-core/src/errors.rs (contents: ...)
- crates/agency-core/src/types.rs (contents: ...)

[Spec to Implement - from L2/L3]
[Paste the schema, API contract, etc.]

[Task - from L3]
Create these files with these exact contents...

[Constraints - from L3]
DO NOT...

[Format]
Output only the code, no explanations.

[Checklist]
Verify before responding:
- [ ] All files match spec exactly
- [ ] All tests are included
- [ ] No extra code beyond spec

### The Art of Context Selection

What to include:

| Include                   | Why                           |
|---------------------------|-------------------------------|
| Exact type definitions    | LLM will match them precisely |
| Exact function signatures | No room for invention         |
| Exact error codes         | Consistent error handling     |
| Example code patterns     | LLM imitates style            |
| Explicit non-goals        | Prevents scope creep          |
| Explicit constraints      | Prevents bad patterns         |

What to exclude:

| Exclude             | Why                                                    |
|---------------------|--------------------------------------------------------|
| Full constitution   | Too long, dilutes focus                                |
| Other slices' specs | Irrelevant, confusing                                  |
| Historical context  | "We used to do X" is noise                             |
| Justifications      | "We chose Rust because..." doesn't help implementation |
| Future plans        | "Later we'll add..." invites premature implementation  |

### The Reference Pattern

Don't make the LLM read everything. Extract and paste the relevant parts.

Bad:
"Read the constitution at docs/v1/constitution.md and follow all conventions."
(LLM might not have access, might misinterpret, might get lost)

Good:
"Follow these conventions (from project constitution):
  1. Errors: E_CATEGORY_NAME pattern
  2. Timestamps: Unix milliseconds
  3. IDs: ULIDs
  4. No panics in production"
(Exact rules, inline, no ambiguity)

### The Layered Validation Pattern

After generation, validate at each layer:

L0 Check: Does it follow conventions?
  - Error patterns correct?
  - Naming patterns correct?
  - No forbidden patterns (panics, unwrap)?

L2 Check: Does it match the slice spec?
  - Schema matches exactly?
  - State machine implemented correctly?
  - All error codes present?

L3 Check: Does it match the PR spec?
  - All deliverables present?
  - All tests present?
  - No extra files?
  - No extra functions?

### The Prompt Template (Gold Standard)

# Context

You are implementing PR-{N} for the Agency project.

## Project Conventions
{Extract from L0 - 5-10 bullet points max}

## Existing Code
{Paste relevant existing files or excerpts}

## Specification
{Paste from L2 - exact schemas, state machines, APIs}

---

# Task

{Paste from L3 - exact deliverables}

---

# Constraints

DO:
{List of requirements}

DO NOT:
{List of prohibitions}

---

# Output Format

{Exact format requirements}

---

# Verification

Before outputting, verify:
{Checklist}

### The Meta-Principle

Every piece of context should either:
1. Narrow what the LLM can output, or
2. Provide information required to produce correct output

If a piece of context does neither, remove it. It's noise.

---

## The Development Workflow

We've covered what each document contains. Now let's cover how they flow together in practice.

### The Lifecycle of a Feature

```
1. IDEA
    ↓
2. Update Constitution (if needed - rare)
    ↓
3. Add to Roadmap (create a new slice)
    ↓
4. Write Slice Spec
    ↓
5. Break into PRs (PR Roadmap)
    ↓
6. Write PR Specs
    ↓
7. Implement PRs
    ↓
8. Review & Merge
    ↓
9. Update docs if reality diverged from spec
```

### When Each Document Gets Created

| Document     | Created When                                  | By Whom                 |
|--------------|-----------------------------------------------|-------------------------|
| Constitution | Project inception                             | Tech lead / Architect   |
| Roadmap      | Project inception, updated per planning cycle | Product + Tech lead     |
| Slice Spec   | When starting work on a slice                 | Engineers on that slice |
| PR Spec      | Immediately before implementation             | Engineer doing the PR   |

### When Each Document Gets Updated

| Document     | Updated When                                 |
|--------------|----------------------------------------------|
| Constitution | Almost never. Major pivots only.             |
| Roadmap      | When priorities shift (monthly/quarterly)    |
| Slice Spec   | When implementation reveals spec was wrong   |
| PR Spec      | Never. It's disposable. Deleted after merge. |

---

## The Spec-First Discipline

### The Golden Rule

Never write code that isn't specified.

This sounds rigid. It is. Here's why:

Without spec-first:
1. Start coding
2. Discover edge case
3. Make a decision on the spot
4. Forget the decision
5. Later, another engineer hits same edge case
6. Makes a different decision
7. System is now inconsistent
8. Bug reports
9. Nobody knows what the "correct" behavior is

With spec-first:
1. Write spec, including edge cases
2. Discover edge case while writing spec
3. Make a decision, document it
4. Implement according to spec
5. Later, another engineer hits same edge case
6. Reads spec, sees decision
7. Implements consistently
8. System is consistent

### The Spec Review

Before implementation, specs should be reviewed:

Slice Spec Review (by tech lead or senior engineer):
- Does this align with the constitution?
- Are the interfaces clean?
- Are all edge cases covered?
- Are error codes defined?
- Are invariants testable?

PR Spec Review (quick, often self-review):
- Does this implement part of the slice spec correctly?
- Are deliverables clear?
- Are tests specified?
- Are non-goals explicit?

---

## How Documents Reference Each Other

Documents form a dependency graph:

Constitution
    ↑ (references conventions from)
Slice Spec
    ↑ (references schemas/APIs from)
PR Spec
    ↑ (references deliverables from)
Code

### The Reference Rules

**Slice Spec references Constitution:**
```markdown
## Conventions (from Constitution)
- Errors follow E_CATEGORY_NAME pattern
- All IDs are ULIDs

## API Contract
POST /runs
  Error codes:
    E_REPO_NOT_FOUND (404)  ← follows E_CATEGORY_NAME convention
    E_INVALID_REQUEST (400)
```

**PR Spec references Slice Spec:**
```markdown
## Context
The runs table schema is defined in s1_spec.md section 3.2.
The Run state machine is defined in s1_spec.md section 4.1.

## Task
Implement the state transition function as specified.
```

**Code references PR Spec:**
```rust
/// Implements state transition per s1_pr03_spec.md
///
/// Valid transitions:
/// - queued → running
/// - running → completed | failed | killed
pub fn transition_state(run: &mut Run, new_state: RunState) -> Result<(), StateError> {
```

### Never Duplicate, Always Reference

**Bad** (subtle difference → future bugs):
```markdown
# Slice Spec
Error E_RUN_NOT_FOUND means the run doesn't exist.

# PR Spec
Error E_RUN_NOT_FOUND means the run ID was not found in the database.
```

**Good** (single source of truth):
```markdown
# Slice Spec
Error E_RUN_NOT_FOUND: The requested run_id does not exist in the runs table.

# PR Spec
Return E_RUN_NOT_FOUND as defined in s1_spec.md section 5.1.
```

---

## Complete Documentation Example: Bookmark Manager Web App

This section provides a complete, working example of all four documentation layers for a simple web application with a REST API backend.

---

### L0: Constitution

```markdown
# Bookmark Manager - Constitution v1

## 1. Vision

### Problem
Developers accumulate hundreds of browser bookmarks that become impossible to
organize, search, or access across devices. Browser bookmark UIs are clunky
and don't support tagging or full-text search.

### Solution
A web app with a REST API backend that lets users save, tag, search, and
organize bookmarks. Clean UI, fast search, accessible from any device.

### Scope (v1)
- User accounts (signup, login, logout)
- Save bookmarks with title, URL, description
- Tag bookmarks for organization
- Search bookmarks (title, URL, description, tags)
- Import/export bookmarks

### Non-Scope (v1)
- No browser extension (users paste URLs manually)
- No social/sharing features
- No bookmark folders/hierarchy (tags only)
- No automatic URL metadata fetching
- No mobile app (responsive web only)
- No team/organization accounts
- No two-factor authentication
- No bookmark archiving/wayback

---

## 2. Core Abstractions

| Concept | Definition |
|---------|------------|
| **User** | An authenticated account that owns bookmarks |
| **Bookmark** | A saved URL with title, optional description, and tags |
| **Tag** | A label for grouping bookmarks (e.g., "devtools", "recipes") |
| **Session** | An authenticated user session (JWT token) |

---

## 3. Architecture

### Components

┌─────────────────────────────────────────────────────────┐
│                     Frontend (SPA)                      │
│  React + TypeScript, served as static files             │
└───────────────────────┬─────────────────────────────────┘
                        │ HTTPS
┌───────────────────────▼─────────────────────────────────┐
│                     API Server                          │
│  Node.js + Express, handles all business logic          │
└───────────────────────┬─────────────────────────────────┘
                        │
┌───────────────────────▼─────────────────────────────────┐
│                     PostgreSQL                          │
│  Primary data store                                     │
└─────────────────────────────────────────────────────────┘

### Data Flow
Browser → API Server → PostgreSQL → Response → Browser

### Trust Model
- API server trusts PostgreSQL
- Frontend is untrusted (validate all input server-side)
- Users authenticated via JWT in Authorization header
- All user data is private to that user

---

## 4. Hard Constraints

| Constraint | Value |
|------------|-------|
| Backend Language | TypeScript (Node.js) |
| Frontend Language | TypeScript (React) |
| Database | PostgreSQL 15+ |
| Authentication | JWT (access token + refresh token) |
| API Style | REST, JSON request/response bodies |
| Hosting | Single VPS (API + static files + DB) |

---

## 5. Conventions

### API Design
- Base path: `/api/v1`
- Resources: plural nouns (`/bookmarks`, `/tags`, `/users`)
- Actions: HTTP verbs (GET, POST, PUT, DELETE)
- IDs: UUIDs (never sequential integers for security)

### Request/Response Format
- Content-Type: `application/json`
- Dates: ISO 8601 strings (`2024-01-15T10:30:00Z`)
- Pagination: `?page=1&limit=20`, response includes `total`, `page`, `limit`

### Errors
- Pattern: `{ "error": { "code": "E_CATEGORY_NAME", "message": "..." } }`
- HTTP status codes: 400 (validation), 401 (auth), 403 (forbidden), 404 (not found), 500 (server)

### Naming
- Database: snake_case (`created_at`, `user_id`)
- API JSON: camelCase (`createdAt`, `userId`)
- TypeScript: camelCase for variables, PascalCase for types

### Authentication
- Access token: JWT, 15 minute expiry, sent in `Authorization: Bearer <token>`
- Refresh token: opaque string, 7 day expiry, sent in HTTP-only cookie
- Password: bcrypt hashed, minimum 8 characters

---

## 6. Invariants

1. A bookmark MUST have a valid URL (parseable, http/https scheme)
2. A bookmark MUST belong to exactly one user
3. A user email MUST be unique (case-insensitive)
4. A tag name MUST be lowercase, 1-50 chars, alphanumeric + hyphens only
5. All API endpoints except /auth/* MUST require valid JWT
6. Deleting a user MUST delete all their bookmarks and tags
7. A bookmark's URL + user_id combination MUST be unique (no duplicate URLs per user)

---

## 7. API Overview

### Auth
POST   /api/v1/auth/signup     Create account
POST   /api/v1/auth/login      Get tokens
POST   /api/v1/auth/refresh    Refresh access token
POST   /api/v1/auth/logout     Invalidate refresh token

### Bookmarks
GET    /api/v1/bookmarks       List bookmarks (supports search, filter, pagination)
POST   /api/v1/bookmarks       Create bookmark
GET    /api/v1/bookmarks/:id   Get bookmark
PUT    /api/v1/bookmarks/:id   Update bookmark
DELETE /api/v1/bookmarks/:id   Delete bookmark

### Tags
GET    /api/v1/tags            List user's tags with counts

### Import/Export
POST   /api/v1/import          Import bookmarks (JSON or Netscape HTML)
GET    /api/v1/export          Export all bookmarks as JSON
```

---

### L1: Slice Roadmap

```markdown
# Bookmark Manager - Slice Roadmap

## Slice 0: Bootstrap
**Goal**: Project scaffolding and database setup.

**Outcome**:
- Backend compiles and runs
- Database migrations run
- Health check endpoint works

**Dependencies**: None

**Acceptance**:
- `npm run dev` starts the server
- `GET /api/v1/health` returns `{ "status": "ok" }`
- PostgreSQL tables created via migrations


## Slice 1: Authentication
**Goal**: Users can create accounts and log in.

**Outcome**:
- Signup creates a user account
- Login returns JWT tokens
- Protected routes require valid token

**Dependencies**: Slice 0

**Acceptance**:
- Signup with email/password creates user
- Login with valid credentials returns tokens
- Request to /bookmarks without token returns 401
- Request to /bookmarks with valid token succeeds


## Slice 2: Bookmark CRUD
**Goal**: Users can save and manage bookmarks.

**Outcome**:
- Create bookmark with title/URL
- List user's bookmarks
- Update and delete bookmarks

**Dependencies**: Slice 1

**Acceptance**:
- Create bookmark, verify it appears in list
- Update bookmark title, verify change persists
- Delete bookmark, verify it's gone
- User A cannot see User B's bookmarks


## Slice 3: Tags
**Goal**: Users can organize bookmarks with tags.

**Outcome**:
- Create bookmark with tags
- Filter bookmarks by tag
- List all tags with counts

**Dependencies**: Slice 2

**Acceptance**:
- Create bookmark with tags, verify tags in response
- Filter by tag, only matching bookmarks returned
- GET /tags shows all user's tags with bookmark counts


## Slice 4: Search
**Goal**: Users can search their bookmarks.

**Outcome**:
- Search by title, URL, description
- Search by tag
- Results paginated

**Dependencies**: Slice 2, 3

**Acceptance**:
- Search "github" finds bookmarks with github in title/URL
- Search returns paginated results
- Empty search returns all bookmarks


## Slice 5: Import/Export
**Goal**: Users can import and export bookmarks.

**Outcome**:
- Export all bookmarks as JSON
- Import from JSON
- Import from browser HTML format

**Dependencies**: Slice 3

**Acceptance**:
- Export creates valid JSON with all bookmarks
- Import JSON creates bookmarks
- Import HTML from Chrome/Firefox works


## Slice 6: Frontend
**Goal**: Web UI for all features.

**Outcome**:
- Login/signup pages
- Bookmark list with search
- Add/edit bookmark forms

**Dependencies**: Slice 1, 2, 3, 4

**Acceptance**:
- User can sign up and log in
- User can add, edit, delete bookmarks
- Search and tag filtering works
- Responsive on mobile


## Dependency Graph

Slice 0: Bootstrap
    │
    ▼
Slice 1: Authentication
    │
    ▼
Slice 2: Bookmark CRUD
    │
    ├───────────────┐
    ▼               ▼
Slice 3: Tags    (parallel)
    │               │
    ├───────────────┘
    ▼
Slice 4: Search
    │
    ├───────────────┐
    ▼               │
Slice 5: Import     │
                    │
                    ▼
              Slice 6: Frontend
```

---

### L2: Slice Spec (Example: Slice 2 - Bookmark CRUD)

```markdown
# Slice 2 Spec: Bookmark CRUD

## 1. Goal & Scope

**Goal**: Users can save and manage bookmarks.

**In Scope**:
- POST /bookmarks - create bookmark
- GET /bookmarks - list bookmarks (paginated)
- GET /bookmarks/:id - get single bookmark
- PUT /bookmarks/:id - update bookmark
- DELETE /bookmarks/:id - delete bookmark
- bookmarks table in PostgreSQL
- Input validation

**Out of Scope**:
- Tags (Slice 3)
- Search/filtering (Slice 4)
- Import/export (Slice 5)

---

## 2. Domain Models

### Bookmark

| Field | Type | Constraints |
|-------|------|-------------|
| id | UUID | Primary key, auto-generated |
| user_id | UUID | Foreign key to users, not null |
| url | TEXT | Not null, valid URL, max 2048 chars |
| title | TEXT | Not null, 1-500 chars |
| description | TEXT | Nullable, max 2000 chars |
| created_at | TIMESTAMPTZ | Not null, auto-set |
| updated_at | TIMESTAMPTZ | Not null, auto-updated |

Note: `tags` added in Slice 3.

---

## 3. Database Schema

```sql
CREATE TABLE bookmarks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url TEXT NOT NULL CHECK(length(url) <= 2048),
    title TEXT NOT NULL CHECK(length(title) >= 1 AND length(title) <= 500),
    description TEXT CHECK(description IS NULL OR length(description) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, url)
);

CREATE INDEX idx_bookmarks_user_id ON bookmarks(user_id);
CREATE INDEX idx_bookmarks_created_at ON bookmarks(created_at DESC);
```

---

## 4. API Endpoints

### POST /api/v1/bookmarks

Create a new bookmark.

**Auth**: Required (JWT)

**Request Body**:
```json
{
  "url": "https://example.com",      // required, valid URL
  "title": "Example Site",           // required, 1-500 chars
  "description": "A great site"      // optional, max 2000 chars
}
```

**Response 201**:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "url": "https://example.com",
  "title": "Example Site",
  "description": "A great site",
  "createdAt": "2024-01-15T10:30:00Z",
  "updatedAt": "2024-01-15T10:30:00Z"
}
```

**Errors**:
| Code | Status | When |
|------|--------|------|
| E_VALIDATION_ERROR | 400 | Missing/invalid fields |
| E_URL_INVALID | 400 | URL not parseable or not http/https |
| E_DUPLICATE_URL | 409 | User already has bookmark with this URL |
| E_UNAUTHORIZED | 401 | Missing or invalid token |

---

### GET /api/v1/bookmarks

List user's bookmarks (paginated).

**Auth**: Required (JWT)

**Query Parameters**:
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| page | int | 1 | Page number (1-indexed) |
| limit | int | 20 | Items per page (max 100) |

**Response 200**:
```json
{
  "bookmarks": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "url": "https://example.com",
      "title": "Example Site",
      "description": "A great site",
      "createdAt": "2024-01-15T10:30:00Z",
      "updatedAt": "2024-01-15T10:30:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 1,
    "totalPages": 1
  }
}
```

**Behavior**:
- Only returns bookmarks owned by authenticated user
- Ordered by created_at DESC (newest first)
- Empty array if no bookmarks

**Errors**:
| Code | Status | When |
|------|--------|------|
| E_UNAUTHORIZED | 401 | Missing or invalid token |

---

### GET /api/v1/bookmarks/:id

Get a single bookmark.

**Auth**: Required (JWT)

**Response 200**:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "url": "https://example.com",
  "title": "Example Site",
  "description": "A great site",
  "createdAt": "2024-01-15T10:30:00Z",
  "updatedAt": "2024-01-15T10:30:00Z"
}
```

**Errors**:
| Code | Status | When |
|------|--------|------|
| E_UNAUTHORIZED | 401 | Missing or invalid token |
| E_NOT_FOUND | 404 | Bookmark doesn't exist OR belongs to another user |

Note: Return 404 (not 403) when bookmark belongs to another user to avoid leaking existence.

---

### PUT /api/v1/bookmarks/:id

Update a bookmark.

**Auth**: Required (JWT)

**Request Body** (all fields optional):
```json
{
  "url": "https://updated.com",
  "title": "Updated Title",
  "description": "Updated description"
}
```

**Response 200**: Updated bookmark object (same as GET)

**Behavior**:
- Only provided fields are updated
- updated_at is auto-set to NOW()

**Errors**:
| Code | Status | When |
|------|--------|------|
| E_VALIDATION_ERROR | 400 | Invalid field values |
| E_URL_INVALID | 400 | URL not parseable or not http/https |
| E_DUPLICATE_URL | 409 | New URL already exists for this user |
| E_UNAUTHORIZED | 401 | Missing or invalid token |
| E_NOT_FOUND | 404 | Bookmark doesn't exist or not owned |

---

### DELETE /api/v1/bookmarks/:id

Delete a bookmark.

**Auth**: Required (JWT)

**Response 204**: No content

**Errors**:
| Code | Status | When |
|------|--------|------|
| E_UNAUTHORIZED | 401 | Missing or invalid token |
| E_NOT_FOUND | 404 | Bookmark doesn't exist or not owned |

---

## 5. Service Functions

```typescript
// src/services/bookmark.service.ts

interface CreateBookmarkInput {
  userId: string;
  url: string;
  title: string;
  description?: string;
}

interface UpdateBookmarkInput {
  url?: string;
  title?: string;
  description?: string | null;  // null to clear
}

interface PaginationOptions {
  page: number;
  limit: number;
}

interface PaginatedResult<T> {
  items: T[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
}

// Create a new bookmark
async function createBookmark(input: CreateBookmarkInput): Promise<Bookmark>;

// List bookmarks for a user
async function listBookmarks(
  userId: string,
  options: PaginationOptions
): Promise<PaginatedResult<Bookmark>>;

// Get a single bookmark (throws if not found or not owned)
async function getBookmark(userId: string, bookmarkId: string): Promise<Bookmark>;

// Update a bookmark (throws if not found or not owned)
async function updateBookmark(
  userId: string,
  bookmarkId: string,
  input: UpdateBookmarkInput
): Promise<Bookmark>;

// Delete a bookmark (throws if not found or not owned)
async function deleteBookmark(userId: string, bookmarkId: string): Promise<void>;
```

---

## 6. Error Codes

| Code | Message | HTTP |
|------|---------|------|
| E_VALIDATION_ERROR | "Validation failed: {details}" | 400 |
| E_URL_INVALID | "Invalid URL: must be a valid http or https URL" | 400 |
| E_TITLE_EMPTY | "Title cannot be empty" | 400 |
| E_TITLE_TOO_LONG | "Title cannot exceed 500 characters" | 400 |
| E_DESCRIPTION_TOO_LONG | "Description cannot exceed 2000 characters" | 400 |
| E_DUPLICATE_URL | "A bookmark with this URL already exists" | 409 |
| E_NOT_FOUND | "Bookmark not found" | 404 |
| E_UNAUTHORIZED | "Authentication required" | 401 |

---

## 7. Invariants (This Slice)

1. A bookmark MUST belong to exactly one user
2. A user CANNOT have two bookmarks with the same URL
3. All bookmark URLs MUST be valid http:// or https:// URLs
4. getBookmark/updateBookmark/deleteBookmark MUST verify ownership
5. Listing bookmarks MUST only return bookmarks owned by the requesting user
6. Deleting a user MUST cascade delete all their bookmarks

---

## 8. Acceptance Scenarios

**Scenario: Create and list bookmark**
```
Given: User is authenticated
When: POST /bookmarks with {"url": "https://github.com", "title": "GitHub"}
Then: Response is 201 with bookmark object including generated ID
When: GET /bookmarks
Then: Response includes the created bookmark
```

**Scenario: Update bookmark**
```
Given: Bookmark exists with title "Old Title"
When: PUT /bookmarks/:id with {"title": "New Title"}
Then: Response shows updated title
And: updatedAt is newer than createdAt
```

**Scenario: Delete bookmark**
```
Given: Bookmark exists
When: DELETE /bookmarks/:id
Then: Response is 204
When: GET /bookmarks/:id
Then: Response is 404
```

**Scenario: Cannot access other user's bookmark**
```
Given: User A has a bookmark
When: User B tries GET /bookmarks/:id (User A's bookmark)
Then: Response is 404 (not 403)
```

**Scenario: Duplicate URL rejected**
```
Given: User has bookmark with url "https://example.com"
When: POST /bookmarks with same URL
Then: Response is 409 E_DUPLICATE_URL
```

**Scenario: Invalid URL rejected**
```
Given: User is authenticated
When: POST /bookmarks with {"url": "not-a-url", "title": "Test"}
Then: Response is 400 E_URL_INVALID
```
```

---

### L3: PR Spec (Example: PR-01 of Slice 2)

```markdown
# PR-01: Bookmark Model and Database Migration

## Goal
Add the Bookmark type and create the PostgreSQL migration for the bookmarks table.

## Context
- Slice 2 spec defines the Bookmark model and schema
- Project structure from Slice 0 exists (Express app, db connection)
- Users table exists from Slice 1

## Dependencies
- Slice 1 must be complete (users table exists)

---

## Files to Create

### src/models/bookmark.model.ts

```typescript
/**
 * Bookmark entity representing a saved URL.
 */
export interface Bookmark {
  id: string;
  userId: string;
  url: string;
  title: string;
  description: string | null;
  createdAt: Date;
  updatedAt: Date;
}

/**
 * Input for creating a new bookmark.
 * userId is added by the service layer from auth context.
 */
export interface CreateBookmarkDto {
  url: string;
  title: string;
  description?: string;
}

/**
 * Input for updating a bookmark.
 * All fields optional - only provided fields are updated.
 */
export interface UpdateBookmarkDto {
  url?: string;
  title?: string;
  description?: string | null;
}

/**
 * Convert database row to Bookmark entity.
 * Handles snake_case → camelCase conversion.
 */
export function rowToBookmark(row: {
  id: string;
  user_id: string;
  url: string;
  title: string;
  description: string | null;
  created_at: Date;
  updated_at: Date;
}): Bookmark {
  return {
    id: row.id,
    userId: row.user_id,
    url: row.url,
    title: row.title,
    description: row.description,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}
```

### src/db/migrations/002_create_bookmarks.sql

```sql
-- Migration: Create bookmarks table
-- Depends on: 001_create_users.sql

CREATE TABLE bookmarks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url TEXT NOT NULL CHECK(length(url) <= 2048),
    title TEXT NOT NULL CHECK(length(title) >= 1 AND length(title) <= 500),
    description TEXT CHECK(description IS NULL OR length(description) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, url)
);

CREATE INDEX idx_bookmarks_user_id ON bookmarks(user_id);
CREATE INDEX idx_bookmarks_created_at ON bookmarks(created_at DESC);

-- Trigger to auto-update updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_bookmarks_updated_at
    BEFORE UPDATE ON bookmarks
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
```

---

## Files to Modify

### src/models/index.ts

Add exports:
```typescript
export * from './bookmark.model';
```

---

## Test Specifications

### File: src/models/__tests__/bookmark.model.test.ts

```typescript
import { rowToBookmark } from '../bookmark.model';

describe('rowToBookmark', () => {
  it('converts snake_case row to camelCase Bookmark', () => {
    const row = {
      id: '550e8400-e29b-41d4-a716-446655440000',
      user_id: '660e8400-e29b-41d4-a716-446655440000',
      url: 'https://example.com',
      title: 'Example',
      description: 'A site',
      created_at: new Date('2024-01-15T10:30:00Z'),
      updated_at: new Date('2024-01-15T10:30:00Z'),
    };

    const bookmark = rowToBookmark(row);

    expect(bookmark.id).toBe(row.id);
    expect(bookmark.userId).toBe(row.user_id);
    expect(bookmark.url).toBe(row.url);
    expect(bookmark.title).toBe(row.title);
    expect(bookmark.description).toBe(row.description);
    expect(bookmark.createdAt).toEqual(row.created_at);
    expect(bookmark.updatedAt).toEqual(row.updated_at);
  });

  it('handles null description', () => {
    const row = {
      id: '550e8400-e29b-41d4-a716-446655440000',
      user_id: '660e8400-e29b-41d4-a716-446655440000',
      url: 'https://example.com',
      title: 'Example',
      description: null,
      created_at: new Date('2024-01-15T10:30:00Z'),
      updated_at: new Date('2024-01-15T10:30:00Z'),
    };

    const bookmark = rowToBookmark(row);

    expect(bookmark.description).toBeNull();
  });
});
```

### File: src/db/__tests__/migrations.test.ts (add to existing)

```typescript
describe('002_create_bookmarks migration', () => {
  it('creates bookmarks table', async () => {
    const result = await db.query(`
      SELECT column_name, data_type, is_nullable
      FROM information_schema.columns
      WHERE table_name = 'bookmarks'
      ORDER BY ordinal_position
    `);

    expect(result.rows).toEqual([
      { column_name: 'id', data_type: 'uuid', is_nullable: 'NO' },
      { column_name: 'user_id', data_type: 'uuid', is_nullable: 'NO' },
      { column_name: 'url', data_type: 'text', is_nullable: 'NO' },
      { column_name: 'title', data_type: 'text', is_nullable: 'NO' },
      { column_name: 'description', data_type: 'text', is_nullable: 'YES' },
      { column_name: 'created_at', data_type: 'timestamp with time zone', is_nullable: 'NO' },
      { column_name: 'updated_at', data_type: 'timestamp with time zone', is_nullable: 'NO' },
    ]);
  });

  it('enforces unique constraint on user_id + url', async () => {
    const userId = await createTestUser();

    await db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', 'First')
    `, [userId]);

    await expect(db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', 'Duplicate')
    `, [userId])).rejects.toThrow(/unique/i);
  });

  it('cascades delete when user is deleted', async () => {
    const userId = await createTestUser();

    await db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', 'Test')
    `, [userId]);

    await db.query('DELETE FROM users WHERE id = $1', [userId]);

    const result = await db.query(
      'SELECT COUNT(*) FROM bookmarks WHERE user_id = $1',
      [userId]
    );
    expect(result.rows[0].count).toBe('0');
  });

  it('enforces title length constraints', async () => {
    const userId = await createTestUser();

    // Empty title should fail
    await expect(db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', '')
    `, [userId])).rejects.toThrow(/check/i);

    // Title > 500 chars should fail
    const longTitle = 'x'.repeat(501);
    await expect(db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', $2)
    `, [userId, longTitle])).rejects.toThrow(/check/i);
  });

  it('auto-updates updated_at on UPDATE', async () => {
    const userId = await createTestUser();

    const insert = await db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', 'Original')
      RETURNING updated_at
    `, [userId]);

    const originalUpdatedAt = insert.rows[0].updated_at;

    // Wait a moment to ensure timestamp differs
    await new Promise(r => setTimeout(r, 10));

    const update = await db.query(`
      UPDATE bookmarks SET title = 'Updated'
      WHERE user_id = $1
      RETURNING updated_at
    `, [userId]);

    expect(update.rows[0].updated_at.getTime())
      .toBeGreaterThan(originalUpdatedAt.getTime());
  });
});
```

---

## Non-Goals

- Does NOT implement bookmark service functions (PR-02)
- Does NOT implement API routes (PR-03)
- Does NOT implement input validation (PR-02)
- Does NOT implement bookmark repository/queries (PR-02)
- Does NOT add tags support (Slice 3)

---

## Constraints

- Only create/modify files listed above
- No new npm dependencies
- Migration must be idempotent (use IF NOT EXISTS where applicable)
- All TypeScript must pass strict type checking
- Follow existing naming conventions in the codebase

---

## Checklist

- [ ] npm run build succeeds with no errors
- [ ] npm run test passes (all new tests)
- [ ] npm run lint produces no warnings
- [ ] Migration runs successfully on fresh database
- [ ] Migration runs successfully on database with existing data
- [ ] Only listed files modified
```

---

## Summary: The Template Pattern

| Layer | Length | Key Sections |
|-------|--------|--------------|
| **L0** | 2-4 pages | Vision, Abstractions, Architecture, Constraints, Conventions, Invariants |
| **L1** | 1-2 pages | Slice list with Goal, Outcome, Dependencies, Acceptance |
| **L2** | 3-6 pages | Scope, Models, Schema, State Machines, Commands, Functions, Errors, Scenarios |
| **L3** | 1-3 pages | Goal, Deliverables (exact code), Tests (exact cases), Non-goals, Constraints |

---
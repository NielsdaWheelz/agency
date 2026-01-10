L0: Constitution (also called "Charter" or "System Design")

Purpose: Prevent project drift. Lock in irreversible decisions.

Analogy: A country's constitution. It doesn't tell you what laws to pass - it tells you what kinds of laws are allowed. It's very hard to change.

Contains:
- Goals and explicit non-goals (what we refuse to build)
- System boundaries (what talks to what)
- Trust model (what is trusted vs untrusted)
- Core abstractions (the 3-5 fundamental concepts)
- Irreversible technology choices (language, database, deployment model)
- Cross-cutting conventions (error handling style, logging format, testing patterns)

Does NOT contain:
- Specific endpoints
- Database table schemas
- UI flows
- Implementation details

Decision test: If this changes, does most of the codebase need to change?

The Sections of a Gold-Standard Constitution

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

---
What Makes a Constitution Good vs Bad

Bad constitution:
We're building a tool to help developers. It will be fast and reliable.
We'll use modern best practices.

This constrains nothing. An engineer could build anything and claim it follows this.

Good constitution:
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

This constrains heavily. An engineer cannot deviate without explicitly violating the document.

---
The "Non-Scope" Section is the Most Important

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

---
Invariants: Your System's Laws of Physics

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
L1: Slice Roadmap (also called "Milestone Plan" or "Delivery Sequence")

Purpose: Order the work. Define dependencies.

Analogy: A construction schedule. "Foundation before walls. Walls before roof. Electrical before drywall."

Contains:
- Slices (chunks of user-visible value)
- Dependencies between slices
- Acceptance criteria per slice (how do we know it's done?)
- Risk spikes (unknowns we need to investigate early)

Does NOT contain:
- How to implement each slice
- Database schemas
- API designs

Decision test: If this changes, does the timeline change more than the code?

What Is a Slice?

A slice is a vertical chunk of user-visible value that can be delivered independently.

Analogy: Slicing a cake vertically, not horizontally.

Horizontal (bad): "First build all the database tables. Then build all the APIs. Then build all the UI."
- Problem: Nothing works until everything works. No feedback until the end.

Vertical (good): "First build login (DB + API + UI). Then build posting (DB + API + UI). Then build search."
- Benefit: Each slice is usable. You get feedback early. You can ship incrementally.

---
What Goes in a Roadmap

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

---
What Does NOT Go in a Roadmap

- Database schemas
- API specifications
- Function signatures
- Implementation details
- File structures

The roadmap is purely about ordering and dependencies. All technical details belong in L2 (Slice Spec).

---
The Dependency Graph

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

---
Slicing Strategies

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

---
Good vs Bad Roadmap

Bad roadmap:
Phase 1: Backend
Phase 2: Frontend
Phase 3: Testing
Phase 4: Polish

Problems:
- Horizontal, not vertical (nothing works until Phase 3)
- No acceptance criteria
- No dependencies specified
- "Polish" is not a slice, it's procrastination

Good roadmap:
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

---
The "Walking Skeleton" Pattern

A powerful slicing technique: Build a walking skeleton first.

A walking skeleton is the thinnest possible end-to-end path through your system.

For Agency:
- Slice 0 creates config
- Slice 1 creates a worktree, starts tmux, runs a fake runner that just echoes "hello"
- Slice 3 pushes and creates a PR

This skeleton doesn't do anything useful, but it proves all the pieces connect. Then you flesh out each piece.

---
L2: Slice Spec (also called "Feature Contract" or "Module Spec")

Purpose: Define what multiple PRs must agree on. Prevent parallel work from colliding.

Analogy: The electrical blueprint for one room. It shows where every outlet goes, what voltage, what wire gauge. Any electrician can wire it without asking questions.

Contains:
- Exact API contracts (request/response shapes)
- Exact database schema for tables touched by this slice
- State machines (what states exist, what transitions are legal)
- Error codes (what can go wrong, and how we report it)
- Invariants (rules that must never be violated)
- Acceptance scenarios (given X, when Y, then Z)

Does NOT contain:
- Internal helper function signatures
- Which files to create
- Implementation choices (like "use library X")

Decision test: If this changes, would multiple PRs break?

Why does L2 exist?

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

---
The Sections of a Gold-Standard Slice Spec

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

---
What Does NOT Go in a Slice Spec

- Implementation details ("use library X")
- File paths ("put this in src/services/run.rs")
- Internal helper functions
- Performance optimizations
- Anything only one PR needs to know

---
The "Multiple PRs" Test

For every item in your slice spec, ask:

"Do multiple PRs need to agree on this?"

- Table schema → Yes, the PR that creates the table and the PR that queries it must agree → Include
- Function signature for public API → Yes, caller and callee must agree → Include
- Internal helper function → No, only one file uses it → Exclude
- Error code → Yes, thrower and catcher must agree → Include
- Variable name inside a function → No → Exclude

---
Common Slice Spec Mistakes

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
L3: PR Spec (also called "Task Spec" or "Work Unit")

Purpose: Make one PR trivially reviewable and low-risk.

Analogy: A single work order. "Install outlet #3 at position (x,y). Use 12-gauge wire. Connect to circuit B. Test with multimeter."

Contains:
- Goal (one sentence)
- Exact public surface being added (function signature, endpoint, table column)
- Acceptance tests (specific inputs → expected outputs)
- Constraints (what files may be touched)
- Non-goals (what this PR explicitly does NOT do)

Does NOT contain:
- Restating the whole slice spec
- Architecture decisions
- Long explanations

Decision test: If this changes, does it only invalidate this branch?

---
Lesson 4: The Critical Insight - Negative Space

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

  Quick Reference Card

  | Level            | Contains                                                   | Does NOT Contain                   | Changes When                |
  |------------------|------------------------------------------------------------|------------------------------------|-----------------------------|
  | L0: Constitution | Language, architecture, conventions, boundaries, non-goals | Schemas, APIs, file structure      | Almost never                |
  | L1: Roadmap      | Slice order, dependencies, milestones                      | How to build each slice            | Priorities shift            |
  | L2: Slice Spec   | Exact schemas, APIs, state machines, errors, invariants    | Implementation details, file names | Learning during slice       |
  | L3: PR Spec      | Exact functions, files, tests, constraints                 | Anything outside this PR           | Never (deleted after merge) |

  | Level | Scope        | Content Type                                     |
  |-------|--------------|--------------------------------------------------|
  | L0    | Whole system | Conventions, boundaries, architecture            |
  | L1    | Whole system | Ordering and dependencies (NO technical details) |
  | L2    | One feature  | Technical contracts (schemas, APIs, errors)      |
  | L3    | One PR       | Exact implementation details                     |


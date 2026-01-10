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


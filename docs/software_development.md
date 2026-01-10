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


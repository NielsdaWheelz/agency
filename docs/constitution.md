# agency — constitution

---

## preamble (binding)

this document defines the invariants, scope, and direction of **agency**, a terminal-native ai orchestration system.

it contains three categories of content, distinguished by authority level:

| label | meaning |
|-------|---------|
| **binding** | must not be violated without amending this constitution |
| **binding (v1)** | applies to current implementation scope; will be revisited in future versions |
| **directional (non-binding)** | future intent; may change freely without constitutional amendment |

slices and PRs may only derive requirements from **binding** or **binding (v1)** sections. directional sections describe aspirations, not constraints.

---

## core thesis (binding)

modern ai coding tools are operationally sloppy: they blur context, hide state, conflate planning with execution, and make parallel work fragile.

**agency** exists to impose structure on ai-assisted development.

core beliefs:

* **ai work is a lifecycle, not a chat.** a task has a beginning, execution, review, and completion. these phases must be explicit and observable.
* **git is the source of truth.** provenance, history, and product state live in the repository. agency does not invent a parallel reality.
* **tooling must be transparent.** no opaque orchestration, no hidden state, no magic. a human can always inspect what happened and why.
* **coordination, not replacement.** agency does not replace git, editors, or ai coding tools. it coordinates them.

this thesis is not negotiable. features that contradict it are bugs.

---

## system invariants (binding)

these invariants must hold across all implementations. violating them is a correctness bug, not a feature gap.

### run isolation

* one run owns exactly one worktree
* a worktree is never reused
* a run cannot modify the base branch directly
* runs must not share filesystem state except through the base repo
* killing a run does not affect other runs

### merge control

* merge is impossible without explicit user action
* agency itself must never modify repo state outside worktrees

### resource lifecycle

* explicit cleanup removes the worktree; worktrees are never removed implicitly
* a run's execution is complete when its runner process exits; review and cleanup are separate, explicit phases

### reproducibility

* a run must be reconstructible from its recorded inputs (prompt, references), runner configuration, and repository state at start time
* agency must not depend on hidden or ephemeral context to execute a run

### trust boundaries

* runners are untrusted with respect to correctness
* runners are trusted with local filesystem access (v1 only; subject to future hardening)
* git is the arbiter of truth

### run types (binding, introduced in v2)

* a run has a type that determines intended behavior and default policies (e.g. plan vs code)
* type does not change isolation, logging, or lifecycle invariants; it only affects runner selection and commit/approval defaults
* v1 treats all runs as coding runs; run type distinctions are deferred to v2

---

## architectural commitments (binding, amendable)

these choices are locked unless this constitution is amended.

* **terminal-first.** the primary interface is a CLI. gui and ide integrations are additive, not primary.
* **local-first.** agency runs on the user's machine against local repositories. remote execution is out of scope for v1.
* **isolated worktrees.** git worktrees are the isolation mechanism for runs.
* **tmux supervision.** tmux is the process/session supervisor for runners.
* **interactive sessions.** agency treats runner sessions as interactive terminals; runner lifecycle is not assumed to be one-shot. sessions may persist until explicitly stopped or cleaned up.
* **daemon model.** a background daemon manages state, lifecycle, and coordination.
* **external runners.** agency does not implement code editing. external runners (e.g. claude-code, codex) perform all code modifications.
* **human-gated merges.** merge requires explicit human approval. no autonomous or automatic merges.

---

## supported usage patterns (binding by interpretation)

agency supports multiple usage patterns. these are **compositions of the same primitive** (the run), not separate modes.

agency distinguishes between:

* **planners:** LLM interactions that produce artifacts (specs, plans, prompts)
* **runners:** execution tools that modify code in isolated worktrees

planners are not required to run inside agency; their outputs are handed off explicitly as artifacts. planning artifacts are versioned as commits in git; handoffs between planning and coding are explicit via branch refs and files.

### pattern 1: external planning → handoff

a human or external tool (e.g. a chat-based LLM) produces a plan or specification. the user hands off execution to agency by starting a run with the plan as input. agency supervises execution; the human reviews and merges.

### pattern 2: planning inside agency (directional, v2+)

user starts a planning run (planner type) that produces or updates artifacts and proposes commits. user approves commits to the planning branch. subsequent coding runs fork from that planning branch, using planning artifacts as input.

this pattern requires run types and planner runners, which are out of scope for v1.

### pattern 3: multi-run coordination

the user starts multiple runs, potentially with different runners or different slices of work. each run is independent. the human coordinates review and merge order.

---

all patterns use the same primitives: runs, worktrees, runners, and human-gated merge. there are no special modes or alternate execution paths.

runs may produce artifacts that are used as inputs to subsequent runs. such handoffs are explicit and mediated by files and user approval; there is no implicit continuation of context between runs.

---

## current scope: v1 (binding v1)

v1 delivers a complete loop for **parallel code execution**:

* create isolated git worktrees
* launch external ai coding runners inside those worktrees
* track run status and logs
* allow humans to inspect results
* merge approved changes
* support explicit deterministic cleanup

v1 is usable without any conversational system.

### core abstractions (v1)

these names are stable and must not drift.

**run** — a single execution of an external ai coding agent against a git repository, isolated in its own worktree. a run has:

* a unique id
* a target repo + base branch
* a worktree path
* a lifecycle state
* associated logs
* inputs: a prompt (opaque to agency) and optional read-only references

**runner** — an external program that edits files and runs commands (e.g. claude-code, codex). agency launches and supervises runners. runners may create commits during execution; agency must preserve commit history as produced.

**worktree** — a git worktree created for exactly one run. worktrees are isolated, disposable, and destroyed after merge or cancellation. users may freely inspect and edit files inside a run's worktree at any time.

**artifact** — any file produced or consumed by a run that represents intent, state, or output (e.g. prompts, specs, plans, reports). artifacts live in the repository or run workspace and are versioned via git. agency does not interpret artifact semantics.

**state** — git is the source of truth for product and code. agency maintains local metadata (sqlite / files) for indexing and status. metadata is operational, not product or code. agency does not maintain semantic product state outside git; all durable state is represented as files and commits.

**approval** — a human decision to merge a run's changes into the base branch. approval is explicit and manual in v1.

### golden path (v1)

1. user invokes `agency run` in a git repo with a prompt (and optional read-only references)
2. agency creates a new git worktree and branch
3. agency launches the configured runner in a tmux session
4. runner edits files, runs tests, and exits
5. user inspects the tmux session (diff, worktree, logs)
6. user merges the branch via `agency merge`
7. agency deletes the worktree and terminates the session

no other flow is required to succeed for v1.

### explicit non-goals (v1)

out of scope for v1:

* chat systems or editable conversations
* multi-actor "councils"
* structured product/spec state
* automatic or autonomous merges
* remote execution or cloud services
* collaboration / multi-user sync
* gui or ide-specific ui
* security hardening (sandboxing, network isolation)

if a change meaningfully advances any of the above, it does not belong in v1.

### success criteria (v1)

agency v1 is successful if:

* multiple runs can execute concurrently without interference
* failures are visible and diagnosable
* no run can corrupt the main working tree
* cleanup is reliable and boring
* the tool is useful without any chat system

---

## future direction: v2+ (directional, non-binding)

this section describes aspirational capabilities. these are not requirements, not constraints, and may change freely. slices and PRs must not derive requirements from this section.

### planner / chat runners

future versions may support runners optimized for planning, specification, or conversation rather than code execution. these would use the same run primitive with different runner configurations.

planner runners are an optional packaging of planner interactions into a run, so that artifact edits are isolated, logged, and committed under user approval. planners may still operate outside agency; planner runners bring them inside the run lifecycle.

### structured artifacts

future versions may introduce first-class support for structured artifacts (specs, plans, task lists) that persist across runs via git in the repo and inform downstream work.

### councils

future versions may support multi-agent coordination patterns where multiple runners collaborate or deliberate on a shared problem, with human oversight.

### policy engines

future versions may introduce configurable policies for merge approval, runner selection, or resource limits.

### remote and mobile access

future versions may support remote execution, cloud-hosted daemons, or mobile interfaces for monitoring and approval.

---

these capabilities are under consideration. their inclusion, design, and priority are subject to change.

---

## amendment rules (binding)

### modifying binding sections

changes to **binding** or **binding (amendable)** sections require:

* explicit proposal with rationale
* review of downstream impact on existing slices and implementations
* amendment of this constitution before implementation

### modifying v1 scope

changes to **binding (v1)** sections follow the same process but are expected to evolve as v1 stabilizes and v2 planning begins.

### directional sections

**directional (non-binding)** sections may be updated freely without constitutional amendment. they represent current thinking, not commitments.

### derivation rules

* slices and PRs may derive requirements from **binding** and **binding (v1)** sections
* slices and PRs must not derive requirements from **directional (non-binding)** sections
* if a directional concept is ready for implementation, it must first be promoted to a binding section through the amendment process

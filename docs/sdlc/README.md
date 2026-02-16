# Software Development Lifecycle

> each layer constrains the next, narrowing the solution space until implementation is unambiguous.

## hierarchy

```
┌─────────────────────────────────────────────────────────────────┐
│  l0  constitution        system-wide constraints (rarely changes) │
└───────────────────────────────┬─────────────────────────────────┘
                                ▼
        ┌───────────────────────────────────────────┐
        │  l1  slice roadmap     ordered slices      │
        └───────────────────┬───────────────────────┘
                            ▼
              ┌─────────────────────────────┐
              │  l2  slice spec   contract  │
              └─────────┬───────────────────┘
                        ▼
                ┌───────────────────┐
                │  l3  pr roadmap   │
                └───────┬───────────┘
                        ▼
                  ┌───────────┐
                  │  l4  pr   │
                  │  spec     │
                  └─────┬─────┘
                        ▼
                    ┌──────┐
                    │ code │
                    └──────┘
```

spec layers (l0, l2, l4) define contracts. decomposition layers (l1, l3) break contracts into ordered children.

## core operating rules

- never skip layers: `l0 -> l1 -> l2 -> l3 -> l4 -> code`.
- never code ahead of spec.
- never silently deviate from spec.
- never leave scope boundaries implicit.
- every claim in l2/l4 must be testable.

## orchestrator role

you are the orchestrator. you select the next layer, assemble minimal context, and run that layer's skill. you do not mix layers in one output.

### dispatch logic

```
start:
  if no l0 exists             -> run l0
  if no l1 exists             -> run l1
  if next slice has no l2     -> run l2
  if slice has no l3          -> run l3
  if next pr has no l4        -> run l4
  if l4 exists                -> implement pr
  after merge                 -> repeat l4 until l3 complete
  after slice complete        -> repeat l2 for next slice
```

## bounded-context authoring protocol (mandatory for l2/l3/l4)

this protocol is required to avoid context-window failure and spec drift.

authoring is not `gather all info -> ask all questions -> decide all things`.
authoring is iterative and section-local.

### phase 1: skeleton first

write the document skeleton from upstream contracts only:
- l2: core 7 contract sections + traceability/default sections.
- l3: dependency graph + pr list.
- l4: goal/context/dependencies/deliverables/tests/non-goals/constraints.

no detailed implementation decisions in this phase.

### phase 2: contract cluster loop

for each acceptance cluster (one cluster at a time):
1. gather only code facts needed for that cluster.
2. extract only forced questions.
3. make explicit decisions (or explicit temporary defaults).
4. patch the spec immediately.
5. log evidence and decision in companion artifacts.

never batch all clusters in one pass.

### phase 3: hardening passes

run these passes in order:
1. completeness pass: every upstream acceptance item is covered.
2. consistency pass: no contradictions across sections.
3. traceability pass: every contract maps to deliverables and tests.
4. boundary pass: remove anything owned by another pr/slice.
5. ambiguity pass: remove vague language, add exact behavior.

## required companion artifacts

l2/l3/l4 specs must maintain companion files during drafting:

| artifact | purpose | minimum contents |
|---|---|---|
| `{doc}_worklog.md` | context memory | code facts with file:line evidence |
| `{doc}_decisions.md` | decision control | question, decision, rationale, fallback/default, owner |
| `{doc}.md` | normative contract | only finalized contract language |

rules:
- unresolved questions are allowed only if they include a temporary default behavior.
- every temporary default has an owner and a deadline (or owning next pr).

## handoff protocol

each layer passes minimum viable context downstream.

| from | to | must pass |
|---|---|---|
| l0 -> l1 | constitution summary | hard constraints, non-scope, architecture axes |
| l1 -> l2 | slice entry | goal, dependencies, acceptance |
| l0 + l2 -> l3 | constitution + slice contract | contract clusters, invariants, error model |
| l2 + l3 -> l4 | slice contract + pr entry | exact relevant contracts, pr ownership, acceptance |

## context assembly standard

goal: enough context to force the right output, no more.

include:
- exact upstream contract snippets.
- exact existing code patterns.
- exact accepted error codes and status mappings.
- explicit non-goals and boundaries.

exclude:
- whole-document dumps when section excerpts suffice.
- unrelated slices/prs.
- historical rationales that do not change behavior.

## decision quality bar

every decision in l2/l3/l4 must satisfy:
- explicit problem statement.
- chosen behavior.
- rejected alternatives (short).
- invariant impact.
- test impact.

if any item is missing, decision is not accepted.

## iteration + escalation protocol

| trigger | action | cascade |
|---|---|---|
| l4 is wrong | fix l4 and re-implement | none |
| l2 contract is wrong | stop, fix l2, regenerate l3 and affected l4s | all unstarted prs in slice |
| l1 ordering is wrong | fix l1, re-plan affected slices | downstream slice docs |
| l0 constraint is wrong | full stop, revise l0, audit all layers | potentially whole project |

rules:
1. never silently deviate.
2. fix at highest affected layer.
3. regenerate downstream layers, do not hand-patch them.
4. merged prs are reopened only when contract-invalid, otherwise fix forward.

## structural validation

**l2 must pass:**
- all core contract sections exist and required l2 appendices are present.
- every acceptance scenario maps to concrete api/model/invariant items.
- no unresolved ambiguity without default behavior.
- no file paths or helper-level implementation details.

**l3 must pass:**
- dependency graph is a DAG.
- each l2 contract cluster has exactly one owning pr.
- acceptance coverage exists for every cluster.
- no file paths, signatures, or test-case detail.

**l4 must pass:**
- includes decision ledger and traceability matrix.
- every l3 acceptance bullet maps to at least one deliverable and one test.
- every behavior-changing decision has test coverage.
- all dependencies reference merged state only.

## examples

complete worked example across layers:
- [l0: constitution](./examples/bookmark-manager/constitution.md)
- [l1: roadmap](./examples/bookmark-manager/roadmap.md)
- [l2: slice spec](./examples/bookmark-manager/slice-2-bookmark-crud.md)
- [l3: pr roadmap](./examples/bookmark-manager/pr-roadmap-slice-2.md)
- [l4: pr spec](./examples/bookmark-manager/pr-01-bookmark-model.md)

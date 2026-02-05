# advisory standards

this is advisory guidance. binding rules live in `docs/standards/binding.md`.

## design

- favor clear single-owner modules over shared state.
- prefer explicit contracts (docs/contracts) over implied behavior.
- isolate side effects at the edge (fs, exec, git).

## code

- keep public interfaces narrow and stable.
- avoid cross-package globals; inject dependencies.
- use typed errors and stable error codes.
- keep functions small and single-purpose.

## data

- schema changes require a version bump and migration story.
- never silently coerce or drop user data.
- prefer append-only logs for auditability.

## testing

- add tests for new error codes and contract changes.
- use deterministic inputs and stable ordering.
- avoid network and time sleeps.

## docs

- update contracts with every schema change.
- document public commands and api changes.

## stubs

- logging and tracing guidelines

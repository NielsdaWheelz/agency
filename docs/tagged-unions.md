# Tagged Unions

## Scope

This document covers repository-wide finite-state rules.

## Rules

- Represent finite persisted or API-visible states with typed string constants in the owning package.
- Keep the state spelling stable once persisted or published.
- Branch exhaustively on those constants.
- Do not scatter raw state strings across packages.

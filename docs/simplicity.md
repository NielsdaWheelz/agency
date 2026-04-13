# Simplicity

## Scope

This document covers repository-wide implementation simplicity rules.

## Rules

- When there are multiple reasonable ways to write something, prefer fewer lines and fewer characters within reason.
- Default to fewer code paths.
- Each additional code path should be justifiable.
- Do not add speculative API surface.
- Do not add optional parameters, option structs, or flags until a real call site needs them.
- Apply the same bias to error handling, schema validation, and branching.
- Do not add code paths for scenarios that cannot be constructed.

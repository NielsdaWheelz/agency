# Release Gates

## Scope

This document covers `internal/releasegates`.

## Rules

- Release-gate state is derived from repository docs, not from a database.
- Parsing, evaluation, transition validation, and drift detection belong in this package.
- Test fixtures for gate evaluation should remain representative repository snapshots under package testdata.

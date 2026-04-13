# Testing

## Scope

This document covers repository-wide testing rules.

## Rules

- Prefer unit tests with injected dependencies over tests that require real external tools.
- Use fake command runners, fake tmux clients, fake clocks, and temporary directories where possible.
- Environment-dependent tests should use explicit env overrides and restore isolation through temp dirs.
- Slow or externally authenticated flows belong behind tags or explicit opt-in environment variables.
- If a rule is binding and enforceable in code, add or update tests in the same change.

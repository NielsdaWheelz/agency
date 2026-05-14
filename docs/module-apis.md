# Module APIs

## Scope

This document covers how packages should expose capabilities.

## Rules

- Expose each capability in one primary form.
- Do not expose interchangeable duplicate APIs for the same capability.
- If a daemon DTO or lower-level helper already exists for a view, prefer using it over reconstructing the same view elsewhere.
- Command packages should orchestrate and render.
- Lower-level packages should return data and errors, not print.

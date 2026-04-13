# JSON Values

## Scope

This document covers JSON and JSONL boundary rules.

## Rules

- Parse unknown JSON immediately at the boundary.
- Reject unknown or wrongly typed fields in strict config and metadata schemas.
- Persist human-owned JSON files with stable formatting and a trailing newline.
- JSONL event streams are append-only and should contain exactly one object per line.
- Keep JSONL line sizes bounded.
- Do not persist secret environment values when a stable key list is sufficient.

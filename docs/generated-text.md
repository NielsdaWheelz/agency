# Generated Text

## Scope

This document covers escaping and quoting at generated-text boundaries.

## Rules

- Prefer argv slices over concatenated shell text.
- When shell text is unavoidable, quote every interpolated value at the use site.
- Do not rely on prior validation or informal knowledge that a value cannot contain special characters.
- Keep generated reports, prompts, PR bodies, and merge logs deterministic and bounded.
- Do not scatter duplicate text-generation logic when one canonical renderer already exists.

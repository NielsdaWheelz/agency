# Function Parameters

## Scope

This document covers function and method parameter shape rules.

## Rules

- Use option structs when a function has multiple optional parameters or an evolving parameter set.
- Keep required identifiers explicit when that keeps the call site clearer than an option struct.
- Do not add boolean positional parameters when the meaning is unclear at the call site.
- Keep option structs near the package that owns the behavior.
- Small pure helpers and primitives may remain positional when the arguments are obvious and tightly coupled.

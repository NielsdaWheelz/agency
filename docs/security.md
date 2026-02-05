# security

this document defines the security policy and threat model.

## scope

- local, single-user tool
- git and gh are trusted dependencies
- no multi-tenant isolation

## principles

- least privilege for filesystem and processes
- no secret leakage in logs or events
- fail fast on validation errors

## rules

1. never execute arbitrary shell strings; use `internal/exec` with structured argv.
2. never write outside the data dir or repo root without explicit allowlist.
3. validate all inputs that cross process or network boundaries.
4. all sensitive files are 0600 and directories 0700.

## reporting

- security issues are filed privately to the maintainer.

## stubs

- formal security contact and response timeline
- threat model table

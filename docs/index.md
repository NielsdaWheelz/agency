# Docs

## Role

This directory is the canonical home for repository documentation.

## Goals

- MECE organization: documents are mutually exclusive and collectively exhaustive.
- Concision
- Clear boundaries

## Starting Points

- [tech-stack.md](tech-stack.md): runtime and tooling stack
- [codebase.md](codebase.md): package layout and ownership boundaries
- [entrypoints.md](entrypoints.md): CLI, daemon, and TUI entrypoints
- [process-execution.md](process-execution.md): external command execution, runner, tmux, and env rules
- [daemon.md](daemon.md): daemon ownership, lifecycle, and API behavior
- [git-worktrees.md](git-worktrees.md): repo, integration worktree, sandbox, and landing rules
- [persistence.md](persistence.md): JSON and JSONL contracts, atomic writes, and file permissions
- [environment.md](environment.md): environment variables and config precedence
- [errors.md](errors.md): stable error-code and corruption rules
- [concurrency.md](concurrency.md): repo locks and mutation ordering
- [modules/index.md](modules/index.md): subsystem-owned docs
- [testing.md](testing.md): test layers, fixtures, and e2e rules

## Placement Rules

- Each rule lives in exactly one document.
- Put content in the narrowest document that fully owns it.
- Link to related docs instead of restating them.
- If two docs need the same text, the split is wrong.
- If a document covers multiple unrelated topics, split it.
- Small docs are fine when they keep ownership and boundaries sharp.
- Keep repo-wide rule docs flat until a topic clearly needs its own directory.
- Use subdirectories for service-owned, module-owned, or feature-owned docs when that keeps them separate from repo-wide rules.
- Avoid over-categorized hierarchies and umbrella docs with weak boundaries.

## Rule Shape

- Prefer unconditional rules.
- Do not write soft rules with words like `usually`, `generally`, or `normally`.
- State the unconditional rule or the explicit exception.
- Prefer narrowing scope or splitting a rule over adding exceptions.
- If a rule needs many exceptions, the rule or the document boundary is probably wrong.

## Ownership

This file defines the documentation system itself: purpose and placement rules. It does not own product or codebase rules beyond that.

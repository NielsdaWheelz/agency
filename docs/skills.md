# skills and prompts

this document defines how agent skills and prompts are managed.

## goals

- make guidance discoverable and enforceable
- separate binding rules from advisory guidance
- keep prompts short, targeted, and versioned

## structure

- binding rules: `docs/standards/binding.md`
- advisory guidance: `docs/standards/advisory.md`
- contracts: `docs/contracts/*`
- prompts: `.claude/prompts/*`

## rules

1. prompts must be task-scoped and named for the task.
2. prompts must not conflict with binding rules.
3. prompt changes require review and changelog note in the file header.
4. if a prompt enforces a contract, the contract must exist in `docs/contracts/*`.

## stubs

- prompt versioning scheme
- skill taxonomy and ownership

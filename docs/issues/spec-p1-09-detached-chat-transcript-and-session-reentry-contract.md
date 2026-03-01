# [p1][spec][design] detached chat transcript and session reentry contract

labels: `p1`, `type:design`, `area:spec`

## summary
define the invocation chat/history contract so users can inspect full activity,
send follow-up prompts, and re-enter/detach repeatedly without losing context.

## context
- section: v2.1 parity baseline
- source: docs/v2.1/constitution.md
- details:
  - detached users need visibility into prompts, assistant messages, tool usage,
    and raw logs from one invocation timeline.
  - chat continuation after detach requires write-path semantics and clear idempotency/error behavior.
  - history and restore UX must be compatible with both interactive and scriptable flows.

## acceptance criteria
- [ ] define daemon endpoints/DTOs for transcript read and follow-up prompt write
- [ ] specify event ordering and cursor semantics for interactive history navigation
- [ ] define CLI behaviors for enter/chat/detach/re-enter flows with stable JSON mode
- [ ] add tests for repeated detach/re-entry and follow-up prompt continuity

# Overrides

## Scope

This document covers configuration and code escape hatches.

## Configuration Overrides

- Directory overrides are explicit: `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, and `AGENCY_CACHE_DIR`.
- CLI flags may override user defaults only on surfaces that document that precedence.
- `--repo` is a repo ref selector, not a filesystem path.
- `--worktree` is the explicit worktree selector for `agency agent start`.
- `--path` is the explicit checkout-path override for path-targeted commands such as `agency init` and `agency doctor`.
- `agency worktree create <name>` takes the worktree name positionally; there is no `--name` compatibility flag.
- `agency context use <worktree-ref>` takes the worktree ref positionally and may use `--repo` to scope lookup.
- `agency agent start` takes no positional worktree argument; there is no compatibility path for the removed `agency agent start <worktree-ref>` spelling.
- `--agency-config` is the explicit override for the selected agency config file.
- `agency agent start` accepts `--agency-config` and uses it before repo-local and per-repo agency config.
- Daemon APIs that accept an agency config override require an absolute path.
- Runner and editor command mappings must stay explicit in user config.
- Runner-specific model and effort defaults belong in `runner_defaults`, not in `defaults`.
- Legacy verb-first target forms are removed. Targeted commands should use noun-scoped target-first spellings such as `agency repo <repo-ref>`, `agency worktree <worktree-ref> open`, and `agency agent <invocation-ref> kill`.

## Dead Code

- Delete dead code by default.
- Do not keep compatibility layers or aliases once the canonical surface has changed unless the compatibility is an intentional public contract.

## Code Escape Hatches

- Keep test-only overrides injected through dependencies, not hidden global state.
- Any override that changes safety-sensitive behavior should be local, explicit, and documented in the owning package or doc.

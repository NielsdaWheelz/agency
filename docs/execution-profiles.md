# Execution Profiles

## Scope

This document owns execution-profile selection, runner environment materialization, managed checkout placement, and the hard cutover rules.

## Cutover

- `config.json` and `agency.json` are version `4`.
- Repo index, repo records, integration worktree metadata, invocation metadata, task metadata, and worktree merge state use schema version `2.0`.
- Older config and metadata versions are rejected.
- Agency does not support mixed old/new worktree layouts.
- Agency does not provide legacy path derivation, legacy managed-tree detection, or schema compatibility branches.
- The reset path for incompatible local state is to remove old state and reinitialize with current commands.

## Model

- An execution profile is a symbolic label such as `personal`, `work`, or `client-a`.
- User `config.json` defines profile env values in `execution_profiles`.
- User `config.json` selects the fallback profile with `defaults.execution_profile`.
- Repo `agency.json` may select a repo profile with `execution.profile`.
- Repo `agency.json` may select checkout placement with `execution.checkout_root`.
- Repo config stays symbolic and secret-free. It must not define profile env vars.
- `AGENCY_DATA_DIR` stores metadata, events, logs, checkpoints, and sockets. It does not determine checkout placement.

## User Config

```json
{
  "version": 4,
  "defaults": {
    "runner": "claude-code",
    "editor": "code",
    "base_branch": "main",
    "execution_profile": "personal"
  },
  "execution_profiles": {
    "personal": {
      "env": {
        "CODEX_HOME": "/Users/me/.codex-personal",
        "CLAUDE_CONFIG_DIR": "/Users/me/.claude",
        "GH_CONFIG_DIR": "/Users/me/.config/gh-personal"
      }
    },
    "work": {
      "env": {
        "CODEX_HOME": "/Users/me/.codex-work",
        "CLAUDE_CONFIG_DIR": "/Users/me/.claude-work",
        "GH_CONFIG_DIR": "/Users/me/.config/gh-work"
      }
    }
  }
}
```

- `defaults.execution_profile` is required.
- `execution_profiles` is required.
- Every referenced profile label must exist locally.
- Profile labels must match `^[a-z0-9]+(-[a-z0-9]+)*$`.
- Profile env values are opaque strings. Agency does not expand `~`, substitute variables, or interpret shell syntax.

## Repo Config

```json
{
  "version": 4,
  "scripts": {
    "setup": { "path": "scripts/agency_setup.sh", "timeout": "10m" },
    "verify": { "path": "scripts/agency_verify.sh", "timeout": "30m" },
    "archive": { "path": "scripts/agency_archive.sh", "timeout": "5m" }
  },
  "execution": {
    "profile": "work",
    "checkout_root": "repo-sibling"
  }
}
```

- `execution.profile` is optional.
- `execution.checkout_root` is optional.
- `execution.checkout_root` must be `repo-sibling` or an absolute path.
- Omitted `execution.checkout_root` means `repo-sibling`.
- Relative checkout roots are invalid.

## Resolution

- The daemon resolves final execution policy for mutation surfaces that create or relaunch runner processes.
- On `agency task start`, `agency task <task-ref> retry`, and `agency agent start`, effective execution-profile precedence is explicit `--execution-profile`, then selected `agency.json` `execution.profile`, then user `config.json` `defaults.execution_profile`.
- Other daemon-owned worktree flows use the profile persisted on the selected integration worktree.
- Effective checkout-root precedence is selected `agency.json` `execution.checkout_root`, then `repo-sibling`.
- `--agency-config` remains the explicit repo-config file override on `agency task start`, `agency task <task-ref> retry`, `agency agent start`, `agency doctor`, and `agency worktree <worktree-ref> pr merge`.
- There is no cwd-based identity fallback.
- There is no environment-derived identity fallback inside Agency.

## Checkout Placement

- `repo-sibling` resolves to `<canonical-repo-parent>/.agency/checkouts/<repo-id>/`.
- Absolute `execution.checkout_root` resolves to `<execution.checkout_root>/<repo-id>/`.
- Integration worktrees live at `<checkout-root>/worktrees/<worktree-name>-<worktree-id-short>`.
- Sandboxes live at `<checkout-root>/sandboxes/<invocation-id>`.
- Managed checkout roots must be absolute after resolution.
- Managed checkout roots must not equal, contain, or be contained by the canonical repo root.
- Sandbox paths must not equal, contain, or be contained by their integration worktree path.
- Managed-tree detection uses marker files plus store metadata, not `AGENCY_DATA_DIR` path prefixes.

## Launch

- The daemon materializes runner env from the resolved execution profile.
- The same profile applies to headed, headless, retry, recreate, and daemon-owned relaunch flows.
- Headless runner env precedence is daemon process environment, resolved execution-profile env, explicit request env overrides, then daemon-owned safety env.
- Headed tmux starts receive resolved execution-profile env plus explicit request env; headed recreate receives current execution-profile env only.
- Daemon-owned noninteractive Git/`gh`/verify/archive cleanup flows use the persisted worktree profile env plus daemon-owned safety env.
- Agency persists env key names for explainability. It does not persist env values.
- If Git, SSH, or `gh` identity should follow the same account, set the relevant variables in the profile env, such as `GH_CONFIG_DIR`, `GH_TOKEN`, `GIT_SSH_COMMAND`, `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, and `GIT_COMMITTER_EMAIL`.

## Store

- Integration worktree metadata persists `tree_path`, `checkout_root`, and `execution_profile`.
- Invocation metadata persists `sandbox_path`, `checkout_root`, `execution_profile`, and `custom_env_keys`.
- Task metadata persists `worktree_path`, `checkout_root`, and `execution_profile`.
- Store scans read persisted paths from metadata instead of deriving paths from `AGENCY_DATA_DIR`.
- Task start and retry fingerprints include execution-profile label, resolved checkout root, runner options, prompt hash, and env key names. They do not include raw env values.

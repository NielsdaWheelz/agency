# Configuration

## Scope

This document owns setup and the version `4` schemas for `config.json` and `agency.json`.

For file locations and precedence, see [environment.md](environment.md).

## Setup

1. Run `agency config init`.
2. Edit `config.json` when you need different runner defaults, Claude `permission_mode`, or command mappings.
3. Register a repo with `agency repo add /path/to/repo`.
4. Run `agency init --path /path/to/repo` for local per-repo config, or `agency init --path /path/to/repo --repo-config` for the shareable repo file at the registered repo canonical root.

`agency config init` owns user config. `agency init` owns repo config and repo scripts only. Integration worktrees and sandboxes do not own repo-shared config.
Version `4` is required. `config.json` and `agency.json` files with any other version are invalid; recreate them with the current init commands.

## User Config: `config.json`

- `version` must be `4`.
- `defaults.runner` and `defaults.editor` are required.
- `defaults.base_branch` is optional.
- `defaults.execution_profile` is required and names the default execution profile.
- `execution_profiles` is required.
- Each `execution_profiles.<name>.env` maps environment variable names to opaque string values.
- Profile labels must match `^[a-z0-9]+(-[a-z0-9]+)*$`.
- Every profile referenced by `defaults.execution_profile`, `agency.json` `execution.profile`, or `--execution-profile` must exist in user config.
- Agency does not expand `~`, substitute variables, or interpret shell syntax in profile env values.
- `runners` maps canonical runner ids to a single executable name.
- `editors` maps editor ids to a single executable name.
- Runner and editor command mappings must be a single executable with no inline args. Use a wrapper script if you need args.
- `runner_defaults` is optional.
- Typed `runner_defaults` are supported only for `claude-code`, `codex`, and `cursor`.
- `runner_defaults.claude-code` supports `model`, `effort`, and `permission_mode`.
- `runner_defaults.codex` supports `model` and `effort`.
- `runner_defaults.cursor` supports `model`.
- Each typed runner default must set at least one supported field.
- `runner_defaults.claude-code.permission_mode` is user-config only.
- Unknown fields and wrong types are rejected.

Supported canonical runner ids: `claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`.

```json
{
  "version": 4,
  "defaults": {
    "runner": "claude-code",
    "editor": "code",
    "base_branch": "main",
    "execution_profile": "personal"
  },
  "runner_defaults": {
    "claude-code": {
      "model": "claude-opus-4-7[1m]",
      "effort": "max",
      "permission_mode": "bypassPermissions"
    },
    "codex": {
      "model": "gpt-5.4",
      "effort": "xhigh"
    },
    "cursor": {
      "model": "sonnet-4.6-thinking"
    }
  },
  "runners": {
    "codex": "codex",
    "claude-code": "claude"
  },
  "editors": {
    "code": "code"
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

`defaults` does not accept runner-specific fields such as `model`, `effort`, `thinking`, or Claude `permission_mode`. Put those under `runner_defaults`.

## Repo Config: `agency.json`

- `version` must be `4`.
- `scripts.setup.path`, `scripts.verify.path`, and `scripts.archive.path` are required.
- `scripts.*.timeout` is optional.
- Timeouts use Go duration strings such as `10m` or `1h`.
- Timeout values must stay within `1m` to `24h`.
- Relative script paths resolve from the directory that contains the selected `agency.json`.
- Repo-shared `agency.json` lives at the registered repo canonical root: `PreferredRoot`, else `RepoRootLastSeen`.
- Integration worktrees and sandboxes are execution surfaces only and do not own repo-shared `agency.json`.
- Repo-shared writes must target the canonical repo root, not an agency-managed worktree.
- `execution.profile` is optional and selects a user-defined execution profile for the repo.
- `execution.checkout_root` is optional. It must be `repo-sibling` or an absolute path.
- Omitted `execution.checkout_root` means `repo-sibling`.
- Relative checkout roots are invalid.
- Repo config may select a profile label but must not define profile env vars.
- `runner_defaults` is optional.
- Repo-scoped typed `runner_defaults` are supported only for `claude-code`, `codex`, and `cursor`.
- Repo-scoped `runner_defaults.claude-code` supports `model` and `effort`.
- `runner_defaults.claude-code.permission_mode` is not part of `agency.json`.
- `runner_defaults.cursor.effort` is invalid.
- Unknown fields and wrong types are rejected.

```json
{
  "version": 4,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "5m"
    }
  },
  "runner_defaults": {
    "claude-code": {
      "model": "claude-opus-4-7[1m]",
      "effort": "max"
    }
  },
  "execution": {
    "profile": "work",
    "checkout_root": "repo-sibling"
  }
}
```

## Execution Profiles

- Effective execution-profile precedence is explicit `--execution-profile`, then selected `agency.json` `execution.profile`, then user `config.json` `defaults.execution_profile`.
- The daemon resolves the final profile and materializes runner env for headed, headless, retry, and recreate launches.
- For headless runner launches and daemon-owned noninteractive Git/`gh`/script flows, profile env overlays the daemon process environment and daemon-owned safety env wins for safety-sensitive keys.
- Headed tmux launches receive the resolved profile env plus explicit request env; headed recreate materializes the current profile env only and does not resurrect old request env values from the daemon process.
- Agency persists profile labels and explicit request/custom env key names for explainability. It does not persist profile env key names or any env values.
- Use profile env for account-specific state such as `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `GH_CONFIG_DIR`, `GH_TOKEN`, `GIT_SSH_COMMAND`, or Git author variables.
- Agency does not infer Git identity from runner identity. Put Git and `gh` env in the same profile when they should move together.

## Claude Startup Contract

- Agency owns Claude `model`, `effort`, and `permission_mode`.
- Set Claude `model` and `effort` through typed `runner_defaults` or explicit CLI flags on `agency task start`, `agency task <task-ref> retry`, or `agency agent start`.
- Claude `permission_mode` comes from explicit `--permission-mode`, then user `config.json` `runner_defaults.claude-code.permission_mode`.
- Repo `agency.json` may set Claude `model` and `effort`; it cannot set Claude `permission_mode`.
- Headed Claude starts launch interactive `claude` in tmux. Agency applies configured Claude settings without the print/stream-json startup path.
- Headless Claude starts launch daemon-backed `claude` in print/stream-json mode. Agency owns the Claude print/stream flags and the effective permission behavior.
- Do not pass Claude `--model`, `--effort`, or `--permission-mode` through runner command mappings or `--runner-arg`.

## Split

- `config.json` owns global defaults, explicit runner and editor command mappings, and the user default for Claude `permission_mode`.
- `agency.json` owns repo scripts and optional repo-scoped runner defaults.
- Repo-shared `agency.json` belongs to the registered repo canonical root, not to integration worktrees or sandboxes.
- Use repo-scoped runner defaults when a repo needs different model or effort settings from your user defaults.
- Repo config may override user Claude `model` and `effort`. Claude `permission_mode` comes from explicit `--permission-mode`, then user config.

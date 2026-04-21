# Configuration

## Scope

This document owns setup and the version `3` schemas for `config.json` and `agency.json`.

For file locations and precedence, see [environment.md](environment.md).

## Setup

1. Run `agency config init`.
2. Edit `config.json` when you need different runner defaults, Claude `permission_mode`, or command mappings.
3. Register a repo with `agency repo add /path/to/repo`.
4. Run `agency init --path /path/to/repo` for local per-repo config, or `agency init --path /path/to/repo --repo-config` for the shareable repo file at the registered repo canonical root.

`agency config init` owns user config. `agency init` owns repo config and repo scripts only. Integration worktrees and sandboxes do not own repo-shared config.

## User Config: `config.json`

- `version` must be `3`.
- `defaults.runner` and `defaults.editor` are required.
- `defaults.base_branch` is optional.
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
  "version": 3,
  "defaults": {
    "runner": "claude-code",
    "editor": "code",
    "base_branch": "main"
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
  }
}
```

`defaults` does not accept runner-specific fields such as `model`, `effort`, `thinking`, or Claude `permission_mode`. Put those under `runner_defaults`.

## Repo Config: `agency.json`

- `version` must be `3`.
- `scripts.setup.path`, `scripts.verify.path`, and `scripts.archive.path` are required.
- `scripts.*.timeout` is optional.
- Timeouts use Go duration strings such as `10m` or `1h`.
- Timeout values must stay within `1m` to `24h`.
- Relative script paths resolve from the directory that contains the selected `agency.json`.
- Repo-shared `agency.json` lives at the registered repo canonical root: `PreferredRoot`, else `RepoRootLastSeen`.
- Integration worktrees and sandboxes are execution surfaces only and do not own repo-shared `agency.json`.
- Repo-shared writes must target the canonical repo root, not an agency-managed worktree.
- `runner_defaults` is optional.
- Repo-scoped typed `runner_defaults` are supported only for `claude-code`, `codex`, and `cursor`.
- Repo-scoped `runner_defaults.claude-code` supports `model` and `effort`.
- `runner_defaults.claude-code.permission_mode` is not part of `agency.json`.
- `runner_defaults.cursor.effort` is invalid.
- Unknown fields and wrong types are rejected.

```json
{
  "version": 3,
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
  }
}
```

## Claude Startup Contract

- Agency owns Claude `model`, `effort`, and `permission_mode`.
- Set Claude `model` and `effort` through typed `runner_defaults` or `agency agent start --model/--effort`.
- Set Claude `permission_mode` only in user `config.json`.
- Headed Claude starts launch interactive `claude` in tmux. Agency applies configured Claude settings without the print/stream-json startup path.
- Headless Claude starts launch daemon-backed `claude` in print/stream-json mode. Agency owns the Claude print/stream flags and the effective permission behavior.
- Do not pass Claude `--model`, `--effort`, or `--permission-mode` through runner command mappings or `--runner-arg`.

## Split

- `config.json` owns global defaults, explicit runner and editor command mappings, and user-scoped Claude `permission_mode`.
- `agency.json` owns repo scripts and optional repo-scoped runner defaults.
- Repo-shared `agency.json` belongs to the registered repo canonical root, not to integration worktrees or sandboxes.
- Use repo-scoped runner defaults when a repo needs different model or effort settings from your user defaults.
- Repo config may override user Claude `model` and `effort`. User config remains the only source of Claude `permission_mode`.

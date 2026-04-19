# Configuration

## Scope

This document owns setup and the version `2` schemas for `config.json` and `agency.json`.

For file locations and precedence, see [environment.md](environment.md).

## Setup

1. Run `agency config init`.
2. Edit `config.json` only if you need different runner defaults or command mappings.
3. Register a repo with `agency repo add /path/to/repo`.
4. Run `agency init --path /path/to/repo` for local per-repo config, or `agency init --path /path/to/repo --repo-config` for shareable repo files.

`agency config init` owns user config. `agency init` owns repo config and repo scripts only.

## User Config: `config.json`

- `version` must be `2`.
- `defaults.runner` and `defaults.editor` are required.
- `defaults.base_branch` is optional.
- `runners` maps canonical runner ids to a single executable name.
- `editors` maps editor ids to a single executable name.
- Runner and editor command mappings must be a single executable with no inline args. Use a wrapper script if you need args.
- `runner_defaults` is optional.
- Typed `runner_defaults` are supported only for `claude-code`, `codex`, and `cursor`.
- Each typed runner default must set at least one of `model` or `effort`.
- `runner_defaults.cursor.effort` is invalid.
- Unknown fields and wrong types are rejected.

Supported canonical runner ids: `claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`.

```json
{
  "version": 2,
  "defaults": {
    "runner": "codex",
    "editor": "code",
    "base_branch": "main"
  },
  "runner_defaults": {
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

Not supported in version `2`:

- `defaults.model`
- `defaults.effort`
- `defaults.thinking`
- version `1`

## Repo Config: `agency.json`

- `version` must be `2`.
- `scripts.setup.path`, `scripts.verify.path`, and `scripts.archive.path` are required.
- `scripts.*.timeout` is optional.
- Timeouts use Go duration strings such as `10m` or `1h`.
- Timeout values must stay within `1m` to `24h`.
- Relative script paths resolve from the directory that contains the selected `agency.json`.
- `runner_defaults` is optional and follows the same typed rules as user config.
- Unknown fields and wrong types are rejected.

```json
{
  "version": 2,
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
    "codex": {
      "model": "gpt-5.4",
      "effort": "xhigh"
    }
  }
}
```

## Split

- `config.json` owns global defaults and explicit runner and editor command mappings.
- `agency.json` owns repo scripts and optional repo-scoped runner defaults.
- Use repo-scoped runner defaults when a repo needs different model or effort settings from your user defaults.

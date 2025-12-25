# PR-00 — spec

**repo bootstrap + core scaffolding**

---

## goal

establish the rust workspace, shared core crate, config + path contracts, and shared types that all later PRs depend on.

this PR defines **names, shapes, paths, and boundaries**. it must not implement behavior.

---

## scope (in)

### workspace + crates

create a rust workspace with exactly these members:

```
.
├─ Cargo.toml              # workspace
└─ crates/
   ├─ agency-core/         # shared library
   │  └─ src/
   │     ├─ lib.rs
   │     ├─ ids.rs
   │     ├─ types.rs
   │     ├─ errors.rs
   │     ├─ config.rs
   │     └─ paths.rs
   ├─ agency-cli/          # binary: agency
   │  └─ src/main.rs
   └─ agencyd/             # binary: agencyd
      └─ src/main.rs
```

no additional crates, binaries, or scripts.

rust edition: **2021**

---

## shared core contracts (`agency-core`)

### serde policy (applies to all types)

* **struct fields:** use `#[serde(rename_all = "snake_case")]` on structs
* **protocol enums:** use explicit `#[serde(rename = "...")]` on each variant - never rely on `rename_all` for externally visible enums
* **error codes:** serialize as EXACT variant name (no `rename_all`)

this prevents accidental drift when variants are added.

---

### run id

```rust
pub struct RunId(String);
```

* generated via ULID
* string form: `r_<ULID>`
* implements:

  * `new() -> RunId`
  * `as_str(&self) -> &str`
  * `parse(s: &str) -> Result<RunId, ParseError>` (optional but recommended)
* serde serialize/deserialize as string

---

### run state

```rust
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RunState {
    Queued,
    Running,
    Completed,
    Failed,
    Killed,
}
```

**serde:** `rename_all = "snake_case"` is acceptable here since all variants happen to work correctly, but explicit renames per variant is also fine.

no other states.

---

### runner kind

```rust
#[derive(Serialize, Deserialize)]
pub enum RunnerKind {
    #[serde(rename = "claude_code")]
    ClaudeCode,
    #[serde(rename = "codex")]
    Codex,
}
```

**serde:** explicit `rename` on each variant. never rely on `rename_all` for protocol enums.

---

### run summary (shared json shape)

```rust
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct RunSummary {
    pub id: RunId,
    pub state: RunState,
    pub name: Option<String>,
}
```

later PRs may extend this, but fields above are required.

**note:** `schema_version` lives only in the top-level json envelope, not in domain types.

---

### error model

#### error codes enum

```rust
pub enum ErrorCode {
    E_NOT_GIT_REPO,
    E_BAD_REF,
    E_INVALID_PATH,
    E_INPUT_NOT_FILE,
    E_TMUX_NOT_FOUND,
    E_TMUX_START_FAILED,
    E_WORKTREE_CREATE_FAILED,
    E_RUNNER_NOT_CONFIGURED,
    E_RUN_NOT_FOUND,
    E_INVALID_STATE,
    E_CLEANUP_FAILED,
    E_BRANCH_EXISTS,
    E_DB_ERROR,
    E_PERMISSION_DENIED,
}
```

**serde:** serialize as EXACT variant name (no `rename_all`). example: `"E_NOT_GIT_REPO"`

#### error envelope

```rust
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct ErrorEnvelope {
    pub code: ErrorCode,
    pub message: String,
    pub details: Option<serde_json::Value>,
}
```

---

## json output convention (frozen)

define a constant:

```rust
pub const SCHEMA_VERSION: u32 = 1;
```

all `--json` outputs must follow:

```json
{
  "ok": true,
  "schema_version": 1,
  "data": { ... }
}
```

or on error:

```json
{
  "ok": false,
  "schema_version": 1,
  "error": {
    "code": "E_BAD_REF",
    "message": "...",
    "details": { ... }
  }
}
```

no additional top-level keys.

---

## config contract (`config.toml`)

format: **TOML**

### default locations

* mac: `~/Library/Application Support/agency/config.toml`
* linux: `~/.config/agency/config.toml`

precedence:

1. `--config <path>`
2. `$agency_CONFIG`
3. platform default

### env override

* `$agency_SOCKET` overrides daemon socket path

### schema

```toml
[daemon]
socket_path = "~/.agency/agency.sock"

[runners.claude_code]
exec = "/absolute/path/to/claude-code"
default_args = ["--some-flag"]

[runners.codex]
exec = "/absolute/path/to/codex"
default_args = []
```

rust struct (config.rs):

```rust
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Config {
    pub daemon: DaemonConfig,
    pub runners: RunnersConfig,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct DaemonConfig {
    pub socket_path: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RunnersConfig {
    pub claude_code: Option<RunnerConfig>,
    pub codex: Option<RunnerConfig>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RunnerConfig {
    pub exec: String,
    #[serde(default)]
    pub default_args: Vec<String>,
}
```

config loader signature:

```rust
pub fn load_config(explicit_path: Option<&Path>) -> Result<Config, ConfigError>
```

rules:

* `deny_unknown_fields` on all config structs prevents typos and unknown keys
* runner fields are `Option<RunnerConfig>` to allow partial config
* `default_args` has `#[serde(default)]` so it can be omitted
* `~` must be expanded in `socket_path` and runner `exec` paths
* missing config file is allowed (return error or default)
* typo like `[runners.claude_cod]` will fail at parse time due to `deny_unknown_fields`

---

## filesystem path contracts (`paths.rs`)

data root (fixed in v1):

```
~/.agency
```

helpers to expose:

```rust
fn data_root() -> PathBuf
fn db_path() -> PathBuf            // ~/.agency/agency.db
fn runs_root() -> PathBuf          // ~/.agency/runs
fn worktrees_root() -> PathBuf     // ~/.agency/worktrees
fn locks_root() -> PathBuf         // ~/.agency/locks
fn default_socket_path() -> PathBuf // ~/.agency/agency.sock
```

no IO side effects in these helpers.

---

## logging

* use `tracing`
* subscriber via `tracing_subscriber`
* log level controlled by `RUST_LOG`
* no `println!` outside CLI output layer

---

## dependencies (pin majors)

agency-core:

* serde = "1"
* serde_json = "1"
* toml = "0.8"
* ulid = "1"
* thiserror = "1"
* directories = "5"
* tracing = "0.1"

agency-cli / agencyd:

* clap = "4" (derive)
* tracing = "0.1"
* tracing-subscriber = "0.3"
* agency-core (path dep)

---

## explicit non-goals (forbid)

* no sqlite
* no tmux
* no git
* no daemon ipc
* no background tasks
* no run logic
* no migrations
* no network code

---

## acceptance criteria

* `cargo build` succeeds
* `cargo test` passes
* `RunId::new()` produces `r_`-prefixed ULID
* config precedence works (unit test)
* path helpers return correct suffixes
* `agency --help` and `agency --version` work (clap stub)
* no extra crates or binaries

---

# PR-00 — prompt pack (for claude-code)

```
you are implementing PR-00 of the agency project.

this PR is foundational. do not invent behavior. do not add features.
follow the spec exactly.

## context

this project is a terminal-native orchestrator for AI coding runs.
PR-00 establishes the rust workspace, shared contracts, and config/path primitives.

future PRs depend on names, shapes, and boundaries you define here.

## hard rules

- implement only what is explicitly in scope
- do not add sqlite, tmux, git, ipc, or run logic
- do not add commands or flags beyond scaffolding
- do not change public names once introduced
- use rust edition 2021
- use snake_case for all serde json keys
- every `--json` envelope must include `schema_version: 1`
- no println! outside cli layer

## tasks

1. create the rust workspace and crates exactly as specified
2. implement `agency-core` with:
   - RunId (ULID, prefixed)
   - RunState
   - RunnerKind
   - RunSummary
   - ErrorCode + ErrorEnvelope
   - config parsing (toml + precedence + env overrides)
   - filesystem path helpers
3. wire minimal `agency` and `agencyd` binaries that compile and do nothing except initialize logging
4. add minimal unit tests for:
   - RunId format
   - config precedence
   - path helper suffixes

## forbidden

- no placeholder TODO behavior
- no mock implementations of future features
- no extra crates, bins, or modules
- no deviation from specified json shapes

## deliverables

- compiling workspace
- passing tests
- clean, minimal code

stop when done. do not speculate about future PRs.
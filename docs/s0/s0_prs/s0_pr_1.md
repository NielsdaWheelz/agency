# pr-01 spec — daemon + ipc + cli autospawn (s0 foundation)

## goal

introduce `agencyd` (daemon) and a length-prefixed unix-socket rpc channel so `agency` can reliably talk to a single local daemon instance, auto-spawning it when needed.

this pr adds **infrastructure only**. it must not implement run/worktree/tmux/sqlite logic beyond minimal boot wiring.

## hard constraints

- do not modify `docs/agency.md` or `docs/slices.md`
- do not introduce any s1/s2 commands (`ls/show/diff/merge`)
- no user-facing `agency ping` command; ping is **internal only**
- unix domain socket only (no tcp)
- no pidfiles/lockfiles; single-daemon exclusivity is enforced by socket bind
- cli never opens sqlite (daemon owns db in later prs)
- all user-facing commands (none new here) must still support `--json` (plumbing only)

## crates and boundaries

- `agency-core`
  - defines ipc protocol types + framing
  - defines socket path resolution helpers
  - defines shared error envelope + codes (extend as needed)

- `agencyd`
  - binds unix socket
  - accepts connections and serves rpc requests
  - logs to file

- `agency-cli`
  - client for rpc
  - auto-spawns daemon if connect fails
  - uses internal ping to verify daemon ready

## socket path rules

- default socket path (mac+linux): `~/.agency/agency.sock`
- override: `$agency_SOCKET` (absolute path required; must pass `Path::is_absolute()`)
  - no tilde expansion; value used verbatim
- daemon creates parent directories as needed

stale socket handling:
- if connect succeeds → ping
- else:
  - if socket path doesn't exist → spawn daemon
  - else if socket path exists but connect fails → attempt to remove socket file (best-effort), then spawn daemon

## daemon spawn rules (cli side)

- spawn command: `agencyd --socket <socket_path> --log-file <log_path>`
- default log path: `~/.agency/logs/agencyd.log`
- ensure log file parent directories exist before spawn
- spawn via `Command`:
  - set stdin to null
  - set stdout/stderr to the log file (append)
  - do not wait
  - do not set process group / daemonize
  - return immediately
- after spawn: retry connect+ping with backoff:
  - up to 20 attempts
  - sleep 50ms between attempts
  - total ~1s budget
- if still failing: return `E_DAEMON_START_FAILED`

## rpc protocol (length-prefixed json)

transport: unix stream socket.

message framing (both directions):
- 4-byte unsigned length prefix, little-endian (`u32 LE`)
- followed by exactly `len` bytes of utf-8 json payload
- reject frames larger than `MAX_FRAME_LEN = 10_485_760` bytes (10 MB) with `E_IPC_PROTOCOL_ERROR`

payload encoding:
- requests are JSON via `serde_json`
- replies are JSON via `serde_json`

### reply envelope (required)

all replies (success and error) use the same top-level envelope:

success:
```json
{
  "protocol_version": 1,
  "ok": true,
  "response": { "type": "Pong", "daemon_pid": 123, "build_version": "0.1.0" }
}
```

error:
```json
{
  "protocol_version": 1,
  "ok": false,
  "error": { "code": "E_IPC_PROTOCOL_ERROR", "message": "...", "details": {} }
}
```

- `response` is a tagged enum (serde `#[serde(tag = "type")]`)
- `error` uses the shared error envelope type from agency-core
- `build_version` comes from `env!("CARGO_PKG_VERSION")`

### request types

`Request` (externally stable for v1):
- `Ping {}`

## new/updated error codes (agency-core)

add these codes to the shared error enum:

* `E_SOCKET_PATH_INVALID`
* `E_DAEMON_CONNECT_FAILED`
* `E_DAEMON_START_FAILED`
* `E_IPC_PROTOCOL_ERROR`

## cli behavior changes

add a shared internal helper in `agency-cli`:

* `ensure_daemon() -> Client`

  * resolve socket path
  * try connect + ping
  * on failure, spawn daemon and retry
  * returns a connected client

no new public subcommands required in this pr.

## daemon behavior

* `agencyd` parses flags:

  * `--socket <path>` (required)
  * `--log-file <path>` (required)
* daemon binds the socket; if bind fails because the socket is already in use:

  * exit with non-zero status
* accept loop:

  * spawn a tokio task per connection
  * each task: read exactly one framed request, write one framed response, then close
  * daemon reads exactly one request per connection then closes, even if client keeps open
  * client should open new connection per call

## logging

* use `tracing` + `tracing_subscriber`
* file appender (rolling not needed)
* log format: compact
* daemon logs:

  * startup (socket path, pid)
  * each request handled (run pid, request type, ok/error)
  * shutdown
* log file is append-only

## tests

### unit tests (agency-core)

* socket path resolution:

  * default path when `$agency_SOCKET` unset
  * env override requires absolute path; relative path errors with `E_SOCKET_PATH_INVALID`
* framing encode/decode roundtrip:

  * given request object -> bytes -> decode -> same object
  * invalid length prefix / truncated payload -> error

### integration test (gated, but should run on mac+linux CI)

* spawn `agencyd` as child process using a temp socket path under a temp dir
* connect with client and send `Ping`
* assert reply envelope: `protocol_version == 1`, `ok == true`, `response.daemon_pid > 0`
* cleanup:
  * terminate child
  * wait with timeout
  * if not dead, kill
  * delete temp dir

## acceptance criteria

this pr is complete when:

1. `agencyd` starts and binds the socket at the resolved path
2. `agency-cli` can connect and successfully ping via length-prefixed rpc
3. `agency-cli` auto-spawns `agencyd` when daemon is not running and then pings successfully
4. concurrent `agency` invocations do not create multiple daemons (socket bind exclusivity)
5. tests pass (unit + integration)

---

# claude-code prompt pack — pr-01

you are implementing PR-01 per the spec in docs/prs/pr-01-daemon-ipc.md.

## repository context
- rust workspace with crates: agency-core, agency-cli, agencyd (already created in pr-00)
- shared types live in agency-core
- cli must auto-spawn daemon
- rpc framing is length-prefixed json over unix domain sockets

## hard rules
- do not modify docs/agency.md or docs/slices.md
- do not add any s1/s2 commands or functionality
- no user-facing `agency ping` command
- no pidfile/lockfile; use socket bind exclusivity
- unix socket only; no tcp listeners
- keep behavior minimal and deterministic

## implementation constraints

### rust module structure

lock the following module layout:

**agency-core/src/ipc/mod.rs:**
- `pub enum Request { Ping }`
- `pub enum Response` (tagged with `#[serde(tag = "type")]`):
  - `Pong { daemon_pid: u32, build_version: String }`
- `pub struct RpcEnvelope<T> { protocol_version: u32, ok: bool, response: Option<T>, error: Option<ErrorEnvelope> }`
- `pub const MAX_FRAME_LEN: usize = 10_485_760;` (10 MB)
- `pub async fn read_frame(stream: &mut UnixStream) -> Result<Vec<u8>, AgentError>`
- `pub async fn write_frame(stream: &mut UnixStream, bytes: &[u8]) -> Result<(), AgentError>`

**agency-cli/src/daemon.rs:**
- `pub(crate) async fn ensure_daemon(cfg: &Config) -> Result<Client, AgentError>`

**agencyd/src/main.rs:**
- parse args, init logging, bind socket, loop

### tokio requirement

**must use tokio** in all crates:
- `tokio::net::UnixListener/UnixStream`
- `tokio::io::{AsyncReadExt, AsyncWriteExt}`
- server spawns a task per connection

## tasks
1) implement agency-core ipc:
   - Request/Response enums per module structure above
   - RpcEnvelope with protocol_version at top level
   - Error envelope reuse
   - length-prefixed framing helpers with MAX_FRAME_LEN enforcement
   - socket path resolution:
     - default ~/.agency/agency.sock
     - env override $agency_SOCKET (must be absolute via `Path::is_absolute()`, else E_SOCKET_PATH_INVALID)
     - no tilde expansion; value used verbatim

2) implement agencyd:
   - flags: --socket, --log-file (required)
   - initialize logging with tracing + tracing_subscriber (compact format, file appender)
   - bind unix socket; exit non-zero if already bound/in use
   - accept loop:
     - spawn tokio task per connection
     - read one framed Request
     - handle Ping -> RpcEnvelope { protocol_version: 1, ok: true, response: Pong { daemon_pid, build_version } }
     - on error -> RpcEnvelope { protocol_version: 1, ok: false, error: ErrorEnvelope {...} }
     - write framed response then close
     - log each request: run pid, request type, ok/error
   - build_version from `env!("CARGO_PKG_VERSION")`

3) implement agency-cli client + autospawn:
   - Client struct that can connect and ping
   - ensure_daemon() in src/daemon.rs as `pub(crate)`:
     - resolve socket path
     - try connect+ping
     - on failure:
       - if socket path doesn't exist → spawn daemon
       - else if socket path exists but connect fails → remove socket file (best-effort), then spawn daemon
     - spawn `agencyd --socket <sock> --log-file <~/.agency/logs/agencyd.log>`
       - ensure log file parent dirs exist
       - set stdin to null
       - set stdout/stderr to log file (append)
       - do not wait
       - do not set process group / daemonize
       - return immediately
     - retry connect+ping 20 times with 50ms sleep
     - on failure: return E_DAEMON_START_FAILED
   - do not add a public ping subcommand; ensure_daemon can be used by later commands

4) add tests:
   - unit tests for path resolution + framing roundtrip in agency-core
   - unit test: frames larger than MAX_FRAME_LEN rejected with E_IPC_PROTOCOL_ERROR
   - integration test that spawns agencyd with a temp socket/log file, pings it, then:
     - terminate child
     - wait with timeout
     - if not dead, kill
     - delete temp dir

## deliverables
- code in the three crates implementing the above
- tests passing:
  - cargo test -p agency-core
  - cargo test -p agency-cli
  - cargo test -p agencyd
  - cargo test (workspace)

## notes
- **MUST** use tokio in all crates for unix sockets and async io
- framing must be exact: 4-byte u32 little-endian length, then payload bytes
- all replies must use RpcEnvelope with protocol_version: 1 at top level
- build_version from `env!("CARGO_PKG_VERSION")`
- MAX_FRAME_LEN = 10_485_760 bytes (10 MB)
- response is a tagged enum with `type` field
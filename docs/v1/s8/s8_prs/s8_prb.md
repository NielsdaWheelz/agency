PR-B — Offset-Based Logs API + --watch for List Commands

Purpose

Finish v2 usability without a TUI or SSE by adding:
	1.	Offset-based log retrieval (chunked, resumable, followable)
	2.	--watch mode for worktree ls and agent ls using ANSI redraw + polling

This PR intentionally does not introduce:
	•	SSE
	•	TUI frameworks
	•	new daemon background state or long-lived loops (CLI polling loop is foreground, not daemon-side)
	•	new daemon writers

It delivers a “finished” local + ssh experience suitable for v2.

⸻

Scope

In Scope
	•	New daemon logs API with byte offsets
	•	CLI log follow (agent logs --follow)
	•	--watch flag for:
	•	agency worktree ls
	•	agency agent ls
	•	ANSI clear-screen redraw
	•	Interval clamping + defaults

Explicitly Out of Scope
	•	SSE / WebSockets
	•	Bubbletea / TUI
	•	Watch with --json
	•	Event cursors
	•	Repo registry changes
	•	Any new daemon writers

⸻

Daemon API Changes

New Logs Endpoint (Read-Only)

GET /invocations/{ref}/logs

Query Parameters

Param	Type	Default	Notes
repo_id	string	optional	If provided, resolve within that repo only. If omitted, resolve across all repos. If ambiguous, return E_INVOCATION_ID_AMBIGUOUS with candidates.
kind	enum	raw	raw, stderr, stream
offset	int64	0	Byte offset from start of file
limit	int	65536	Max bytes returned; clamped to [1, MAX_LOG_CHUNK]

Clamps
	•	offset >= 0
	•	limit ∈ [1, MAX_LOG_CHUNK]
	•	MAX_LOG_CHUNK = 1_048_576 (1 MB)
	•	Invalid offset or limit → E_INVALID_ARGUMENT

⸻

Response

Uses the standard APIResponse envelope (internal/daemon/read_types.go); only the data payload changes.

Data payload:

    "data": {
      "kind": "raw",
      "data_b64": "BASE64_BYTES",
      "next_offset": 123456,
      "total_bytes": 987654
    }

Semantics
	•	data_b64 = raw bytes, base64-encoded (no UTF-8 assumptions)
	•	next_offset = offset + len(data)
	•	If offset >= total_bytes: return empty data_b64, next_offset = total_bytes
	•	No truncation flags, no tail semantics — client controls paging
	•	Chunks may begin/end mid-line; client must not assume line boundaries

⸻

Implementation Notes (Daemon)
	•	Additive change — support both modes for one cycle:
	•	If offset param is present → offset mode (new fields: data_b64, next_offset, total_bytes)
	•	Else → existing tail mode (TailBytes, existing InvocationLogsData fields: content, truncated, starts_midline, ends_midline)
	•	Existing GetLogsParams and InvocationLogsData remain; new offset fields added alongside. Tail mode deprecated but not removed this PR.
	•	Replace readLogFile(tailBytes) with readLogFileAtOffset(path, offset, limit)
	•	Use os.Open + Seek + io.ReadFull/ReadAtMost
	•	No buffering across requests
	•	No in-memory state
	•	Works for growing files

⸻

CLI Changes

agency agent logs

Usage

agency agent logs <ref> [--repo <repo_id>] [--kind raw|stderr|stream] [--follow] [--offset N]

Flags
	•	--offset N: byte offset to start reading from (default 0)
	•	--follow: poll for new data after reaching EOF

Behavior
Without --follow
	•	Stream pages until EOF:
	1.	Start at offset (default 0)
	2.	Request chunks with limit (default 65536)
	3.	Decode base64 → write bytes to stdout
	4.	Set offset = next_offset
	5.	Stop when next_offset stops advancing (next_offset == offset, i.e. no new data)
	•	Pure client-side paging; no daemon state

With --follow
	•	Initialize offset (default 0)
	•	Page to EOF (same as above)
	•	Then poll loop:
	1.	GET logs with current offset
	2.	If data_b64 non-empty → decode + write
	3.	Set offset = next_offset
	4.	Sleep interval
	•	Exit on Ctrl-C

Defaults
	•	Poll interval: 500 ms
	•	Minimum clamp: 250 ms
	•	Maximum clamp: 5 s

⸻

--watch for List Commands

Commands

agency worktree ls --watch [--interval=500ms]
agency agent ls --watch [--worktree <ref>] [--interval=500ms]

Behavior
	•	Clear screen: \x1b[2J\x1b[H (clear + home) on each tick
	•	Re-execute the underlying list command
	•	Reuse existing renderers:
	•	writeWorktreeLSHumanFromDTO
	•	writeAgentLSHumanFromDTO
	•	Render + flush on each tick
	•	No JSON support with --watch

Entry
	•	Clear screen on entry before first render

Termination
	•	Exit on Ctrl-C (SIGINT)
	•	Ensure cursor is shown at exit (\x1b[?25h)
	•	Print trailing newline on exit so shell prompt doesn't stick mid-line
	•	On error mid-watch: print error to stderr, exit nonzero (do not loop forever)

Interval Rules

Rule	Value	Rationale
Default	500 ms	Feels responsive for interactive use
Minimum	250 ms	Prevents terminal spam and avoids overloading daemon over SSH
Maximum	5 s	Prevents "watch looks dead" footguns

Parsing
	•	Uses time.ParseDuration (Go stdlib)
	•	Accepted: 500ms, 1s, 2.5s
	•	Rejected: 500 (no unit) → error with clear message
	•	Out of bounds → error:

error: --interval must be between 250ms and 5s

Test cases: parse failure (no unit), below minimum, above maximum, valid values at boundaries (250ms, 5s)


⸻

Error Handling & UX (Gold Standard)

Outside Repo + Missing --repo

error: no repo context (not in a git repository)
hint: run `agency repo ls` and re-run with --repo <repo_id>, or `agency repo add <path>`

Ambiguous Name Across Repos

error: ambiguous ref "foo" across multiple repos
hint: re-run with --repo <repo_id>

Error Code Mapping

Condition	Error Code
Invocation ref not found	E_INVOCATION_NOT_FOUND
Ambiguous ref (repo_id omitted, multiple matches)	E_INVOCATION_ID_AMBIGUOUS
Log file missing / kind not available	E_LOG_NOT_FOUND (new code)
Invalid params (offset < 0, bad limit)	E_INVALID_ARGUMENT (new code)

Logs File Missing
	•	Return E_LOG_NOT_FOUND
	•	Message:

error: logs not found for invocation <ref>
hint: invocation may not have started or produced output yet

kind=stream File Missing

Some invocations (headed, or parsing disabled) may not produce stream.jsonl.

	•	Return E_LOG_NOT_FOUND with hint:

error: stream logs unavailable for this invocation
hint: try --kind raw



⸻

Tests

Daemon Tests
	•	Offset read correctness:
	•	Write file abcdef
	•	Read offset 0 limit 2 → ab
	•	Read offset 2 limit 2 → cd
	•	Offset beyond EOF returns empty
	•	Limit clamp enforced

CLI Tests
	•	agent logs (non-follow) pages to EOF:
	•	Fake daemon returns: call1 → data="ab" next=2, call2 → data="cd" next=4, call3 → data="ef" next=6, call4 → empty next=6
	•	Assert stdout == "abcdef"
	•	agent logs --follow with fake daemon client:
	•	Same paging sequence, then poll returns empty, then new data arrives
	•	No real time.Sleep — use fake/injected clock or channel-based control
	•	--watch redraws: fake list responses + "run N iterations then stop" hook (no real sleeps)
	•	Interval parsing: test no-unit rejection, below-min, above-max, boundary values (250ms, 5s)

⸻

Invariants
	•	CLI never reads log files directly
	•	Daemon never buffers log state
	•	No new long-lived background loops in daemon; log handler is stateless per request
	•	No TUI introduced
	•	No new store writes

⸻

Acceptance Criteria
	•	agent logs --follow works for headless invocations
	•	Offset API supports large logs without truncation
	•	worktree ls --watch redraws live
	•	agent ls --watch redraws live
	•	Works over ssh / tailscale
	•	No JSON + watch mixing
	•	No regression to v2 daemon invariants

⸻

Why This Is Gold-Standard
	•	Offset-based logs are simpler, safer, and more debuggable than SSE
	•	Poll-based watch is predictable and robust over SSH
	•	ANSI redraw is universally supported on macOS + Linux
	•	Keeps daemon stateless and auditable
	•	Leaves SSE/TUI as optional future optimizations, not dependencies

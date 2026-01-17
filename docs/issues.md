3) repo_id collision handling is overkill, but you’re missing the real collision: repo moves + path-key

you already track multiple paths. good. but your fallback key uses sha256(abs_path), so moving the repo generates a new repo_key and therefore a new repo_id. you’ll “lose history” on move.

you can accept that for v1, but you should call it out as a limitation:
	•	“path-based repo identity is not stable across moves; moving a non-github repo will be treated as a new repo.”

otherwise you’ll get “why did agency forget my runs” bug reports.

4) init creates stub scripts but doctor requires scripts exist + executable

nice. but your init semantics say “scripts are never overwritten”, and you also say agency.json overwriting requires --force.
edge case: user runs init once, gets stub verify exiting 1, then doctor fails forever until they edit it. that’s intended. just ensure your doctor error message is unmissable: “verify script is a stub and exits 1; replace it”.

also: require scripts be relative to repo root (you implied in init) and enforce it in validation. right now you allow any non-empty string. make it explicit: reject absolute paths and .. path traversal in v1. otherwise you’ve created a path injection footgun.

5) repo_index.json merge behavior: case sensitivity

“paths de-duplicated case-sensitively” is wrong on mac’s default FS (case-insensitive). you’ll get duplicates with different casing.

v1 fix: normalize paths via filepath.Clean + maybe EvalSymlinks. don’t pretend case sensitivity is meaningful on all platforms. if you don’t want to do FS calls, just say: “paths de-duplicated by exact string match” and accept duplicates. but don’t claim it’s principled.

6) status: your active (report missing) clause is unreachable / mis-ordered

you check:
	•	“PR exists and last_push_at and report exists” => ready
	•	else if active and PR open => “active (report missing)”
but you don’t define “PR open” vs “PR exists” consistently; and you don’t store PR state in meta.

in v1, don’t try to know “open/closed” without calling gh pr view. so either:
	•	display “pr: yes” without asserting open/closed
or
	•	define that ls may call gh pr view (slow, but accurate) and cache it.

recommendation: don’t hit network in ls by default. show only what’s in meta. add agency ls --fresh later.

so update display logic to only use meta fields:
	•	if pr_url present => “(pr)” indicator, not “open”
	•	E_PR_NOT_OPEN can exist for merge time when you query gh.

7) push step 1 git fetch origin can be surprisingly slow and may prompt for creds

fetch can hang if the user’s git remote needs authentication. you can’t eliminate this, but add:
	•	timeout for git/gh commands too (not just scripts)
	•	and in docs: “git must be configured for non-interactive auth (ssh agent, credential helper).”

8) you’re missing one constraint that prevents accidental bad roots

add to invariants:
	•	refuse to run if repo root is inside ${AGENCY_DATA_DIR} (avoid recursion weirdness)
	•	refuse to run if worktree path already exists (should be impossible but worth asserting)

9) the one big product concern

you keep saying “tui optional” but you also say “essential this functions like a program, a tui”. pick.

right now, v1 is a cli tool with tmux sessions for runners. that’s fine. a full-screen agency tui can come later. don’t pretend it’s v1-critical if it isn’t.

if you truly need the agency tui in v1, add it as slice 7 and make it explicitly “thin wrapper” (no new logic).

---
don't make code changes yet. survey and explore the relevant parts of the codebase - start with the README.md and the docs/constitution.md. think deeply on the following: is this a worthy goal? is there a better way of achieving this? is it achievable? explain the core problem, our options, and the professional, best practice gold standard solution. 
---

1) i want a `agency code <run-id>` command. this would open the user's ide in the target worktree. this would require setting up a `code` command (like we did for `claude` and `codex`), and setting it in the agency.json.

2) i want a command that opens a tmux terminal in the worktree directory, something like `agency open <run-id>`. probably just use tmux still, so the user can detach. tho, thinking on it, it makes more sense maybe to `cd` into the directory instead, since there's no easy way in the `agency` to them reattach with that terminal (unless we save it as a run, which doesn't seem good). what i want, fundamentally, is an easy and fast way for the user to jump in and out of the wortree directory so they can use the terminal there and run commands. 

3) should we be kicked out of tmux when the runner exits? i'm not convinced that is the best product choice. agency attach fails when runner is dead. at minimum, the error message should be more clear. should we be able to attach anyway without having to `resume` and start a new runner? relatedly, i don't think the tmux should exit if the runner exits/closes for some reason. e.g. what if i just want to work in the terminal there? or want to close claude, do stuff, then open claude again?

4) we need to improve the statuses and logging. `idle` is not a clear descriptor. furthermore, we have too little information about the state of the runner, whether it needs user input, whether it's done, whether it's stuck, etc.:
i'm wondering if we should make the runner write status artifacts
a) runner status contract (agency-owned, runner-updated)
we already do this for scripts. do it for the runner too.
mechanism:
	•	agency creates .agency/state/runner_status.json
	•	system prompt (or CLAUDE.md / config) instructs the runner:
	•	update status at milestones
	•	when it needs user input, write needs_input + a short question list
	•	when it believes it’s “done”, write ready_for_review + “how to test” + “risks”
	•	agency ls/show reads that file, not terminal vibes
pros
	•	cross-platform
	•	deterministic
	•	composable with future headless mode
	•	lets you define your state machine (not the tool’s)
cons
	•	relies on runner compliance (but that’s true of everything with llms)
	•	still can’t detect “hung” unless you add a watchdog (below)
b) watchdog to catch stalls (still agency-owned)
combine status contract with a watchdog:
	•	record last_status_update_at + last_pane_output_at
	•	if neither changes for N minutes → show “stalled (no signals)” and set needs_attention
this catches hangs without pretending to know “thinking vs waiting”.
define agency-owned statuses (artifact-first)
•	runner_status.json is the primary truth for “needs input / ready / blocked”
•	verify.json + todos.json are the primary truth for merge gating
add tmux-based activity hints (clearly labeled as hints)
•	last_pane_output_at
•	pane_output_rate (rough)
•	optionally cpu_hint
add a stall watchdog
•	if no status updates + no pane output for N minutes → stalled
make “stdin blocked” a manual diagnostic
•	agency debug stdin-wait <id> (linux-only, requires permissions)
this yields a professional UX without lying.
i think we should at least have have an Agency system prompt, created on `init`, automatically included in all runner prompts (like a CLAUDE.md or AGENTS.md); more and clearer statuses on the runners; runner watchdog; runner status contract.

5) if the worktree has uncommitted changes, push should at least be prompt-blocked, if not blocked entirely (without a --force).

6) `push` shouldn't just emit a warning and proceed anyway if the worktree has uncommitted changes, no? that seems crazy. it should probably be prompt-blocked (e.g. require user to type 'reckless'). i think it should actually just require a `--force` or something, since this is very dangerous behaviour. 

7) `merge` should be a little more protected, it's a dangerous action. should be prompt-blocked and require user to type 'merge' or something, no?

8) we should add e2e tests for creating, pushing, and merging prs from off the non-main branch.

9) we need to rethink the limitation that agency can only work inside a repo. most commands should work anywhere (ls, push, merge, clean, etc.). users should be able to just set `--repo` or `--parent` (branch) or whatever manually. especially when we one day add remote actions, so the user doesn't have to have the repo cloned locally to use agency.

10) the report.md still looks like the template after push and merge, and so do the commit and pr notes. it is never filled out. i guess that's something i have to tell the runner to do in the prompt? could we instead use the default agency prompt file (like AGENTs.md or CLAUDE.md system prompt), which contains some info about agency and what to do? (e.g. 'make incremental commits, at the end update the report file with...' etc.)? and have this by default created on init so that new runs use this agent system prompt?

11) `agency run` should, by default, start attached, not detached.

12) we should enable users to customize script timeouts. they shouldn't be hardcoded, users should be able to set them, ideally within the script itself (sice that's what they'll be editing).

13) we should add headless mode, `--headless` (e.g. `claude -p "Find and fix the bug in auth.py" --allowedTools "Read,Edit,Bash"` and `codex exec`). this requires a lot of changes tho: we'd need to add an option to attach a text prompt (e.g. `--prompt "fix bug"`), we'd need to log all the outputs, etc.. see v1.5


---

what i need from your codebase
	•	current file layout and whether you already have a “run directory” abstraction
	•	how you record events today (is events.jsonl consistent and complete?)
	•	where you store “workspace-local” vs “global” and how you load it
	•	whether you can currently capture runner input/output at all (tmux capture? file logging?)
	•	whether you plan to add a wrapper around runner invocation (pty logger). if no, don’t pretend you can do “per-message.”
	•	do you allow uncommitted changes in the worktree? (runner will create them)
	•	do you have a rule like “runner must commit before push”? currently you allow any. that affects checkpoint reliability.
	•	are your .agency/* files small and stable? what will verify.json size look like?
	•	do you track last_push_at and can you detect “pushed” reliably? (yes via git rev-parse @{u} / compare to origin)
	•	do you ever allow PRs from forks? (probably no)
	•	do you want a new config key in agency.json for force policy? (probably yes)
	•	what “workspace clean” means for you: do you allow untracked files? ignored files? (define now)
	•	what repos are you targeting (node, python, go)? verify script needs to handle anything.
	•	do you want multiple checks or a single command? (schema supports both; decide)
	•	do you want the runner to be instructed to update todos? (system prompt policy)
	•	do you want a command surface (agency todo add/done/ls)? (likely v1.5)
	•	evidence from conductor docs on how spotlight works (so we’re not inventing parity goals)
	•	what languages/repos you care about; some toolchains embed paths in build artifacts
	•	where you compute status today (single function?) and how you store derived state
	•	whether you’re ok adding state to meta.json (schema bump)

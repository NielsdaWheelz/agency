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


10) Reviewing Changes in a Worktree                                                  
how do i review changes in a worktree? can i easily open my ide in a runner (e.g. `agency <run-id> code`)?
Option A: Use agency show to get the path, then cd there                            
                                                                                    
agency show <run_id> --path                                                         
# Output:                                                                           
# worktree_root: /Users/you/Library/Application                                     
Support/agency/repos/.../worktrees/...                                              
                                                                                    
cd "$(agency show <run_id> --path | grep worktree_root | cut -d' ' -f2)"            
git diff                                                                            
git log --oneline main..HEAD                                                        
                                                                                    
Option B: Open your IDE directly (see next section)                                 
                                                                                    
Option C: Review via the GitHub PR                                                  
                                                                                    
agency push <run_id>    # creates PR                                                
# Then review on GitHub                                                             
                                                                                    
Agency doesn't have a built-in agency code <run_id> command

11) customize script timeouts. they shouldn't be hardcoded, users should be able to set them, ideally within the script itself (sice that's what they'll be editing).

12) 
some notes:
---
agency attach fails when runner is dead. at minimum, the error message should be more clear, and i think we should be able to attach anyway. relatedly, i don't think the tmux should exit if the runner exits/closes for some reason. e.g. what if i just want to work in the terminal there? or want to close claude, do stuff, then open claude again? 
if you want reliable statuses, make the runner write status artifacts.
a) runner status contract (agency-owned, runner-updated)
you already do this for scripts. do it for the runner too.
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
---
i think we should change the way runs work. 1) if the runner inside of a run exits, the user should still be able to to attach to that tmux, and if they are already in it it should not close and boot them. 2) the run status should be clearer. we should add reliable statuses to the runner itself, so the user better understands exactly what it's doing. don't make code changes yet. survey and explore the related code, think deeply on the problem and the possible solutions, and provide professional, best practice, gold standard recommendations.

13) if the worktree has uncommitted changes, push should at least be prompt-blocked, if not blocked entirely (without a --force).

14) we need to fix the pr review process. users should be able to `agency code run-name` and simply attach the terminal to that directory via `agency attach run-name`. 

16) we need to rethink the limitation that agency can only work inside a repo. most commands should work anywhere (ls, push, merge, clean, etc.) to set necessary arguments. 

17) agency push is too fast, github doesnt create it in time to get the url back. can we wait? detect when it's ready somehow?

18) the report.md still looks like the template, it is never filled out. i guess that's something i have to tell the runner to do in the prompt. could we instead create a default agency prompt file (like AGENT or CLAUDE instructions), which contains some info about agency and what to do? (e.g. 'make incremental commits, at the end update the report file with...' etc.)? and have this by default created on init so that new runs use this agent system prompt?

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

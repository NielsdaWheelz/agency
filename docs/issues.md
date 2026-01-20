1) repo_id collision handling is overkill, but you’re missing the real collision: repo moves + path-key

you already track multiple paths. good. but your fallback key uses sha256(abs_path), so moving the repo generates a new repo_key and therefore a new repo_id. you’ll “lose history” on move.

you can accept that for v1, but you should call it out as a limitation:
	•	“path-based repo identity is not stable across moves; moving a non-github repo will be treated as a new repo.”

otherwise you’ll get “why did agency forget my runs” bug reports.

2) init creates stub scripts but doctor requires scripts exist + executable

nice. but your init semantics say “scripts are never overwritten”, and you also say agency.json overwriting requires --force.
edge case: user runs init once, gets stub verify exiting 1, then doctor fails forever until they edit it. that’s intended. just ensure your doctor error message is unmissable: “verify script is a stub and exits 1; replace it”.

also: require scripts be relative to repo root (you implied in init) and enforce it in validation. right now you allow any non-empty string. make it explicit: reject absolute paths and .. path traversal in v1. otherwise you’ve created a path injection footgun.

3) repo_index.json merge behavior: case sensitivity

“paths de-duplicated case-sensitively” is wrong on mac’s default FS (case-insensitive). you’ll get duplicates with different casing.

v1 fix: normalize paths via filepath.Clean + maybe EvalSymlinks. don’t pretend case sensitivity is meaningful on all platforms. if you don’t want to do FS calls, just say: “paths de-duplicated by exact string match” and accept duplicates. but don’t claim it’s principled.

4) status: your active (report missing) clause is unreachable / mis-ordered

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

5) push step 1 git fetch origin can be surprisingly slow and may prompt for creds

fetch can hang if the user’s git remote needs authentication. you can’t eliminate this, but add:
	•	timeout for git/gh commands too (not just scripts)
	•	and in docs: “git must be configured for non-interactive auth (ssh agent, credential helper).”

6) you’re missing one constraint that prevents accidental bad roots

add to invariants:
	•	refuse to run if repo root is inside ${AGENCY_DATA_DIR} (avoid recursion weirdness)
	•	refuse to run if worktree path already exists (should be impossible but worth asserting)

7) the one big product concern

you keep saying “tui optional” but you also say “essential this functions like a program, a tui”. pick.

right now, v1 is a cli tool with tmux sessions for runners. that’s fine. a full-screen agency tui can come later. don’t pretend it’s v1-critical if it isn’t.

if you truly need the agency tui in v1, add it as slice 7 and make it explicitly “thin wrapper” (no new logic).

8) should we be kicked out of tmux when the runner exits? i'm not convinced that is the best product choice. agency attach fails when runner is dead. at minimum, the error message should be more clear. should we be able to attach anyway without having to `resume` and start a new runner? relatedly, i don't think the tmux should exit if the runner exits/closes for some reason. e.g. what if i just want to work in the terminal there? or want to close claude, do stuff, then open claude again?

9) should we add e2e tests for creating, pushing, and merging prs from off the non-main branch?

10) we should add headless mode, `--headless` (e.g. `claude -p "Find and fix the bug in auth.py" --allowedTools "Read,Edit,Bash"` and `codex exec`). this requires a lot of changes tho: we'd need to add an option to attach a text prompt (e.g. `--prompt "fix bug"`), we'd need to log all the outputs, etc.. see v1.5

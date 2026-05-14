package scaffold

import (
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/fs"
)

// ClaudeMDFileName is the name of the runner protocol file.
const ClaudeMDFileName = "CLAUDE.md"

// ClaudeMDTemplate is the content of CLAUDE.md that instructs runners
// to use .agency/state/runner_status.json as the only runner contract.
const ClaudeMDTemplate = `# Agency Runner Protocol

` + "`" + `.agency/state/runner_status.json` + "`" + ` is the only runner contract.

It is the only semantic state contract for an invocation.
Do not model separate semantic, display, or readiness layers in runner output.

Update it at milestones:

| State | When | Required Fields |
|--------|------|-----------------|
| ` + "`" + `running` + "`" + ` | Actively executing work | ` + "`" + `summary` + "`" + ` |
| ` + "`" + `waiting` + "`" + ` | Not executing right now. Use this for both turn-complete idle and waiting for user input. | ` + "`" + `summary` + "`" + ` |
| ` + "`" + `succeeded` + "`" + ` | Work is complete and validated enough to hand back. | ` + "`" + `summary` + "`" + `, ` + "`" + `how_to_test` + "`" + ` |
| ` + "`" + `failed` + "`" + ` | Work cannot complete successfully. | ` + "`" + `summary` + "`" + ` |

Rules:

- Use exactly one canonical ` + "`" + `state` + "`" + `.
- ` + "`" + `waiting` + "`" + ` covers both done-and-idle and waiting-for-user cases.
- Use ` + "`" + `reason` + "`" + ` when ` + "`" + `waiting` + "`" + ` or ` + "`" + `failed` + "`" + ` needs clarification.
- When ` + "`" + `state` + "`" + ` is ` + "`" + `waiting` + "`" + ` because the runner needs a user answer, include ` + "`" + `questions[]` + "`" + `.
- ` + "`" + `blocked` + "`" + ` is removed from the runner and user-facing vocabulary. Do not write it.
- ` + "`" + `ready` + "`" + ` is removed. Use ` + "`" + `succeeded` + "`" + `.
- ` + "`" + `needs_input` + "`" + ` is removed. Use ` + "`" + `waiting` + "`" + `.
- ` + "`" + `working` + "`" + ` is removed. Use ` + "`" + `running` + "`" + `.

Schema:

` + "```" + `json
{
  "schema_version": "2.0",
  "state": "waiting",
  "updated_at": "2026-01-19T12:00:00Z",
  "reason": "awaiting_user_input",
  "summary": "I finished the API refactor and need the preferred webhook path before I update the client.",
  "questions": [
    "Should the webhook stay at /webhooks/github or move to /api/github/webhook?"
  ]
}
` + "```" + `

Before finishing successfully, set ` + "`" + `state` + "`" + ` to ` + "`" + `succeeded` + "`" + ` and include ` + "`" + `summary` + "`" + ` and ` + "`" + `how_to_test` + "`" + `.
`

// WriteClaudeMD writes the CLAUDE.md file to the repo root if it doesn't exist.
// Returns (true, nil) if the file was created.
// Returns (false, nil) if the file already exists.
// Returns (false, error) if there was an error.
func WriteClaudeMD(fsys fs.FS, repoRoot string) (created bool, err error) {
	claudeMDPath := filepath.Join(repoRoot, ClaudeMDFileName)

	// Check if file already exists
	_, err = fsys.Stat(claudeMDPath)
	if err == nil {
		// File exists, don't overwrite
		return false, nil
	}
	if !os.IsNotExist(err) {
		// Unexpected error
		return false, err
	}

	// File doesn't exist, create it
	if err := fsys.WriteFile(claudeMDPath, []byte(ClaudeMDTemplate), 0644); err != nil {
		return false, err
	}

	return true, nil
}

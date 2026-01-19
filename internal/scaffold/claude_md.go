package scaffold

import (
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/fs"
)

// ClaudeMDFileName is the name of the runner protocol file.
const ClaudeMDFileName = "CLAUDE.md"

// ClaudeMDTemplate is the content of CLAUDE.md that instructs runners
// on how to communicate status via .agency/state/runner_status.json.
const ClaudeMDTemplate = `# Agency Runner Protocol

Update ` + "`" + `.agency/state/runner_status.json` + "`" + ` at milestones:

| Status | When | Required Fields |
|--------|------|-----------------|
| ` + "`" + `working` + "`" + ` | Actively making progress | ` + "`" + `summary` + "`" + ` |
| ` + "`" + `needs_input` + "`" + ` | Waiting for user answer | ` + "`" + `summary` + "`" + `, ` + "`" + `questions[]` + "`" + ` |
| ` + "`" + `blocked` + "`" + ` | Cannot proceed | ` + "`" + `summary` + "`" + `, ` + "`" + `blockers[]` + "`" + ` |
| ` + "`" + `ready_for_review` + "`" + ` | Work complete | ` + "`" + `summary` + "`" + `, ` + "`" + `how_to_test` + "`" + ` |

Schema:

` + "```" + `json
{
  "schema_version": "1.0",
  "status": "working",
  "updated_at": "2026-01-19T12:00:00Z",
  "summary": "Implementing user authentication",
  "questions": [],
  "blockers": [],
  "how_to_test": "",
  "risks": []
}
` + "```" + `

Before ` + "`" + `ready_for_review` + "`" + `, update ` + "`" + `.agency/report.md` + "`" + ` with summary, decisions, testing instructions, and risks.
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

package scaffold

import (
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/fs"
)

const agencyIgnoreEntry = ".agency/"

// gitignoreResult indicates what happened to .gitignore.
type gitignoreResult string

const (
	gitignoreUpdated   gitignoreResult = "updated"
	gitignoreUnchanged gitignoreResult = "unchanged"
)

// EnsureGitignore ensures .agency/ is in .gitignore.
// Creates the file if missing. Does not add duplicate entries.
// Ensures file ends with newline.
//
// Returns the result indicating what action was taken.
func EnsureGitignore(fsys fs.FS, gitignorePath string) (gitignoreResult, error) {
	content, err := fsys.ReadFile(gitignorePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		// File doesn't exist, create it with the entry
		newContent := agencyIgnoreEntry + "\n"
		if err := fsys.WriteFile(gitignorePath, []byte(newContent), 0644); err != nil {
			return "", err
		}
		return gitignoreUpdated, nil
	}

	// File exists, check if entry is already present
	if hasAgencyEntry(string(content)) {
		// Entry exists, ensure trailing newline
		if len(content) > 0 && content[len(content)-1] != '\n' {
			newContent := string(content) + "\n"
			if err := fsys.WriteFile(gitignorePath, []byte(newContent), 0644); err != nil {
				return "", err
			}
			return gitignoreUpdated, nil
		}
		return gitignoreUnchanged, nil
	}

	// Entry not present, append it
	newContent := string(content)
	// Ensure content ends with newline before appending
	if len(newContent) > 0 && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += agencyIgnoreEntry + "\n"

	if err := fsys.WriteFile(gitignorePath, []byte(newContent), 0644); err != nil {
		return "", err
	}
	return gitignoreUpdated, nil
}

// hasAgencyEntry checks if the .agency/ or .agency entry exists in content.
// Treats ".agency/" and ".agency" as equivalent per spec.
func hasAgencyEntry(content string) bool {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == ".agency/" || trimmed == ".agency" {
			return true
		}
	}
	return false
}

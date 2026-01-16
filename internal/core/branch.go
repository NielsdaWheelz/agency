package core

// BranchName returns "agency/<name>-<shortid>".
// Name is pre-validated, so no slugification is needed.
func BranchName(name, runID string) string {
	shortID := ShortID(runID)
	return "agency/" + name + "-" + shortID
}

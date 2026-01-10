package core

// BranchName returns "agency/<slug>-<shortid>".
// slug max len must be 30 (call Slugify(title, 30)).
func BranchName(title, runID string) string {
	slug := Slugify(title, 30)
	shortID := ShortID(runID)
	return "agency/" + slug + "-" + shortID
}

package core

import (
	"strings"
	"unicode"
)

// Slugify converts a title into a lowercase hyphen slug.
// - allowed: [a-z0-9-]
// - whitespace/underscore => hyphen
// - drop all other chars
// - collapse multiple hyphens
// - trim leading/trailing hyphens
// - maxLen enforced (truncate after cleanup)
// - after truncation, re-trim leading/trailing hyphens and collapse repeats
// if result empty or maxLen <= 0 => "untitled"
func Slugify(title string, maxLen int) string {
	if maxLen <= 0 {
		return "untitled"
	}

	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '_' || r == '-':
			b.WriteRune('-')
		// drop all other chars
		}
	}

	result := collapseHyphens(b.String())
	result = strings.Trim(result, "-")

	// truncate
	if len(result) > maxLen {
		result = result[:maxLen]
	}

	// re-trim after truncation
	result = collapseHyphens(result)
	result = strings.Trim(result, "-")

	if result == "" {
		return "untitled"
	}
	return result
}

// collapseHyphens replaces multiple consecutive hyphens with a single hyphen.
func collapseHyphens(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		if r == '-' {
			if !prevHyphen {
				b.WriteRune(r)
				prevHyphen = true
			}
		} else {
			b.WriteRune(r)
			prevHyphen = false
		}
	}
	return b.String()
}

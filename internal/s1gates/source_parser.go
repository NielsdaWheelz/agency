package s1gates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// gateHeadingRe matches ## Gate A or ## Gate B headings.
var gateHeadingRe = regexp.MustCompile(`(?i)^##\s+Gate\s+(A|B)\b`)

// anyH2Re matches any ## heading.
var anyH2Re = regexp.MustCompile(`^##\s+`)

// numberedItemRe matches "1. `path`" list entries.
var numberedItemRe = regexp.MustCompile("^\\d+\\.\\s+`([^`]+)`")

// fenceRe matches opening/closing fenced code block markers.
var fenceRe = regexp.MustCompile("^```")

// RepoFileExists returns a FileExistsFn that checks paths relative to repoRoot.
func RepoFileExists(repoRoot string) FileExistsFn {
	return func(path string) bool {
		_, err := os.Stat(filepath.Join(repoRoot, path))
		return err == nil
	}
}

// ParseGateSet parses a release-gates markdown document and returns the
// resolved GateSet with deterministic Gate A/B membership. Only Gate A and
// Gate B sections are parsed; other gates (C, D, etc.) are ignored.
//
// fileExists is called for each issue path to verify it resolves to an
// existing file. Unresolved paths, duplicates, and malformed source
// content produce E_GATE_SET_INVALID.
func ParseGateSet(content string, sourceRef string, fileExists FileExistsFn) (*GateSet, error) {
	lines := strings.Split(content, "\n")

	var gateAItems []string
	var gateBItems []string
	seen := make(map[string]string) // issue_path -> gate_id

	var currentGate string // "", "A", "B", or "other"
	inFence := false

	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
		}
		if inFence {
			continue
		}

		// Check for gate headings.
		if m := gateHeadingRe.FindStringSubmatch(line); m != nil {
			currentGate = strings.ToUpper(m[1])
			continue
		}
		if anyH2Re.MatchString(line) {
			currentGate = "other"
			continue
		}

		if currentGate != "A" && currentGate != "B" {
			continue
		}

		// Check for numbered list items with backtick-wrapped paths.
		if m := numberedItemRe.FindStringSubmatch(line); m != nil {
			issuePath := m[1]

			// Reject duplicates across gates.
			if prevGate, dup := seen[issuePath]; dup {
				return nil, agencyerrors.NewWithDetails(
					agencyerrors.EGateSetInvalid,
					fmt.Sprintf("duplicate issue path %q (Gate %s and Gate %s)", issuePath, prevGate, currentGate),
					map[string]string{"issue_path": issuePath},
				)
			}
			seen[issuePath] = currentGate

			// Verify issue path exists.
			if !fileExists(issuePath) {
				return nil, agencyerrors.NewWithDetails(
					agencyerrors.EGateSetInvalid,
					fmt.Sprintf("issue path does not exist: %s", issuePath),
					map[string]string{"issue_path": issuePath},
				)
			}

			switch currentGate {
			case "A":
				gateAItems = append(gateAItems, issuePath)
			case "B":
				gateBItems = append(gateBItems, issuePath)
			}
		}
	}

	if len(gateAItems) == 0 && len(gateBItems) == 0 {
		return nil, agencyerrors.New(agencyerrors.EGateSetInvalid, "no Gate A or Gate B items found in source")
	}

	return &GateSet{
		Slice:      "S1",
		GateAItems: gateAItems,
		GateBItems: gateBItems,
		SourceRef:  sourceRef,
	}, nil
}

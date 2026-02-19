package releasegates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

var gateHeadingRe = regexp.MustCompile(`(?i)^##\s+Gate\s+(A|B)\b`)
var anyH2Re = regexp.MustCompile(`^##\s+`)
var numberedItemRe = regexp.MustCompile("^\\d+\\.\\s+`([^`]+)`")
var fenceRe = regexp.MustCompile("^```")

// RepoFileExists returns a FileExistsFn that checks paths relative to repoRoot.
func RepoFileExists(repoRoot string) FileExistsFn {
	return func(path string) bool {
		_, err := os.Stat(filepath.Join(repoRoot, path))
		return err == nil
	}
}

// ParseGateSet parses a release-gates markdown document and returns the
// resolved GateSet with deterministic Gate A/B membership.
func ParseGateSet(content string, sourceRef string, fileExists FileExistsFn) (*GateSet, error) {
	lines := strings.Split(content, "\n")

	var gateAItems []string
	var gateBItems []string
	seen := make(map[string]string)

	var currentGate string
	inFence := false

	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
		}
		if inFence {
			continue
		}

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

		if m := numberedItemRe.FindStringSubmatch(line); m != nil {
			issuePath := m[1]

			if prevGate, dup := seen[issuePath]; dup {
				return nil, agencyerrors.NewWithDetails(
					agencyerrors.EGateSetInvalid,
					fmt.Sprintf("duplicate issue path %q (Gate %s and Gate %s)", issuePath, prevGate, currentGate),
					map[string]string{"issue_path": issuePath},
				)
			}
			seen[issuePath] = currentGate

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

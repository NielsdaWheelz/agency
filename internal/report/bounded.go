package report

import (
	"fmt"
	"os"

	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
)

const (
	// MaxPRBodyReportBytes is the contract limit for report reads used by PR-body
	// selection paths (canonical and compatibility).
	MaxPRBodyReportBytes = 256 * 1024
)

// ReadFileBounded reads a file with deterministic over-limit behavior.
//
// Returns:
//   - exists=false when the file does not exist.
//   - oversized=true when file size exceeds maxBytes (without loading full content).
//   - data only when exists=true and oversized=false.
func ReadFileBounded(fsys agencyfs.FS, path string, maxBytes int) (data []byte, exists bool, oversized bool, err error) {
	if maxBytes <= 0 {
		return nil, false, false, fmt.Errorf("maxBytes must be > 0")
	}

	info, statErr := fsys.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, false, false, nil
		}
		return nil, false, false, statErr
	}
	if info.IsDir() {
		return nil, true, false, fmt.Errorf("path is a directory: %s", path)
	}
	if info.Size() > int64(maxBytes) {
		return nil, true, true, nil
	}

	data, readErr := fsys.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, false, false, nil
		}
		return nil, true, false, readErr
	}
	if len(data) > maxBytes {
		return data[:maxBytes], true, true, nil
	}
	return data, true, false, nil
}

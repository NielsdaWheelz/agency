package checkpoint

import (
	"bufio"
	"encoding/json"
	"os"
)

const maxEventLineBytes = 4 * 1024 * 1024

// loadMaxEventSeq returns the highest seq observed in events.jsonl.
// Invalid lines are ignored to preserve best-effort forward progress.
func loadMaxEventSeq(eventsPath string) uint64 {
	f, err := os.Open(eventsPath)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	var maxSeq uint64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxEventLineBytes)
	for scanner.Scan() {
		var line struct {
			Seq uint64 `json:"seq"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Seq > maxSeq {
			maxSeq = line.Seq
		}
	}

	return maxSeq
}

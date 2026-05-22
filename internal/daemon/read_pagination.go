package daemon

import (
	"encoding/base64"
	"encoding/json"
)

func paginateWorktrees(all []WorktreeDTO, cursor string, limit int) ([]WorktreeDTO, string) {
	if len(all) == 0 {
		return []WorktreeDTO{}, ""
	}

	startIdx := 0
	if cursor != "" {
		var c worktreeCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, w := range all {
				if w.LastUsedAt < c.LastUsedAt || (w.LastUsedAt == c.LastUsedAt && w.WorktreeID > c.WorktreeID) {
					startIdx = i
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := worktreeCursor{LastUsedAt: last.LastUsedAt, WorktreeID: last.WorktreeID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}

func paginateInvocations(all []InvocationDTO, cursor string, limit int) ([]InvocationDTO, string) {
	if len(all) == 0 {
		return []InvocationDTO{}, ""
	}

	startIdx := 0
	if cursor != "" {
		var c invocationCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, inv := range all {
				if inv.StartedAt < c.StartedAt || (inv.StartedAt == c.StartedAt && inv.InvocationID > c.InvocationID) {
					startIdx = i
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := invocationCursor{StartedAt: last.StartedAt, InvocationID: last.InvocationID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}

func paginateCheckpoints(all []CheckpointDTO, cursor string, limit int) ([]CheckpointDTO, string) {
	if len(all) == 0 {
		return []CheckpointDTO{}, ""
	}

	startIdx := 0
	if cursor != "" {
		var c checkpointCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, cp := range all {
				if cp.ID < c.ID {
					startIdx = i
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := checkpointCursor{ID: last.ID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}

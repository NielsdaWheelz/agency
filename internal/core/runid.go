// Package core provides foundational utilities for agency slice 1.
package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// NewRunID returns "<yyyymmddhhmmss>-<rand4>" in UTC time.
// Example: "20260109013207-a3f2"
// Error only if crypto/rand read fails.
func NewRunID(now time.Time) (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	ts := now.UTC().Format("20060102150405")
	suffix := hex.EncodeToString(b)
	return ts + "-" + suffix, nil
}

// ShortID returns the 4-char random suffix (after the last '-').
// If the format is unexpected, return "xxxx".
func ShortID(runID string) string {
	idx := strings.LastIndex(runID, "-")
	if idx == -1 || idx+1 >= len(runID) {
		return "xxxx"
	}
	suffix := runID[idx+1:]
	if len(suffix) != 4 {
		return "xxxx"
	}
	return suffix
}

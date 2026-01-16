// Package commands implements agency CLI commands.
package commands

import (
	"math/rand"
	"sync"
	"time"
)

var prViewRetryDelays = []time.Duration{
	0,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
}

var jitterDelay = func(d time.Duration) time.Duration {
	return applyJitter(d)
}

var jitterMu sync.Mutex
var jitterRand = rand.New(rand.NewSource(time.Now().UnixNano()))

func applyJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}

	jitter := float64(d) * 0.2
	jitterMu.Lock()
	delta := (jitterRand.Float64()*2 - 1) * jitter
	jitterMu.Unlock()

	delay := float64(d) + delta
	if delay < 0 {
		return 0
	}
	return time.Duration(delay)
}

func headRef(repoRef ghRepoRef, branch string) string {
	if repoRef.Owner == "" {
		return branch
	}
	return repoRef.Owner + ":" + branch
}

type prViewAttempt struct {
	ExitCode int
	Stderr   string
	Err      error
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

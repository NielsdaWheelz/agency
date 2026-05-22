package verify

import "fmt"

func deriveOK(timedOut, cancelled bool, exitCode *int, vj *verifyJSON) bool {
	// 1. Timeout or cancellation always means failure
	if timedOut || cancelled {
		return false
	}

	// 2. No exit code (failed to start, signaled) means failure
	if exitCode == nil {
		return false
	}

	// 3. Non-zero exit code means failure
	if *exitCode != 0 {
		return false
	}

	// 4. If verify.json is valid, use its ok value (may downgrade success to failure)
	if vj != nil {
		return vj.OK
	}

	// 5. Exit code 0 with no verify.json means success
	return true
}

func deriveSummary(timedOut, cancelled bool, exitCode *int, vj *verifyJSON) string {
	// Prefer verify.json summary if present
	if vj != nil && vj.Summary != "" {
		return vj.Summary
	}

	// Use the outcome-derived summary when verify.json has no summary.
	if timedOut {
		return "verify timed out"
	}

	if cancelled {
		return "verify cancelled"
	}

	if exitCode == nil {
		return "verify failed (no exit code)"
	}

	if *exitCode == 0 {
		return "verify succeeded"
	}

	return fmt.Sprintf("verify failed (exit %d)", *exitCode)
}

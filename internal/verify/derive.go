package verify

import "fmt"

// DeriveOK computes the final verification result using the locked precedence rules (v1).
//
// Precedence order:
//  1. if timedOut or cancelled => false
//  2. else if exitCode == nil => false
//  3. else if *exitCode != 0 => false
//  4. else if vj != nil => vj.OK (verify.json may downgrade success but never upgrades failure)
//  5. else => true
func DeriveOK(timedOut, cancelled bool, exitCode *int, vj *VerifyJSON) bool {
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

// DeriveSummary computes the human-readable summary for the verify result.
//
// Summary rules:
//   - if vj != nil and vj.Summary != "" => use it
//   - else if timedOut => "verify timed out"
//   - else if cancelled => "verify cancelled"
//   - else if exitCode == nil => "verify failed (no exit code)"
//   - else if *exitCode == 0 => "verify succeeded"
//   - else => fmt.Sprintf("verify failed (exit %d)", *exitCode)
func DeriveSummary(timedOut, cancelled bool, exitCode *int, vj *VerifyJSON) string {
	// Prefer verify.json summary if present
	if vj != nil && vj.Summary != "" {
		return vj.Summary
	}

	// Fall back to generic messages based on outcome
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

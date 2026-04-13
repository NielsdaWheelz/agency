# Errors

## Scope

This document covers stable error-code and defect modeling.

## Error Codes

- User-facing command and daemon boundaries should return stable `E_*` codes from `internal/errors`.
- Preserve one primary error code per failure path.
- Wrap causes for debugging, but do not replace the public code accidentally.
- Distinguish missing state, invalid input, corrupted persisted state, and internal defects with different codes.

## Corruption And Defects

- Missing files that represent optional state are normal absence, not corruption.
- Invalid JSON, missing `schema_version`, and unsupported schema versions in required state files are corruption.
- Unexpected internal invariants should fail loudly and surface as `E_INTERNAL` or an explicit corruption code.
- Do not silently coerce corrupted state into a fallback shape.

## Placement

- Construct the public error code in the layer that has enough context to classify the failure correctly.
- Lower-level packages should prefer returning structured errors or plain causes over printing or inventing user-facing prose.

# PR Report: Configurable Script Timeouts

## Summary of Changes

This PR enables users to configure script timeouts in `agency.json` instead of relying on hardcoded values. Previously, timeouts were constants in the codebase (setup: 10m, verify: 30m, archive: 5m). Now users can customize these per-repo.

### New `agency.json` Schema

**Before:**
```json
{
  "version": 1,
  "scripts": {
    "setup": "scripts/agency_setup.sh",
    "verify": "scripts/agency_verify.sh",
    "archive": "scripts/agency_archive.sh"
  }
}
```

**After:**
```json
{
  "version": 1,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "5m"
    }
  }
}
```

### Files Changed

| Category | Files | Changes |
|----------|-------|---------|
| Schema Definition | `internal/config/agencyjson.go` | New `ScriptConfig` struct with `Path` and `Timeout` fields; new `parseScriptConfig()` function; timeout constants moved here |
| Validation | `internal/config/validate.go` | Updated to validate `scripts.X.path` instead of `scripts.X` |
| Scaffold | `internal/scaffold/template.go` | Updated template to generate new format |
| Run Pipeline | `internal/runservice/service.go` | Removed `SetupTimeout` constant; uses config timeout |
| Pipeline State | `internal/pipeline/pipeline.go` | Added `SetupTimeout` field to `PipelineState` |
| Verify Service | `internal/verifyservice/service.go` | Uses config timeout as default |
| Verify Runner | `internal/verify/runner.go` | Uses `config.DefaultVerifyTimeout` as fallback |
| Archive Pipeline | `internal/archive/pipeline.go` | Removed constant; uses `config.DefaultArchiveTimeout` |
| Commands | `internal/commands/{doctor,clean,merge,verify}.go` | Updated to access `.Path` and `.Timeout` |
| CLI | `internal/cli/dispatch.go` | Updated `--timeout` flag to override config (empty = use config) |
| Tests | `internal/config/config_test.go`, `internal/runservice/service_test.go`, `internal/commands/doctor_test.go` | Updated fixtures and assertions |
| Test Fixtures | `internal/config/testdata/*.json` | Updated to new object format |
| Documentation | `README.md`, `docs/v1/constitution.md`, `agency.json` | Updated schema examples and descriptions |

---

## Problems Encountered

### 1. Type System Cascading Changes

**Problem:** Changing `Scripts` struct fields from `string` to `ScriptConfig` caused compile errors in ~15 files that accessed `cfg.Scripts.Setup` directly.

**Solution:** Systematically updated all access points to use `cfg.Scripts.Setup.Path` and added `.Timeout` access where needed.

### 2. Test Fixtures Using Old Format

**Problem:** Test fixtures in `testdata/` and inline JSON in test files used the old string format, causing test failures.

**Solution:** Updated all test fixtures to use the new object format with `path` and `timeout` fields.

### 3. Multiple Layers of Timeout Defaults

**Problem:** The verify command had timeout defaults at four layers:
1. CLI flag default (`"30m"`)
2. `commands/verify.go` default check
3. `verifyservice/service.go` default check
4. `verify/runner.go` default check

This made it unclear which layer should use the config value.

**Solution:**
- CLI flag default changed to empty string (meaning "use config")
- Commands layer removed hardcoded default
- Service layer applies config timeout if caller passes 0
- Runner layer uses `config.DefaultVerifyTimeout` as defensive fallback

### 4. SetupTimeout Not Passed Through Pipeline

**Problem:** `PipelineState` only had `SetupScript` field but not `SetupTimeout`, so the timeout couldn't flow from config to execution.

**Solution:** Added `SetupTimeout time.Duration` field to `PipelineState` struct.

### 5. Archive Timeout Reference After Removal

**Problem:** After removing `archive.DefaultArchiveTimeout` constant, `commands/clean.go` and `commands/merge.go` still referenced it.

**Solution:** Updated to use `agencyJSON.Scripts.Archive.Timeout` from loaded config.

---

## Solutions Implemented

### Layered Configuration Model

```
Priority (lowest to highest):
1. Package defaults (config.DefaultSetupTimeout, etc.)
2. agency.json configuration (scripts.X.timeout)
3. CLI flags (--timeout for verify command)
```

### Centralized Timeout Constants

All default timeouts are now defined in `internal/config/agencyjson.go`:

```go
const (
    DefaultSetupTimeout   = 10 * time.Minute
    DefaultVerifyTimeout  = 30 * time.Minute
    DefaultArchiveTimeout = 5 * time.Minute
    MinTimeout            = 1 * time.Minute
    MaxTimeout            = 24 * time.Hour
)
```

### Strict Timeout Validation

Timeouts are validated during JSON parsing:
- Must be valid Go duration format (`10m`, `1h30m`, `90s`)
- Minimum: 1 minute (prevents DoS via tiny timeouts)
- Maximum: 24 hours (sanity limit)

### Defensive Fallbacks

Each layer has a defensive fallback to the package default if timeout is 0:

```go
if timeout == 0 {
    timeout = config.DefaultVerifyTimeout
}
```

---

## Decisions Made

### 1. Object Format Over String-with-Comments

**Decision:** Use `{"path": "...", "timeout": "..."}` instead of magic comments in scripts.

**Rationale:**
- Clean separation of configuration and implementation
- Type-safe, schema-validated
- Language-agnostic (works for any script type)
- Follows established patterns (Docker Compose, GitHub Actions)

### 2. No Backward Compatibility Shim

**Decision:** Did not implement fallback parsing for old string format.

**Rationale:** Per user request to "disregard backward compatibility" and implement "best-practice gold standard."

### 3. Timeout Field is Optional

**Decision:** If `timeout` is omitted from a script config, use the default.

**Rationale:** Reduces migration burden; existing behavior preserved if timeout not specified.

### 4. CLI Override Only for Verify

**Decision:** Only `agency verify` has `--timeout` flag; setup and archive don't.

**Rationale:**
- Verify is the most commonly run ad-hoc
- Setup runs during `agency run` (user can modify config)
- Archive runs during cleanup (typically not user-initiated)

### 5. Validation at Parse Time

**Decision:** Timeout validation happens during `parseScriptConfig()`, not during `ValidateAgencyConfig()`.

**Rationale:** Fail fast with clear error messages pointing to exact field.

---

## Deviations from Prompt/Spec

### 1. No Script-Embedded Timeouts

**Original suggestion:** Allow timeouts as magic comments in script files.

**Deviation:** Implemented in `agency.json` instead.

**Reason:** Better separation of concerns; configuration belongs in config files.

### 2. Kept Defensive Defaults in Runners

**Spec implication:** Config should be the single source of truth.

**Deviation:** Runner layers still have `if timeout == 0` checks with package defaults.

**Reason:** Defense in depth; prevents unexpected behavior if config loading has edge cases.

### 3. Did Not Add --timeout to All Commands

**Potential scope:** Could add `--timeout` to `agency run` for setup, `agency clean` for archive.

**Deviation:** Only implemented for `agency verify`.

**Reason:** Matches existing behavior; minimizes scope while proving the pattern.

---

## How to Run Commands

### Check New agency.json Format

```bash
# Validate your agency.json
agency doctor
```

### Initialize a New Repo with New Format

```bash
# Creates agency.json with new object format
agency init
```

### Run with Custom Verify Timeout

```bash
# Use timeout from agency.json (configured value)
agency verify my-feature

# Override with CLI flag
agency verify my-feature --timeout 45m

# Quick timeout for fast feedback
agency verify my-feature --timeout 5m
```

### Verify Configuration is Loaded

```bash
# Run setup - will use scripts.setup.timeout from config
agency run my-feature

# Check the setup log for timing
cat ~/.local/share/agency/repos/*/runs/*/logs/setup.log
```

---

## Testing the Changes

### Run All Tests

```bash
cd /path/to/agency
go test ./...
```

### Test Specific Packages

```bash
# Config parsing and validation
go test ./internal/config/... -v

# Run service (includes setup timeout)
go test ./internal/runservice/... -v

# Commands (includes doctor, verify, clean, merge)
go test ./internal/commands/... -v
```

### Manual Testing

```bash
# 1. Update your agency.json to new format
cat > agency.json << 'EOF'
{
  "version": 1,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "15m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "60m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "10m"
    }
  }
}
EOF

# 2. Run doctor to validate
agency doctor

# 3. Create a run to test setup timeout
agency run test-timeouts

# 4. Test verify with config timeout
agency verify test-timeouts

# 5. Test verify with override
agency verify test-timeouts --timeout 2m

# 6. Clean up (tests archive timeout)
agency clean test-timeouts
```

### Test Invalid Configurations

```bash
# Test minimum timeout validation
cat > /tmp/test-agency.json << 'EOF'
{
  "version": 1,
  "scripts": {
    "setup": {"path": "setup.sh", "timeout": "30s"}
  }
}
EOF
# Should fail with "timeout must be at least 1m"

# Test maximum timeout validation
cat > /tmp/test-agency.json << 'EOF'
{
  "version": 1,
  "scripts": {
    "setup": {"path": "setup.sh", "timeout": "48h"}
  }
}
EOF
# Should fail with "timeout must be at most 24h"

# Test invalid duration format
cat > /tmp/test-agency.json << 'EOF'
{
  "version": 1,
  "scripts": {
    "setup": {"path": "setup.sh", "timeout": "ten minutes"}
  }
}
EOF
# Should fail with "invalid duration"
```

---

## Migration Guide

To migrate existing repos:

1. Update `agency.json` from string format to object format
2. Add `timeout` field if you want non-default values
3. Run `agency doctor` to validate

**Before:**
```json
{"version": 1, "scripts": {"setup": "scripts/setup.sh", ...}}
```

**After:**
```json
{"version": 1, "scripts": {"setup": {"path": "scripts/setup.sh", "timeout": "10m"}, ...}}
```

The `timeout` field is optional; if omitted, defaults apply (10m/30m/5m).

// Package scaffold provides helpers for creating agency.json and stub scripts.
package scaffold

// AgencyJSONTemplate is the exact template for agency.json per L0 spec.
// This must match the constitution exactly.
// Each script config has a "path" (required) and "timeout" (optional, Go duration format).
// Default timeouts: setup=10m, verify=30m, archive=5m.
const AgencyJSONTemplate = `{
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
`

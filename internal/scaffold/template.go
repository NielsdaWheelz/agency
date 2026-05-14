// Package scaffold provides helpers for creating agency.json and stub scripts.
package scaffold

// AgencyJSONTemplate is the default repo-shared agency.json scaffold.
// Each script config has a "path" (required) and "timeout" (optional, Go duration format).
// Default timeouts: setup=10m, verify=30m, archive=5m.
const AgencyJSONTemplate = `{
  "version": 4,
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
  },
  "execution": {
    "checkout_root": "repo-sibling"
  }
}
`

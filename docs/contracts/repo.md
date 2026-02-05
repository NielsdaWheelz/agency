# repo.json contract

this file defines the contract for repo metadata.

## location

- `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`

## schema (v1)

root fields:
- `schema_version` (string, must equal `1.0`)
- `repo_key` (string)
- `repo_id` (string)
- `repo_root_last_seen` (string, absolute)
- `agency_json_path` (string, absolute)
- `origin_present` (bool)
- `origin_url` (string)
- `origin_host` (string)
- `capabilities` (object)
- `created_at` (rfc3339 utc)
- `updated_at` (rfc3339 utc)

capabilities object:
- `github_origin` (bool)
- `origin_host` (string)
- `gh_authed` (bool)

## rules

1. schema_version is required and validated on read.
2. `created_at` is immutable.
3. `updated_at` is monotonic and updated on any change.
4. `origin_present=false` implies `origin_url` is empty.
5. writes are atomic and use store helpers only.
6. permissions are private: 0700 dirs, 0600 files.

## stubs

- repo_key definition and normalization
- canonicalization of repo_root_last_seen

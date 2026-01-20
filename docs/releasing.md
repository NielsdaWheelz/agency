# releasing agency

## 1. purpose and scope

this document describes how to cut releases for agency.

**covers:**
- prerequisites for releasing
- pre-flight checks
- step-by-step release procedure
- post-release verification
- failure recovery

**does not cover:**
- feature development workflow
- linux distribution packaging (apt, yum, pacman)
- code signing
- homebrew/core submission

## 2. prerequisites

### 2.1 local tools

| tool | minimum version | install |
|------|-----------------|---------|
| go | 1.21+ | https://go.dev/dl/ |
| git | 2.x | system package manager |
| gh | 2.x | `brew install gh` or https://cli.github.com/ |
| goreleaser | 2.x | `go install github.com/goreleaser/goreleaser/v2@latest` |

verify installation:

```bash
go version
git --version
gh --version
goreleaser --version
```

### 2.2 GitHub permissions

the releasing maintainer must have:
- write access to `NielsdaWheelz/agency`
- permission to create tags
- permission to create releases

### 2.3 GitHub secrets

the following secret must be configured in repository settings:

| secret | purpose | required scopes |
|--------|---------|-----------------|
| `HOMEBREW_TAP_TOKEN` | push formula to `NielsdaWheelz/homebrew-tap` | `repo` |

**configuration location:** https://github.com/NielsdaWheelz/agency/settings/secrets/actions

**token creation:** https://github.com/settings/tokens (classic PAT with `repo` scope)

verify secret is configured:
1. go to repository settings > secrets and variables > actions
2. confirm `HOMEBREW_TAP_TOKEN` appears in repository secrets

## 3. repository state checks (pre-flight)

run all checks before proceeding. all must pass.

### 3.1 clean working tree

```bash
git status --porcelain
```

expected output: empty (no output)

if output is non-empty: commit or stash changes before proceeding.

### 3.2 on main branch

```bash
git branch --show-current
```

expected output: `main`

### 3.3 main is up to date

```bash
git fetch origin main
git diff HEAD origin/main --stat
```

expected output: empty (no output)

if output is non-empty:

```bash
git pull origin main
```

### 3.4 tests pass

```bash
go test ./...
```

expected output: `ok` for all packages, exit code 0

### 3.5 lint passes

```bash
make lint
```

expected output: no errors, exit code 0

### 3.6 version string shows dev

```bash
go run ./cmd/agency --version
```

expected output: `agency dev`

this confirms version is derived from git tags, not hardcoded.

### 3.7 goreleaser dry run

```bash
goreleaser release --snapshot --skip=publish --clean
```

expected output: build succeeds for all targets, exit code 0

verify archives:

```bash
ls dist/*.tar.gz
tar -tzf dist/agency_*_linux_amd64.tar.gz | grep completions
```

expected output: archives exist, completions directory present

## 4. release version decision

### 4.1 semantic versioning

this project uses semantic versioning: `vMAJOR.MINOR.PATCH`

| change type | version bump | example |
|-------------|--------------|---------|
| breaking API/CLI change | MAJOR | v1.0.0 -> v2.0.0 |
| new feature, backwards compatible | MINOR | v0.1.0 -> v0.2.0 |
| bug fix, no new features | PATCH | v0.1.0 -> v0.1.1 |

### 4.2 version source

version is derived from git tags at build time. there is no version file to edit.

### 4.3 choosing the version

1. review commits since last release:
   ```bash
   git log $(git describe --tags --abbrev=0)..HEAD --oneline
   ```

2. determine bump type based on changes

3. compute next version:
   ```bash
   git describe --tags --abbrev=0
   ```
   if output is `v0.1.0`, next patch is `v0.1.1`, next minor is `v0.2.0`

## 5. step-by-step release procedure

### 5.1 update main branch

```bash
git checkout main
git pull origin main
```

### 5.2 create annotated tag

replace `vX.Y.Z` with the chosen version:

```bash
git tag -a vX.Y.Z -m "vX.Y.Z"
```

### 5.3 push tag

```bash
git push origin vX.Y.Z
```

### 5.4 automated workflow

pushing the tag triggers `.github/workflows/release.yml`, which:

1. checks out the tagged commit
2. runs tests
3. runs goreleaser
4. builds binaries for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64
5. generates shell completions
6. creates GitHub release with archives and checksums
7. pushes homebrew formula to `NielsdaWheelz/homebrew-tap`

### 5.5 monitor workflow

```bash
gh run list --workflow=release.yml --limit=1
gh run watch
```

or view at: https://github.com/NielsdaWheelz/agency/actions/workflows/release.yml

## 6. verification checklist (post-release)

complete all checks after workflow succeeds.

### 6.1 GitHub release

- [ ] release page exists: https://github.com/NielsdaWheelz/agency/releases/tag/vX.Y.Z
- [ ] release title matches tag
- [ ] assets include:
  - [ ] `agency_X.Y.Z_darwin_amd64.tar.gz`
  - [ ] `agency_X.Y.Z_darwin_arm64.tar.gz`
  - [ ] `agency_X.Y.Z_linux_amd64.tar.gz`
  - [ ] `agency_X.Y.Z_linux_arm64.tar.gz`
  - [ ] `checksums.txt`

### 6.2 archive contents

```bash
curl -sL https://github.com/NielsdaWheelz/agency/releases/download/vX.Y.Z/agency_X.Y.Z_linux_amd64.tar.gz | tar -tzf -
```

expected contents:
```
agency
README.md
LICENSE
completions/agency.bash
completions/_agency
```

### 6.3 homebrew formula

- [ ] formula updated in `NielsdaWheelz/homebrew-tap`:
  ```bash
  gh api repos/NielsdaWheelz/homebrew-tap/commits --jq '.[0].commit.message'
  ```

### 6.4 homebrew install (macOS)

```bash
brew update
brew install NielsdaWheelz/tap/agency
```

or if already installed:

```bash
brew update
brew upgrade NielsdaWheelz/tap/agency
```

### 6.5 version verification

```bash
agency --version
```

expected output: `agency vX.Y.Z (commit <hash>)`

### 6.6 homebrew completions (bash)

```bash
ls $(brew --prefix)/share/bash-completion/completions/agency
```

expected output: file exists

### 6.7 homebrew completions (zsh)

```bash
ls $(brew --prefix)/share/zsh/site-functions/_agency
```

expected output: file exists

### 6.8 manual completion generation

```bash
agency completion bash > /dev/null && echo "bash: ok"
agency completion zsh > /dev/null && echo "zsh: ok"
```

expected output: both print ok

## 7. failure modes and recovery

### 7.1 GoReleaser fails

**symptoms:** workflow fails at goreleaser step

**diagnosis:**
```bash
gh run view --log-failed
```

**common causes:**
- goreleaser config syntax error in `.goreleaser.yaml`
- tests failing
- go build errors

**recovery:**
1. fix the issue on main
2. delete the tag:
   ```bash
   git tag -d vX.Y.Z
   git push origin :refs/tags/vX.Y.Z
   ```
3. delete the draft release if created (GitHub UI or `gh release delete vX.Y.Z`)
4. restart from section 5

### 7.2 homebrew tap update fails

**symptoms:** GitHub release exists but formula not updated

**diagnosis:**
1. check workflow logs for homebrew step
2. verify `HOMEBREW_TAP_TOKEN` secret exists and is not expired
3. verify token has `repo` scope

**recovery:**
1. fix token if expired (create new PAT, update secret)
2. re-run failed workflow:
   ```bash
   gh run rerun <run-id>
   ```
   or manually trigger by deleting and re-pushing tag (see 7.1)

### 7.3 release needs to be re-cut (same version)

**allowed when:**
- release was never publicly announced
- no users have installed the broken release
- issue is critical and cannot wait for next version

**not allowed when:**
- release has been publicly announced
- users have installed the release
- issue is minor (cut a patch release instead)

**procedure:**
1. delete GitHub release:
   ```bash
   gh release delete vX.Y.Z --yes
   ```
2. delete remote tag:
   ```bash
   git push origin :refs/tags/vX.Y.Z
   ```
3. delete local tag:
   ```bash
   git tag -d vX.Y.Z
   ```
4. fix the issue, commit to main
5. restart from section 5

**post-recovery for homebrew users:**
```bash
brew update && brew upgrade agency
```

### 7.4 release needs to be re-cut (new version)

if the release was public, cut a new patch version instead of replacing:

1. fix the issue, commit to main
2. tag with incremented patch version (e.g., v0.1.1)
3. follow standard release procedure

## 8. non-goals / future work

the following are explicitly out of scope for this release process:

| item | status | notes |
|------|--------|-------|
| linux distribution packages (apt, yum, pacman) | deferred | no current demand |
| homebrew/core submission | deferred | requires higher install count |
| code signing (macOS notarization) | deferred | requires Apple Developer account |
| windows builds | deferred | no current demand |
| automatic changelog generation | deferred | manual release notes preferred |
| release candidates (vX.Y.Z-rc.N) | deferred | not needed at current scale |

## appendix: distribution matrix

| platform | homebrew | GitHub releases |
|----------|----------|-----------------|
| darwin/arm64 | yes | yes |
| darwin/amd64 | yes | yes |
| linux/amd64 | yes | yes |
| linux/arm64 | no | yes |

| install method | binary | completions |
|----------------|--------|-------------|
| homebrew tap | automatic | automatic |
| GitHub releases tarball | manual | manual |
| `go install` | automatic | manual |

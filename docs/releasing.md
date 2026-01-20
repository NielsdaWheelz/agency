# releasing agency

this document describes how to cut releases for agency.

## overview

releases are tag-driven via `.github/workflows/release.yml` and `.goreleaser.yaml`.

### distribution targets

| platform | homebrew | github releases |
|----------|----------|-----------------|
| darwin/arm64 | ✓ | ✓ |
| darwin/amd64 | ✓ | ✓ |
| linux/amd64 | ✓ | ✓ |
| linux/arm64 | - | ✓ |

### install methods

| method | binary | completions |
|--------|--------|-------------|
| homebrew tap | auto | auto |
| github releases (tar.gz) | manual | manual |
| `go install` | auto | manual |

## prerequisites

### repository secrets

the following secrets must be configured in the repository:

- `HOMEBREW_TAP_TOKEN`: PAT with write access to `NielsdaWheelz/homebrew-tap`
  - required scopes: `repo` (for pushing to homebrew-tap)
  - create at: https://github.com/settings/tokens

### local tools (for preflight)

- goreleaser (`go install github.com/goreleaser/goreleaser/v2@latest`)
- go 1.21+

## cutting a release

### 1. ensure main is ready

```bash
git checkout main
git pull origin main
make lint
go test ./...
```

### 2. run preflight verification

```bash
# generate completions (goreleaser will also do this, but test locally first)
make completions

# run goreleaser in snapshot mode
goreleaser release --snapshot --skip=publish --clean
```

verify:
- [ ] `make completions` succeeds and creates non-empty files
- [ ] archives contain `completions/agency.bash` and `completions/_agency`
- [ ] `./dist/agency_linux_amd64_v1/agency --version` shows correct version
- [ ] no errors in goreleaser output

### 3. tag and push

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

the tag push triggers the release workflow which:
1. runs tests
2. builds binaries for all platforms
3. generates shell completions
4. creates github release with archives
5. pushes homebrew formula to tap repo

### 4. verify release

#### github release

- [ ] release page exists at https://github.com/NielsdaWheelz/agency/releases
- [ ] binaries uploaded for all platforms
- [ ] checksums.txt present
- [ ] archives contain `completions/` directory

#### homebrew

- [ ] formula updated in `NielsdaWheelz/homebrew-tap`
- [ ] `brew install NielsdaWheelz/tap/agency` succeeds on clean system
- [ ] completions installed automatically

#### verification commands

```bash
# homebrew (macos)
brew install NielsdaWheelz/tap/agency
agency --version
agency doctor

# tarball (linux/manual)
curl -LO https://github.com/NielsdaWheelz/agency/releases/download/v0.1.0/agency_0.1.0_linux_amd64.tar.gz
tar xzf agency_0.1.0_linux_amd64.tar.gz
./agency --version

# completions (after manual install)
./agency completion bash > ~/.agency-completion.bash
source ~/.agency-completion.bash
agency <TAB>
```

## version format

release binaries print:
```
agency v0.1.0 (commit abc1234)
```

dev builds (via `go install` or `go build`) print:
```
agency dev
```

## troubleshooting

### release workflow failed

1. check workflow logs in github actions
2. common issues:
   - `HOMEBREW_TAP_TOKEN` not set or expired
   - goreleaser config syntax error
   - tests failing

### homebrew formula not updated

1. verify `HOMEBREW_TAP_TOKEN` secret is set
2. check homebrew-tap repo for recent commits
3. verify token has `repo` scope

### completion generation failed

1. verify `agency completion bash` works locally
2. check for syntax errors in completion.go

## archive contents

each release archive contains:
```
agency_0.1.0_darwin_arm64/
├── agency
├── README.md
├── LICENSE
└── completions/
    ├── agency.bash
    └── _agency
```

## rollback

if a release has critical issues:

1. delete the github release (does not delete the tag)
2. fix the issue
3. force-push a new tag:
   ```bash
   git tag -d v0.1.0
   git push origin :refs/tags/v0.1.0
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

note: homebrew users may have cached the old version; advise `brew update && brew upgrade agency`.

add go project skeleton + shared contracts for agency CLI

pr-00: establish foundational infrastructure for slice-00 implementation.

scaffolding:
- go module github.com/NielsdaWheelz/agency (go 1.21)
- cmd/agency/main.go entry point
- internal/cli/dispatch.go with init/doctor stubs
- internal/version/version.go for build-time injection

error system (internal/errors):
- AgencyError type with Code, Msg, Cause fields
- stable format: "CODE: message" for Error()
- Print() outputs "error_code: CODE\nmessage" to stderr
- ExitCode() returns 0/1/2 per spec
- initial codes: E_USAGE, E_NOT_IMPLEMENTED

interfaces (stub-friendly):
- CommandRunner: Run(ctx, name, args, opts) -> CmdResult
  - returns exit code in result, error only for exec failures
- FS: MkdirAll, ReadFile, WriteFile, Stat, Rename, Remove, Chmod, CreateTemp
  - thin wrappers over os package

atomic write (internal/fs/atomic.go):
- WriteFileAtomic: temp file + chmod + rename pattern
- temp created in same dir as target for POSIX atomicity
- defer-based cleanup on any error path
- preserves original file on failure

CLI behavior:
- agency (no args): print usage, E_USAGE, exit 2
- agency -h/--help: print usage, exit 0
- agency -v/--version: print version, exit 0
- agency init: E_NOT_IMPLEMENTED, exit 1
- agency doctor: E_NOT_IMPLEMENTED, exit 1
- agency <unknown>: print usage, E_USAGE, exit 2

tests:
- errors_test.go: format stability, GetCode, ExitCode, Print
- atomic_test.go: basic write, overwrite, permissions, rename failure recovery
- dispatch_test.go: all CLI paths including help flags

docs:
- README.md: installation, usage, development instructions
- .gitignore: binaries, test output, .agency/ dirs
- Makefile: build, test, run targets

conforms to:
- docs/v1/constitution.md (error codes, output format)
- docs/v1/s0/s0_prs/s0_pr00.md (exact scope)

no behavior implemented beyond stubs; ready for pr-01.
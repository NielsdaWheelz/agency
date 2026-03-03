package mergeflow

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	mergeLogDirPerm  = 0o700
	mergeLogFilePerm = 0o600
)

// VerifyEnvInput captures merge verify environment contract fields.
type VerifyEnvInput struct {
	RunID         string
	Name          string
	RepoRoot      string
	WorkspaceRoot string
	Branch        string
	ParentBranch  string
	Runner        string
	PRURL         string
	PRNumber      int
	InvocationDir string
}

// BuildVerifyEnv builds deterministic merge verify environment.
func BuildVerifyEnv(baseEnv []string, input VerifyEnvInput) []string {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = input.RunID
	}

	agencyEnv := map[string]string{
		"AGENCY_RUN_ID":         input.RunID,
		"AGENCY_NAME":           name,
		"AGENCY_REPO_ROOT":      input.RepoRoot,
		"AGENCY_WORKSPACE_ROOT": input.WorkspaceRoot,
		"AGENCY_BRANCH":         input.Branch,
		"AGENCY_PARENT_BRANCH":  input.ParentBranch,
		"AGENCY_ORIGIN_NAME":    "origin",
		"AGENCY_ORIGIN_URL":     "",
		"AGENCY_RUNNER":         input.Runner,
		"AGENCY_PR_URL":         input.PRURL,
		"AGENCY_PR_NUMBER":      "",
		"AGENCY_DOTAGENCY_DIR":  filepath.Join(input.WorkspaceRoot, ".agency"),
		"AGENCY_OUTPUT_DIR":     filepath.Join(input.WorkspaceRoot, ".agency", "out"),
		"AGENCY_LOG_DIR":        filepath.Join(input.InvocationDir, "logs"),
		"AGENCY_NONINTERACTIVE": "1",
	}
	if input.PRNumber != 0 {
		agencyEnv["AGENCY_PR_NUMBER"] = strconv.Itoa(input.PRNumber)
	}

	return MergeEnvDeterministic(baseEnv, nonInteractiveRunnerEnv(), agencyEnv)
}

// MergeEnvDeterministic merges base and overlay env maps with deterministic
// ordering, no duplicate keys, and "overlay wins" semantics.
func MergeEnvDeterministic(baseEnv []string, overlays ...map[string]string) []string {
	merged := make(map[string]string, len(baseEnv))

	for _, entry := range baseEnv {
		key, val, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		merged[key] = val
	}
	for _, overlay := range overlays {
		for k, v := range overlay {
			merged[k] = v
		}
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, k+"="+merged[k])
	}
	return result
}

// ResolveRepoRoot resolves repository root from repo record first, then git.
func ResolveRepoRoot(ctx context.Context, cr exec.CommandRunner, st *store.Store, repoID, workspaceRoot string) (string, error) {
	if st != nil && strings.TrimSpace(repoID) != "" {
		repoRecord, exists, err := st.LoadRepoRecord(repoID)
		if err == nil && exists {
			root := strings.TrimSpace(repoRecord.PreferredRoot)
			if root == "" {
				root = strings.TrimSpace(repoRecord.RepoRootLastSeen)
			}
			if root != "" {
				return CanonicalizePath(root), nil
			}
		}
	}

	root, err := git.GetRepoRoot(ctx, cr, workspaceRoot)
	if err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to resolve repository root", err)
	}
	return CanonicalizePath(root.Path), nil
}

// CanonicalizePath normalizes path comparisons to abs-clean-resolved path.
func CanonicalizePath(path string) string {
	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err == nil {
		clean = abs
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	return clean
}

// WriteMergeLog persists merge logs atomically with private permissions.
func WriteMergeLog(fsys agencyfs.FS, mergeLogPath, command string, result exec.CmdResult, runErr error) error {
	logDir := filepath.Dir(mergeLogPath)
	if err := fsys.MkdirAll(logDir, mergeLogDirPerm); err != nil {
		return err
	}
	if err := fsys.Chmod(logDir, mergeLogDirPerm); err != nil {
		return err
	}

	var builder strings.Builder
	builder.WriteString("=== ")
	builder.WriteString(command)
	builder.WriteString(" ===\n")
	builder.WriteString("Exit code: ")
	builder.WriteString(strconv.Itoa(result.ExitCode))
	builder.WriteString("\n")
	if strings.TrimSpace(result.Stdout) != "" {
		builder.WriteString("\n=== stdout ===\n")
		builder.WriteString(result.Stdout)
		if !strings.HasSuffix(result.Stdout, "\n") {
			builder.WriteString("\n")
		}
	}
	if strings.TrimSpace(result.Stderr) != "" {
		builder.WriteString("\n=== stderr ===\n")
		builder.WriteString(result.Stderr)
		if !strings.HasSuffix(result.Stderr, "\n") {
			builder.WriteString("\n")
		}
	}
	if runErr != nil {
		builder.WriteString("\n=== execution_error ===\n")
		builder.WriteString(runErr.Error())
		builder.WriteString("\n")
	}

	if err := agencyfs.WriteFileAtomic(fsys, mergeLogPath, []byte(builder.String()), mergeLogFilePerm); err != nil {
		return err
	}
	if err := fsys.Chmod(mergeLogPath, mergeLogFilePerm); err != nil {
		return err
	}
	return nil
}

func nonInteractiveRunnerEnv() map[string]string {
	return map[string]string{
		"CI":                  "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
	}
}

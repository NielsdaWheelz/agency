package daemon

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/mergeflow"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const worktreeMergeArchiveRemoveTimeout = 30 * time.Second

func (s *Server) runWorktreeArchive(
	ctx context.Context,
	record *store.IntegrationWorktreeRecord,
	pr *mergePRView,
	repoRoot string,
	agencyJSON config.AgencyConfig,
	profileEnv map[string]string,
) (string, error) {
	if record == nil || record.Meta == nil {
		return "", errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta

	logsDir := s.store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID)
	archiveLogPath := filepath.Join(logsDir, "archive.log")
	treeExists := true
	if _, err := s.fsys.Stat(wtMeta.TreePath); err != nil {
		if os.IsNotExist(err) {
			treeExists = false
		} else {
			return "", errors.Wrap(errors.EInternal, "failed to stat integration worktree", err)
		}
	}

	if !treeExists {
		if err := s.store.UpdateIntegrationWorktreeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
			m.State = store.WorktreeStateArchived
		}); err != nil {
			code := errors.CodeOr(err, errors.EMetaWriteFailed)
			return "", errors.Wrap(code, "failed to persist archived state", err)
		}
		if err := mergeflow.WriteMergeLog(s.fsys, archiveLogPath, "archive skipped: worktree already removed", exec.CmdResult{ExitCode: 0}, nil); err != nil {
			return "", errors.WrapWithDetails(
				errors.EPersistFailed,
				"failed to persist archive log",
				err,
				map[string]string{
					"archive_log_path": archiveLogPath,
					"hint":             "inspect archive cleanup state and retry if needed",
				},
			)
		}
		return archiveLogPath, nil
	}

	worktreeDir := s.store.IntegrationWorktreeDir(record.RepoID, record.WorktreeID)
	envList := buildWorktreeMergeScriptEnv(record, repoRoot, worktreeDir, pr, profileEnv)
	env := make(map[string]string, len(envList))
	for _, entry := range envList {
		key, val, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			env[key] = val
		}
	}

	archiveCmd := agencyJSON.Scripts.Archive.Path
	runCtx, cancel := context.WithTimeout(ctx, agencyJSON.Scripts.Archive.Timeout)
	defer cancel()
	result, runErr := s.runner.Run(runCtx, archiveCmd, nil, exec.RunOpts{
		Dir: wtMeta.TreePath,
		Env: env,
	})
	if err := mergeflow.WriteMergeLog(s.fsys, archiveLogPath, archiveCmd, result, runErr); err != nil {
		return "", errors.WrapWithDetails(
			errors.EPersistFailed,
			"failed to persist archive log",
			err,
			map[string]string{
				"archive_log_path": archiveLogPath,
				"hint":             "inspect archive cleanup state and retry if needed",
			},
		)
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return "", errors.Wrap(errors.EWorktreeMergeInterrupted, "merge interrupted while running archive script", ctx.Err())
		}
		return "", errors.WrapWithDetails(
			errors.EArchiveFailed,
			"archive script failed to start",
			runErr,
			map[string]string{
				"archive_log_path": archiveLogPath,
				"command":          archiveCmd,
			},
		)
	}
	if result.ExitCode != 0 {
		return "", errors.NewWithDetails(
			errors.EArchiveFailed,
			fmt.Sprintf("archive script exited %d", result.ExitCode),
			map[string]string{
				"archive_log_path": archiveLogPath,
				"command":          archiveCmd,
				"exit_code":        fmt.Sprintf("%d", result.ExitCode),
				"hint":             "inspect archive.log, fix the archive step, and rerun worktree pr merge",
			},
		)
	}

	removeArgs := []string{"-C", repoRoot, "worktree", "remove", "--force", wtMeta.TreePath}
	removeCtx, cancel := context.WithTimeout(ctx, worktreeMergeArchiveRemoveTimeout)
	defer cancel()

	removeResult, removeRunErr := s.runner.Run(removeCtx, "git", removeArgs, exec.RunOpts{Env: withNonInteractiveEnv(profileEnv)})
	removeCmd := "git " + strings.Join(removeArgs, " ")
	s.appendArchiveLogSection(archiveLogPath, removeCmd, removeResult, removeRunErr)
	if removeRunErr != nil {
		if ctx.Err() != nil {
			return "", errors.Wrap(errors.EWorktreeMergeInterrupted, "merge interrupted while removing archived worktree", ctx.Err())
		}
		if stderrors.Is(removeRunErr, context.DeadlineExceeded) {
			return "", errors.NewWithDetails(
				errors.EArchiveFailed,
				"git worktree remove timed out after archive cleanup",
				map[string]string{
					"archive_log_path": archiveLogPath,
					"command":          removeCmd,
					"hint":             "inspect archive.log, retry the merge cleanup, or remove the worktree manually if git is blocked",
				},
			)
		}
		return "", errors.WrapWithDetails(
			errors.EArchiveFailed,
			"git worktree remove failed to start",
			removeRunErr,
			map[string]string{
				"archive_log_path": archiveLogPath,
				"command":          removeCmd,
			},
		)
	}
	if removeResult.ExitCode != 0 {
		return "", errors.NewWithDetails(
			errors.EArchiveFailed,
			fmt.Sprintf("git worktree remove exited %d", removeResult.ExitCode),
			map[string]string{
				"archive_log_path": archiveLogPath,
				"command":          removeCmd,
				"exit_code":        fmt.Sprintf("%d", removeResult.ExitCode),
				"stderr":           strings.TrimSpace(removeResult.Stderr),
			},
		)
	}

	if err := s.store.UpdateIntegrationWorktreeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.State = store.WorktreeStateArchived
	}); err != nil {
		s.appendArchiveLogSection(archiveLogPath, "metadata", exec.CmdResult{
			Stdout: fmt.Sprintf("failed to persist archived state: %v\n", err),
		}, nil)
		code := errors.CodeOr(err, errors.EMetaWriteFailed)
		return "", errors.WrapWithDetails(
			code,
			"failed to persist archived worktree state",
			err,
			map[string]string{
				"archive_log_path": archiveLogPath,
				"hint":             "worktree removal completed; fix metadata persistence and rerun worktree pr merge to reconcile archived state",
			},
		)
	}

	return archiveLogPath, nil
}

// appendArchiveLogSection appends a delimited "=== title ===" block plus
// optional stdout/stderr/execution_error sections to archiveLogPath, then
// re-applies the 0o600 mode. Best-effort: open failures silently drop the
// section so the surrounding flow's primary error is preserved.
func (s *Server) appendArchiveLogSection(archiveLogPath, title string, result exec.CmdResult, runErr error) {
	logFile, err := os.OpenFile(archiveLogPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = logFile.Close() }()

	_, _ = fmt.Fprintln(logFile)
	_, _ = fmt.Fprintf(logFile, "=== %s ===\n", title)
	_, _ = fmt.Fprintf(logFile, "Exit code: %d\n", result.ExitCode)
	writeNamedSection := func(name, content string) {
		if strings.TrimSpace(content) == "" {
			return
		}
		_, _ = fmt.Fprintln(logFile)
		_, _ = fmt.Fprintln(logFile, "=== "+name+" ===")
		_, _ = fmt.Fprint(logFile, content)
		if !strings.HasSuffix(content, "\n") {
			_, _ = fmt.Fprintln(logFile)
		}
	}
	writeNamedSection("stdout", result.Stdout)
	writeNamedSection("stderr", result.Stderr)
	if runErr != nil {
		_, _ = fmt.Fprintln(logFile)
		_, _ = fmt.Fprintln(logFile, "=== execution_error ===")
		_, _ = fmt.Fprintln(logFile, runErr.Error())
	}
	_ = s.fsys.Chmod(archiveLogPath, 0o600)
}

func buildWorktreeMergeScriptEnv(
	record *store.IntegrationWorktreeRecord,
	repoRoot string,
	worktreeDir string,
	pr *mergePRView,
	profileEnv map[string]string,
) []string {
	name := ""
	runner := "worktree"
	workspaceRoot := ""
	branch := ""
	baseBranch := ""
	if record != nil && record.Meta != nil {
		name = strings.TrimSpace(record.Meta.Name)
		workspaceRoot = record.Meta.TreePath
		branch = record.Meta.Branch
		baseBranch = record.Meta.BaseBranch
	}
	if name == "" && record != nil {
		name = record.WorktreeID
	}

	return mergeflow.BuildVerifyEnv(exec.MergeEnv(os.Environ(), profileEnv), mergeflow.VerifyEnvInput{
		RunID:         record.WorktreeID,
		Name:          name,
		RepoRoot:      repoRoot,
		WorkspaceRoot: workspaceRoot,
		Branch:        branch,
		BaseBranch:    baseBranch,
		Runner:        runner,
		PRURL:         pr.URL,
		PRNumber:      pr.Number,
		InvocationDir: worktreeDir,
	})
}

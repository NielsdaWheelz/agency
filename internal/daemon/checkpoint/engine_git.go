package checkpoint

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/exec"
)

func (e *Engine) getGitDir(ctx context.Context) (string, error) {
	result, err := e.runGit(ctx, e.sandboxPath, e.config.Env, "git rev-parse --git-dir", "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(result.Stdout)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(e.sandboxPath, gitDir)
	}
	return gitDir, nil
}

// DiscoverGitIgnoredDirs returns gitignored directories using git's own
// exclude-standard resolution.
func DiscoverGitIgnoredDirs(ctx context.Context, runner exec.CommandRunner, sandboxPath string, env map[string]string) (map[string]bool, error) {
	result, err := runner.Run(ctx, "git", []string{
		"-C", sandboxPath,
		"ls-files", "--others", "--ignored", "--exclude-standard", "--directory",
	}, exec.RunOpts{Env: env})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("git ls-files ignored dirs failed: %s", result.Stderr)
	}
	return parseGitIgnoredDirs(sandboxPath, result.Stdout), nil
}

func (e *Engine) checkDenylist(ctx context.Context) ([]string, error) {
	result, err := e.runGit(ctx, e.sandboxPath, e.config.Env, "git ls-files", "ls-files", "-o", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		return nil, nil
	}

	var denied []string
	for _, file := range strings.Split(output, "\n") {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		base := filepath.Base(file)
		if matchesDenylist(base) {
			denied = append(denied, file)
		}
	}

	return denied, nil
}

func matchesDenylist(basename string) bool {
	for _, pattern := range denylistPatterns {
		if pattern == basename {
			return true
		}

		matched, err := filepath.Match(pattern, basename)
		if err == nil && matched {
			return true
		}

		if pattern == ".env.*" && strings.HasPrefix(basename, ".env.") {
			return true
		}
	}
	return false
}

func (e *Engine) computeDiffstat(ctx context.Context, base, commit string) string {
	result, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"diff", "--stat", "--stat-width=80", base + ".." + commit,
	}, exec.RunOpts{Env: e.config.Env})
	if err != nil || result.ExitCode != 0 {
		return ""
	}

	trimmed := strings.TrimSpace(result.Stdout)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	summary := lines[len(lines)-1]

	filesRe := regexp.MustCompile(`(\d+) files? changed`)
	insertionsRe := regexp.MustCompile(`(\d+) insertions?\(\+\)`)
	deletionsRe := regexp.MustCompile(`(\d+) deletions?\(-\)`)

	files := "0"
	insertions := "0"
	deletions := "0"

	if m := filesRe.FindStringSubmatch(summary); len(m) > 1 {
		files = m[1]
	}
	if m := insertionsRe.FindStringSubmatch(summary); len(m) > 1 {
		insertions = m[1]
	}
	if m := deletionsRe.FindStringSubmatch(summary); len(m) > 1 {
		deletions = m[1]
	}

	return fmt.Sprintf("+%s -%s in %s files", insertions, deletions, files)
}

func (e *Engine) computeChangedPaths(ctx context.Context, base, commit string, maxPaths int) ([]string, int, bool) {
	if strings.TrimSpace(base) == "" || strings.TrimSpace(commit) == "" || maxPaths <= 0 {
		return nil, 0, false
	}

	result, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"diff", "--name-status", "--find-renames", base + ".." + commit,
	}, exec.RunOpts{Env: e.config.Env})
	if err != nil || result.ExitCode != 0 {
		return nil, 0, false
	}

	trimmed := strings.TrimSpace(result.Stdout)
	if trimmed == "" {
		return nil, 0, false
	}

	lines := strings.Split(trimmed, "\n")
	seen := make(map[string]struct{}, len(lines))
	paths := make([]string, 0, maxPaths)
	total := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		path := ""
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if len(fields) >= 3 {
				path = strings.TrimSpace(fields[2])
			}
		} else {
			path = strings.TrimSpace(fields[1])
		}
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		total++
		if len(paths) < maxPaths {
			paths = append(paths, path)
		}
	}

	if total == 0 {
		return nil, 0, false
	}
	return paths, total, total > len(paths)
}

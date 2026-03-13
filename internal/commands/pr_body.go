package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	maxPRBodyCommits = 10
	maxPRBodyFiles   = 20

	maxPRBodyStatWidth     = 120
	maxPRBodyStatNameWidth = 80
	maxPRBodySubjectChars  = 200
)

func writeFallbackPRBody(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, workDir, parentRef, branch string, meta *store.RunMeta) (string, string, error) {
	bodyPath := filepath.Join(workDir, ".agency", "tmp", "pr_body.md")
	bodyDir := filepath.Dir(bodyPath)
	if err := fsys.MkdirAll(bodyDir, 0o700); err != nil {
		return "", "", fmt.Errorf("failed to create pr body dir: %w", err)
	}
	if err := fsys.Chmod(bodyDir, 0o700); err != nil {
		return "", "", fmt.Errorf("failed to set pr body dir permissions: %w", err)
	}

	rangeRef := parentRef + ".." + branch
	commitCount, commitCountOK := gitCount(ctx, cr, workDir, []string{"rev-list", "--count", rangeRef})
	commitSubjects, commitsOK, commitsTruncated := gitLinesBounded(
		ctx,
		cr,
		workDir,
		[]string{
			"log",
			fmt.Sprintf("--format=%%<(%d,trunc)%%s", maxPRBodySubjectChars),
			"-n",
			strconv.Itoa(maxPRBodyCommits + 1),
			rangeRef,
		},
		maxPRBodyCommits,
	)
	diffStat, fileList, diffOK, filesTruncated := gitDiffStatBounded(ctx, cr, workDir, rangeRef, maxPRBodyFiles)
	fileCount, fileCountOK := gitChangedFileCount(ctx, cr, workDir, rangeRef)

	commitCountStr := "unknown"
	if commitCountOK {
		commitCountStr = fmt.Sprintf("%d", commitCount)
	} else if commitsOK {
		commitCountStr = fmt.Sprintf("%d+", len(commitSubjects))
	}
	fileCountStr := "unknown"
	if fileCountOK {
		fileCountStr = fmt.Sprintf("%d", fileCount)
	} else if diffOK {
		fileCountStr = fmt.Sprintf("%d+", len(fileList))
	}

	summaryLine := "auto-generated summary"
	if commitsOK && len(commitSubjects) > 0 {
		summaryLine = commitSubjects[0]
	}

	if diffOK {
		diffStat = strings.TrimSpace(diffStat)
		if diffStat == "" {
			diffStat = "diffstat unavailable"
		}
	} else {
		diffStat = "diffstat unavailable"
	}

	title := meta.Name
	if title == "" {
		title = meta.Branch
	}

	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## summary\n")
	b.WriteString("- " + summaryLine + "\n")
	_, _ = fmt.Fprintf(&b, "- %s commits, %s files changed\n\n", commitCountStr, fileCountStr)

	b.WriteString("## commits\n")
	appendList(&b, commitSubjects, commitsOK, commitsTruncated, maxPRBodyCommits, "commit list unavailable", commitCount, commitCountOK)
	b.WriteString("\n")

	b.WriteString("## changes\n")
	b.WriteString("```text\n")
	b.WriteString(diffStat)
	b.WriteString("\n```\n\n")

	b.WriteString("## files\n")
	appendList(&b, fileList, diffOK, filesTruncated, maxPRBodyFiles, "file list unavailable", fileCount, fileCountOK)
	b.WriteString("\n")

	b.WriteString("## tests\n")
	b.WriteString("- not run (report missing or incomplete)\n\n")

	b.WriteString("## meta\n")
	b.WriteString("- run_id: " + meta.RunID + "\n")
	b.WriteString("- branch: " + meta.Branch + "\n")
	b.WriteString("- parent: " + meta.ParentBranch + "\n")
	b.WriteString("- generated_at: " + time.Now().UTC().Format(time.RFC3339) + "\n")

	if err := fsys.WriteFile(bodyPath, []byte(b.String()), 0o600); err != nil {
		return "", "", fmt.Errorf("failed to write pr body: %w", err)
	}
	if err := fsys.Chmod(bodyPath, 0o600); err != nil {
		return "", "", fmt.Errorf("failed to set pr body permissions: %w", err)
	}

	bodyHash := computeReportHash(fsys, bodyPath)
	if bodyHash == "" {
		return "", "", fmt.Errorf("failed to compute pr body hash")
	}

	return bodyPath, bodyHash, nil
}

func gitLinesBounded(ctx context.Context, cr exec.CommandRunner, workDir string, args []string, max int) ([]string, bool, bool) {
	text, ok := gitText(ctx, cr, workDir, args)
	if !ok {
		return nil, false, false
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil, true, false
	}
	if len(out) > max {
		return out[:max], true, true
	}
	return out, true, false
}

func gitText(ctx context.Context, cr exec.CommandRunner, workDir string, args []string) (string, bool) {
	result, err := cr.Run(ctx, "git", args, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil || result.ExitCode != 0 {
		return "", false
	}
	return result.Stdout, true
}

func gitCount(ctx context.Context, cr exec.CommandRunner, workDir string, args []string) (int, bool) {
	text, ok := gitText(ctx, cr, workDir, args)
	if !ok {
		return 0, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}

var shortStatFilesPattern = regexp.MustCompile(`(\d+)\s+files?\s+changed`)

func gitChangedFileCount(ctx context.Context, cr exec.CommandRunner, workDir, rangeRef string) (int, bool) {
	text, ok := gitText(ctx, cr, workDir, []string{"diff", "--shortstat", rangeRef})
	if !ok {
		return 0, false
	}
	match := shortStatFilesPattern.FindStringSubmatch(text)
	if len(match) != 2 {
		if strings.TrimSpace(text) == "" {
			return 0, true
		}
		return 0, false
	}
	count, err := strconv.Atoi(match[1])
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}

func gitDiffStatBounded(ctx context.Context, cr exec.CommandRunner, workDir, rangeRef string, maxFiles int) (diffStat string, files []string, ok bool, truncated bool) {
	statArg := fmt.Sprintf("--stat=%d,%d,%d", maxPRBodyStatWidth, maxPRBodyStatNameWidth, maxFiles+1)
	diffStat, ok = gitText(ctx, cr, workDir, []string{"diff", statArg, rangeRef})
	if !ok {
		return "", nil, false, false
	}
	files, truncated = parseDiffStatFiles(diffStat, maxFiles)
	return diffStat, files, true, truncated
}

func parseDiffStatFiles(diffStat string, max int) ([]string, bool) {
	lines := strings.Split(diffStat, "\n")
	files := make([]string, 0, max+1)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.Contains(line, "files changed") || strings.Contains(line, "file changed") {
			continue
		}
		pipeIdx := strings.Index(line, "|")
		if pipeIdx <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:pipeIdx])
		if name == "" {
			continue
		}
		files = append(files, name)
	}
	if len(files) <= max {
		return files, false
	}
	return files[:max], true
}

func appendList(b *strings.Builder, items []string, ok bool, truncated bool, max int, unavailable string, totalCount int, totalCountOK bool) {
	if !ok || len(items) == 0 {
		b.WriteString("- " + unavailable + "\n")
		return
	}

	limit := len(items)
	if limit > max {
		limit = max
	}
	for i := 0; i < limit; i++ {
		b.WriteString("- " + items[i] + "\n")
	}
	if totalCountOK && totalCount > limit {
		fmt.Fprintf(b, "- ... and %d more\n", totalCount-limit)
		return
	}
	if truncated {
		b.WriteString("- ... and more\n")
		return
	}
	if len(items) > max {
		fmt.Fprintf(b, "- ... and %d more\n", len(items)-max)
	}
}

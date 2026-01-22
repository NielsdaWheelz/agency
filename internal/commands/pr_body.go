package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	maxPRBodyCommits = 10
	maxPRBodyFiles   = 20
)

func writeFallbackPRBody(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, workDir, parentRef, branch string, meta *store.RunMeta) (string, string, error) {
	bodyPath := filepath.Join(workDir, ".agency", "tmp", "pr_body.md")
	if err := fsys.MkdirAll(filepath.Dir(bodyPath), 0o755); err != nil {
		return "", "", fmt.Errorf("failed to create pr body dir: %w", err)
	}

	rangeRef := parentRef + ".." + branch
	commitSubjects, commitsOK := gitLines(ctx, cr, workDir, []string{"log", "--format=%s", rangeRef})
	diffStat, diffOK := gitText(ctx, cr, workDir, []string{"diff", "--stat", rangeRef})
	fileList, filesOK := gitLines(ctx, cr, workDir, []string{"diff", "--name-only", rangeRef})

	commitCountStr := "unknown"
	if commitsOK {
		commitCountStr = fmt.Sprintf("%d", len(commitSubjects))
	}
	fileCountStr := "unknown"
	if filesOK {
		fileCountStr = fmt.Sprintf("%d", len(fileList))
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
	b.WriteString(fmt.Sprintf("- %s commits, %s files changed\n\n", commitCountStr, fileCountStr))

	b.WriteString("## commits\n")
	appendList(&b, commitSubjects, commitsOK, maxPRBodyCommits, "commit list unavailable")
	b.WriteString("\n")

	b.WriteString("## changes\n")
	b.WriteString("```text\n")
	b.WriteString(diffStat)
	b.WriteString("\n```\n\n")

	b.WriteString("## files\n")
	appendList(&b, fileList, filesOK, maxPRBodyFiles, "file list unavailable")
	b.WriteString("\n")

	b.WriteString("## tests\n")
	b.WriteString("- not run (report missing or incomplete)\n\n")

	b.WriteString("## meta\n")
	b.WriteString("- run_id: " + meta.RunID + "\n")
	b.WriteString("- branch: " + meta.Branch + "\n")
	b.WriteString("- parent: " + meta.ParentBranch + "\n")
	b.WriteString("- generated_at: " + time.Now().UTC().Format(time.RFC3339) + "\n")

	if err := fsys.WriteFile(bodyPath, []byte(b.String()), 0o644); err != nil {
		return "", "", fmt.Errorf("failed to write pr body: %w", err)
	}

	bodyHash := computeReportHash(fsys, bodyPath)
	if bodyHash == "" {
		return "", "", fmt.Errorf("failed to compute pr body hash")
	}

	return bodyPath, bodyHash, nil
}

func gitLines(ctx context.Context, cr exec.CommandRunner, workDir string, args []string) ([]string, bool) {
	text, ok := gitText(ctx, cr, workDir, args)
	if !ok {
		return nil, false
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
		return nil, true
	}
	return out, true
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

func appendList(b *strings.Builder, items []string, ok bool, max int, unavailable string) {
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
	if len(items) > max {
		fmt.Fprintf(b, "- ... and %d more\n", len(items)-max)
	}
}

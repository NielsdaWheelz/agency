package watch

import (
	"context"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const logReadLimit = 65536

func loadInvocationLogs(ctx context.Context, client *daemonclient.Client, invocationID, repoID, kind string) (string, error) {
	if client == nil {
		return "", errors.New(errors.EInternal, "watch runtime requires a daemon client")
	}
	if strings.TrimSpace(invocationID) == "" || strings.TrimSpace(repoID) == "" {
		return "", errors.New(errors.EInvalidArgument, "logs page requires an invocation and repo")
	}
	if strings.TrimSpace(kind) == "" {
		kind = daemon.InvocationLogKindRaw
	}

	var builder strings.Builder
	if _, err := client.DrainInvocationLogs(ctx, invocationID, repoID, daemon.GetLogsParams{
		Kind:  kind,
		Limit: logReadLimit,
	}, &builder); err != nil {
		return "", err
	}

	return builder.String(), nil
}

func (m model) renderLogs() string {
	width := m.viewWidth()
	lines := m.renderPageHeader("logs (" + m.currentLogsKind() + ")")
	if line := m.styledActionLine(width); line != "" {
		lines = append(lines, line, "")
	}
	lines = appendPageError(lines, "logs", m.logsError, width)
	lines = appendPageLoading(lines, "logs", m.logsLoading)
	lines = append(lines, m.renderScrollViewport(logLines(m.logsContent), m.logsScroll)...)
	lines = append(lines, "")
	lines = append(lines, warningStyle.Render("j/k move • a attach • d review • x actions • r refresh • b back • q quit"))
	return strings.Join(lines, "\n")
}

func logLines(content string) []string {
	return contentLines(content, "(no log output yet)")
}

func (m model) maxLogsScroll() int {
	return m.pageMaxScroll(logLines(m.logsContent))
}

func contentLines(content, emptyMessage string) []string {
	if strings.TrimSpace(content) == "" {
		return []string{emptyMessage}
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []string{emptyMessage}
	}
	return lines
}

func (m model) currentLogsKind() string {
	if strings.TrimSpace(m.logsKind) != "" {
		return m.logsKind
	}
	selected, ok := m.selectedInvocation()
	if ok && strings.TrimSpace(selected.Mode) == "headed" {
		return daemon.InvocationLogKindTerminal
	}
	return daemon.InvocationLogKindRaw
}

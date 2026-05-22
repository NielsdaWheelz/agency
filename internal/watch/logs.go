package watch

import (
	"context"
	"strconv"
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
	width := m.width
	if width <= 0 {
		width = 120
	}

	lines := m.renderPageHeader("logs (" + m.currentLogsKind() + ")")
	if m.lastActionMessage != "" {
		actionLine := "action: " + truncateWithEllipsis(m.lastActionMessage, width-10)
		switch {
		case m.lastActionError:
			lines = append(lines, errorStyle.Render(actionLine))
		case m.actionRunning:
			lines = append(lines, warningStyle.Render(actionLine))
		default:
			lines = append(lines, actionStyle.Render(actionLine))
		}
		lines = append(lines, "")
	}
	if m.logsError != "" {
		lines = append(lines, errorStyle.Render("logs error: "+truncateWithEllipsis(m.logsError, width-4)))
		lines = append(lines, "")
	}
	if m.logsLoading {
		lines = append(lines, warningStyle.Render("loading logs..."))
		lines = append(lines, "")
	}

	logLines := logLines(m.logsContent)
	visible := m.logVisibleLines()
	start := 0
	end := len(logLines)
	if len(logLines) > visible {
		start = clamp(m.logsScroll, 0, max(0, len(logLines)-visible))
		end = clamp(start+visible, 0, len(logLines))
	}

	for _, line := range logLines[start:end] {
		lines = append(lines, truncateWithEllipsis(line, width))
	}
	if len(logLines) > visible {
		lines = append(lines, "")
		lines = append(lines, warningStyle.Render(
			"showing "+truncateWithEllipsis(strconv.Itoa(start+1)+"-"+strconv.Itoa(end)+" of "+strconv.Itoa(len(logLines)), width-12),
		))
	}
	lines = append(lines, "")
	lines = append(lines, warningStyle.Render("j/k move • a attach • d review • x actions • r refresh • b back • q quit"))
	return strings.Join(lines, "\n")
}

func logLines(content string) []string {
	return contentLines(content, "(no log output yet)")
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

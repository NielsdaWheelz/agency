package cobra

import (
	"strconv"
	"strings"
	"testing"

	spf13cobra "github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func TestCompletionStaticRunnerModeFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "agent start",
			args: []string{"agent", "start", "--mode", ""},
		},
		{
			name: "task start",
			args: []string{"task", "start", "demo", "--mode", ""},
		},
		{
			name: "task retry",
			args: []string{"task", "task-1", "retry", "--mode", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir, configDir := setIsolatedCompletionEnv(t)

			stdout, _, err := executeCmd(append([]string{"__complete"}, tt.args...)...)
			require.NoError(t, err, "expected static mode completion to succeed")
			assertCompletionValues(t, stdout, string(store.RunnerModeHeadless), string(store.RunnerModeHeaded))
			assertNoFileCompletionDirective(t, stdout)
			assert.NoDirExists(t, dataDir)
			assert.NoDirExists(t, configDir)
		})
	}
}

func TestCompletionStaticRunnerFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "agent start",
			args: []string{"agent", "start", "--runner", ""},
		},
		{
			name: "task start",
			args: []string{"task", "start", "demo", "--runner", ""},
		},
		{
			name: "task retry",
			args: []string{"task", "task-1", "retry", "--runner", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir, configDir := setIsolatedCompletionEnv(t)

			stdout, _, err := executeCmd(append([]string{"__complete"}, tt.args...)...)
			require.NoError(t, err, "expected static runner completion to succeed")
			assertCompletionValues(t, stdout, runners.CanonicalIDs()...)
			assertNoFileCompletionDirective(t, stdout)
			assert.NoDirExists(t, dataDir)
			assert.NoDirExists(t, configDir)
		})
	}
}

func TestCompletionStaticLogKindFlag(t *testing.T) {
	dataDir, configDir := setIsolatedCompletionEnv(t)

	stdout, _, err := executeCmd("__complete", "agent", "inv-1", "history", "logs", "--kind", "")
	require.NoError(t, err, "expected static log kind completion to succeed")
	assertCompletionValues(t, stdout, daemon.InvocationLogKinds()...)
	assertNoFileCompletionDirective(t, stdout)
	assert.NoDirExists(t, dataDir)
	assert.NoDirExists(t, configDir)
}

func assertCompletionValues(t *testing.T, stdout string, expected ...string) {
	t.Helper()

	assert.ElementsMatch(t, expected, completionValues(stdout))
}

func assertNoFileCompletionDirective(t *testing.T, stdout string) {
	t.Helper()

	directive := ":" + strconv.Itoa(int(spf13cobra.ShellCompDirectiveNoFileComp))
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == directive {
			return
		}
	}
	assert.Failf(t, "missing no-file completion directive", "stdout=%q", stdout)
}

func completionValues(stdout string) []string {
	var values []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		values = append(values, line)
	}
	return values
}

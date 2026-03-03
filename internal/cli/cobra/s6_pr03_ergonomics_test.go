package cobra

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS6PR03_ProgressionAndNavigationShortAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		newCmd    func() *cobra.Command
		flagName  string
		shorthand string
	}{
		{name: "agent review repo", newCmd: newAgentReviewCmd, flagName: "repo", shorthand: "r"},
		{name: "agent review json", newCmd: newAgentReviewCmd, flagName: "json", shorthand: "j"},
		{name: "agent path repo", newCmd: newAgentPathCmd, flagName: "repo", shorthand: "r"},
		{name: "agent open repo", newCmd: newAgentOpenCmd, flagName: "repo", shorthand: "r"},
		{name: "agent attach repo", newCmd: newAgentAttachCmd, flagName: "repo", shorthand: "r"},
		{name: "agent enter repo", newCmd: newAgentEnterCmd, flagName: "repo", shorthand: "r"},
		{name: "path repo", newCmd: newPathCmd, flagName: "repo", shorthand: "r"},
		{name: "open repo", newCmd: newOpenCmd, flagName: "repo", shorthand: "r"},
		{name: "attach repo", newCmd: newAttachCmd, flagName: "repo", shorthand: "r"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			flag := tt.newCmd().Flags().Lookup(tt.flagName)
			require.NotNil(t, flag, "flag %q must exist", tt.flagName)
			assert.Equal(t, tt.shorthand, flag.Shorthand)
		})
	}
}

func TestS6PR03_ProgressionAndNavigationAliasParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		newCmd func() *cobra.Command
		args   []string
	}{
		{name: "agent review parses -r and -j", newCmd: newAgentReviewCmd, args: []string{"-r", "repo-1", "-j"}},
		{name: "agent path parses -r", newCmd: newAgentPathCmd, args: []string{"-r", "repo-1"}},
		{name: "agent open parses -r", newCmd: newAgentOpenCmd, args: []string{"-r", "repo-1"}},
		{name: "agent attach parses -r", newCmd: newAgentAttachCmd, args: []string{"-r", "repo-1"}},
		{name: "agent enter parses -r", newCmd: newAgentEnterCmd, args: []string{"-r", "repo-1"}},
		{name: "path parses -r", newCmd: newPathCmd, args: []string{"-r", "repo-1"}},
		{name: "open parses -r", newCmd: newOpenCmd, args: []string{"-r", "repo-1"}},
		{name: "attach parses -r", newCmd: newAttachCmd, args: []string{"-r", "repo-1"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.newCmd().ParseFlags(tt.args)
			require.NoError(t, err)
		})
	}
}

func TestS6PR03_HelpShowsShortAliases(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		containsParts []string
	}{
		{
			name:          "agent review help",
			args:          []string{"agent", "review", "--help"},
			containsParts: []string{"-r, --repo", "-j, --json"},
		},
		{
			name:          "agent path help",
			args:          []string{"agent", "path", "--help"},
			containsParts: []string{"-r, --repo"},
		},
		{
			name:          "agent open help",
			args:          []string{"agent", "open", "--help"},
			containsParts: []string{"-r, --repo"},
		},
		{
			name:          "agent attach help",
			args:          []string{"agent", "attach", "--help"},
			containsParts: []string{"-r, --repo"},
		},
		{
			name:          "agent enter help",
			args:          []string{"agent", "enter", "--help"},
			containsParts: []string{"-r, --repo"},
		},
		{
			name:          "compat path help",
			args:          []string{"path", "--help"},
			containsParts: []string{"-r, --repo"},
		},
		{
			name:          "compat open help",
			args:          []string{"open", "--help"},
			containsParts: []string{"-r, --repo"},
		},
		{
			name:          "compat attach help",
			args:          []string{"attach", "--help"},
			containsParts: []string{"-r, --repo"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := executeCmd(tt.args...)
			require.NoError(t, err)
			for _, part := range tt.containsParts {
				assert.Contains(t, stdout, part)
			}
		})
	}
}

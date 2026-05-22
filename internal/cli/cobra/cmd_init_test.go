package cobra

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitCmd_HelpSurfacesInitFlags(t *testing.T) {
	stdout, _, err := executeCmd("init", "--help")
	require.NoError(t, err)
	for _, want := range []string{"--path", "--repo-config", "--force", "--no-gitignore"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("init help missing %s:\n%s", want, stdout)
		}
	}
}

package cobra

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDoctorCmd_HelpSurfacesDoctorFlags(t *testing.T) {
	stdout, _, err := executeCmd("doctor", "--help")
	require.NoError(t, err)
	if !strings.Contains(stdout, "--path") {
		t.Fatalf("doctor help missing --path:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--agency-config") {
		t.Fatalf("doctor help missing --agency-config:\n%s", stdout)
	}
}

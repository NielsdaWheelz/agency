package commands

import (
	"fmt"
	"os"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestMain(m *testing.M) {
	if err := testutil.UnsetGitEnv(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

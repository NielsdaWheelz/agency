package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvAssignments_Valid(t *testing.T) {
	t.Parallel()

	env, err := parseEnvAssignments([]string{
		"FOO=bar",
		"EMPTY=",
		"FOO=baz",
	})
	require.NoError(t, err)
	require.NotNil(t, env)
	assert.Equal(t, "baz", env["FOO"])
	assert.Equal(t, "", env["EMPTY"])
}

func TestParseEnvAssignments_Invalid(t *testing.T) {
	t.Parallel()

	_, err := parseEnvAssignments([]string{"NOT_A_PAIR"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected KEY=VALUE")
}

package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeActivityKind_FollowupAliases(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "follow-up", NormalizeActivityKind("followup"))
	assert.Equal(t, "follow-up", NormalizeActivityKind("follow_up"))
	assert.Equal(t, "follow-up", NormalizeActivityKind("follow-up"))
}

func TestFormatActivityLabel_UsesFallbackSummary(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "[assistant] assistant turn", FormatActivityLabel("assistant", ""))
	assert.Equal(t, "[prompt] prompt", FormatActivityLabel("prompt", ""))
	assert.Equal(t, "[follow-up] follow-up prompt", FormatActivityLabel("followup", ""))
}

func TestFormatTurnExtras(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", FormatTurnExtras(0, 0, false))
	assert.Equal(t, " (tools=2)", FormatTurnExtras(2, 0, false))
	assert.Equal(t, " (checkpoint=3)", FormatTurnExtras(0, 3, true))
	assert.Equal(t, " (tools=1, checkpoint=3)", FormatTurnExtras(1, 3, true))
}

func TestFormatToolCallSummary(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "▶ tool", FormatToolCallSummary("", "", false, 0))
	assert.Equal(t, "▶ Read main.go", FormatToolCallSummary("Read", "main.go", false, 0))
	assert.Equal(t, "▶ Bash make test (exit=1)", FormatToolCallSummary("Bash", "make test", true, 1))
}

package daemon

import (
	"testing"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/stretchr/testify/assert"
)

// TestMutatingToolMaps_InSync verifies that the MutatingTools map in the
// checkpoint package and the MutatingStreamTools map in the stream package
// contain the same entries. These are intentionally duplicated to avoid a
// circular import, but must stay in sync.
func TestMutatingToolMaps_InSync(t *testing.T) {
	t.Parallel()

	for tool := range checkpoint.MutatingTools {
		assert.True(t, stream.MutatingStreamTools[tool],
			"checkpoint.MutatingTools has %q but stream.MutatingStreamTools does not", tool)
	}

	for tool := range stream.MutatingStreamTools {
		assert.True(t, checkpoint.MutatingTools[tool],
			"stream.MutatingStreamTools has %q but checkpoint.MutatingTools does not", tool)
	}
}

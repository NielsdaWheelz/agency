package commands

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmitOpenOnCreateStatus_Success(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	emitOpenOnCreateStatus(&stdout, &stderr, nil)

	assert.Equal(t, "open_status: opened\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestEmitOpenOnCreateStatus_Failure(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	emitOpenOnCreateStatus(&stdout, &stderr, errors.New("editor exited with code 17"))

	assert.Equal(t, "open_status: failed\n", stdout.String())
	assert.Contains(t, stderr.String(), "warning: workspace created but open dispatch failed: editor exited with code 17")
}

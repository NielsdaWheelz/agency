package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellEscapePosix_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"simple", "abc", "'abc'"},
		{"single quote", "a'b", "'a'\"'\"'b'"},
		{"empty string", "", "''"},
		{"spaces", "a b c", "'a b c'"},
		{"path with spaces", "/tmp/a b", "'/tmp/a b'"},
		{"double quotes", `a"b`, `'a"b'`},
		{"backslash", `a\b`, `'a\b'`},
		{"dollar sign", "a$b", "'a$b'"},
		{"backticks", "a`b", "'a`b'"},
		{"multiple single quotes", "a''b", "'a'\"'\"''\"'\"'b'"},
		{"newline", "a\nb", "'a\nb'"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ShellEscapePosix(tt.input)
			assert.Equal(t, tt.expect, got)
		})
	}
}

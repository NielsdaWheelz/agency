package render

import "testing"

func TestNormalizeActivityKind_FollowupAliases(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"followup", "follow_up", "follow-up"} {
		if got := NormalizeActivityKind(input); got != "follow-up" {
			t.Fatalf("NormalizeActivityKind(%q) = %q, want %q", input, got, "follow-up")
		}
	}
}

func TestFormatToolCallSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		toolName string
		input    string
		hasExit  bool
		exitCode int
		want     string
	}{
		{name: "empty", want: "▶ tool"},
		{name: "read", toolName: "Read", input: "main.go", want: "▶ Read main.go"},
		{name: "exit", toolName: "Bash", input: "make test", hasExit: true, exitCode: 1, want: "▶ Bash make test (exit=1)"},
	}
	for _, tt := range tests {
		if got := FormatToolCallSummary(tt.toolName, tt.input, tt.hasExit, tt.exitCode); got != tt.want {
			t.Fatalf("FormatToolCallSummary(%q, %q, %v, %d) = %q, want %q", tt.toolName, tt.input, tt.hasExit, tt.exitCode, got, tt.want)
		}
	}
}

func TestFormatActivityWithExtras(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind       string
		summary    string
		toolCount  int
		checkpoint int
		hasCP      bool
		want       string
	}{
		{kind: "assistant", want: "[assistant] assistant turn"},
		{kind: "prompt", want: "[prompt] prompt"},
		{kind: "followup", want: "[follow-up] follow-up prompt"},
		{kind: "assistant", summary: "fixed tests", toolCount: 2, checkpoint: 7, hasCP: true, want: "[assistant] fixed tests (tools=2, checkpoint=7)"},
	}
	for _, tt := range tests {
		if got := FormatActivityWithExtras(tt.kind, tt.summary, tt.toolCount, tt.checkpoint, tt.hasCP); got != tt.want {
			t.Fatalf("FormatActivityWithExtras(%q, %q, %d, %d, %v) = %q, want %q", tt.kind, tt.summary, tt.toolCount, tt.checkpoint, tt.hasCP, got, tt.want)
		}
	}
}

func TestFormatChangedPathSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		paths     []string
		total     int
		truncated bool
		want      string
	}{
		{},
		{paths: []string{"a.go", "b.go"}, total: 2, want: "a.go, b.go"},
		{paths: []string{"a.go", "b.go"}, total: 5, truncated: true, want: "a.go, b.go, ... (+3 more)"},
	}
	for _, tt := range tests {
		if got := FormatChangedPathSummary(tt.paths, tt.total, tt.truncated); got != tt.want {
			t.Fatalf("FormatChangedPathSummary(%v, %d, %v) = %q, want %q", tt.paths, tt.total, tt.truncated, got, tt.want)
		}
	}
}

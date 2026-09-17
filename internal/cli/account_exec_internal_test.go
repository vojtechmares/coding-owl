package cli

import (
	"slices"
	"testing"
)

func TestToolCommandTakesOffTheOptionTerminator(t *testing.T) {
	for _, c := range []struct {
		name  string
		given []string
		want  []string
	}{
		{
			// The ordinary spelling. The terminator is habit, and this
			// command stops reading options at the Account's name anyway, so
			// it would otherwise reach the tool as its own first argument -
			// where `-- --version` is not `--version`.
			name:  "a terminator the user wrote out of habit",
			given: []string{"--", "--version"},
			want:  []string{"--version"},
		},
		{
			name:  "no terminator at all",
			given: []string{"mcp", "list"},
			want:  []string{"mcp", "list"},
		},
		{
			// Only ever one, so somebody who genuinely means to pass a `--`
			// through to the tool writes two and gets one.
			name:  "a terminator the user means the tool to see",
			given: []string{"--", "--", "literal"},
			want:  []string{"--", "literal"},
		},
		{
			// A dash-leading argument that is not the terminator is the
			// tool's, and is left exactly where it was.
			name:  "flags that belong to the tool",
			given: []string{"mcp", "add", "-s", "user", "sentry"},
			want:  []string{"mcp", "add", "-s", "user", "sentry"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := toolCommand(c.given); !slices.Equal(got, c.want) {
				t.Errorf("toolCommand(%v) = %v, want %v", c.given, got, c.want)
			}
		})
	}
}

package cli

import (
	"strings"
	"testing"
)

// TestConfigLine covers the wording of `owl project show`'s config line
// without a daemon. The daemon hands over a path and nothing else, so this is
// where the sentence a user reads is decided (issue #134).
func TestConfigLine(t *testing.T) {
	for _, c := range []struct {
		name          string
		source, stray string
		want          string
	}{
		{
			name:   "a configuration that was found is named on its own",
			source: "main:.coding-owl.yaml",
			want:   "main:.coding-owl.yaml",
		},
		{
			name:   "a found configuration silences a near miss",
			source: "/home/x/.config/coding-owl/api/config.yaml",
			stray:  "/home/x/.config/coding-owl/api/.coding-owl.yaml",
			want:   "/home/x/.config/coding-owl/api/config.yaml",
		},
		{
			name: "nothing found and nothing stray is the bare (none)",
			want: noConfigFound,
		},
		{
			name:  "a near miss names the file, its directory and the name expected",
			stray: "/home/x/.config/coding-owl/api/.coding-owl.yaml",
			want:  "(none) - found .coding-owl.yaml in /home/x/.config/coding-owl/api, but this location expects config.yaml",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := configLine(c.source, c.stray); got != c.want {
				t.Errorf("configLine(%q, %q) = %q, want %q", c.source, c.stray, got, c.want)
			}
		})
	}
}

// TestConfigLineMakesAHostileNameVisible guards the one part of the line that
// is not Owl's own text: the name comes from whatever happens to be in the
// user's configuration directory, so it reaches the terminal as bytes, never
// as an instruction to it.
func TestConfigLineMakesAHostileNameVisible(t *testing.T) {
	got := configLine("", "/home/x/.config/coding-owl/api/\x1b]0;pwned\aowl.yaml")

	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("configLine kept a raw escape byte: %q", got)
	}
	if !strings.Contains(got, `\x1b`) {
		t.Errorf("configLine = %q, want the escape shown as the bytes it is", got)
	}
}

// TestConfigLineStaysOneLine is the same guard for the byte that would not
// rewrite the terminal but forge Owl's own output: a newline is legal in a
// filename, and a config line that ended early could be followed by a
// convincing account line the user would read as Owl's.
func TestConfigLineStaysOneLine(t *testing.T) {
	got := configLine("", "/home/x/.config/coding-owl/api/\naccount: attacker.yaml")

	if strings.Contains(got, "\n") {
		t.Errorf("configLine produced more than one line: %q", got)
	}
	if !strings.Contains(got, `\x0a`) {
		t.Errorf("configLine = %q, want the newline shown as the bytes it is", got)
	}
}

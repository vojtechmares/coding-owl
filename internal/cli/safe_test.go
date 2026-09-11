package cli

import "testing"

func TestTerminalSafeShowsAnEscapeAsTheBytesItIs(t *testing.T) {
	// What a filename or an issue title can carry: an escape that retitles
	// the terminal window, and one that clears the line.
	got := terminalSafe("work.txt\x1b]0;pwned\a and \x1b[2K")

	if got != `work.txt\x1b]0;pwned\x07 and \x1b[2K` {
		t.Errorf("terminalSafe = %q, want the escapes made visible", got)
	}
}

func TestTerminalSafeLeavesTextAndDocumentsAlone(t *testing.T) {
	for _, s := range []string{"", "rebasing onto main conflicts in one.txt, two.txt", "a plan\n\twith a tab\n", "žluťoučký kůň"} {
		if got := terminalSafe(s); got != s {
			t.Errorf("terminalSafe(%q) = %q, want it unchanged", s, got)
		}
	}
}

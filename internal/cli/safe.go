package cli

import (
	"fmt"
	"strings"
	"unicode"
)

// terminalSafe returns s with every control character made visible, so that
// what the daemon reports reaches the terminal as text and never as an
// instruction to it. A block reason names files git could not replay, a
// prompt is whatever a Source carried, and a filename or an issue title can
// hold an escape that retitles the window or rewrites the line: they are
// shown as the bytes they are. A newline and a tab stay as they are, because
// a document is printed with them on purpose.
func terminalSafe(s string) string { return escapeControls(s, isEscape) }

// terminalSafeLine is terminalSafe for a value printed inside a single line of
// output, where a newline is not a document's own but a way to forge further
// lines of Owl's. A filename is whatever somebody put in a directory, and on
// both macOS and Linux a newline is a legal byte in one, so a name could
// otherwise end a line early and write a convincing `account:` of its own.
// Here every control character is made visible, whitespace included.
func terminalSafeLine(s string) string { return escapeControls(s, unicode.IsControl) }

// escapeControls returns s with every rune escape reports made visible as the
// bytes it is.
func escapeControls(s string, escape func(rune) bool) string {
	if !strings.ContainsFunc(s, escape) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if escape(r) {
			_, _ = fmt.Fprintf(&b, "\\x%02x", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isEscape is a control character that is not whitespace a document prints
// with on purpose.
func isEscape(r rune) bool {
	return unicode.IsControl(r) && r != '\n' && r != '\t'
}

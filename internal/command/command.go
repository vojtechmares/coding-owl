// Package command is the gate the chat's commands pass through (ADR-0022).
// The daemon parses a command line into argv itself and calls execve directly,
// so there is no shell: `;`, `&&`, `>` and backticks arrive as ordinary
// arguments and fail argument validation. The gate fails closed - a program,
// a subcommand, an option or an operand it does not know is denied.
package command

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// DeniedError is a command the gate will not let through. It is what the model
// is told, so it says which part was refused and what is allowed instead: a
// refusal the model cannot act on is a refusal it will repeat.
type DeniedError struct{ Err error }

func (e *DeniedError) Error() string { return e.Err.Error() }
func (e *DeniedError) Unwrap() error { return e.Err }

func denied(format string, a ...any) error {
	return &DeniedError{Err: fmt.Errorf(format, a...)}
}

// metacharacters are what a shell would have read as something other than
// text. Nothing here interprets them, so an argument carrying one is an
// argument that was written for a shell - and this is not one.
const metacharacters = ";&|<>`$(){}[]*?!#\\'\"" + "\n\r\t"

// rule is what one program may be asked to do.
type rule struct {
	// subcommands, when set, is what the first argument must be. Everything
	// else it might take is denied, which is what stops a program from being
	// two programs (ADR-0022).
	subcommands []string
	// options are the long options it permits, written exactly.
	options []string
	// prefixes are the long options it permits with a value after them.
	prefixes []string
	// letters are the short options it permits, which may be written together.
	letters string
}

// allowed is every program the chat may run, and what each may be asked to do.
// Widening this is a deliberate act, which is the intended asymmetry
// (ADR-0022).
var allowed = map[string]rule{
	"git": {
		subcommands: []string{"status", "log", "diff", "show", "branch"},
		options: []string{
			"--", "--short", "--branch", "--porcelain", "--oneline", "--stat",
			"--numstat", "--shortstat", "--name-only", "--name-status", "--graph",
			"--decorate", "--no-decorate", "--abbrev-commit", "--no-color",
			"--all", "--patch", "--no-patch", "--summary", "--list", "--merged",
			"--no-merged", "--reverse", "--first-parent", "--follow", "--cached",
			"--staged", "--word-diff", "--no-renames", "--find-renames",
		},
		prefixes: []string{
			"--max-count=", "--skip=", "--since=", "--until=", "--before=",
			"--after=", "--author=", "--committer=", "--grep=", "--pretty=",
			"--format=", "--date=", "--unified=", "--contains=", "--stat=",
			"--color=",
		},
		letters: "nspv",
	},
	"cat": {
		options: []string{"--", "--number", "--squeeze-blank"},
		letters: "nbs",
	},
	"ls": {
		options: []string{"--", "--all", "--almost-all", "--human-readable", "--recursive", "--reverse"},
		letters: "laAhRrt1S",
	},
	"head": {
		options:  []string{"--"},
		prefixes: []string{"--lines=", "--bytes="},
		letters:  "nc",
	},
	"tail": {
		options:  []string{"--"},
		prefixes: []string{"--lines=", "--bytes="},
		letters:  "nc",
	},
	"wc": {
		options: []string{"--", "--lines", "--words", "--bytes", "--chars"},
		letters: "lwcm",
	},
	"grep": {
		options: []string{
			"--", "--line-number", "--recursive", "--ignore-case", "--count",
			"--files-with-matches", "--files-without-match", "--word-regexp",
			"--invert-match", "--fixed-strings", "--extended-regexp",
			"--no-messages", "--with-filename", "--no-filename",
		},
		prefixes: []string{
			"--include=", "--exclude=", "--exclude-dir=", "--context=",
			"--after-context=", "--before-context=", "--max-count=", "--regexp=",
		},
		letters: "nirRlLcwxvFEHs",
	},
}

// Programs is every program the chat may run, in the order a refusal names
// them.
func Programs() []string {
	out := make([]string, 0, len(allowed))
	for name := range allowed {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Parse splits a command line into argv the way a person means it: words, with
// quotes grouping them. Nothing is expanded and no operator is recognised, so
// what a shell would have acted on arrives as an argument (ADR-0022).
func Parse(line string) ([]string, error) {
	var (
		argv    []string
		word    strings.Builder
		started bool
		quote   rune
	)
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			word.WriteRune(r)
		case r == '\'' || r == '"':
			quote, started = r, true
		case unicode.IsSpace(r):
			if started {
				argv = append(argv, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, denied("the command has a %c that is never closed", quote)
	}
	if started {
		argv = append(argv, word.String())
	}
	if len(argv) == 0 {
		return nil, denied("there is no command to run")
	}
	return argv, nil
}

// Check reports whether argv is a command the chat may run. Being denied is an
// answer the model can use, so it says which part was refused.
func Check(argv []string) error {
	if len(argv) == 0 {
		return denied("there is no command to run")
	}
	program := argv[0]
	r, ok := allowed[program]
	if !ok {
		return denied("%s is not a program Owl runs; it runs %s",
			shown(program), strings.Join(Programs(), ", "))
	}
	rest := argv[1:]
	if len(r.subcommands) > 0 {
		if len(rest) == 0 {
			return denied("%s needs one of %s after it", program, strings.Join(r.subcommands, ", "))
		}
		if !slices.Contains(r.subcommands, rest[0]) {
			return denied("%s %s is not something Owl runs; it runs %s %s",
				program, shown(rest[0]), program, strings.Join(r.subcommands, ", "))
		}
		rest = rest[1:]
	}
	for _, arg := range rest {
		if err := checkArgument(program, r, arg); err != nil {
			return err
		}
	}
	return nil
}

// checkArgument is one argument against the rules of the program it was given
// to. Everything the rules do not name is denied.
func checkArgument(program string, r rule, arg string) error {
	if arg == "" {
		return denied("%s was given an empty argument", program)
	}
	// A shell would have read this as something other than text. Nothing here
	// does, so it is an argument - and not one any of these programs take.
	if at := strings.IndexAny(arg, metacharacters); at >= 0 {
		return denied("the argument %s carries %q, which no shell is here to read: "+
			"Owl runs the command itself, so it would be part of the argument",
			shown(arg), string(arg[at]))
	}
	if strings.HasPrefix(arg, "~") {
		return denied("the argument %s starts with ~, which nothing here expands", shown(arg))
	}
	switch {
	case arg == "--":
		return nil
	case strings.HasPrefix(arg, "--"):
		if slices.Contains(r.options, arg) {
			return nil
		}
		for _, prefix := range r.prefixes {
			if strings.HasPrefix(arg, prefix) && len(arg) > len(prefix) {
				return nil
			}
		}
		return denied("%s is not an option Owl gives %s", shown(arg), program)
	case strings.HasPrefix(arg, "-") && len(arg) > 1:
		for _, letter := range arg[1:] {
			if !strings.ContainsRune(r.letters, letter) {
				return denied("%s is not an option Owl gives %s", shown(arg), program)
			}
		}
		return nil
	}
	return checkOperand(arg)
}

// checkOperand is a path or a revision the command is to work on. It stays
// inside the working directory, or confining the working directory to a
// Project would buy nothing (ADR-0022).
func checkOperand(arg string) error {
	if strings.HasPrefix(arg, "/") {
		return denied("the argument %s is an absolute path, which leaves the working directory", shown(arg))
	}
	for part := range strings.SplitSeq(arg, "/") {
		if part == ".." {
			return denied("the argument %s goes up out of the working directory", shown(arg))
		}
	}
	return nil
}

// shown is an argument as a refusal quotes it, so that a refusal about an
// empty or odd-looking argument still reads as a sentence.
func shown(arg string) string { return fmt.Sprintf("%q", arg) }

package command_test

import (
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/command"
)

// A command line is words, and quotes group them. Nothing else is interpreted,
// which is what makes a metacharacter an argument rather than an operator
// (ADR-0022).
func TestParseSplitsIntoWordsWithoutAShell(t *testing.T) {
	for _, c := range []struct {
		line string
		want []string
	}{
		{"git status", []string{"git", "status"}},
		{"  git   status  --short ", []string{"git", "status", "--short"}},
		{`grep -n "two words" file.go`, []string{"grep", "-n", "two words", "file.go"}},
		{`grep -n 'two words' file.go`, []string{"grep", "-n", "two words", "file.go"}},
		{`git log --grep="a b"`, []string{"git", "log", "--grep=a b"}},
		// The whole point: an operator is a word.
		{"git status ; rm -rf /", []string{"git", "status", ";", "rm", "-rf", "/"}},
		{"ls > out", []string{"ls", ">", "out"}},
		{"cat a && b", []string{"cat", "a", "&&", "b"}},
	} {
		got, err := command.Parse(c.line)
		if err != nil {
			t.Errorf("Parse(%q) = %v", c.line, err)
			continue
		}
		if !same(got, c.want) {
			t.Errorf("Parse(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// A line nothing can be made of is refused rather than guessed at.
func TestParseRefusesWhatItCannotRead(t *testing.T) {
	for _, line := range []string{"", "   ", `cat "unclosed`, "cat 'unclosed"} {
		if got, err := command.Parse(line); err == nil {
			t.Errorf("Parse(%q) = %q, want it refused", line, got)
		}
	}
}

// The allowlist is the whole of what the chat may run: anything unmatched is
// denied, and the refusal says what it does run (ADR-0022).
func TestCheckRefusesAProgramThatIsNotAllowed(t *testing.T) {
	for _, argv := range [][]string{
		{"rm", "-rf", "."},
		{"sh", "-c", "id"},
		{"/bin/ls"},
		{"./git", "status"},
		{"GIT", "status"},
	} {
		err := command.Check(argv)
		if err == nil {
			t.Errorf("Check(%q) = nil, want it denied", argv)
			continue
		}
		if !strings.Contains(err.Error(), "git") {
			t.Errorf("Check(%q) = %q, want the refusal to say what owl runs", argv, err)
		}
	}
}

// Table-driven, because the gate is a table: what it lets through and what it
// stops are the same list read twice.
func TestCheckAllowsWhatTheChatMayRun(t *testing.T) {
	for _, line := range []string{
		"git status",
		"git status --short",
		"git log --oneline -n 20",
		"git log --oneline",
		"git diff --stat",
		"git show HEAD",
		"git branch",
		"git branch --list",
		"cat README.md",
		"cat -n internal/chat/chat.go",
		"grep -rn owl internal",
		"ls",
		"ls -la internal",
		"head -n 40 README.md",
		"tail -n 40 README.md",
		"wc -l README.md",
	} {
		argv, err := command.Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", line, err)
		}
		if err := command.Check(argv); err != nil {
			t.Errorf("Check(%q) = %v, want it allowed", line, err)
		}
	}
}

// git's writing subcommands are named in ADR-0022 as denied, and everything it
// does not permit is denied with them.
func TestCheckRefusesTheGitSubcommandsThatWrite(t *testing.T) {
	for _, line := range []string{
		"git checkout main",
		"git reset --hard",
		"git clean -fd",
		"git push",
		"git commit -m x",
		"git rebase main",
		"git",
	} {
		argv, err := command.Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", line, err)
		}
		if err := command.Check(argv); err == nil {
			t.Errorf("Check(%q) = nil, want it denied", line)
		}
	}
}

// A metacharacter that arrived as a literal argument fails argument
// validation, which is why nothing has to detect one (ADR-0022).
func TestCheckRefusesAMetacharacterThatArrivedAsAnArgument(t *testing.T) {
	for _, line := range []string{
		"git status ; rm -rf /",
		"cat a && b",
		"ls > out",
		"ls | wc",
		"cat `id`",
		"cat $HOME/.ssh/id_rsa",
		"ls *.go",
		"cat a?b",
		"cat ~/.netrc",
		"grep -E a|b .",
	} {
		argv, err := command.Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", line, err)
		}
		if err := command.Check(argv); err == nil {
			t.Errorf("Check(%q) = nil, want it denied", line)
		}
	}
}

// An operand is a path inside the working directory. One that leaves it is
// denied, or confining the working directory would buy nothing.
func TestCheckRefusesAnOperandThatLeavesTheWorkingDirectory(t *testing.T) {
	for _, line := range []string{
		"cat /etc/hosts",
		"cat ../secret.txt",
		"cat a/../../secret.txt",
		"ls /",
		"head -n 1 /etc/passwd",
		"grep -rn owl ../..",
	} {
		argv, err := command.Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", line, err)
		}
		err = command.Check(argv)
		if err == nil {
			t.Errorf("Check(%q) = nil, want it denied", line)
			continue
		}
		if !strings.Contains(err.Error(), "working directory") {
			t.Errorf("Check(%q) = %q, want it to say the argument leaves the working directory", line, err)
		}
	}
}

// An option the command's rules do not name is denied: the gate fails closed,
// so an unanticipated case is refused rather than passed (ADR-0022).
func TestCheckRefusesAnOptionItDoesNotKnow(t *testing.T) {
	for _, line := range []string{
		"git log --output=/tmp/x",
		"git diff --ext-diff",
		"git log --textconv",
		"tail -f README.md",
		"ls --colour-me-surprised",
		"grep --devices=read .",
	} {
		argv, err := command.Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", line, err)
		}
		if err := command.Check(argv); err == nil {
			t.Errorf("Check(%q) = nil, want it denied", line)
		}
	}
}

func same(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for at := range got {
		if got[at] != want[at] {
			return false
		}
	}
	return true
}

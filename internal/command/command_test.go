package command_test

import (
	"os"
	"path/filepath"
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

// The literal path rules cannot see a link: a Project holding one to somewhere
// else would otherwise be a way out of the directory the command was confined
// to (ADR-0022).
func TestCheckInRefusesAnOperandThatLinksOutOfTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.txt", "the password is hunter2\n")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret.txt")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "elsewhere")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	write(t, root, "owl.txt", "owl was here\n")

	for _, line := range []string{"cat secret.txt", "ls elsewhere", "grep -rn owl elsewhere"} {
		argv, err := command.Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", line, err)
		}
		err = command.CheckIn(root, argv)
		if err == nil {
			t.Errorf("CheckIn(%q) = nil, want it denied", line)
			continue
		}
		if !strings.Contains(err.Error(), "outside the working directory") {
			t.Errorf("CheckIn(%q) = %q, want it to say the link leaves the working directory", line, err)
		}
	}
	// What is really in the directory is still allowed, and so is an operand
	// that is not a file at all.
	for _, line := range []string{"cat owl.txt", "git show HEAD", "grep -rn owl ."} {
		argv, err := command.Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", line, err)
		}
		if err := command.CheckIn(root, argv); err != nil {
			t.Errorf("CheckIn(%q) = %v, want it allowed", line, err)
		}
	}
}

// Every character a shell reads as something other than text, written out
// here rather than taken from the constant under test: a set compared with
// itself asserts nothing, and a gate whose premise is that it fails closed
// must not be able to lose half of itself unnoticed (ADR-0022).
var shellReads = []rune{
	';', '&', '|', '<', '>', '`', '$', '(', ')', '{', '}', '[', ']',
	'*', '?', '!', '#', '\\', '\'', '"', '\n', '\r', '\t',
}

func TestCheckRefusesEveryMetacharacterAShellReads(t *testing.T) {
	for _, r := range shellReads {
		if !strings.ContainsRune(command.Metacharacters, r) {
			t.Errorf("the gate does not know %q as something a shell reads", string(r))
		}
	}
	// And what the gate says it knows is refused, so the two lists cannot
	// drift apart in either direction.
	for _, r := range append(shellReads, []rune(command.Metacharacters)...) {
		// Built rather than parsed: a newline or a quote would not survive
		// Parse as one argument, and it is Check that has to refuse it.
		argv := []string{"cat", "own" + string(r) + "ed.txt"}
		if err := command.Check(argv); err == nil {
			t.Errorf("Check(%q) = nil, want the argument carrying %q denied", argv, string(r))
		}
	}
	// And a space is not one of them: two words and one word with a space in
	// it are different commands, but neither is a shell operator.
	if err := command.Check([]string{"cat", "two words.txt"}); err != nil {
		t.Errorf("Check of an argument with a space = %v, want it allowed", err)
	}
}

// -R is GNU grep's --dereference-recursive, which follows every link it meets
// while recursing - and a link met that way is one CheckIn never saw.
func TestCheckRefusesTheGrepThatFollowsLinks(t *testing.T) {
	if err := command.Check([]string{"grep", "-R", "owl", "."}); err == nil {
		t.Error("Check of grep -R = nil, want it denied")
	}
	if err := command.Check([]string{"grep", "--dereference-recursive", "owl", "."}); err == nil {
		t.Error("Check of grep --dereference-recursive = nil, want it denied")
	}
	// Recursing without following is what the chat is for.
	if err := command.Check([]string{"grep", "-rn", "owl", "."}); err != nil {
		t.Errorf("Check of grep -rn = %v, want it allowed", err)
	}
}

// git branch with a name after it creates a branch, which is not looking at
// the repository.
func TestCheckRefusesTheGitBranchThatWrites(t *testing.T) {
	for _, argv := range [][]string{
		{"git", "branch", "owl-new"},
		{"git", "branch", "--list", "owl-new"},
		{"git", "branch", "a", "b"},
	} {
		err := command.Check(argv)
		if err == nil {
			t.Errorf("Check(%q) = nil, want it denied", argv)
			continue
		}
		if !strings.Contains(err.Error(), "change the repository") {
			t.Errorf("Check(%q) = %q, want it to say why a name after it is not a read", argv, err)
		}
	}
	for _, argv := range [][]string{{"git", "branch"}, {"git", "branch", "--list"}, {"git", "branch", "-v"}} {
		if err := command.Check(argv); err != nil {
			t.Errorf("Check(%q) = %v, want it allowed", argv, err)
		}
	}
}

// A repository's .git holds the credential its remote is reached with, which
// is not part of what is in the repository (ADR-0022).
func TestCheckRefusesWhatIsInsideDotGit(t *testing.T) {
	for _, argv := range [][]string{
		{"cat", ".git/config"},
		{"grep", "-rn", "token", ".git"},
		{"ls", "sub/.git/refs"},
		// The filesystem Owl ships for folds case, so these are the same
		// directory and a rule that tells them apart is no rule (ADR-0010).
		{"cat", ".Git/config"},
		{"cat", ".GIT/config"},
		{"ls", "sub/.gIt"},
	} {
		err := command.Check(argv)
		if err == nil {
			t.Errorf("Check(%q) = nil, want it denied", argv)
			continue
		}
		if !strings.Contains(err.Error(), ".git") {
			t.Errorf("Check(%q) = %q, want it to name what was refused", argv, err)
		}
	}
}

// And a recursive grep does not walk into it either, because Owl puts the
// exclusion in itself rather than hoping the model does.
func TestArgvCarriesWhatOwlPutsInItself(t *testing.T) {
	got := command.Argv([]string{"grep", "-rn", "token", "."})

	want := []string{"grep", "--exclude-dir=.git", "-rn", "token", "."}
	if !same(got, want) {
		t.Errorf("Argv = %q, want %q", got, want)
	}
	// What Owl adds still passes the gate it added it to.
	if err := command.Check(got); err != nil {
		t.Errorf("Check of what owl runs = %v, want it allowed", err)
	}
	// A program Owl adds nothing to is left as it is.
	if got := command.Argv([]string{"git", "status"}); !same(got, []string{"git", "status"}) {
		t.Errorf("Argv = %q, want it unchanged", got)
	}
}

// What a person is shown is what will run, and two words are not one word with
// a space in it.
func TestLineQuotesAnArgumentWithASpaceInIt(t *testing.T) {
	if got, want := command.Line([]string{"cat", "two words.txt"}), `cat "two words.txt"`; got != want {
		t.Errorf("Line = %q, want %q", got, want)
	}
	if got, want := command.Line([]string{"git", "status", "--short"}), "git status --short"; got != want {
		t.Errorf("Line = %q, want %q", got, want)
	}
}

// A link inside the repository that points at .git reads as an ordinary path,
// so where an operand really leads is what decides (ADR-0022).
func TestCheckInRefusesAnOperandThatLeadsIntoDotGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	write(t, filepath.Join(root, ".git"), "config", "url = https://owl:hunter2@example.invalid/api.git\n")
	if err := os.Symlink(filepath.Join(root, ".git"), filepath.Join(root, "gitdir")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	for _, line := range []string{"cat gitdir/config", "ls gitdir"} {
		argv, err := command.Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", line, err)
		}
		err = command.CheckIn(root, argv)
		if err == nil {
			t.Errorf("CheckIn(%q) = nil, want it denied", line)
			continue
		}
		if !strings.Contains(err.Error(), ".git") {
			t.Errorf("CheckIn(%q) = %q, want it to say where the link leads", line, err)
		}
	}
}

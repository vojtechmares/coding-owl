package behavior_test

// Behavior tests for issue #111. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-111.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// confirm what was written by reading the Account's own configuration
// directory: the file is the whole mechanism, and the tool reads it there
// (ADR-0037). The file's name, where it belongs and the text a scenario writes
// are declared here and shared with issue112_test.go, the desktop app's
// scenarios for the same feature, so that both surfaces are pinned to one file
// in one place.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// instructionsFile is what the claude-code Driver's tool calls an Account's
// standing instructions, in the Account's configuration directory (ADR-0037).
const instructionsFile = "CLAUDE.md"

// standingInstructions is what a scenario types into the editor. It ends in a
// newline, as a text file does, so what is saved and what is on disk are the
// same bytes.
const standingInstructions = "# Standing instructions\n\nWrite commit messages in the imperative mood.\n"

// instructionsAt is where an Account's standing instructions live.
func instructionsAt(l *layout, name string) string {
	return filepath.Join(accountDir(l, name), instructionsFile)
}

// writeInstructions writes an Account's standing instructions the way a user
// does, by piping text into the command.
func writeInstructions(t *testing.T, l *layout, name, text string) result {
	t.Helper()
	res := runOwlStdin(t, l, text, "account", "instructions", "set", name)
	if res.code != 0 {
		t.Fatalf("owl account instructions set %s exited %d\nstdout:\n%s\nstderr:\n%s",
			name, res.code, res.stdout, res.stderr)
	}
	return res
}

// accountWithInstructions is a daemon, the Account `work`, and the standing
// instructions every scenario below starts from.
func accountWithInstructions(t *testing.T) (*layout, *stub) {
	t.Helper()
	l, s := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	writeInstructions(t, l, "work", standingInstructions)
	return l, s
}

func TestS1InstructionsWrittenForAnAccountAreTheToolsOwnFileInItsDirectory(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	path := instructionsAt(l, "work")

	res := writeInstructions(t, l, "work", standingInstructions)

	if !strings.Contains(res.stdout, path) {
		t.Errorf("owl account instructions set does not say where it put them:\n%s", res.stdout)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the account's standing instructions: %v", err)
	}
	if string(data) != standingInstructions {
		t.Errorf("%s holds %q, want %q", path, data, standingInstructions)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The directory holds a coding tool's whole credential state and is the
	// owner's alone (ADR-0019); what Owl writes into it is too.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("%s is %o, want 600, like everything else in that directory", path, perm)
	}
	shown := mustOwl(t, l, "account", "instructions", "show", "work")
	if !strings.Contains(shown.stderr, path) {
		t.Errorf("owl account instructions show does not say where they came from:\n%s", shown.stderr)
	}
	// Standard output is the document and nothing else, so that
	// `show | set` carries it across intact.
	if shown.stdout != standingInstructions {
		t.Errorf("owl account instructions show printed %q, want what was written: %q",
			shown.stdout, standingInstructions)
	}
}

func TestS2AnAgentForTheAccountIsStartedAgainstADirectoryHoldingThem(t *testing.T) {
	l, s := accountWithInstructions(t)
	accountProject(t, l, "api", "apiVersion: codingowl.dev/v1\naccount: work\n")

	_, job := finishedJob(t, l)

	inv := s.invoked(t)
	dir := inv.Env["CLAUDE_CONFIG_DIR"]
	if dir != accountDir(l, "work") {
		t.Fatalf("the agent ran with CLAUDE_CONFIG_DIR=%q, want the account's own directory %q",
			dir, accountDir(l, "work"))
	}
	// Read out of the directory the Agent was actually pointed at, rather than
	// out of the one the scenario wrote to: that the two are the same is the
	// whole of what this pins.
	data, err := os.ReadFile(filepath.Join(dir, instructionsFile))
	if err != nil {
		t.Fatalf("the agent's configuration directory holds no %s: %v", instructionsFile, err)
	}
	if string(data) != standingInstructions {
		t.Errorf("the %s the agent was run against holds %q, want %q", instructionsFile, data, standingInstructions)
	}
	// The Agent works in the Job's worktree (ADR-0006), so the file is nowhere
	// near it: the configuration directory is where the tool reads it from,
	// which is the whole reason it goes there rather than into the repository.
	if worktree := worktreeDir(l, job); !samePath(inv.Dir, worktree) {
		t.Errorf("the agent ran in %s, want the job's worktree %s", inv.Dir, worktree)
	}
}

func TestS3BlankInputTakesTheInstructionsAway(t *testing.T) {
	l, _ := accountWithInstructions(t)
	path := instructionsAt(l, "work")

	res := writeInstructions(t, l, "work", "  \n")

	for _, want := range []string{"cleared", path} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("owl account instructions set does not say %q for instructions it took away:\n%s",
				want, res.stdout)
		}
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is still there after the instructions were cleared: %v", path, err)
	}
	shown := mustOwl(t, l, "account", "instructions", "show", "work")
	if !strings.Contains(shown.stderr, noInstructions) {
		t.Errorf("owl account instructions show does not say there are none:\n%s", shown.stderr)
	}
	// Nothing on standard output, so `show > file` on an Account with none
	// writes an empty file rather than a sentence about there being none.
	if shown.stdout != "" {
		t.Errorf("owl account instructions show printed %q on standard output, want nothing", shown.stdout)
	}
}

// noInstructions is what owl prints for an Account nobody has written any for.
const noInstructions = "no standing instructions yet"

// maxInstructions is how much of them Owl will keep: they are read into the
// context of every Run on the Account, so they are bounded (ADR-0037).
const maxInstructions = 64 << 10

func TestS4InstructionsPastTheBoundAreRefused(t *testing.T) {
	l, _ := accountWithInstructions(t)
	path := instructionsAt(l, "work")

	res := runOwlStdin(t, l, strings.Repeat("x", maxInstructions+1), "account", "instructions", "set", "work")

	if res.code == 0 {
		t.Fatalf("instructions past the bound were accepted\nstdout:\n%s", res.stdout)
	}
	// Saying they are too long without saying how long they may be leaves the
	// user to guess how much to cut.
	if !strings.Contains(res.stderr, strconv.Itoa(maxInstructions)) {
		t.Errorf("stderr does not say how long they may be:\n%s", res.stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the instructions that were already there: %v", err)
	}
	if string(data) != standingInstructions {
		t.Errorf("%s holds %q, want the instructions that were there before the refused write", path, data)
	}
}

func TestS5ASymlinkInTheFilesPlaceIsRefusedRatherThanWrittenThrough(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	// The user's own instructions, in their own setup, which is what an
	// Account exists to keep separate (ADR-0019).
	usersOwn := filepath.Join(l.home, "the-users-own-CLAUDE.md")
	const theirs = "# The user's own\n"
	if err := os.WriteFile(usersOwn, []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}
	path := instructionsAt(l, "work")
	if err := os.Symlink(usersOwn, path); err != nil {
		t.Fatal(err)
	}

	res := runOwlStdin(t, l, standingInstructions, "account", "instructions", "set", "work")

	if res.code == 0 {
		t.Fatalf("writing through a symlink exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "not a regular file") {
		t.Errorf("stderr does not say what is in the file's place:\n%s", res.stderr)
	}
	data, err := os.ReadFile(usersOwn)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != theirs {
		t.Errorf("the user's own file now holds %q: owl wrote through the link", data)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("the link is gone: %v", err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("%s is %s now, want the link left as it was", path, info.Mode().Type())
	}
}

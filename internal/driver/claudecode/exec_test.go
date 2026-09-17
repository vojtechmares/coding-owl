package claudecode_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/driver/claudecode"
)

func TestExecRunsTheToolsOwnCommandAgainstTheAccount(t *testing.T) {
	path := stubClaude(t, "2.1.267 (Claude Code)")

	inv, err := claudecode.New().Exec(workAccount, "sk-ant-oat01-work", []string{"mcp", "list"})

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if inv.Path != path {
		t.Errorf("Exec runs %s, want the tool at %s", inv.Path, path)
	}
	if !slices.Equal(inv.Args, []string{"mcp", "list"}) {
		t.Errorf("Exec runs it with %v, want exactly what the user asked for", inv.Args)
	}
	env := environment(inv.Env)
	if env["CLAUDE_CONFIG_DIR"] != workAccount {
		t.Errorf("the tool is pointed at %s, want the account's own directory %s",
			env["CLAUDE_CONFIG_DIR"], workAccount)
	}
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != "sk-ant-oat01-work" {
		t.Errorf("the tool draws on %q, want the account's own token", env["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	// It acts on the Account, so it runs where that Account's state is
	// rather than wherever the user happened to type the command.
	if inv.Dir != workAccount {
		t.Errorf("the tool runs in %s, want the account's own directory %s", inv.Dir, workAccount)
	}
}

func TestExecPassesTheUsersArgumentsThroughUntouched(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	// Everything after the account name is the tool's, including the flags:
	// Owl reads none of them and must not reorder or drop any.
	want := []string{"mcp", "add", "--scope", "user", "--transport", "http", "sentry", "https://example.test/mcp"}

	inv, err := claudecode.New().Exec(workAccount, "token", want)

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !slices.Equal(inv.Args, want) {
		t.Errorf("the tool is given %v, want %v", inv.Args, want)
	}
}

func TestExecIsGivenNoneOfTheDaemonsCredentials(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	withDaemonCredentials(t)

	inv, err := claudecode.New().Exec(workAccount, "sk-ant-oat01-work", []string{"mcp", "list"})

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	// An Account draws on its own credential and nothing else, whether the
	// tool is run by a Run or by the person at the keyboard (ADR-0019).
	unsetsDaemonCredentials(t, inv)
}

func TestExecWithoutACommandIsRefused(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	_, err := claudecode.New().Exec(workAccount, "token", nil)

	if err == nil {
		t.Fatal("Exec with no command was accepted, want it refused")
	}
	// The refusal has to show what the command looks like: somebody who
	// forgot the arguments does not want to be told they forgot them.
	if !strings.Contains(err.Error(), "owl account exec") {
		t.Errorf("the refusal says %q, want it to show how the command is written", err)
	}
}

func TestExecWithoutAConfigDirIsRefused(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	if _, err := claudecode.New().Exec("", "token", []string{"mcp", "list"}); err == nil {
		t.Error("Exec with no configuration directory was accepted, want it refused")
	}
}

func TestExecWithNoTokenLeavesTheVariableOutRatherThanEmpty(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	inv, err := claudecode.New().Exec(workAccount, "", []string{"mcp", "list"})

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	// An empty one would read to the tool as an account with no
	// subscription, which is a different thing from not saying.
	if _, set := environment(inv.Env)["CLAUDE_CODE_OAUTH_TOKEN"]; set {
		t.Error("the invocation sets an empty token, want the variable left out entirely")
	}
}

func TestInstructionsFileIsWhatTheToolReads(t *testing.T) {
	if got := claudecode.New().InstructionsFile(); got != "CLAUDE.md" {
		t.Errorf("the driver reads standing instructions from %q, want CLAUDE.md", got)
	}
}

package claudecode_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/driver/claudecode"
)

// stubClaude writes a program named claude that prints version for --version,
// and puts it on PATH for the test.
func stubClaude(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '" + version + "'; exit 0; fi\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

func TestCheckAcceptsASupportedVersion(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	if err := claudecode.New().Check(context.Background()); err != nil {
		t.Errorf("Check on a supported version = %v, want nil", err)
	}
}

func TestCheckRefusesAVersionOutsideTheRange(t *testing.T) {
	for _, version := range []string{"1.9.0 (Claude Code)", "3.0.0 (Claude Code)"} {
		t.Run(version, func(t *testing.T) {
			stubClaude(t, version)

			err := claudecode.New().Check(context.Background())

			if err == nil {
				t.Fatalf("Check on %s = nil, want an error", version)
			}
			number, _, _ := strings.Cut(version, " ")
			if !strings.Contains(err.Error(), number) {
				t.Errorf("error %q does not name the version it found", err)
			}
			if !strings.Contains(err.Error(), claudecode.MinVersion) {
				t.Errorf("error %q does not name the supported range", err)
			}
		})
	}
}

func TestCheckReportsAToolThatIsNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := claudecode.New().Check(context.Background())

	if err == nil {
		t.Fatal("Check with no claude on PATH = nil, want an error")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error %q does not name the program it looked for", err)
	}
}

func TestCommandIsPrintModeWithStructuredOutputAndNoPrompts(t *testing.T) {
	path := stubClaude(t, "2.1.267 (Claude Code)")

	inv, err := claudecode.New().Command(driver.Request{
		Prompt:       "fix the flaky test",
		SystemPrompt: "you are running unattended",
		WorkingDir:   "/worktrees/1",
		ConfigDir:    "/accounts/work",
	})

	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if inv.Path != path {
		t.Errorf("path = %q, want the claude on PATH %q", inv.Path, path)
	}
	if inv.Dir != "/worktrees/1" {
		t.Errorf("dir = %q, want the job's worktree", inv.Dir)
	}
	args := strings.Join(inv.Args, " ")
	for _, want := range []string{
		"--print", "--verbose", "--output-format stream-json",
		"--permission-prompts none", "--append-system-prompt you are running unattended",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q do not contain %q", args, want)
		}
	}
	if strings.Contains(args, "dangerously") {
		t.Errorf("args %q skip permission checks", args)
	}
	if last := inv.Args[len(inv.Args)-1]; last != "fix the flaky test" {
		t.Errorf("the prompt is %q, want it last and whole", last)
	}
	if inv.Args[len(inv.Args)-2] != "--" {
		t.Errorf("args %q do not end options before the prompt, so a prompt starting with a dash would be read as a flag", args)
	}
}

func TestCommandCapsSpendOnlyWhenAsked(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	d := claudecode.New()

	capped, err := d.Command(driver.Request{Prompt: "work", BudgetUSD: 5, ConfigDir: "/accounts/work"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	uncapped, err := d.Command(driver.Request{Prompt: "work", ConfigDir: "/accounts/work"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if got := strings.Join(capped.Args, " "); !strings.Contains(got, "--max-budget-usd 5") {
		t.Errorf("args %q do not cap the spend", got)
	}
	if got := strings.Join(uncapped.Args, " "); strings.Contains(got, "--max-budget-usd") {
		t.Errorf("args %q cap the spend of a run that asked for no cap", got)
	}
}

func TestCommandCarriesTheAccountsDirectoryAndToken(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	inv, err := claudecode.New().Command(driver.Request{
		Prompt: "work", WorkingDir: "/worktrees/1",
		ConfigDir: "/accounts/work", Token: "sk-ant-oat01-one",
	})

	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := map[string]string{
		"CLAUDE_CONFIG_DIR":       "/accounts/work",
		"CLAUDE_CODE_OAUTH_TOKEN": "sk-ant-oat01-one",
	}
	got := environment(inv.Env)
	for name, value := range want {
		if got[name] != value {
			t.Errorf("the agent runs with %s=%q, want %q", name, got[name], value)
		}
	}
	if len(inv.Env) != len(want) {
		t.Errorf("the agent's environment is %v, want only the account's own", inv.Env)
	}
}

func TestCommandRefusesARunWithNoAccount(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	_, err := claudecode.New().Command(driver.Request{Prompt: "work", WorkingDir: "/worktrees/1"})

	if err == nil {
		t.Fatal("Command with no account = nil, want an error: a run would fall back on the user's own setup")
	}
}

func TestSetupTokenRunsAgainstTheAccountsOwnDirectory(t *testing.T) {
	path := stubClaude(t, "2.1.267 (Claude Code)")

	inv, err := claudecode.New().SetupToken("/accounts/work")

	if err != nil {
		t.Fatalf("SetupToken: %v", err)
	}
	if inv.Path != path {
		t.Errorf("path = %q, want the claude on PATH %q", inv.Path, path)
	}
	if strings.Join(inv.Args, " ") != "setup-token" {
		t.Errorf("args = %v, want the tool's token setup", inv.Args)
	}
	got := environment(inv.Env)
	if got["CLAUDE_CONFIG_DIR"] != "/accounts/work" {
		t.Errorf("the setup runs with CLAUDE_CONFIG_DIR=%q, want the account's own directory", got["CLAUDE_CONFIG_DIR"])
	}
	if _, ok := got["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Error("the setup that makes a token was given one")
	}
}

// environment reads an invocation's added environment.
func environment(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		out[name] = value
	}
	return out
}

func TestCommandRefusesAnEmptyPrompt(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	_, err := claudecode.New().Command(driver.Request{})

	if err == nil {
		t.Fatal("Command with no prompt = nil, want an error")
	}
}

func TestCapabilitiesSayWhatClaudeCodeCanDo(t *testing.T) {
	c := claudecode.New().Capabilities()

	if !c.StreamingOutput || !c.BudgetCap || !c.PermissionModes {
		t.Errorf("capabilities = %+v, want streaming, a budget cap and permission modes", c)
	}
	if c.UsageReporting {
		t.Error("capabilities claim usage reporting, which Owl does not read yet")
	}
}

func TestSetupTokenRunsInTheAccountsDirectoryRatherThanWhereverItWasTyped(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	inv, err := claudecode.New().SetupToken("/accounts/work")

	if err != nil {
		t.Fatalf("SetupToken: %v", err)
	}
	// A tool started with no working directory inherits the one the process
	// that started it had, which for owl account add is the user's own
	// checkout (ADR-0006).
	if inv.Dir != "/accounts/work" {
		t.Errorf("the setup runs in %q, want the account's own directory", inv.Dir)
	}
}

func TestSkillsDirIsWhereClaudeCodeReadsSkills(t *testing.T) {
	// Not the plugins directory, which is a different thing (ADR-0033).
	if got := claudecode.New().SkillsDir(); got != ".claude/skills" {
		t.Errorf("SkillsDir = %q, want .claude/skills", got)
	}
}

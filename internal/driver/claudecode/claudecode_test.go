package claudecode_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/driver/claudecode"
)

// workAccount is the configuration directory of the Account these tests run
// on. It is a real directory of this process's own rather than a made-up
// path, because building an Agent seeds the Account's settings file there
// (ADR-0035), and it is removed when the tests are done.
var workAccount string

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "coding-owl-driver-tests")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	workAccount = filepath.Join(root, "accounts", "work")
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

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

// claudeAt writes a program named claude at dir/claude that answers --version
// and otherwise exits 0, without putting it on PATH.
func claudeAt(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '2.1.267 (Claude Code)'; exit 0; fi\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// nowhere gives the test a PATH with no claude on it and a home with nothing
// under .local/bin, so a lookup finds only what the scenario put there.
func nowhere(t *testing.T) (home string) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	home = t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// usesTool checks the Driver's check and command both use the tool at path.
func usesTool(t *testing.T, d *claudecode.Driver, path string) {
	t.Helper()
	if err := d.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	inv, err := d.Command(forAccount(filepath.Join(t.TempDir(), "work")))
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if inv.Path != path {
		t.Errorf("the agent is %q, want %q", inv.Path, path)
	}
}

func TestS1AConfiguredPathIsUsedWithoutConsultingPATH(t *testing.T) {
	nowhere(t)
	configured := claudeAt(t, filepath.Join(t.TempDir(), "tools"))

	usesTool(t, claudecode.NewWithPath(configured), configured)
}

func TestS2WithNothingConfiguredTheToolIsFoundUnderTheHomeDirectorysLocalBin(t *testing.T) {
	home := nowhere(t)
	local := claudeAt(t, filepath.Join(home, ".local", "bin"))

	usesTool(t, claudecode.New(), local)
}

func TestS3AToolThatIsNowhereIsRefusedNamingBothPlacesToPutIt(t *testing.T) {
	nowhere(t)

	err := claudecode.New().Check(context.Background())

	if err == nil {
		t.Fatal("Check with claude nowhere = nil, want an error")
	}
	for _, want := range []string{"claude", "PATH", "claudePath"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not name %s", err, want)
		}
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
		ConfigDir:    workAccount,
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

	capped, err := d.Command(driver.Request{Prompt: "work", BudgetUSD: 5, ConfigDir: workAccount})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	uncapped, err := d.Command(driver.Request{Prompt: "work", ConfigDir: workAccount})
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
		ConfigDir: workAccount, Token: "sk-ant-oat01-one",
	})

	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := map[string]string{
		"CLAUDE_CONFIG_DIR":       workAccount,
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

	inv, err := claudecode.New().SetupToken(workAccount)

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
	if got["CLAUDE_CONFIG_DIR"] != workAccount {
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

// daemonCredentials are the variables a daemon started from a shell may carry,
// each of which Claude Code would use in place of an Account's own (ADR-0019).
var daemonCredentials = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"}

// withDaemonCredentials sets all three on the test process, which is where the
// invocation is built.
func withDaemonCredentials(t *testing.T) {
	t.Helper()
	for _, name := range daemonCredentials {
		t.Setenv(name, "the-daemons-own-"+strings.ToLower(name))
	}
}

// unsetsDaemonCredentials checks the invocation names all three as unset.
func unsetsDaemonCredentials(t *testing.T, inv agent.Invocation) {
	t.Helper()
	unset := map[string]bool{}
	for _, name := range inv.Unset {
		unset[name] = true
	}
	for _, name := range daemonCredentials {
		if !unset[name] {
			t.Errorf("the invocation does not unset %s, so the daemon's own would reach the agent", name)
		}
	}
}

func TestS1AnAgentForAnAccountIsGivenNoneOfTheDaemonsCredentials(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	withDaemonCredentials(t)

	inv, err := claudecode.New().Command(driver.Request{
		Prompt: "work", WorkingDir: "/worktrees/1",
		ConfigDir: workAccount, Token: "sk-ant-oat01-one",
	})

	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	unsetsDaemonCredentials(t, inv)
	got := environment(inv.Env)
	if got["CLAUDE_CONFIG_DIR"] != workAccount || got["CLAUDE_CODE_OAUTH_TOKEN"] != "sk-ant-oat01-one" {
		t.Errorf("the agent's environment is %v, want the account's own directory and token", inv.Env)
	}
	if len(inv.Env) != 2 {
		t.Errorf("the agent's environment is %v, want only the account's own two variables", inv.Env)
	}
}

func TestS2AnAccountWithNoTokenRunsWithNoneNotTheDaemons(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	withDaemonCredentials(t)

	inv, err := claudecode.New().Command(driver.Request{
		Prompt: "work", WorkingDir: "/worktrees/1", ConfigDir: workAccount,
	})

	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	unsetsDaemonCredentials(t, inv)
	got := environment(inv.Env)
	if got["CLAUDE_CONFIG_DIR"] != workAccount {
		t.Errorf("the agent runs with CLAUDE_CONFIG_DIR=%q, want the account's own directory", got["CLAUDE_CONFIG_DIR"])
	}
	if _, ok := got["CLAUDE_CODE_OAUTH_TOKEN"]; ok || len(inv.Env) != 1 {
		t.Errorf("the agent's environment is %v, want the directory alone: an account with no token runs with none", inv.Env)
	}
}

func TestS5TheTokenSetupFlowRunsWithoutTheDaemonsCredentialsToo(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	withDaemonCredentials(t)

	inv, err := claudecode.New().SetupToken(workAccount)

	if err != nil {
		t.Fatalf("SetupToken: %v", err)
	}
	unsetsDaemonCredentials(t, inv)
	if got := environment(inv.Env); got["CLAUDE_CONFIG_DIR"] != workAccount || len(inv.Env) != 1 {
		t.Errorf("the setup's environment is %v, want only the account's own directory", inv.Env)
	}
}

// settingsIn reads the allowlist out of an Account's settings file.
func settingsIn(t *testing.T, configDir string) (allow []string, mode os.FileMode, raw string) {
	t.Helper()
	path := filepath.Join(configDir, "settings.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the settings file: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("the settings file is not what the tool reads: %v\n%s", err, data)
	}
	return file.Permissions.Allow, info.Mode().Perm(), string(data)
}

// forAccount is a Run's request on an Account whose configuration directory
// is dir.
func forAccount(dir string, allowed ...string) driver.Request {
	return driver.Request{
		Prompt: "work", WorkingDir: "/worktrees/1", ConfigDir: dir, Token: "sk-ant-oat01-one",
		AllowedTools: allowed,
	}
}

func TestS1AFreshAccountGetsTheSettingsFileWithTheDefaultAllowlist(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	dir := filepath.Join(t.TempDir(), "work")

	if _, err := claudecode.New().Command(forAccount(dir)); err != nil {
		t.Fatalf("Command: %v", err)
	}

	allow, mode, _ := settingsIn(t, dir)
	if !slices.Equal(allow, claudecode.DefaultAllowedTools) {
		t.Errorf("the settings file allows %v, want the default allowlist %v", allow, claudecode.DefaultAllowedTools)
	}
	if mode != 0o600 {
		t.Errorf("the settings file is %o, want 600: it is the account's own", mode)
	}
}

func TestS2ASettingsFileTheUserEditedIsNeverTouched(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	dir := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	theirs := "{\n  \"permissions\": {\"allow\": [\"Read\"]},\n  \"theme\": \"dark\"\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := claudecode.New().Command(forAccount(dir)); err != nil {
		t.Fatalf("Command: %v", err)
	}

	if _, _, raw := settingsIn(t, dir); raw != theirs {
		t.Errorf("the settings file was rewritten:\n%s\nwant what the user wrote:\n%s", raw, theirs)
	}
}

func TestS9ASettingsFileThatCannotBeReadIsRefusedNotSeededOverAndNotWaitedOn(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	dir := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A link to nowhere: the name is taken, and there is nothing to read.
	if err := os.Symlink(filepath.Join(dir, "not-there.json"), filepath.Join(dir, "settings.json")); err != nil {
		t.Fatal(err)
	}
	started := time.Now()

	_, err := claudecode.New().Command(forAccount(dir))

	if err == nil {
		t.Fatal("Command accepted an account whose settings file cannot be read")
	}
	if !strings.Contains(err.Error(), "settings.json") {
		t.Errorf("the error %q does not name the settings file", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("Command took %s, want it back promptly rather than trying again and again", elapsed)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Errorf("the account's directory holds %v, want only the link that was there", entries)
	}
}

func TestS3TheInvocationCarriesThePermissionsInForceAndPassesAProjectsOwn(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")
	dir := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"permissions":{"allow":["Read","Edit"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	inv, err := claudecode.New().Command(forAccount(dir, "Bash(make test:*)", "WebFetch"))

	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if want := []string{"Read", "Edit", "Bash(make test:*)", "WebFetch"}; !slices.Equal(inv.Permissions, want) {
		t.Errorf("the invocation's permissions are %v, want the file's then the project's %v", inv.Permissions, want)
	}
	args := strings.Join(inv.Args, " ")
	if !strings.Contains(args, "--allowedTools Bash(make test:*),WebFetch") {
		t.Errorf("args %q do not pass the project's allowed tools", args)
	}
	if !strings.Contains(args, "--permission-prompts none") {
		t.Errorf("args %q no longer disable permission prompts", args)
	}
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
	// Claimed, and kept: a ceiling is only as good as what it is kept against
	// (ADR-0020), so what claims to report usage must read it.
	if !c.UsageReporting {
		t.Error("capabilities do not claim usage reporting")
	}
	if _, ok := claudecode.New().Usage(`{"type":"system","subtype":"usage_limits","limits":` +
		`{"five_hour":{"utilization":1,"resets_at":"2031-01-01T00:00:00Z"}}}`); !ok {
		t.Error("capabilities claim usage reporting, and no usage was read out of a line that carries it")
	}
}

func TestSetupTokenRunsInTheAccountsDirectoryRatherThanWhereverItWasTyped(t *testing.T) {
	stubClaude(t, "2.1.267 (Claude Code)")

	inv, err := claudecode.New().SetupToken(workAccount)

	if err != nil {
		t.Fatalf("SetupToken: %v", err)
	}
	// A tool started with no working directory inherits the one the process
	// that started it had, which for owl account add is the user's own
	// checkout (ADR-0006).
	if inv.Dir != workAccount {
		t.Errorf("the setup runs in %q, want the account's own directory", inv.Dir)
	}
}

func TestSkillsDirIsWhereClaudeCodeReadsSkills(t *testing.T) {
	// Not the plugins directory, which is a different thing (ADR-0033).
	if got := claudecode.New().SkillsDir(); got != ".claude/skills" {
		t.Errorf("SkillsDir = %q, want .claude/skills", got)
	}
}

func TestUsageReadsTheAccountsWindowsOutOfTheStream(t *testing.T) {
	d := claudecode.New()

	got, ok := d.Usage(`{"type":"system","subtype":"usage_limits","limits":{` +
		`"five_hour":{"utilization":42.5,"resets_at":"2031-01-01T00:00:00Z"},` +
		`"seven_day_opus":{"utilization":80,"resets_at":"2031-01-08T06:30:00Z"}}}`)

	if !ok {
		t.Fatal("Usage read nothing out of a line that carries the limits")
	}
	if len(got.Windows) != 2 {
		t.Fatalf("Usage = %+v, want both windows", got.Windows)
	}
	five := got.Windows[0]
	if five.Name != "five_hour" || five.Utilization != 42.5 {
		t.Errorf("the first window is %+v, want five_hour at 42.5%%", five)
	}
	if want := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC); !five.Resets.Equal(want) {
		t.Errorf("the first window resets at %s, want %s", five.Resets, want)
	}
	if got.Windows[1].Name != "seven_day_opus" || got.Windows[1].Utilization != 80 {
		t.Errorf("the second window is %+v, want seven_day_opus at 80%%", got.Windows[1])
	}
}

func TestUsageSaysNothingAboutALineThatSaysNothing(t *testing.T) {
	d := claudecode.New()
	for what, line := range map[string]string{
		"an ordinary assistant line": `{"type":"assistant","message":"working on it"}`,
		"the result":                 `{"type":"result","subtype":"success","num_turns":1}`,
		"not json at all":            `usage_limits`,
		"json that is not an event":  `{"limits":{"five_hour":{"utilization":10,"resets_at":"2031-01-01T00:00:00Z"}}}`,
		"a window with no figure":    `{"type":"system","subtype":"usage_limits","limits":{"five_hour":{"resets_at":"2031-01-01T00:00:00Z"}}}`,
		"a figure that is not one":   `{"type":"system","subtype":"usage_limits","limits":{"five_hour":{"utilization":140,"resets_at":"2031-01-01T00:00:00Z"}}}`,
		"a figure below nothing":     `{"type":"system","subtype":"usage_limits","limits":{"five_hour":{"utilization":-1,"resets_at":"2031-01-01T00:00:00Z"}}}`,
		"a reset that is not a time": `{"type":"system","subtype":"usage_limits","limits":{"five_hour":{"utilization":10,"resets_at":"soon"}}}`,
		"no limits at all":           `{"type":"system","subtype":"usage_limits","limits":{}}`,
	} {
		if got, ok := d.Usage(line); ok {
			t.Errorf("%s: Usage = %+v, want nothing read out of it", what, got)
		}
	}
}

func TestUsageKeepsTheWindowsItCanReadFromOneItCannot(t *testing.T) {
	d := claudecode.New()

	got, ok := d.Usage(`{"type":"system","subtype":"usage_limits","limits":{` +
		`"five_hour":{"utilization":42,"resets_at":"2031-01-01T00:00:00Z"},` +
		`"seven_day":{"utilization":10,"resets_at":"whenever"}}}`)

	if !ok || len(got.Windows) != 1 || got.Windows[0].Name != "five_hour" {
		t.Errorf("Usage = %+v, %v; want the window it could read and not the one it could not", got, ok)
	}
}

func TestUsageWillNotReadWhatAToolShouldNotBeSaying(t *testing.T) {
	d := claudecode.New()
	long := strings.Repeat("w", 65)
	if got, ok := d.Usage(`{"type":"system","subtype":"usage_limits","limits":{"` + long +
		`":{"utilization":10,"resets_at":"2031-01-01T00:00:00Z"}}}`); ok {
		t.Errorf("Usage = %+v, want nothing read out of a window with a name that long", got)
	}

	// A line about more windows than a tool has is a line about something
	// else.
	var many []string
	for at := range 20 {
		many = append(many, fmt.Sprintf(`"w%d":{"utilization":1,"resets_at":"2031-01-01T00:00:00Z"}`, at))
	}
	if got, ok := d.Usage(`{"type":"system","subtype":"usage_limits","limits":{` +
		strings.Join(many, ",") + `}}`); ok {
		t.Errorf("Usage = %+v, want nothing read out of a line about %d windows", got, len(many))
	}
}

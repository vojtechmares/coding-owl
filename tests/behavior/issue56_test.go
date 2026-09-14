package behavior_test

// Behavior tests for issue #56. TestS5 to TestS8 map to scenarios S5 to S8 in
// tests/behavior/issue-56.md; S1 to S3 live with the Claude Code Driver and S4
// with the config package, whose own APIs they exercise. These drive the built
// owl binary against a daemon, with the stub agent standing in for Claude Code
// and refusing to run without a settings file that grants it something.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/driver/claudecode"
)

// settingsPath is where an Account's settings file lives.
func settingsPath(l *layout, name string) string {
	return filepath.Join(accountDir(l, name), "settings.json")
}

// allowedIn reads the allowlist out of an Account's settings file.
func allowedIn(t *testing.T, l *layout, name string) []string {
	t.Helper()
	data, err := os.ReadFile(settingsPath(l, name))
	if err != nil {
		t.Fatalf("the account's settings file: %v", err)
	}
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("the settings file is not what the tool reads: %v\n%s", err, data)
	}
	return settings.Permissions.Allow
}

// runPermissions reads what owl jobs show printed as a Run's permissions.
func runPermissions(t *testing.T, out, run string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(out, "permissions:\n")
	if !ok {
		t.Fatalf("owl jobs show printed no permissions:\n%s", out)
	}
	for _, ln := range strings.Split(rest, "\n") {
		if rules, found := strings.CutPrefix(ln, "RUN "+run+": "); found {
			return strings.Split(rules, ", ")
		}
		if strings.TrimSpace(ln) == "" {
			break
		}
	}
	t.Fatalf("owl jobs show printed no permissions for run %s:\n%s", run, out)
	return nil
}

// ranOnAccount runs one Job on the Account named work and returns what the
// stub recorded, the run id, and what owl jobs show printed at the end.
func ranOnAccount(t *testing.T, l *layout, s *stub, config string) (invocation, string, string) {
	t.Helper()
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	accountProject(t, l, "api", config)
	out, _ := finishedJob(t, l)
	rows := runRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("owl jobs show reports %d runs, want 1:\n%s", len(rows), out)
	}
	return s.invoked(t), rows[0].id, out
}

func TestS5ARunsAgentFindsTheSettingsFileAndIsStartedWithoutPrompts(t *testing.T) {
	l, s := accountLayout(t)

	inv, _, _ := ranOnAccount(t, l, s, "apiVersion: codingowl.dev/v1\naccount: work\n")

	if got := inv.flag(t, "--permission-prompts"); got != "none" {
		t.Errorf("--permission-prompts = %q, want none", got)
	}
	if got := allowedIn(t, l, "work"); !slices.Equal(got, claudecode.DefaultAllowedTools) {
		t.Errorf("the account's settings file allows %v, want the default allowlist %v", got, claudecode.DefaultAllowedTools)
	}
}

func TestS6JobsShowPrintsThePermissionsTheRunHad(t *testing.T) {
	l, s := accountLayout(t)

	_, run, out := ranOnAccount(t, l, s,
		"apiVersion: codingowl.dev/v1\naccount: work\nallowedTools:\n  - \"Bash(make test:*)\"\n")

	got := runPermissions(t, out, run)
	for _, want := range append(slices.Clone(claudecode.DefaultAllowedTools), "Bash(make test:*)") {
		if !slices.Contains(got, want) {
			t.Errorf("the run's permissions %v do not list %q", got, want)
		}
	}
}

func TestS7ARunLeavesAnEditedSettingsFileAlone(t *testing.T) {
	l, s := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	theirs := "{\n  \"permissions\": {\"allow\": [\"Read\", \"Edit\"]},\n  \"theme\": \"dark\"\n}\n"
	if err := os.WriteFile(settingsPath(l, "work"), []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}
	accountProject(t, l, "api", "apiVersion: codingowl.dev/v1\naccount: work\n")

	out, _ := finishedJob(t, l)

	if data, err := os.ReadFile(settingsPath(l, "work")); err != nil || string(data) != theirs {
		t.Errorf("the settings file reads %q, %v; want exactly what the user wrote", data, err)
	}
	rows := runRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("owl jobs show reports %d runs, want 1:\n%s", len(rows), out)
	}
	if got := runPermissions(t, out, rows[0].id); !slices.Equal(got, []string{"Read", "Edit"}) {
		t.Errorf("the run's permissions are %v, want the user's own list", got)
	}
	_ = s
}

func TestS8TheDecisionRecordNamesTheDefaultAllowlistAndWhoOwnsTheFile(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoDir, "docs", "adr", "0035-agent-permissions-allowlist.md"))
	if err != nil {
		t.Fatalf("reading ADR-0035: %v", err)
	}

	for _, rule := range claudecode.DefaultAllowedTools {
		if !strings.Contains(string(body), "`"+rule+"`") {
			t.Errorf("ADR-0035 does not name the default rule %s", rule)
		}
	}
	if !strings.Contains(string(body), "the user's") {
		t.Error("ADR-0035 does not say whose the settings file is once it exists")
	}
}

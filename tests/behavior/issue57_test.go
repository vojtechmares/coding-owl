package behavior_test

// Behavior tests for issue #57. TestS5 to TestS7 map to scenarios S5 to S7 in
// tests/behavior/issue-57.md; S1 to S3 live with the Claude Code Driver and S4
// with the config package, whose own APIs they exercise. These run the release
// script that renders the formula, drive the built owl binary against a daemon
// whose PATH holds no claude, and read the install docs.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS5TheFormulasServicePATHHoldsTheHomeDirectorysLocalBin(t *testing.T) {
	tp := newTap(t)

	res := runScript(t, repoScript("bump-formula.sh"), repoDir,
		bumpEnv(tp, "0.1.0", testDigest, "", "DRY_RUN=1"))

	if res.code != 0 {
		t.Fatalf("bump-formula.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	body, err := os.ReadFile(tp.formula())
	if err != nil {
		t.Fatalf("no rendered formula: %v", err)
	}
	var pathLine string
	for _, ln := range strings.Split(string(body), "\n") {
		if strings.Contains(ln, "environment_variables PATH:") {
			pathLine = strings.TrimSpace(ln)
		}
	}
	if pathLine == "" {
		t.Fatalf("the formula's service block sets no PATH:\n%s", body)
	}
	if !strings.Contains(pathLine, "std_service_path_env") || !strings.HasSuffix(pathLine, `.local/bin"`) {
		t.Errorf("the service PATH is %q, want the standard service path followed by the home directory's .local/bin", pathLine)
	}
	if !strings.Contains(pathLine, "Dir.home") {
		t.Errorf("the service PATH is %q, want .local/bin under the home directory", pathLine)
	}
}

func TestS6ADaemonWhosePATHLacksTheToolRunsAJobWithClaudePathSet(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	// The daemon's PATH holds the stub machine but not the stub agent, as a
	// daemon under brew services holds no ~/.local/bin; its own file says
	// where the tool is instead.
	l = l.withoutEnv("PATH").withEnv("PATH=" + fakeMachineDir + string(os.PathListSeparator) + os.Getenv("PATH"))
	globalConfig(t, l, fileStore+"claudePath: "+filepath.Join(fakeClaudeDir, "claude")+"\n")
	daemonUp(t, l)
	runnableJob(t, l, "work")

	_, job := startRun(t, l)
	out := finished(t, l, job)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review:\n%s", got, out)
	}
	if got := s.invoked(t).Argv[0]; got != filepath.Join(fakeClaudeDir, "claude") {
		t.Errorf("the agent was invoked as %q, want the configured path", got)
	}
}

func TestS7TheInstallDocsDescribeTheLimitationAndTheSetting(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(repoDir, "README.md"))
	if err != nil {
		t.Fatalf("reading the README: %v", err)
	}
	for _, want := range []string{"brew services", "~/.local/bin", "claudePath"} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("the README does not mention %s", want)
		}
	}
	script, err := os.ReadFile(repoScript("bump-formula.sh"))
	if err != nil {
		t.Fatalf("reading the release script: %v", err)
	}
	_, caveats, ok := strings.Cut(string(script), "def caveats")
	if !ok {
		t.Fatal("the formula has no caveats")
	}
	if !strings.Contains(caveats, "claudePath") {
		t.Error("the formula's caveats do not mention claudePath")
	}
}

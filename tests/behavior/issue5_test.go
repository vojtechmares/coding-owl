package behavior_test

// Behavior tests for issue #5. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-5.md. They drive the built owl binary from the outside
// against a daemon started as a subprocess, whose PATH puts the stub agent of
// tests/behavior/fakeclaude where Claude Code would be. S21 is about the test
// suite itself and runs `go test` for the smoke test alone.

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// smokeEnv enables the smoke test that runs the real Claude Code, and
// smokeTokenEnv carries the token it runs on: a Job runs on its Project's
// Account (ADR-0023), and the real tool needs a real one.
const (
	smokeEnv      = "OWL_SMOKE_CLAUDE"
	smokeTokenEnv = "OWL_SMOKE_CLAUDE_TOKEN"
)

// agentScript is what the stub agent emits when a scenario does not care what
// the Agent said, only that it said it.
var agentScript = []string{
	`{"type":"system","subtype":"init","cwd":"."}`,
	`{"type":"assistant","message":"working on it"}`,
	`{"type":"result","subtype":"success","num_turns":1}`,
}

// stub is where a scenario's stub agent reads its script and writes what it
// was asked to do.
type stub struct {
	script  string
	argv    string
	release string
}

// invocation is what the stub agent recorded about how it was called.
type invocation struct {
	Argv []string `json:"argv"`
	PID  int      `json:"pid"`
	PPID int      `json:"ppid"`
	Dir  string   `json:"dir"`
	// Entries are the names in its working directory when it started, which is
	// how a scenario sees what a setup command left there.
	Entries []string `json:"entries"`
	// Env holds the CLAUDE_ variables it was given, which is how a scenario
	// sees the Account it was run as (ADR-0019).
	Env map[string]string `json:"env"`
}

// agentLayout returns an XDG layout whose daemon finds the stub agent first on
// its PATH, scripted to emit these lines and exit with this status.
func agentLayout(t *testing.T, script []string, exitCode int) (*layout, *stub) {
	t.Helper()
	return agentLayoutVersion(t, script, exitCode, "")
}

// agentLayoutVersion is agentLayout with the version the stub reports, for the
// scenarios about the supported range.
func agentLayoutVersion(t *testing.T, script []string, exitCode int, version string) (*layout, *stub) {
	t.Helper()
	l := newLayout(t)
	s := &stub{
		script:  filepath.Join(l.root, "agent-script"),
		argv:    filepath.Join(l.root, "agent-argv.json"),
		release: filepath.Join(l.root, "agent-release"),
	}
	if err := os.WriteFile(s.script, []byte(strings.Join(script, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"PATH=" + fakeClaudeDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"OWL_FAKE_CLAUDE_SCRIPT=" + s.script,
		"OWL_FAKE_CLAUDE_ARGV=" + s.argv,
		"OWL_FAKE_CLAUDE_WAIT=" + s.release,
		"OWL_FAKE_CLAUDE_EXIT=" + strconv.Itoa(exitCode),
	}
	if version != "" {
		env = append(env, "OWL_FAKE_CLAUDE_VERSION="+version)
	}
	out := l.withoutEnv("PATH").withEnv(env...)
	out.agent = true
	return out, s
}

// release lets a stub agent waiting on a `#wait` line continue.
func (s *stub) let(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(s.release, []byte("go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// invoked reads what the stub agent recorded about its most recent invocation.
func (s *stub) invoked(t *testing.T) invocation {
	t.Helper()
	all := s.invocations(t)
	if len(all) == 0 {
		t.Fatal("the stub agent recorded no invocation")
	}
	return all[len(all)-1]
}

// invocations reads every invocation the stub agent recorded, in order: a Job
// takes a Run per phase, and each one is worth reading back.
func (s *stub) invocations(t *testing.T) []invocation {
	t.Helper()
	data, err := os.ReadFile(s.argv)
	if err != nil {
		t.Fatalf("the stub agent recorded no invocation: %v", err)
	}
	var out []invocation
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var inv invocation
		if err := json.Unmarshal([]byte(ln), &inv); err != nil {
			t.Fatalf("unreadable invocation record: %v\n%s", err, ln)
		}
		out = append(out, inv)
	}
	return out
}

// flag returns the value that follows name in the invocation.
func (inv invocation) flag(t *testing.T, name string) string {
	t.Helper()
	for i, a := range inv.Argv {
		if a == name && i+1 < len(inv.Argv) {
			return inv.Argv[i+1]
		}
	}
	t.Fatalf("no %s in the agent's argv: %v", name, inv.Argv)
	return ""
}

func (inv invocation) has(name string) bool {
	for _, a := range inv.Argv {
		if a == name {
			return true
		}
	}
	return false
}

var startedRE = regexp.MustCompile(`started run (\d+) for job (\d+)`)

// startRun triggers a Run and returns the run and job ids owl start reported.
func startRun(t *testing.T, l *layout) (run, job string) {
	t.Helper()
	res := mustOwl(t, l, "start")
	m := startedRE.FindStringSubmatch(res.stdout)
	if m == nil {
		t.Fatalf("owl start did not report the run it started:\n%s", res.stdout)
	}
	return m[1], m[2]
}

// finished waits for a Job to leave the states a Run passes through, and
// returns the owl jobs show output it ended on.
func finished(t *testing.T, l *layout, job string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var out string
	for time.Now().Before(deadline) {
		out = mustOwl(t, l, "jobs", "show", job).stdout
		switch line(t, out, "state") {
		case "pending", "active":
			time.Sleep(50 * time.Millisecond)
		default:
			return out
		}
	}
	t.Fatalf("job %s was still running after 20s:\n%s", job, out)
	return ""
}

// systemPrompt is everything owl jobs show printed under its system prompt
// heading.
func systemPrompt(t *testing.T, out string) string {
	t.Helper()
	_, prompt, ok := strings.Cut(out, "system prompt:\n")
	if !ok {
		t.Fatalf("owl jobs show printed no system prompt:\n%s", out)
	}
	return strings.TrimRight(prompt, "\n")
}

// runLog is where a Run's captured output is kept.
func runLog(l *layout, run string) string {
	return filepath.Join(l.state, "coding-owl", "logs", run+".jsonl")
}

// worktreeDir is where a Job's worktree belongs.
func worktreeDir(l *layout, job string) string {
	return filepath.Join(l.data, "coding-owl", "worktrees", job)
}

// runnableJob registers a Project and queues one Job that runs in a single
// Run. Planning is the default since issue #6, and these scenarios are about
// carrying a Job out rather than about planning it.
func runnableJob(t *testing.T, l *layout, prompt string) *repo {
	t.Helper()
	r := project(t, l, "api")
	addJob(t, l, r.dir, prompt, "--no-plan")
	return r
}

func TestS1RunStartsTheOldestPendingJobInItsOwnWorktree(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := runnableJob(t, l, "first")
	addJob(t, l, r.dir, "second", "--no-plan")

	run, job := startRun(t, l)
	finished(t, l, job)

	if job != jobID(t, queueList(t, l, "--all"), "first") {
		t.Errorf("owl start ran job %s, want the oldest pending one", job)
	}
	if run == "" {
		t.Error("owl start reported no run id")
	}
	worktree := worktreeDir(l, job)
	if !listsWorktree(t, r, worktree) {
		t.Errorf("git worktree list does not report %s:\n%s", worktree, r.git("worktree", "list"))
	}
	branch := strings.TrimSpace(gitIn(t, r, worktree, "rev-parse", "--abbrev-ref", "HEAD"))
	if want := "owl/job-" + job; branch != want {
		t.Errorf("the worktree is on branch %q, want %q", branch, want)
	}
	wantQueue(t, l, "1|api|pending|second")
	second := jobID(t, queueList(t, l), "second")
	if got := line(t, mustOwl(t, l, "jobs", "show", second).stdout, "runs"); got != "none" {
		t.Errorf("job %s reports runs %q, want none started for it", second, got)
	}
}

// listsWorktree reports whether git knows about a worktree at that path,
// comparing through symlinks: git prints the resolved path and a temporary
// directory is reached by more than one spelling.
func listsWorktree(t *testing.T, r *repo, worktree string) bool {
	t.Helper()
	for _, ln := range strings.Split(r.git("worktree", "list"), "\n") {
		fields := strings.Fields(ln)
		if len(fields) > 0 && samePath(fields[0], worktree) {
			return true
		}
	}
	return false
}

// gitIn runs git inside a directory that is not the Project's own root, such
// as a Job's worktree, with the repository's test environment.
func gitIn(t *testing.T, r *repo, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func TestS2RunLeavesTheUsersOwnCheckoutAlone(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := runnableJob(t, l, "work")
	r.write("scratch.txt", "mine\n")

	_, job := startRun(t, l)
	finished(t, l, job)

	if branch := strings.TrimSpace(r.git("rev-parse", "--abbrev-ref", "HEAD")); branch != "main" {
		t.Errorf("the project's checkout is on %q, want main", branch)
	}
	data, err := os.ReadFile(filepath.Join(r.dir, "scratch.txt"))
	if err != nil || string(data) != "mine\n" {
		t.Errorf("the uncommitted file is %q (%v), want it untouched", data, err)
	}
	if status := strings.TrimSpace(r.git("status", "--porcelain")); status != "?? scratch.txt" {
		t.Errorf("git status reports %q, want only the untouched scratch file", status)
	}
}

func TestS3RunsAgentIsADirectChildOfTheDaemon(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	p := daemonUp(t, l)
	runnableJob(t, l, "work")

	_, job := startRun(t, l)
	finished(t, l, job)

	if got := s.invoked(t).PPID; got != p.cmd.Process.Pid {
		t.Errorf("the agent's parent is pid %d, want the daemon's own pid %d", got, p.cmd.Process.Pid)
	}
}

func TestS4RunCleanExitLandsTheJobInReview(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	_, job := startRun(t, l)
	out := finished(t, l, job)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
	runs := runRows(t, out)
	if len(runs) != 1 {
		t.Fatalf("owl jobs show reports %d runs, want 1:\n%s", len(runs), out)
	}
	if runs[0].outcome != "succeeded" || runs[0].exit != "0" {
		t.Errorf("run = %+v, want it succeeded with exit status 0", runs[0])
	}
	wantEmptyQueue(t, l)
}

// runRow is one row of the runs table owl jobs show prints.
type runRow struct {
	id, attempt, phase, outcome, exit, log string
}

var runRowRE = regexp.MustCompile(`^(\d+)\s+(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)$`)

// runRows parses the runs table out of owl jobs show output.
func runRows(t *testing.T, out string) []runRow {
	t.Helper()
	_, rest, ok := strings.Cut(out, "runs:\n")
	if !ok {
		return nil
	}
	var rows []runRow
	for _, ln := range strings.Split(rest, "\n") {
		if strings.TrimSpace(ln) == "" {
			break
		}
		if strings.HasPrefix(ln, "RUN") {
			continue
		}
		m := runRowRE.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			t.Fatalf("cannot parse run row %q in:\n%s", ln, out)
		}
		rows = append(rows, runRow{id: m[1], attempt: m[2], phase: m[3], outcome: m[4], exit: m[5], log: m[8]})
	}
	return rows
}

func TestS5RunNonZeroExitBlocksTheJobWithTheReason(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 3)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	_, job := startRun(t, l)
	out := finished(t, l, job)

	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	runs := runRows(t, out)
	if len(runs) != 1 || runs[0].outcome != "failed" || runs[0].exit != "3" {
		t.Fatalf("runs = %+v, want one failed run that exited 3:\n%s", runs, out)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "3") {
		t.Errorf("reason = %q, does not name the exit status", reason)
	}
}

func TestS6RunPersistsItsStructuredOutput(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	run, job := startRun(t, l)
	finished(t, l, job)

	data, err := os.ReadFile(runLog(l, run))
	if err != nil {
		t.Fatalf("no log for run %s: %v", run, err)
	}
	if got, want := string(data), strings.Join(agentScript, "\n")+"\n"; got != want {
		t.Errorf("log =\n%s\nwant\n%s", got, want)
	}
}

func TestS7RunLogsPrintsTheWholeLog(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")
	run, job := startRun(t, l)
	finished(t, l, job)

	res := mustOwl(t, l, "logs", run)

	if want := strings.Join(agentScript, "\n") + "\n"; res.stdout != want {
		t.Errorf("owl logs printed\n%s\nwant\n%s", res.stdout, want)
	}
}

func TestS8RunLogsFollowsWhileTheRunIsInProgress(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, s := agentLayout(t, script, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")
	run, job := startRun(t, l)

	follow := followLogs(t, l, run)
	if got := follow.next(t); got != agentScript[0] {
		t.Fatalf("first streamed line = %q, want %q", got, agentScript[0])
	}

	s.let(t)

	if got := follow.next(t); got != agentScript[2] {
		t.Errorf("second streamed line = %q, want %q", got, agentScript[2])
	}
	if code := follow.wait(t); code != 0 {
		t.Errorf("owl logs -f exited %d, want 0", code)
	}
	finished(t, l, job)
}

// following is an `owl logs -f` reading its output as it arrives.
type following struct {
	cmd   *exec.Cmd
	lines *bufio.Scanner
	read  chan string
}

func followLogs(t *testing.T, l *layout, run string) *following {
	t.Helper()
	cmd := exec.Command(owlBin, "logs", run, "-f")
	cmd.Env = l.env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	f := &following{cmd: cmd, lines: bufio.NewScanner(stdout), read: make(chan string)}
	go func() {
		defer close(f.read)
		for f.lines.Scan() {
			f.read <- f.lines.Text()
		}
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return f
}

// next returns the next line the follower printed, failing if none arrives.
func (f *following) next(t *testing.T) string {
	t.Helper()
	select {
	case ln, ok := <-f.read:
		if !ok {
			t.Fatal("owl logs -f ended before the line arrived")
		}
		return ln
	case <-time.After(15 * time.Second):
		t.Fatal("no line from owl logs -f within 15s")
		return ""
	}
}

func (f *following) wait(t *testing.T) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- f.cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		t.Fatalf("owl logs -f: %v", err)
		return -1
	case <-time.After(15 * time.Second):
		t.Fatal("owl logs -f did not end with the run")
		return -1
	}
}

func TestS9RunSystemPromptCarriesTheUnattendedContract(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	out := mustOwl(t, l, "jobs", "show", "1").stdout

	prompt := systemPrompt(t, out)
	for _, want := range []string{
		"unattended", "nobody", ".coding-owl/HANDOFF.md", "assumption", "commit", "destructive",
	} {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(want)) {
			t.Errorf("the system prompt does not mention %q:\n%s", want, prompt)
		}
	}
}

func TestS10RunProjectClausesFollowTheContract(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - run gofmt before committing\n", "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	prompt := systemPrompt(t, mustOwl(t, l, "jobs", "show", "1").stdout)

	clause := "run gofmt before committing"
	if !strings.Contains(prompt, clause) {
		t.Fatalf("the project's clause is missing from the system prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "unattended") {
		t.Fatalf("the standing contract is missing from the system prompt:\n%s", prompt)
	}
	if strings.Index(prompt, clause) < strings.Index(prompt, "unattended") {
		t.Errorf("the project's clause comes before the standing contract:\n%s", prompt)
	}
}

func TestS11RunInvokesPrintModeWithoutPermissionPrompts(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "fix the flaky test")

	_, job := startRun(t, l)
	finished(t, l, job)

	inv := s.invoked(t)
	if !inv.has("--print") {
		t.Errorf("the agent was not run in print mode: %v", inv.Argv)
	}
	if got := inv.flag(t, "--output-format"); got != "stream-json" {
		t.Errorf("--output-format = %q, want stream-json", got)
	}
	if !inv.has("--verbose") {
		t.Errorf("stream-json was asked for without --verbose: %v", inv.Argv)
	}
	if got := inv.flag(t, "--permission-prompts"); got != "none" {
		t.Errorf("--permission-prompts = %q, want none", got)
	}
	for _, forbidden := range []string{"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions"} {
		if inv.has(forbidden) {
			t.Errorf("the agent was run with %s: %v", forbidden, inv.Argv)
		}
	}
	// The Job's prompt is carried inside the prompt the phase builds around
	// it, so it is in the last argument rather than being one of its own.
	if last := inv.Argv[len(inv.Argv)-1]; !strings.Contains(last, "fix the flaky test") {
		t.Errorf("the job's prompt is not in the prompt the agent was given: %q", last)
	}
}

func TestS12RunContractReachesTheAgent(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	_, job := startRun(t, l)
	out := finished(t, l, job)

	if got, want := s.invoked(t).flag(t, "--append-system-prompt"), systemPrompt(t, out); got != want {
		t.Errorf("the agent was given\n%s\nbut owl jobs show prints\n%s", got, want)
	}
}

func TestS13RunAppliesASpendCapOnlyWhenConfigured(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nbudgetUSD: 5\n", "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work", "--no-plan")

	_, job := startRun(t, l)
	finished(t, l, job)

	if got := s.invoked(t).flag(t, "--max-budget-usd"); got != "5" {
		t.Errorf("--max-budget-usd = %q, want 5", got)
	}

	plain, plainStub := agentLayout(t, agentScript, 0)
	daemonUp(t, plain)
	runnableJob(t, plain, "work")
	_, plainJob := startRun(t, plain)
	finished(t, plain, plainJob)

	if inv := plainStub.invoked(t); inv.has("--max-budget-usd") {
		t.Errorf("a project with no budget still capped the agent's spend: %v", inv.Argv)
	}
}

func TestS14RunRefusesAnUnsupportedClaudeCode(t *testing.T) {
	l, _ := agentLayoutVersion(t, agentScript, 0, "1.9.0 (Claude Code)")
	daemonUp(t, l)
	runnableJob(t, l, "work")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start exited 0 with an unsupported agent\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "1.9.0") {
		t.Errorf("stderr does not name the version it found:\n%s", res.stderr)
	}
	if !strings.Contains(res.stderr, "2.1.0") || !strings.Contains(res.stderr, "3.0.0") {
		t.Errorf("stderr does not name the range Owl supports:\n%s", res.stderr)
	}
	out := mustOwl(t, l, "jobs", "show", "1").stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job still pending", got)
	}
	if got := line(t, out, "runs"); got != "none" {
		t.Errorf("runs = %q, want none", got)
	}
	if _, err := os.Stat(worktreeDir(l, "1")); err == nil {
		t.Errorf("a worktree was created for a run that never started")
	}
}

func TestS15RunBranchesFromTheBaseBranchUnderThePrefix(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("nightly/"), "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work", "--no-plan")

	_, job := startRun(t, l)
	finished(t, l, job)

	worktree := worktreeDir(l, job)
	branch := strings.TrimSpace(gitIn(t, r, worktree, "rev-parse", "--abbrev-ref", "HEAD"))
	if want := "nightly/job-" + job; branch != want {
		t.Errorf("branch = %q, want %q", branch, want)
	}
	head := strings.TrimSpace(gitIn(t, r, worktree, "rev-parse", "HEAD"))
	if base := strings.TrimSpace(r.git("rev-parse", "refs/heads/main")); head != base {
		t.Errorf("the branch is at %s, want main's %s", head, base)
	}
}

func TestS16RunStartWithNothingPendingSaysSo(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)

	res := mustOwl(t, l, "start")

	if startedRE.MatchString(res.stdout) {
		t.Errorf("owl start reported a run with an empty queue:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "nothing pending") {
		t.Errorf("owl start does not say the queue holds nothing to run:\n%s", res.stdout)
	}
	if logs, err := os.ReadDir(filepath.Join(l.state, "coding-owl", "logs")); err == nil && len(logs) > 0 {
		t.Errorf("owl start recorded %d run logs with an empty queue", len(logs))
	}
}

func TestS17RunOnlyOneAtATime(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, s := agentLayout(t, script, 0)
	daemonUp(t, l)
	r := runnableJob(t, l, "first")
	addJob(t, l, r.dir, "second", "--no-plan")
	_, job := startRun(t, l)

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("a second owl start exited 0 while a run was in progress:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "in progress") || !strings.Contains(res.stderr, job) {
		t.Errorf("stderr does not say a run for job %s is in progress:\n%s", job, res.stderr)
	}
	wantQueue(t, l, "2|api|pending|second")

	s.let(t)
	finished(t, l, job)
}

func TestS18RunJobsShowReportsTheJobAndItsRuns(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	run, job := startRun(t, l)
	out := finished(t, l, job)

	wantLine(t, out, "id", job)
	wantLine(t, out, "project", "api")
	wantLine(t, out, "state", "review")
	wantLine(t, out, "branch", "owl/job-"+job)
	wantLine(t, out, "worktree", worktreeDir(l, job))
	rows := runRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("owl jobs show reports %d runs, want 1:\n%s", len(rows), out)
	}
	if rows[0].id != run || rows[0].attempt != "1" {
		t.Errorf("run row = %+v, want run %s attempt 1", rows[0], run)
	}
	if rows[0].log != runLog(l, run) {
		t.Errorf("run log = %q, want %q", rows[0].log, runLog(l, run))
	}
	if unknown := runOwl(t, l, "jobs", "show", "999"); unknown.code == 0 || !strings.Contains(unknown.stderr, "999") {
		t.Errorf("owl jobs show 999 = %+v, want a non-zero exit naming 999", unknown)
	}
}

func TestS19RunLogsOnAnUnknownRunFailsClearly(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)

	res := runOwl(t, l, "logs", "999")

	if res.code == 0 {
		t.Fatalf("owl logs 999 exited 0:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "999") {
		t.Errorf("stderr does not name 999:\n%s", res.stderr)
	}
}

func TestS20RunCommandsReportAStoppedDaemon(t *testing.T) {
	l := newLayout(t)

	for _, args := range [][]string{{"start"}, {"logs", "1"}, {"jobs", "show", "1"}} {
		res := runOwl(t, l, args...)
		if res.code == 0 {
			t.Errorf("owl %v exited 0 with no daemon running:\n%s", args, res.stdout)
		}
		if !strings.Contains(res.stderr, "daemon not running") || !strings.Contains(res.stderr, l.socket()) {
			t.Errorf("owl %v stderr does not report a stopped daemon at %s:\n%s", args, l.socket(), res.stderr)
		}
	}
}

func TestS21RunSmokeTestIsSkippedUnlessEnabled(t *testing.T) {
	if os.Getenv(smokeEnv) != "" {
		t.Skipf("%s is set, so the smoke test is not skipped", smokeEnv)
	}
	cmd := exec.Command("go", "test", "./tests/behavior/", "-run", "^TestSmokeRealClaudeCode$", "-v", "-count=1")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), smokeEnv+"=")
	out, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("running the smoke test: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- SKIP: TestSmokeRealClaudeCode") {
		t.Errorf("the smoke test did not skip itself:\n%s", out)
	}
	if !strings.Contains(string(out), smokeEnv) {
		t.Errorf("the skip message does not name %s:\n%s", smokeEnv, out)
	}
}

func TestS22RunAgentWorksInTheJobsWorktree(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := runnableJob(t, l, "work")

	_, job := startRun(t, l)
	finished(t, l, job)

	dir := s.invoked(t).Dir
	if !samePath(dir, worktreeDir(l, job)) {
		t.Errorf("the agent ran in %s, want the job's worktree %s", dir, worktreeDir(l, job))
	}
	if samePath(dir, r.dir) {
		t.Errorf("the agent ran in the project's own checkout %s", r.dir)
	}
}

// samePath compares two paths through symlinks, since a temporary directory is
// reached by more than one spelling on macOS.
func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = filepath.Clean(a)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = filepath.Clean(b)
	}
	return ra == rb
}

// TestSmokeRealClaudeCode runs one Job through the real Claude Code. It spends
// tokens, so it only runs when OWL_SMOKE_CLAUDE is set.
func TestSmokeRealClaudeCode(t *testing.T) {
	if os.Getenv(smokeEnv) == "" {
		t.Skipf("set %s=1 to run the smoke test against the real Claude Code; it spends tokens", smokeEnv)
	}
	token := os.Getenv(smokeTokenEnv)
	if token == "" {
		t.Skipf("set %s to a token from `claude setup-token`; the smoke test runs on an account like every other job", smokeTokenEnv)
	}
	l := newLayout(t)
	// The real Claude Code is an Agent like the stub, so this layout runs one
	// and its Project is given an Account to run on (ADR-0023) - this one,
	// added before the Project so that the harness does not add its own with a
	// token the real tool cannot use.
	l.agent = true
	daemonUp(t, l)
	addAccount(t, l, harnessAccount, token)
	r := project(t, l, "api")
	addJob(t, l, r.dir,
		"Write a file called SMOKE.md containing the single word owl, then commit it.", "--no-plan")

	run, job := startRun(t, l)
	out := finished(t, l, job)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review\n%s", got, out)
	}
	data, err := os.ReadFile(runLog(l, run))
	if err != nil || len(data) == 0 {
		t.Errorf("the run captured no output: %v", err)
	}
}

func TestS23RunAnUnfinishedRunDoesNotHoldTheQueue(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, _ := agentLayout(t, script, 0)
	p := daemonUp(t, l)
	r := runnableJob(t, l, "first")
	addJob(t, l, r.dir, "second", "--no-plan")
	run, job := startRun(t, l)

	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	p.exit(t, 10*time.Second)
	// The killed daemon left its socket behind, so waiting for the file to
	// exist would see the dead one; the new daemon's own log line is what says
	// it is listening.
	waitForLog(t, startDaemon(t, l), "daemon listening")

	out := mustOwl(t, l, "jobs", "show", job).stdout
	rows := runRows(t, out)
	if len(rows) != 1 || rows[0].id != run || rows[0].outcome != "interrupted" {
		t.Fatalf("runs = %+v, want run %s reported as interrupted:\n%s", rows, run, out)
	}
	secondRun, secondJob := startRun(t, l)
	if secondRun == run {
		t.Errorf("owl start reported run %s again, want a new one", secondRun)
	}
	if secondJob == job {
		t.Errorf("owl start ran job %s again, want the second job", secondJob)
	}
	if got := line(t, mustOwl(t, l, "jobs", "show", secondJob).stdout, "prompt"); got != "second" {
		t.Errorf("owl start ran the job prompted %q, want second", got)
	}
}

func TestS24RunDaemonStopsCleanlyWithAFollowerAttached(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, _ := agentLayout(t, script, 0)
	p := daemonUp(t, l)
	runnableJob(t, l, "work")
	run, _ := startRun(t, l)
	follow := followLogs(t, l, run)
	if got := follow.next(t); got != agentScript[0] {
		t.Fatalf("first streamed line = %q, want %q", got, agentScript[0])
	}

	stopDaemon(t, p)

	if code := follow.wait(t); code != 0 {
		t.Errorf("owl logs -f exited %d after the daemon stopped, want 0", code)
	}
}

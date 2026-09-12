package behavior_test

// Behavior tests for issue #23. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-23.md. The scenarios drive the built owl binary against
// a daemon whose Agents hold, so that several Runs can be in flight at once,
// with the stub machine of issue #19 standing in for the real one.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// caps is a layout whose Agents hold where they are, so a scenario can look at
// several Runs at once, with a machine it can send away and a file that lets
// every Agent finish.
type caps struct {
	l       *layout
	m       *machine
	release string
}

// capsLayout is that layout, with the daemon up and the machine still in use.
// global is added to the configuration that makes the daemon look at the
// machine often.
func capsLayout(t *testing.T, global string) *caps {
	t.Helper()
	script := []string{
		agentScript[0],
		// What the account has used, so a scenario can put one over its
		// ceiling (ADR-0020). Nothing is held to one unless it says so.
		reports(window{name: "five_hour", utilization: 90, resets: far()}),
		"#wait",
		agentScript[2],
	}
	l, _ := agentLayout(t, script, 0)
	release := filepath.Join(l.root, "release")
	l = l.withEnv("OWL_FAKE_CLAUDE_WAIT=" + release)
	l, m := watching(t, l, fastIdle+global)
	daemonUp(t, l)
	return &caps{l: l, m: m, release: release}
}

// project registers a Project of its own with that configuration and queues
// jobs Jobs against it, and returns the name it was registered under.
func (c *caps) project(t *testing.T, name, config string, jobs int) string {
	t.Helper()
	r := newRepo(t, c.l, name)
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\n"+config, "configure owl")
	addProject(t, c.l, r)
	for range jobs {
		addJob(t, c.l, r.dir, "work in "+name, "--no-plan")
	}
	return name
}

// away sends the machine away, which is what starts anything (ADR-0011).
func (c *caps) away(t *testing.T) {
	t.Helper()
	c.m.away(t)
}

// finish lets every held Agent carry on, so a scenario can watch Runs end.
func (c *caps) finish(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(c.release, []byte("go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// status is owl status, which is where everything this issue is about is
// reported.
func (c *caps) status(t *testing.T) string {
	t.Helper()
	return mustOwl(t, c.l, "status").stdout
}

// running is the Jobs with a Run in flight, by the JOB column of owl status.
func (c *caps) running(t *testing.T) []string {
	t.Helper()
	return columnOf(c.status(t), "runs in progress:", 1)
}

// columns splits a padded row into its cells. The table is written with a
// tabwriter, so two or more spaces separate the columns and one never does.
var columns = regexp.MustCompile(`\s{2,}`)

// passedOver is each Job owl status says was passed over, and why. The reason
// is the REASON column alone: reading the PROJECT column as part of it would
// let a reason that never names a project look as though it did.
func (c *caps) passedOver(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range sectionOf(c.status(t), "passed over:") {
		cells := columns.Split(strings.TrimSpace(row), 3)
		if len(cells) != 3 {
			t.Fatalf("a passed-over row is not job, project and reason: %q", row)
		}
		out[cells[0]] = cells[2]
	}
	return out
}

// sectionOf is the rows of one section of owl status, without its heading or
// its column names.
func sectionOf(out, heading string) []string {
	_, rest, ok := strings.Cut(out, heading+"\n")
	if !ok {
		return nil
	}
	var rows []string
	for at, line := range strings.Split(rest, "\n") {
		if strings.TrimSpace(line) == "" {
			break
		}
		if at == 0 {
			// The column names.
			continue
		}
		rows = append(rows, line)
	}
	return rows
}

// columnOf is one column of a section, by index, with the reason column left
// whole.
func columnOf(out, heading string, at int) []string {
	var vals []string
	for _, row := range sectionOf(out, heading) {
		fields := strings.Fields(row)
		if at < len(fields) {
			vals = append(vals, fields[at])
		}
	}
	return vals
}

// waitRunning waits until that many Runs are in flight, and no longer. What
// owl status said goes into the failure: which Runs are going and what was
// passed over is the whole of what these scenarios are about.
func (c *caps) waitRunning(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(c.running(t)) != n {
		if time.Now().After(deadline) {
			t.Fatalf("waited for %d runs to be in flight; owl status says:\n%s", n, c.status(t))
		}
		sleep(100 * time.Millisecond)
	}
	// And it stays there: a cap that is not held would let another one start a
	// moment later.
	looked()
	if got := c.running(t); len(got) != n {
		t.Fatalf("%d runs are in flight, want %d: %v", len(got), n, got)
	}
}

func TestS1CapsOneProjectRunsOneRunHoweverHighTheGlobalCapIs(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	c.project(t, "api", "", 2)

	c.away(t)

	c.waitRunning(t, 1)
	// The Project's own cap is what stopped the second, not the global one.
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the one it could not start: %v", len(over), over)
	}
	for job, why := range over {
		if !strings.Contains(why, "api") {
			t.Errorf("job %s was passed over for %q, want it to name the project", job, why)
		}
		if !strings.Contains(why, "1") {
			t.Errorf("job %s was passed over for %q, want it to name the cap", job, why)
		}
	}
}

func TestS2CapsFourProjectsRunFourRuns(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	for _, name := range []string{"api", "web", "cli", "docs"} {
		c.project(t, name, "", 1)
	}

	c.away(t)

	c.waitRunning(t, 4)
	// One per Project, rather than four of anything.
	projects := columnOf(c.status(t), "runs in progress:", 2)
	seen := map[string]bool{}
	for _, p := range projects {
		if seen[p] {
			t.Errorf("two runs are in the project %s:\n%s", p, c.status(t))
		}
		seen[p] = true
	}
	if len(seen) != 4 {
		t.Errorf("runs are in %d projects, want four: %v", len(seen), seen)
	}
}

func TestS3CapsTheGlobalCapBindsWhenThereAreMoreProjectsThanItAllows(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 2\n")
	for _, name := range []string{"api", "web", "cli"} {
		c.project(t, name, "", 1)
	}

	c.away(t)

	c.waitRunning(t, 2)
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the third: %v", len(over), over)
	}
	for job, why := range over {
		if !strings.Contains(why, "owl") || !strings.Contains(why, "2") {
			t.Errorf("job %s was passed over for %q, want it to name owl's own cap of 2", job, why)
		}
	}
}

func TestS4CapsAProjectThatSaysItIsHermeticRunsTwoOfItsOwn(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	c.project(t, "api", "maxParallelRuns: 2\n", 3)

	c.away(t)

	c.waitRunning(t, 2)
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the third: %v", len(over), over)
	}
	for job, why := range over {
		if !strings.Contains(why, "api") || !strings.Contains(why, "2") {
			t.Errorf("job %s was passed over for %q, want it to name the project's cap of 2", job, why)
		}
	}
}

func TestS5CapsAnIneligibleJobDoesNotBlockAYoungerEligibleOne(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	// The older Project holds its own cap of one with two Jobs; the younger
	// one has nothing in flight.
	c.project(t, "api", "", 2)
	c.project(t, "web", "", 1)

	c.away(t)

	c.waitRunning(t, 2)
	// One from each Project: the api Job that could not start did not stop the
	// web Job behind it.
	projects := columnOf(c.status(t), "runs in progress:", 2)
	if len(projects) != 2 || projects[0] == projects[1] {
		t.Fatalf("runs are in %v, want one in each project:\n%s", projects, c.status(t))
	}
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the second api job: %v", len(over), over)
	}
}

func TestS6CapsAJobThatIsPassedOverKeepsItsPosition(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	c.project(t, "api", "", 2)
	c.project(t, "web", "", 1)
	before := queuePositions(t, c.l)

	c.away(t)
	c.waitRunning(t, 2)

	// The Job that could not start is still pending, and still where it was:
	// being passed over is not being moved (ADR-0025).
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the second api job: %v", len(over), over)
	}
	after := queuePositions(t, c.l)
	for job := range over {
		if before[job] == "" {
			t.Fatalf("job %s was not in the queue to begin with: %v", job, before)
		}
		if after[job] != before[job] {
			t.Errorf("the passed-over job moved from position %s to %s", before[job], after[job])
		}
	}
}

// queuePositions is each queued Job's place, by id.
func queuePositions(t *testing.T, l *layout) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range strings.Split(mustOwl(t, l, "queue", "list").stdout, "\n") {
		fields := strings.Fields(row)
		if len(fields) < 2 || fields[0] == "POSITION" {
			continue
		}
		out[fields[1]] = fields[0]
	}
	return out
}

func TestS7CapsStatusSaysWhyEachPassedOverJobWasPassedOver(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 2\n")
	// api holds its own cap with two Jobs; cli is stopped by owl's own cap.
	c.project(t, "api", "", 2)
	c.project(t, "web", "", 1)
	c.project(t, "cli", "", 1)

	c.away(t)

	c.waitRunning(t, 2)
	over := c.passedOver(t)
	if len(over) != 2 {
		t.Fatalf("owl status passed over %d jobs, want two: %v\n%s", len(over), over, c.status(t))
	}
	var project, global int
	for job, why := range over {
		switch {
		case strings.Contains(why, "api"):
			project++
		case strings.Contains(why, "owl"):
			global++
		default:
			t.Errorf("job %s was passed over for %q, which names no cap", job, why)
		}
	}
	if project != 1 || global != 1 {
		t.Errorf("the reasons are %v, want one naming the project's cap and one owl's own", over)
	}
}

func TestS8CapsAJobOverItsAccountsCeilingIsPassedOver(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\naccounts:\n  spent:\n    limits:\n      fiveHourMax: 60\n")
	addAccount(t, c.l, "spent", testToken)
	addAccount(t, c.l, "fresh", testToken)
	c.project(t, "api", "account: spent\n", 1)
	c.project(t, "web", "account: fresh\n", 1)

	c.away(t)

	// The Run on spent says what that account has used, which is past what it
	// is held to, so Owl ends it and its Job waits (ADR-0020). The Project on
	// the account with headroom carries on.
	c.waitRunning(t, 1)
	if got := columnOf(c.status(t), "runs in progress:", 2); len(got) != 1 || got[0] != "web" {
		t.Fatalf("the run in flight is in %v, want the project on the account with headroom", got)
	}
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the one on the spent account: %v", len(over), over)
	}
	for job, why := range over {
		if !strings.Contains(why, "spent") {
			t.Errorf("job %s was passed over for %q, want it to name the account", job, why)
		}
		if !strings.Contains(why, "ceiling") {
			t.Errorf("job %s was passed over for %q, want it to say the ceiling stopped it", job, why)
		}
	}
}

func TestS9CapsAnAccountsOwnCapHoldsAcrossProjects(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\naccounts:\n  shared:\n    maxParallel: 1\n")
	addAccount(t, c.l, "shared", testToken)
	c.project(t, "api", "account: shared\n", 1)
	c.project(t, "web", "account: shared\n", 1)

	c.away(t)

	c.waitRunning(t, 1)
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the second: %v", len(over), over)
	}
	for job, why := range over {
		if !strings.Contains(why, "shared") || !strings.Contains(why, "1") {
			t.Errorf("job %s was passed over for %q, want it to name the account's cap of 1", job, why)
		}
	}
}

func TestS10CapsAnAccountWithNoCapIsHeldOnlyByTheOthers(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	addAccount(t, c.l, "shared", testToken)
	c.project(t, "api", "account: shared\n", 1)
	c.project(t, "web", "account: shared\n", 1)

	c.away(t)

	c.waitRunning(t, 2)
}

func TestS11CapsConcurrentRunsEachStreamTheirOwnLog(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	c.project(t, "api", "", 1)
	c.project(t, "web", "", 1)

	c.away(t)
	c.waitRunning(t, 2)

	// Each Run's log is its own Agent's output. The two Agents write the same
	// lines, so what tells them apart is the count: one log carrying both
	// would have twice what one Agent wrote.
	runs := columnOf(c.status(t), "runs in progress:", 0)
	if len(runs) != 2 {
		t.Fatalf("there are %d runs to read, want two", len(runs))
	}
	for _, run := range runs {
		out := mustOwl(t, c.l, "logs", run).stdout
		lines := 0
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.TrimSpace(line) != "" {
				lines++
			}
		}
		if lines != 2 {
			t.Errorf("run %s streamed %d lines, want the two its own agent wrote:\n%s", run, lines, out)
		}
	}
}

func TestS12CapsPauseFreezesEveryRunInFlight(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	c.project(t, "api", "", 1)
	c.project(t, "web", "", 1)
	c.away(t)
	c.waitRunning(t, 2)

	mustOwl(t, c.l, "pause")

	out := c.status(t)
	if n := strings.Count(out, "paused"); n < 2 {
		t.Errorf("owl status says %d runs are paused, want both:\n%s", n, out)
	}
	mustOwl(t, c.l, "resume")
	if out := c.status(t); strings.Contains(out, "paused") {
		t.Errorf("a run is still paused after owl resume:\n%s", out)
	}
}

func TestS13CapsComingBackToTheMachineFreezesEveryRunInFlight(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	c.project(t, "api", "", 1)
	c.project(t, "web", "", 1)
	c.away(t)
	c.waitRunning(t, 2)

	c.m.inUse(t)

	waitFor(t, "both runs to be frozen", func() bool {
		return strings.Count(c.status(t), "paused") >= 2
	})
	c.m.away(t)
	waitFor(t, "both runs to be going again", func() bool {
		return !strings.Contains(c.status(t), "paused")
	})
}

func TestS14CapsStartRefusesWhenEverythingIsAtItsCapAndSaysWhich(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 1\n")
	c.project(t, "api", "", 1)
	c.project(t, "web", "", 1)
	c.away(t)
	c.waitRunning(t, 1)

	res := runOwl(t, c.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start exited 0 with everything at its cap:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "owl") || !strings.Contains(res.stderr, "1") {
		t.Errorf("owl start says %q, want it to name the cap that is binding", res.stderr)
	}
}

func TestS15CapsACapThatIsNotOneIsRefused(t *testing.T) {
	for _, bad := range []string{"maxParallelRuns: 0\n", "maxParallelRuns: -1\n", "maxParallelRuns: many\n"} {
		l := newLayout(t)
		globalConfig(t, l, "apiVersion: codingowl.dev/v1\n"+bad)

		// A daemon whose own configuration cannot be read does not start: it
		// would otherwise run on numbers nobody wrote.
		p := startDaemon(t, l)

		if code := p.exit(t, 10*time.Second); code == 0 {
			t.Errorf("the daemon started with %q configured:\n%s", bad, p.out())
			continue
		}
		if !strings.Contains(p.out(), "maxParallelRuns") {
			t.Errorf("the daemon does not say what it refused for %q:\n%s", bad, p.out())
		}
	}
}

func TestS16CapsTheAppShowsWhatIsRunningAndWhyTheRestIsNot(t *testing.T) {
	src := filepath.Join(repoDir, "cmd", "owl-desktop", "frontend", "src")
	body := readFile(t, filepath.Join(src, "views", "Overview.tsx"))

	for _, want := range []string{"PassedOver", "Reason"} {
		if !strings.Contains(body, want) {
			t.Errorf("the overview view does not render %s", want)
		}
	}
	// And the app is told about them at all.
	api := readFile(t, filepath.Join(src, "lib", "api.ts"))
	if !strings.Contains(api, "PassedOver") {
		t.Errorf("the app is never told what was passed over:\n%s", api)
	}
}

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
	d       *daemonProc
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
	// Owl looks at the machine often unless a scenario says otherwise, so that
	// most of them do not wait on the clock.
	config := "apiVersion: codingowl.dev/v1\n" + global
	if !strings.Contains(global, "idle:") {
		config += "idle:\n  interval: 100ms\n"
	}
	l, m := watching(t, l, config)
	d := daemonUp(t, l)
	return &caps{l: l, m: m, d: d, release: release}
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
	// A slow look on purpose: the watcher starts what the caps allow each time
	// it looks (ADR-0021), and one that started a single Run per look would
	// need four looks and would not be finished in time.
	c := capsLayout(t, "maxParallelRuns: 4\nidle:\n  interval: 3s\n")
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
	// And one that says nothing, beside it: each Project is held to its own
	// cap, so reading one Project's file cannot answer for another's.
	c.project(t, "web", "", 2)

	c.away(t)

	c.waitRunning(t, 3)
	inProject := map[string]int{}
	for _, name := range columnOf(c.status(t), "runs in progress:", 2) {
		inProject[name]++
	}
	if inProject["api"] != 2 || inProject["web"] != 1 {
		t.Errorf("the runs in flight are %v, want two in api and one in web", inProject)
	}
	over := c.passedOver(t)
	if len(over) != 2 {
		t.Fatalf("owl status passed over %d jobs, want one from each project: %v", len(over), over)
	}
	var hermetic, ordinary int
	for job, why := range over {
		switch {
		case strings.Contains(why, "the project api is at its cap of 2 runs"):
			hermetic++
		case strings.Contains(why, "the project web is at its cap of 1 run"):
			ordinary++
		default:
			t.Errorf("job %s was passed over for %q, which names neither project's own cap", job, why)
		}
	}
	if hermetic != 1 || ordinary != 1 {
		t.Errorf("the reasons are %v, want each project held to its own cap", over)
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
	// Two Runs at once, and three Projects: one Job is passed over for its own
	// Project's cap and the one behind it stays queued, so that "still ahead of
	// what came after it" is something the scenario can see.
	c := capsLayout(t, "maxParallelRuns: 2\n")
	c.project(t, "api", "", 2)
	c.project(t, "web", "", 1)
	c.project(t, "cli", "", 1)
	before := queuePositions(t, c.l)

	c.away(t)
	c.waitRunning(t, 2)

	// The Jobs that could not start are still pending, and still where they
	// were: being passed over is not being moved (ADR-0025).
	over := c.passedOver(t)
	if len(over) != 2 {
		t.Fatalf("owl status passed over %d jobs, want the api job and the cli job: %v", len(over), over)
	}
	after := queuePositions(t, c.l)
	for job := range over {
		if before[job] == "" {
			t.Fatalf("job %s was not in the queue to begin with: %v", job, before)
		}
		if after[job] != before[job] {
			t.Errorf("the passed-over job moved from position %s to %s", before[job], after[job])
		}
		// And it is still ahead of what was queued after it, which started
		// while it waited.
		for other, was := range before {
			if other == job || was <= before[job] {
				continue
			}
			if after[other] != "" && after[other] < after[job] {
				t.Errorf("job %s was queued after the passed-over job and is now ahead of it", other)
			}
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
	// And they carry on to the end rather than merely being unfrozen.
	c.finish(t)
	waitFor(t, "both runs to finish", func() bool { return len(c.running(t)) == 0 })
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
	// The whole sentence: "owl" alone is free, because the CLI prefixes every
	// error with it.
	if !strings.Contains(res.stderr, "owl is at its cap of 1 run") {
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
		// The setting and the value, in the sentence they belong to, so that a
		// number that happens to appear in a timestamp does not answer for
		// them.
		value := strings.TrimSpace(strings.SplitN(bad, ":", 2)[1])
		if value == "many" {
			value = `"many"`
		}
		want := "maxParallelRuns: " + value + " is not a number of runs"
		if !strings.Contains(p.out(), want) {
			t.Errorf("the daemon does not say %q when it refuses %q:\n%s", want, bad, p.out())
		}
	}
}

func TestS16CapsTheAppShowsWhatIsRunningAndWhyTheRestIsNot(t *testing.T) {
	src := filepath.Join(repoDir, "cmd", "owl-desktop", "frontend", "src")
	body := readFile(t, filepath.Join(src, "views", "Overview.tsx"))

	// The Runs in flight, and each passed-over Job with the reason itself
	// rather than a column heading that happens to spell it.
	for _, want := range []string{
		`title="Running"`,
		`title="Passed over"`,
		"{p.Job.ID}",
		"{p.Job.Project}",
		"{p.Reason}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the overview view does not render %s", want)
		}
	}
	// And the app is told about them at all.
	api := readFile(t, filepath.Join(src, "lib", "api.ts"))
	if !strings.Contains(api, "o.PassedOver = list(o.PassedOver)") {
		t.Errorf("the app does not read what was passed over:\n%s", api)
	}
}

func TestS17CapsAProjectsCapThatIsNotOneRefusesTheProject(t *testing.T) {
	l := newLayout(t)
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\n")
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nmaxParallelRuns: 0\n", "configure owl")
	addProject(t, l, r)

	res := runOwl(t, l, "project", "show", "api")

	if res.code == 0 {
		t.Fatalf("owl project show exited 0 for a project whose cap is not one:\n%s", res.stdout)
	}
	for _, want := range []string{".coding-owl.yaml", "maxParallelRuns", "0"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, res.stderr)
		}
	}
	// And the daemon is fine: one Project's file is not everybody's.
	mustOwl(t, l, "status")
}

func TestS18CapsNothingRunsInParallelUntilSomebodyAsks(t *testing.T) {
	// A file that says nothing about it at all.
	c := capsLayout(t, "")
	c.project(t, "api", "", 1)
	c.project(t, "web", "", 1)

	c.away(t)

	c.waitRunning(t, 1)
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the second: %v", len(over), over)
	}
	for job, why := range over {
		if why != "owl is at its cap of 1 run" {
			t.Errorf("job %s was passed over for %q, want owl's own cap of one", job, why)
		}
	}
}

func TestS19CapsWhatWillNotClearItselfIsSaidOutLoud(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\naccounts:\n  spent:\n    limits:\n      fiveHourMax: 60\n")
	addAccount(t, c.l, "spent", testToken)
	// One Project, on an account whose window it reports past. Its Run is
	// ended and its Job waits for hours, which nothing but time will change.
	c.project(t, "api", "account: spent\n", 1)

	c.away(t)

	// Said on the line about nothing running, rather than left to the list: a
	// person looking at an idle machine is owed the reason it is idle.
	waitFor(t, "owl status to say why nothing is running", func() bool {
		return strings.Contains(maybeLine(c.status(t), "nothing is running"), "ceiling")
	})
	// And it is in the passed-over list too, because that is where a Job's own
	// reason lives.
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the one with no headroom: %v", len(over), over)
	}
	for job, why := range over {
		if !strings.Contains(why, "ceiling") {
			t.Errorf("job %s was passed over for %q, want the ceiling", job, why)
		}
	}
}

func TestS20CapsAProjectNobodyCanReadIsPassedOverNotAWall(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	// Registered while it reads, then broken on its base branch, which is
	// where Owl reads it from (ADR-0014).
	r := newRepo(t, c.l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\n", "configure owl")
	addProject(t, c.l, r)
	addJob(t, c.l, r.dir, "work in api", "--no-plan")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nmaxParallelRuns: 0\n", "break owl")
	c.project(t, "web", "", 1)

	c.away(t)

	// The younger Job runs: one Project nobody can read is not a wall the rest
	// of the queue stands behind.
	c.waitRunning(t, 1)
	if got := columnOf(c.status(t), "runs in progress:", 2); len(got) != 1 || got[0] != "web" {
		t.Fatalf("the run in flight is in %v, want the project that reads", got)
	}
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the one whose project is broken: %v", len(over), over)
	}
	for job, why := range over {
		if !strings.Contains(why, "could not be read") {
			t.Errorf("job %s was passed over for %q, want it to say its project could not be read", job, why)
		}
		if !strings.Contains(why, "maxParallelRuns") {
			t.Errorf("job %s was passed over for %q, want it to carry what the file said", job, why)
		}
	}
}

func TestS21CapsAFailureToStartIsRaisedNotBuried(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 2\n")
	// A plain file where the worktrees directory goes, so that making one
	// fails with something that is neither a cap nor a refusal: the daemon
	// logs at its default level, where a Debug line would not be seen at all.
	if err := os.WriteFile(filepath.Join(c.l.data, "coding-owl", "worktrees"), []byte("in the way\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.project(t, "api", "", 1)

	c.away(t)

	waitForLog(t, c.d, `level=ERROR msg="starting a run on an idle machine"`)
}

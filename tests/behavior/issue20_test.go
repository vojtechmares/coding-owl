package behavior_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// window is one of an Account's usage windows, as the Agent reports it.
type window struct {
	name        string
	utilization int
	resets      time.Time
}

// farReset is a window that starts again long after any scenario here ends,
// and no longer ahead than a window Owl keeps a figure about: the windows a
// tool reports reset within a week, so one that resets next year is a tool's
// mistake rather than a window.
var farReset = time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)

// far is that time, and past one that has already gone.
func far() time.Time  { return farReset }
func past() time.Time { return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) }

// reports is the line an Agent emits about the account's utilization. The
// shape is the tool's, and nothing outside the Driver reads it (ADR-0020).
func reports(windows ...window) string {
	limits := map[string]any{}
	for _, w := range windows {
		limits[w.name] = map[string]any{
			"utilization": w.utilization,
			"resets_at":   w.resets.UTC().Format(time.RFC3339),
		}
	}
	line, err := json.Marshal(map[string]any{
		"type": "system", "subtype": "usage_limits", "limits": limits,
	})
	if err != nil {
		panic(err)
	}
	return string(line)
}

// reporting is the stub agent's script with a usage line in the middle of it,
// which is where one arrives.
func reporting(windows ...window) []string {
	return []string{agentScript[0], reports(windows...), agentScript[1], agentScript[2]}
}

// limitsFor is the global configuration that gives the harness Account those
// ceilings, in the daemon's own file.
func limitsFor(limits string) string {
	if limits == "" {
		return "apiVersion: codingowl.dev/v1\n"
	}
	return "apiVersion: codingowl.dev/v1\naccounts:\n  " + harnessAccount + ":\n    limits:\n" + limits
}

// ceilingLayout is an agent layout whose Agent says those lines, with those
// ceilings on the Account its Projects run on. Nothing starts by itself: the
// machine every layout reports is one somebody is at.
func ceilingLayout(t *testing.T, script []string, limits string) (*layout, *stub) {
	t.Helper()
	l, s := agentLayout(t, script, 0)
	globalConfig(t, l, limitsFor(limits))
	return l, s
}

// usageRow is a row of the accounts table `owl status` prints.
type usageRow struct {
	account, window, used, ceiling, resets string
}

// usageRows reads that table, which is empty when there is nothing to say.
func usageRows(t *testing.T, out string) []usageRow {
	t.Helper()
	_, rest, ok := strings.Cut(out, "accounts:\n")
	if !ok {
		return nil
	}
	var rows []usageRow
	for _, ln := range strings.Split(rest, "\n") {
		if strings.TrimSpace(ln) == "" {
			break
		}
		if strings.HasPrefix(ln, "ACCOUNT") {
			continue
		}
		f := strings.Fields(ln)
		if len(f) < 5 {
			t.Fatalf("cannot read the account row %q in:\n%s", ln, out)
		}
		rows = append(rows, usageRow{account: f[0], window: f[1], used: f[2], ceiling: f[3], resets: f[4]})
	}
	return rows
}

// usageOf is the row for that Account and window, and fails when there is none.
func usageOf(t *testing.T, out, account, window string) usageRow {
	t.Helper()
	for _, row := range usageRows(t, out) {
		if row.account == account && row.window == window {
			return row
		}
	}
	t.Fatalf("no %s row for account %s in:\n%s", window, account, out)
	return usageRow{}
}

func TestS1CeilingWhatARunReportsIsKept(t *testing.T) {
	l, _ := ceilingLayout(t, reporting(
		window{"five_hour", 42, far()},
		window{"seven_day", 10, far().Add(12 * time.Hour)},
	), "")
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")

	finishedJob(t, l)

	out := mustOwl(t, l, "status").stdout
	five := usageOf(t, out, harnessAccount, "five-hour")
	if five.used != "42%" {
		t.Errorf("the five-hour window is at %q, want what the run reported", five.used)
	}
	if !strings.Contains(five.resets, far().Format(time.RFC3339)) {
		t.Errorf("the five-hour window resets at %q, want what the run reported", five.resets)
	}
	weekly := usageOf(t, out, harnessAccount, "weekly")
	if weekly.used != "10%" {
		t.Errorf("the weekly window is at %q, want what the run reported", weekly.used)
	}
	if !strings.Contains(weekly.resets, far().Add(12*time.Hour).Format(time.RFC3339)) {
		t.Errorf("the weekly window resets at %q, want what the run reported", weekly.resets)
	}
}

func TestS2CeilingAnAccountAboveItsCeilingIsNotScheduled(t *testing.T) {
	// The figure is reported first and the ceiling set afterwards, so that the
	// Job refused below is one nothing has happened to: a Run ended for
	// crossing a ceiling mid-flight is S5's, not this one's.
	l, _ := ceilingLayout(t, reporting(window{"five_hour", 75, far()}), "")
	daemonUp(t, l)
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
	globalConfig(t, l, limitsFor("      fiveHourMax: 60\n"))
	addJob(t, l, r.dir, "and another", "--no-plan")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran a job on an account over its ceiling:\n%s", res.stdout)
	}
	for _, want := range []string{harnessAccount, "75%", "60%", far().Format(time.RFC3339)} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, res.stderr)
		}
	}
	// The Job the queue would have taken, which is the one the refusal was
	// about: it is waiting its turn, not blamed for anything.
	out := mustOwl(t, l, "jobs", "show", "2").stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job still waiting its turn", got)
	}
	if got := maybeLine(out, "reason"); got != "(none)" && got != "" {
		t.Errorf("reason = %q, want nothing held against the job", got)
	}
}

func TestS3CeilingTheWeeklyCeilingTakesTheMostUtilizedWindow(t *testing.T) {
	l, _ := ceilingLayout(t, reporting(
		window{"seven_day", 10, far()},
		window{"seven_day_opus", 80, far()},
	), "")
	daemonUp(t, l)
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
	globalConfig(t, l, limitsFor("      weeklyMax: 50\n"))
	addJob(t, l, r.dir, "and another", "--no-plan")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran a job on an account over its weekly ceiling:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "80%") {
		t.Errorf("the refusal does not name the most utilized window:\n%s", res.stderr)
	}
	if strings.Contains(res.stderr, "10%") {
		t.Errorf("the refusal names the least utilized window:\n%s", res.stderr)
	}
}

func TestS4CeilingAReadingPastItsResetIsDiscarded(t *testing.T) {
	l, _ := ceilingLayout(t, reporting(window{"five_hour", 75, past()}), "      fiveHourMax: 60\n")
	daemonUp(t, l)
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
	addJob(t, l, r.dir, "and another", "--no-plan")

	run, job := startRun(t, l)

	if row := waitRun(t, l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want the job carried out on a window that has reset", row.outcome)
	}
	// And what Owl knew about that window is not reported as though it still
	// said something about this one.
	five := usageOf(t, mustOwl(t, l, "status").stdout, harnessAccount, "five-hour")
	if five.used != "(none)" {
		t.Errorf("status reports %q used from a reading whose window had already reset", five.used)
	}
}

func TestS5CeilingARunCrossingTheCeilingIsEnded(t *testing.T) {
	b := beatingReporting(t, []string{
		agentScript[0],
		reports(window{"five_hour", 75, far()}),
		"#wait",
		agentScript[2],
	}, "      fiveHourMax: 60\n")
	daemonUp(t, b.l)
	r := checkedProject(t, b.l, "apiVersion: codingowl.dev/v1\n")

	run, job := startRun(t, b.l)

	row := waitRun(t, b.l, job, run)
	if row.outcome != "interrupted" {
		t.Errorf("run outcome = %q, want interrupted", row.outcome)
	}
	b.gone(t)
	out := mustOwl(t, b.l, "jobs", "show", job).stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job queued again", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "ceiling") {
		t.Errorf("reason = %q, want it to say the ceiling was crossed", reason)
	}
	branch := line(t, out, "branch")
	if !strings.Contains(r.git("branch", "--list", branch), branch) {
		t.Errorf("branch %q is gone, so the next run cannot carry on in place", branch)
	}
	if st, err := os.Stat(line(t, out, "worktree")); err != nil || !st.IsDir() {
		t.Errorf("the worktree is gone: %v", err)
	}
}

func TestS6CeilingTheJobDoesNotStartAgainUntilTheReset(t *testing.T) {
	b := beatingReporting(t, []string{
		agentScript[0],
		reports(window{"five_hour", 75, far()}),
		"#wait",
		agentScript[2],
	}, "      fiveHourMax: 60\n")
	daemonUp(t, b.l)
	checkedProject(t, b.l, "apiVersion: codingowl.dev/v1\n")
	run, job := startRun(t, b.l)
	waitRun(t, b.l, job, run)

	res := runOwl(t, b.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran the job again on an account still over its ceiling:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, far().Format(time.RFC3339)) {
		t.Errorf("the refusal does not say when the window resets:\n%s", res.stderr)
	}
}

func TestS7CeilingAnAccountWithNoCeilingRunsAnyway(t *testing.T) {
	l, _ := ceilingLayout(t, reporting(window{"five_hour", 95, far()}), "")
	daemonUp(t, l)
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
	addJob(t, l, r.dir, "and another", "--no-plan")

	run, job := startRun(t, l)

	if row := waitRun(t, l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want the job carried out: nobody asked owl to stand down", row.outcome)
	}
}

func TestS8CeilingFiguresBelowTheCeilingStopNothing(t *testing.T) {
	l, _ := ceilingLayout(t, reporting(window{"five_hour", 42, far()}), "      fiveHourMax: 60\n")
	daemonUp(t, l)
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
	addJob(t, l, r.dir, "and another", "--no-plan")

	run, job := startRun(t, l)

	if row := waitRun(t, l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want the job carried out under its ceiling", row.outcome)
	}
}

func TestS9CeilingStatusShowsEachAccountAgainstItsCeilings(t *testing.T) {
	l, s := ceilingLayout(t, reporting(window{"five_hour", 75, far()}), "")
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)

	// A second Account, held to the same ceiling and under it: what the Agent
	// says next is 42%, and a Project of its own runs on it.
	addAccount(t, l, "spare", testToken)
	says(t, s, reporting(window{"five_hour", 42, far()}))
	other := newRepo(t, l, "other")
	other.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\naccount: spare\n", "configure owl")
	addProject(t, l, other)
	addJob(t, l, other.dir, "work", "--no-plan")
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\naccounts:\n"+
		"  "+harnessAccount+":\n    limits:\n      fiveHourMax: 60\n"+
		"  spare:\n    limits:\n      fiveHourMax: 60\n")
	run, job := startRun(t, l)
	waitRun(t, l, job, run)

	out := mustOwl(t, l, "status").stdout

	over := usageOf(t, out, harnessAccount, "five-hour")
	if over.used != "75%" || over.ceiling != "60%" {
		t.Errorf("the account over its ceiling reads %+v, want 75%% against 60%%", over)
	}
	under := usageOf(t, out, "spare", "five-hour")
	if under.used != "42%" || under.ceiling != "60%" {
		t.Errorf("the account under its ceiling reads %+v, want 42%% against 60%%", under)
	}
	// And which Account is waiting, and until when - that one and not the
	// other, or the report says nothing by saying it about everybody.
	waiting := waitingLines(out)
	if len(waiting) != 1 {
		t.Fatalf("status says %d accounts are waiting, want the one that is:\n%s", len(waiting), out)
	}
	if !strings.Contains(waiting[0], harnessAccount) || strings.Contains(waiting[0], "spare") {
		t.Errorf("status says %q is waiting, want the account over its ceiling", waiting[0])
	}
	if !strings.Contains(waiting[0], far().Format(time.RFC3339)) {
		t.Errorf("status says %q, want it to say until when", waiting[0])
	}
}

// waitingLines is what status says about the Accounts that are waiting.
func waitingLines(out string) []string {
	var lines []string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "waiting:") {
			lines = append(lines, ln)
		}
	}
	return lines
}

// says rewrites what the stub agent will say from its next invocation on.
func says(t *testing.T, s *stub, script []string) {
	t.Helper()
	if err := os.WriteFile(s.script, []byte(strings.Join(script, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestS10CeilingACeilingThatIsNotAPercentageIsRefused(t *testing.T) {
	for _, value := range []string{"soon", "200", "-5"} {
		l, _ := agentLayout(t, agentScript, 0)
		globalConfig(t, l, limitsFor("      fiveHourMax: "+value+"\n"))

		p := startDaemon(t, l)

		if code := p.exit(t, 10*time.Second); code == 0 {
			t.Errorf("the daemon started with a ceiling of %q", value)
		}
		for _, want := range []string{"config.yaml", "fiveHourMax"} {
			if !strings.Contains(p.out(), want) {
				t.Errorf("the daemon's refusal of %q does not name %s:\n%s", value, want, p.out())
			}
		}
	}
}

func TestS11CeilingAWindowThatStartsAgainReleasesTheJob(t *testing.T) {
	// A window that starts again while the scenario watches: long enough to
	// be refused for, short enough to wait out.
	soon := time.Now().Add(6 * time.Second).UTC().Truncate(time.Second)
	l, _ := ceilingLayout(t, reporting(window{"five_hour", 75, soon}), "      fiveHourMax: 60\n")
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	// The Run reports the figure and is ended for crossing the ceiling, which
	// leaves the Job pending and the Account over.
	run, job := startRun(t, l)
	if row := waitRun(t, l, job, run); row.outcome != "interrupted" {
		t.Fatalf("run outcome = %q, want the run ended for crossing the ceiling", row.outcome)
	}

	refused := runOwl(t, l, "start")

	if refused.code == 0 {
		t.Fatalf("owl start ran the job while the window was still open:\n%s", refused.stdout)
	}
	if !strings.Contains(refused.stderr, soon.Format(time.RFC3339)) {
		t.Errorf("the refusal does not say when the window starts again:\n%s", refused.stderr)
	}

	// And after it starts again, what Owl knew is about a window that is over.
	// This is waiting for a moment to arrive rather than for anything to
	// happen, so it is waited out rather than polled for.
	if left := time.Until(soon); left > 0 {
		sleep(left)
	}
	sleep(time.Second)

	// The window is still reported, because a ceiling is set on it; what is
	// gone is the figure, which was about the window that has started again.
	out := mustOwl(t, l, "status").stdout
	five := usageOf(t, out, harnessAccount, "five-hour")
	if five.used != "(none)" {
		t.Errorf("status still reports %q used of a window that has started again", five.used)
	}
	if five.ceiling != "60%" {
		t.Errorf("status reports the ceiling as %q, want the one that is set", five.ceiling)
	}
	again, sameJob := startRun(t, l)
	if sameJob != job {
		t.Fatalf("owl start took job %s, want the one that was waiting", sameJob)
	}
	if row := waitRun(t, l, job, again); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want the job carried out once the window started again", row.outcome)
	}
}

func TestS12CeilingTheAppShowsEachAccountAgainstItsCeilings(t *testing.T) {
	l, _ := ceilingLayout(t, reporting(window{"five_hour", 75, far()}), "      fiveHourMax: 60\n")
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
	app, _ := desktopApp(t, l)

	o, err := app.Overview()
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if len(o.Accounts) == 0 {
		t.Fatal("the app was told nothing about the accounts")
	}
	var found bool
	for _, a := range o.Accounts {
		if a.Name != harnessAccount {
			continue
		}
		for _, w := range a.Windows {
			if w.Name != "five-hour" {
				continue
			}
			found = true
			if w.Utilization != 75 || w.Ceiling != 60 {
				t.Errorf("the app was told %+v, want 75%% against a ceiling of 60%%", w)
			}
			if !w.Resets.Equal(far()) {
				t.Errorf("the app was told the window resets at %s, want %s", w.Resets, far())
			}
		}
	}
	if !found {
		t.Errorf("the app was told nothing about the five-hour window: %+v", o.Accounts)
	}

	// The window shows it, rather than the app merely knowing it.
	src := filepath.Join(repoDir, "cmd", "owl-desktop", "frontend", "src")
	body := readFile(t, filepath.Join(src, "views", "Overview.tsx"))
	for _, want := range []string{`title="Accounts"`, "Ceiling", "Resets"} {
		if !strings.Contains(body, want) {
			t.Errorf("the overview view does not render %s", want)
		}
	}
	// An Account nothing has been read about yet crosses the bindings as null
	// rather than as an empty list, and a view that read it as a list would
	// take the window down with it.
	if got := readFile(t, filepath.Join(src, "lib", "api.ts")); !strings.Contains(got, "a.Windows = list(") {
		t.Error("the bindings do not make an account's windows a list")
	}
}

// beatingReporting is the beating layout of issue #11 with a script of the
// scenario's own and ceilings on the Account its Projects run on: an Agent that
// starts a child, says these lines, and waits.
func beatingReporting(t *testing.T, script []string, limits string) *beating {
	t.Helper()
	l, s := agentLayout(t, script, 0)
	beats := filepath.Join(l.root, "heartbeat")
	l = l.withEnv(
		"OWL_FAKE_CLAUDE_CHILD="+beats,
		"OWL_FAKE_CLAUDE_WRITE="+fmt.Sprintf(`{"work.txt":"the agent's work\n","%s":"# Handoff\n\nhalf way through\n"}`, handoffPath),
		"OWL_FAKE_CLAUDE_COMMIT=1",
	)
	globalConfig(t, l, limitsFor(limits))
	return &beating{l: l, stub: s, beats: beats}
}

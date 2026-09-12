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

// far is a reset time nothing in a scenario will reach, and past one that has
// already gone.
func far() time.Time  { return time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC) }
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
		window{"seven_day", 10, far().AddDate(0, 0, 7)},
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
}

func TestS2CeilingAnAccountAboveItsCeilingIsNotScheduled(t *testing.T) {
	l, _ := ceilingLayout(t, reporting(window{"five_hour", 75, far()}), "      fiveHourMax: 60\n")
	daemonUp(t, l)
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
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
	), "      weeklyMax: 50\n")
	daemonUp(t, l)
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
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
	for _, row := range usageRows(t, mustOwl(t, l, "status").stdout) {
		if row.account == harnessAccount && row.window == "five-hour" && row.used == "75%" {
			t.Errorf("status still reports a reading whose window has reset: %+v", row)
		}
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
	l, _ := ceilingLayout(t, reporting(window{"five_hour", 75, far()}), "      fiveHourMax: 60\n")
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	finishedJob(t, l)
	// A second Account, under no ceiling and with nothing observed, is still
	// an Account somebody may be waiting on.
	addAccount(t, l, "spare", testToken)

	out := mustOwl(t, l, "status").stdout

	over := usageOf(t, out, harnessAccount, "five-hour")
	if over.used != "75%" || over.ceiling != "60%" {
		t.Errorf("the account over its ceiling reads %+v, want 75%% against 60%%", over)
	}
	if len(usageRows(t, out)) < 2 {
		t.Errorf("status reports only one account:\n%s", out)
	}
	if !strings.Contains(out, "spare") {
		t.Errorf("status does not report the account nothing was observed for:\n%s", out)
	}
	// And which Account is waiting, and until when.
	if !strings.Contains(out, "waiting") || !strings.Contains(out, far().Format(time.RFC3339)) {
		t.Errorf("status does not say which account is waiting and until when:\n%s", out)
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

func TestS11CeilingTheAppShowsEachAccountAgainstItsCeilings(t *testing.T) {
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
	body := readFile(t, filepath.Join(repoDir, "cmd", "owl-desktop", "frontend", "src", "views", "Overview.tsx"))
	for _, want := range []string{`title="Accounts"`, "Ceiling", "Resets"} {
		if !strings.Contains(body, want) {
			t.Errorf("the overview view does not render %s", want)
		}
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

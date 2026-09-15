package behavior_test

// Behavior tests for issue #103. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-103.md. They drive the built owl binary, and the daemon's
// API the way the desktop app does, against a daemon whose PATH puts the stub
// agent of issue #5 where Claude Code would be. What an Agent was given is what
// the stub recorded after --append-system-prompt; what owl jobs show says it
// was given is read from under the headings it prints.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver, for S7 and S8

	"github.com/vojtechmares/coding-owl/internal/run"
)

// The clauses a scenario configures a Project with, and changes it to.
const (
	gofmtClause  = "run gofmt before committing"
	linterClause = "run the linter before committing"
)

// nextPromptHeading heads the prompt the Job's next Run will be given.
const nextPromptHeading = "the next run will be given this system prompt:"

// givenPromptPhrase is how every heading of a prompt a Run was given ends,
// whatever Runs it names.
const givenPromptPhrase = "was given this system prompt:"

// The Runs a heading or a line names, in the wording the sheet quotes: `run
// <id> was` for one Run, `runs <id>, <id> were` for several. The one Run is the
// first group and the several the second, so namedRuns reads either.
var (
	// givenPromptRE heads a prompt the named Runs were given.
	givenPromptRE = regexp.MustCompile(`^(?:run (\d+) was|runs (\d+(?:, \d+)+) were) given this system prompt:$`)
	// unrecordedPromptRE is the line for Runs whose prompt was not recorded.
	unrecordedPromptRE = regexp.MustCompile(`^the system prompt (?:run (\d+) was|runs (\d+(?:, \d+)+) were) given was not recorded$`)
)

// namedRuns is the Run ids a match of one of those names, whichever of its
// wordings matched.
func namedRuns(m []string) []string {
	return strings.Split(m[1]+m[2], ", ")
}

// printedPrompt is one system prompt owl jobs show printed: the Runs its
// heading says were given it, or that it is what the next Run will be given,
// and the prompt under the heading.
type printedPrompt struct {
	runs []string
	next bool
	text string
}

// printedPrompts reads every system prompt owl jobs show printed, in the order
// it printed them. A prompt runs from its heading to the next heading of any
// kind, or to the end of the output, less the blank line before what follows.
func printedPrompts(out string) []printedPrompt {
	var got []printedPrompt
	var open *printedPrompt
	var body []string
	flush := func() {
		if open != nil {
			open.text = strings.TrimRight(strings.Join(body, "\n"), "\n")
			got = append(got, *open)
		}
		open, body = nil, nil
	}
	for _, ln := range strings.Split(out, "\n") {
		m := givenPromptRE.FindStringSubmatch(ln)
		switch {
		case m != nil:
			flush()
			open = &printedPrompt{runs: namedRuns(m)}
		case ln == nextPromptHeading:
			flush()
			open = &printedPrompt{next: true}
		case unrecordedPromptRE.MatchString(ln) || ln == "verifier system prompt:":
			flush()
		case open != nil:
			body = append(body, ln)
		}
	}
	flush()
	return got
}

// unrecordedPrompts is the Runs owl jobs show says it has no recorded prompt
// for, line by line.
func unrecordedPrompts(out string) [][]string {
	var got [][]string
	for _, ln := range strings.Split(out, "\n") {
		if m := unrecordedPromptRE.FindStringSubmatch(ln); m != nil {
			got = append(got, namedRuns(m))
		}
	}
	return got
}

// recordedPrompts is what every Agent the stub stood in for was given after
// --append-system-prompt, in the order they were started.
func recordedPrompts(t *testing.T, s *stub) []string {
	t.Helper()
	var out []string
	for _, inv := range s.invocations(t) {
		out = append(out, inv.flag(t, "--append-system-prompt"))
	}
	return out
}

// awaitInvoked waits for the stub agent to have recorded an invocation, which
// is how a scenario knows an Agent it holds open has started.
func awaitInvoked(t *testing.T, s *stub) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(s.argv); err == nil && strings.TrimSpace(string(data)) != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the stub agent was not invoked within 20s")
}

// clausedProject registers a Project named api whose base branch carries one
// unattended clause.
func clausedProject(t *testing.T, l *layout, clause string) *repo {
	t.Helper()
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - "+clause+"\n", "configure owl")
	addProject(t, l, r)
	return r
}

// changeClause changes a Project's clause on its base branch, keeping the rest
// of the file as it is - the Account the harness gave the Project among it.
func changeClause(t *testing.T, r *repo, from, to string) {
	t.Helper()
	body := baseConfig(r, ".coding-owl.yaml")
	if !strings.Contains(body, from) {
		t.Fatalf("the project's configuration does not hold %q:\n%s", from, body)
	}
	r.commit(".coding-owl.yaml", strings.Replace(body, from, to, 1), "change the clause")
}

// unrecord makes a Job's Runs into Runs from before prompts were recorded: it
// stops the daemon, empties what their prompts were recorded as, and starts
// the daemon again. A database from before holds exactly that, because every
// Run already there is given an empty prompt when the database gains
// somewhere to keep one.
func unrecord(t *testing.T, l *layout, d *daemonProc, job string) *daemonProc {
	t.Helper()
	stopDaemon(t, d)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(l.data, "coding-owl", "owl.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer func() { _ = db.Close() }()
	res, err := db.Exec(`UPDATE runs SET system_prompt = '' WHERE job_id = ?`, id(t, job))
	if err != nil {
		t.Fatalf("emptying the recorded prompts: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		t.Fatalf("emptied the prompts of %d runs (%v), want the job's runs", n, err)
	}
	return daemonUp(t, l)
}

// frontendDetails is a Job's detail as the desktop frontend receives it: Wails
// hands the frontend the JSON of what the app returns, so this is read from
// that JSON by the names the frontend reads.
type frontendDetails struct {
	SystemPrompt string
	Runs         []struct {
		ID           int64
		SystemPrompt string
	}
}

// frontendJob asks the desktop app for a Job and reads the answer the way its
// frontend does.
func frontendJob(t *testing.T, l *layout, job string) frontendDetails {
	t.Helper()
	app, _ := desktopApp(t, l)
	d, err := app.Job(id(t, job))
	if err != nil {
		t.Fatalf("the app could not read job %s: %v", job, err)
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var out frontendDetails
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// onlyPrompt is the one prompt owl jobs show printed, failing unless there is
// exactly one.
func onlyPrompt(t *testing.T, out string) printedPrompt {
	t.Helper()
	got := printedPrompts(out)
	if len(got) != 1 {
		t.Fatalf("owl jobs show printed %d system prompts, want 1:\n%s", len(got), out)
	}
	return got[0]
}

// wantGiven fails unless a printed prompt is headed as given to exactly these
// Runs, and holds exactly the text an Agent was given.
func wantGiven(t *testing.T, p printedPrompt, text string, runs ...string) {
	t.Helper()
	if p.next || strings.Join(p.runs, ",") != strings.Join(runs, ",") {
		t.Errorf("a prompt is headed as given to runs %v (next run: %v), want runs %v", p.runs, p.next, runs)
	}
	if p.text != text {
		t.Errorf("the prompt printed for runs %v is\n%s\nbut the agent was given\n%s", runs, p.text, text)
	}
}

// withClause is the standing contract with one Project clause after it, as the
// Agent is given it.
func withClause(clause string) string {
	return run.Contract + "\n\nThis project also asks that you:\n\n- " + clause
}

func TestS1PromptIsRecordedWhenTheRunStarts(t *testing.T) {
	l, s := agentLayout(t, []string{agentScript[0], "#wait", agentScript[2]}, 0)
	daemonUp(t, l)
	r := clausedProject(t, l, gofmtClause)
	addJob(t, l, r.dir, "work", "--no-plan")

	runID, job := startRun(t, l)
	awaitInvoked(t, s)
	during := mustOwl(t, l, "jobs", "show", job).stdout
	s.let(t)
	after := finished(t, l, job)

	given := s.invoked(t).flag(t, "--append-system-prompt")
	if rows := runRows(t, during); len(rows) != 1 || rows[0].ended != "(none)" {
		t.Fatalf("the run had ended before it was looked at:\n%s", during)
	}
	wantGiven(t, onlyPrompt(t, during), given, runID)
	wantGiven(t, onlyPrompt(t, after), given, runID)
}

func TestS2PromptGivenToSeveralRunsIsPrintedOnce(t *testing.T) {
	l, s := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	plannedJob(t, l, "work")
	planning, job := phase(t, l)
	executing, _ := phase(t, l)

	out := mustOwl(t, l, "jobs", "show", job).stdout

	given := recordedPrompts(t, s)
	if len(given) != 2 || given[0] != given[1] {
		t.Fatalf("the agents were given %q, want two runs given the same prompt", given)
	}
	wantGiven(t, onlyPrompt(t, out), given[0], planning.id, executing.id)
	if n := strings.Count(out, run.Contract); n != 1 {
		t.Errorf("the standing contract is printed %d times, want once:\n%s", n, out)
	}
}

func TestS3PromptOfARunOutlivesAChangeToTheClauses(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := clausedProject(t, l, gofmtClause)
	addJob(t, l, r.dir, "work", "--no-plan")
	runID, job := startRun(t, l)
	before := finished(t, l, job)
	if state := line(t, before, "state"); state != "review" {
		t.Fatalf("the job is %s after its run, want review:\n%s", state, before)
	}

	changeClause(t, r, gofmtClause, linterClause)
	after := mustOwl(t, l, "jobs", "show", job).stdout

	given := s.invoked(t).flag(t, "--append-system-prompt")
	wantGiven(t, onlyPrompt(t, before), given, runID)
	wantGiven(t, onlyPrompt(t, after), given, runID)
	if strings.Contains(after, linterClause) {
		t.Errorf("owl jobs show prints the clause the run was never given:\n%s", after)
	}
}

func TestS4PromptOfAJobWithNoRunsIsTheNextRuns(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := clausedProject(t, l, gofmtClause)
	addJob(t, l, r.dir, "work", "--no-plan")

	out := mustOwl(t, l, "jobs", "show", "1").stdout

	p := onlyPrompt(t, out)
	if !p.next {
		t.Errorf("the prompt of a job with no runs is headed as given to runs %v, want the next run's:\n%s", p.runs, out)
	}
	if p.text != withClause(gofmtClause) {
		t.Errorf("the next run's prompt is\n%s\nwant the contract with the project's clause after it", p.text)
	}
	if got := unrecordedPrompts(out); len(got) != 0 {
		t.Errorf("a job with no runs reports runs %v with no recorded prompt:\n%s", got, out)
	}
	if strings.Contains(out, givenPromptPhrase) {
		t.Errorf("a job with no runs says a run was given a system prompt:\n%s", out)
	}

	_, job := startRun(t, l)
	finished(t, l, job)
	if given := s.invoked(t).flag(t, "--append-system-prompt"); given != p.text {
		t.Errorf("the first run was given\n%s\nbut owl jobs show said it would be given\n%s", given, p.text)
	}
}

func TestS5PromptOfTheNextRunIsShownWhenItDiffers(t *testing.T) {
	l, s := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	r := clausedProject(t, l, gofmtClause)
	addJob(t, l, r.dir, "work")
	planning, job := phase(t, l)
	if state := jobState(t, l, job); state != "pending" {
		t.Fatalf("the planned job is %s after planning, want pending", state)
	}

	changeClause(t, r, gofmtClause, linterClause)
	out := mustOwl(t, l, "jobs", "show", job).stdout

	got := printedPrompts(out)
	if len(got) != 2 {
		t.Fatalf("owl jobs show printed %d system prompts, want the planning run's and the next run's:\n%s", len(got), out)
	}
	wantGiven(t, got[0], recordedPrompts(t, s)[0], planning.id)
	if !strings.Contains(got[0].text, gofmtClause) {
		t.Errorf("the planning run's prompt does not hold the clause it was given:\n%s", got[0].text)
	}
	if !got[1].next {
		t.Errorf("the second prompt is headed as given to runs %v, want the next run's:\n%s", got[1].runs, out)
	}
	if !strings.Contains(got[1].text, linterClause) || strings.Contains(got[1].text, gofmtClause) {
		t.Errorf("the next run's prompt is not the changed one:\n%s", got[1].text)
	}

	phase(t, l)
	if given := recordedPrompts(t, s); len(given) != 2 || given[1] != got[1].text {
		t.Errorf("the execution run was given\n%v\nbut owl jobs show said it would be given\n%s", given, got[1].text)
	}
}

func TestS6PromptOfTheNextRunIsNotRepeatedWhenTheSame(t *testing.T) {
	l, s := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	plannedJob(t, l, "work")
	planning, job := phase(t, l)
	if state := jobState(t, l, job); state != "pending" {
		t.Fatalf("the planned job is %s after planning, want pending", state)
	}

	out := mustOwl(t, l, "jobs", "show", job).stdout

	wantGiven(t, onlyPrompt(t, out), recordedPrompts(t, s)[0], planning.id)
	if strings.Contains(out, nextPromptHeading) {
		t.Errorf("owl jobs show repeats a prompt the next run shares with the last:\n%s", out)
	}
	if n := strings.Count(out, run.Contract); n != 1 {
		t.Errorf("the standing contract is printed %d times, want once:\n%s", n, out)
	}
}

func TestS7PromptNotRecordedIsNotSubstituted(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	d := daemonUp(t, l)
	runnableJob(t, l, "work")
	runID, job := startRun(t, l)
	if state := line(t, finished(t, l, job), "state"); state != "review" {
		t.Fatalf("the job is %s after its run, want review", state)
	}
	unrecord(t, l, d, job)

	out := mustOwl(t, l, "jobs", "show", job).stdout

	if got := unrecordedPrompts(out); len(got) != 1 || strings.Join(got[0], ",") != runID {
		t.Errorf("owl jobs show reports runs %v with no recorded prompt, want run %s:\n%s", got, runID, out)
	}
	if got := printedPrompts(out); len(got) != 0 {
		t.Errorf("owl jobs show printed %d system prompts for a job whose only run has none recorded:\n%s", len(got), out)
	}
	if strings.Contains(out, givenPromptPhrase) || strings.Contains(out, nextPromptHeading) {
		t.Errorf("owl jobs show heads a prompt for a job whose only run has none recorded:\n%s", out)
	}
	if strings.Contains(out, run.Contract) {
		t.Errorf("today's prompt stands in for the one that was not recorded:\n%s", out)
	}
	seen := frontendJob(t, l, job)
	if len(seen.Runs) != 1 || seen.Runs[0].SystemPrompt != "" {
		t.Errorf("the app reports the run's prompt as %+v, want it empty", seen.Runs)
	}
	if seen.SystemPrompt != "" {
		t.Errorf("the app reports the job's prompt as\n%s\nwant it empty: its most recent run's was not recorded", seen.SystemPrompt)
	}
}

func TestS8PromptNotRecordedStillShowsTheNextRuns(t *testing.T) {
	l, _ := planningLayout(t, "# Handoff\n\nstep one done\n")
	d := daemonUp(t, l)
	plannedJob(t, l, "work")
	planning, job := phase(t, l)
	unrecord(t, l, d, job)

	out := mustOwl(t, l, "jobs", "show", job).stdout

	if got := unrecordedPrompts(out); len(got) != 1 || strings.Join(got[0], ",") != planning.id {
		t.Errorf("owl jobs show reports runs %v with no recorded prompt, want run %s:\n%s", got, planning.id, out)
	}
	p := onlyPrompt(t, out)
	if !p.next || p.text != run.Contract {
		t.Errorf("the prompt printed is headed as given to runs %v (next run: %v), want the contract as the next run's:\n%s", p.runs, p.next, out)
	}
	if n := strings.Count(out, run.Contract); n != 1 {
		t.Errorf("the standing contract is printed %d times, want once:\n%s", n, out)
	}
	if strings.Index(out, "was not recorded") > strings.Index(out, nextPromptHeading) {
		t.Errorf("the next run's prompt comes before what the last run was given:\n%s", out)
	}
}

func TestS9PromptOnTheAPIIsWhatEachRunWasGiven(t *testing.T) {
	l, s := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	r := clausedProject(t, l, gofmtClause)
	addJob(t, l, r.dir, "work")
	addJob(t, l, r.dir, "later", "--no-plan")
	planning, job := phase(t, l)
	changeClause(t, r, gofmtClause, linterClause)
	executing, again := phase(t, l)
	if again != job {
		t.Fatalf("the second run was of job %s, want job %s carried out", again, job)
	}
	later := jobID(t, queueList(t, l), "later")

	first := frontendJob(t, l, job)
	second := frontendJob(t, l, later)

	given := recordedPrompts(t, s)
	if len(given) != 2 || given[0] == given[1] {
		t.Fatalf("the agents were given %q, want two different prompts", given)
	}
	if len(first.Runs) != 2 {
		t.Fatalf("the app reports %d runs of job %s, want 2", len(first.Runs), job)
	}
	for i, want := range []string{planning.id, executing.id} {
		got := first.Runs[i]
		if id(t, want) != got.ID || got.SystemPrompt != given[i] {
			t.Errorf("the app reports run %d with the prompt\n%s\nbut run %s was given\n%s", got.ID, got.SystemPrompt, want, given[i])
		}
	}
	if first.SystemPrompt != given[1] {
		t.Errorf("the app reports the job's prompt as\n%s\nwant the most recent run's\n%s", first.SystemPrompt, given[1])
	}
	if second.SystemPrompt != withClause(linterClause) {
		t.Errorf("the app reports the prompt of a job with no runs as\n%s\nwant the one its next run will be given", second.SystemPrompt)
	}
	if len(second.Runs) != 0 {
		t.Errorf("the job that has not run reports runs %+v", second.Runs)
	}
}

func TestS10PromptGivenToTheAgentIsUnchanged(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	api := newRepo(t, l, "api")
	api.commit(".coding-owl.yaml",
		"apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - "+gofmtClause+"\n  - \"  \"\n", "configure owl")
	addProject(t, l, api)
	web := project(t, l, "web")
	addJob(t, l, api.dir, "api work", "--no-plan")
	addJob(t, l, web.dir, "web work", "--no-plan")

	for range 2 {
		_, job := startRun(t, l)
		finished(t, l, job)
	}

	given := recordedPrompts(t, s)
	if len(given) != 2 {
		t.Fatalf("the agents were given %d prompts, want 2", len(given))
	}
	if given[0] != withClause(gofmtClause) {
		t.Errorf("the agent of a project with clauses was given\n%s\nwant the contract, a blank line, the heading, a blank line and the clause", given[0])
	}
	if given[1] != run.Contract {
		t.Errorf("the agent of a project with no clauses was given\n%s\nwant exactly the standing contract", given[1])
	}
}

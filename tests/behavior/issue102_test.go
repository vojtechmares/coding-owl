package behavior_test

// Behavior tests for issue #102. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-102.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// read what owl jobs show prints of text that came from outside Owl. A value
// the daemon would refuse to write itself is written into its database
// directly, as the issue #11 scenarios do for what an earlier daemon left.

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// pwned is an escape a document from outside Owl can carry: it retitles the
// terminal window, and then erases the line the cursor is on.
const pwned = "\x1b]0;pwned\x07\x1b[2K"

// pwnedShown is that escape as text, which is how owl jobs show is to print it.
const pwnedShown = `\x1b]0;pwned\x07\x1b[2K`

// shown is text as owl jobs show is to print it: with the escape made visible,
// and everything else - newlines and tabs included - exactly as it was.
func shown(text string) string {
	return strings.ReplaceAll(text, pwned, pwnedShown)
}

// yamlString spells text as a YAML double-quoted scalar. A configuration file
// may not carry a control character as it is, and Go's quoting of these
// strings - `\x1b`, `\a`, `\t` - is also YAML's, which reads the character
// back.
func yamlString(text string) string {
	return strconv.Quote(text)
}

// wantClean fails unless owl jobs show printed no control character but
// newlines and tabs, which is what leaves a terminal nothing to obey.
func wantClean(t *testing.T, out string) {
	t.Helper()
	for i, ln := range strings.Split(out, "\n") {
		if strings.ContainsFunc(ln, func(r rune) bool { return unicode.IsControl(r) && r != '\t' }) {
			t.Errorf("line %d of owl jobs show reaches the terminal with a control character in it: %q", i+1, ln)
		}
	}
}

// documentHeadings are the headings owl jobs show prints a document under, in
// the order it prints them.
var documentHeadings = []string{"plan", "handoff", "system prompt", "verifier system prompt"}

// document is what owl jobs show printed under a document's heading: every
// line of it, blank ones included, up to the heading after it or the end of
// the report.
func document(t *testing.T, out, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(out, "\n"+heading+":\n")
	if !ok {
		t.Fatalf("owl jobs show prints no %s document:\n%s", heading, out)
	}
	for _, next := range documentHeadings {
		if next == heading {
			continue
		}
		if doc, _, found := strings.Cut(rest, "\n\n"+next+":"); found {
			return doc
		}
	}
	return strings.TrimSuffix(rest, "\n")
}

// inDatabase writes to the daemon's database directly, for a value the daemon
// words itself or would refuse to write: what owl jobs show prints is what the
// daemon reports, whoever wrote it.
func inDatabase(t *testing.T, l *layout, write func(context.Context, *store.Store) error) {
	t.Helper()
	st, _, err := store.Open(filepath.Join(l.data, "coding-owl", "owl.db"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer func() { _ = st.Close() }()
	if err := write(context.Background(), st); err != nil {
		t.Fatalf("writing to the database: %v", err)
	}
}

// ids reads a Run or Job id owl printed.
func ids(t *testing.T, id string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		t.Fatalf("%q is not an id: %v", id, err)
	}
	return n
}

// ranJob runs one Job added with --no-plan to the end of its Run, and returns
// the ids of the Run and the Job.
func ranJob(t *testing.T, l *layout) (run, job string) {
	t.Helper()
	runnableJob(t, l, "work")
	run, job = startRun(t, l)
	waitRun(t, l, job, run)
	return run, job
}

// escapedHandoff is a handoff an Agent could write: the escape, a line
// indented with a tab, and a third line.
const escapedHandoff = "step one: read the tests " + pwned + "\n\tthen the parser\nstep two: fix it\n"

func TestS1JobsShowShowsTheEscapeInThePlanAsText(t *testing.T) {
	l, _ := planningLayout(t, escapedHandoff)
	daemonUp(t, l)
	plannedJob(t, l, "work")

	_, job := phase(t, l)
	out := mustOwl(t, l, "jobs", "show", job).stdout

	want := strings.TrimRight(shown(escapedHandoff), "\n")
	if got := document(t, out, "plan"); got != want {
		t.Errorf("the plan section is\n%q\nwant\n%q", got, want)
	}
	wantClean(t, out)
}

func TestS2JobsShowShowsTheEscapeInTheHandoffAsText(t *testing.T) {
	l, _ := writingLayout(t, map[string]string{handoffPath: escapedHandoff}, true, agentScript)
	daemonUp(t, l)

	_, job := ranJob(t, l)
	out := mustOwl(t, l, "jobs", "show", job).stdout

	want := strings.TrimRight(shown(escapedHandoff), "\n")
	if got := document(t, out, "handoff"); got != want {
		t.Errorf("the handoff section is\n%q\nwant\n%q", got, want)
	}
	wantClean(t, out)
}

func TestS3JobsShowShowsTheEscapeInAProjectsClauseAsText(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - "+
		yamlString("run gofmt before committing "+pwned)+"\n")

	out, _ := finishedJob(t, l)

	given := s.invoked(t).flag(t, "--append-system-prompt")
	if !strings.Contains(given, pwned) {
		t.Fatalf("the agent was not given the project's clause with its escape, so there is nothing to show:\n%q", given)
	}
	if got, want := document(t, out, "system prompt"), shown(given); got != want {
		t.Errorf("the system prompt section is\n%q\nwant what the agent was given, with the escape shown as text\n%q", got, want)
	}
	wantClean(t, out)
}

func TestS4JobsShowPrintsTheVerifiersSystemPromptAsItWasGiven(t *testing.T) {
	l, s := verifying(t, "verdict: pass\n\nThe change does what the plan said.\n")
	daemonUp(t, l)
	checkedProject(t, l, verifiedConfig)

	out, _ := finishedJob(t, l)

	given := verifyInvocation(t, s).flag(t, "--append-system-prompt")
	if got := document(t, out, "verifier system prompt"); got != given {
		t.Errorf("the verifier system prompt section is\n%q\nwant exactly what the verifying agent was given\n%q", got, given)
	}
	wantClean(t, out)
}

func TestS5JobsShowShowsTheEscapeInAChecksNameAndOutputAsText(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	name := "lint " + pwned
	// The command spells the escape in printf's own octal, so what it prints
	// holds the characters themselves.
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\nchecks:\n  - name: "+yamlString(name)+"\n"+
		"    run: |\n      printf 'lint is unhappy \\033]0;pwned\\007\\033[2K\\n\\tat line two\\n'; exit 1\n")

	out, _ := finishedJob(t, l)

	row := wantCheck(t, checks(t, out), shown(name), "failed")
	if want := "lint is unhappy " + pwnedShown + "\n\tat line two\n"; row.output != want {
		t.Errorf("the check's output is\n%q\nwant\n%q", row.output, want)
	}
	wantClean(t, out)
}

func TestS6JobsShowShowsTheEscapeInACheckReasonAsText(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	run, job := ranJob(t, l)
	reason := "could not be run: " + pwned
	inDatabase(t, l, func(ctx context.Context, st *store.Store) error {
		return st.SaveCheckResults(ctx, ids(t, run), []store.CheckResult{{
			Name: "lint", Command: "make lint", ExitCode: -1, Reason: reason, Verifier: "command",
		}})
	})

	out := mustOwl(t, l, "jobs", "show", job).stdout

	if row := wantCheck(t, checks(t, out), "lint", "failed"); row.why != shown(reason) {
		t.Errorf("the check's reason is %q, want %q", row.why, shown(reason))
	}
	wantClean(t, out)
}

func TestS7JobsShowShowsTheEscapeInARunsSkillsAsText(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	run, job := ranJob(t, l)
	read := store.RunSkill{
		Name:   "go-review" + pwned,
		Source: "github.com/x/go-review" + pwned,
		Ref:    "v1.4.0" + pwned,
		Commit: "0123456789abcdef0123456789abcdef01234567",
		Digest: "sha256:" + strings.Repeat("0", 64),
	}
	inDatabase(t, l, func(ctx context.Context, st *store.Store) error {
		return st.SetRunSkills(ctx, ids(t, run), []store.RunSkill{read})
	})

	out := mustOwl(t, l, "jobs", "show", job).stdout

	rows := section(t, out, "skills:")
	lines := strings.Split(rows, "\n")
	if len(lines) != 2 {
		t.Fatalf("the skills section holds %d lines, want the run's heading and one skill:\n%s", len(lines), out)
	}
	fields := strings.Fields(lines[1])
	if len(fields) != 4 {
		t.Fatalf("cannot read the skill row %q, want name, source, ref and commit:\n%s", lines[1], out)
	}
	for i, want := range []string{shown(read.Name), shown(read.Source), shown(read.Ref)} {
		if fields[i] != want {
			t.Errorf("the skill row gives %q, want %q", fields[i], want)
		}
	}
	wantClean(t, out)
}

func TestS8JobsShowShowsTheEscapeInARunsPermissionsAsText(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	run, job := ranJob(t, l)
	rule := "Bash(make " + pwned + ":*)"
	inDatabase(t, l, func(ctx context.Context, st *store.Store) error {
		return st.SetRunPermissions(ctx, ids(t, run), []string{"Read", rule})
	})

	out := mustOwl(t, l, "jobs", "show", job).stdout

	got := runPermissions(t, out, run)
	if want := []string{"Read", shown(rule)}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the run's permissions are %q, want %q", got, want)
	}
	wantClean(t, out)
}

func TestS9JobsShowShowsTheEscapeInAPhasesModelAndEffortAsText(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	model, effort := "opus"+pwned, "high"+pwned
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\nphases:\n  execute:\n"+
		"    model: "+yamlString(model)+"\n    effort: "+yamlString(effort)+"\n")
	job := jobID(t, queueList(t, l), "work")

	out := mustOwl(t, l, "jobs", "show", job).stdout

	rows := phaseRows(t, out)
	want := strings.Join([]string{"execute", shown(model), "project", shown(effort), "project"}, "|")
	if len(rows) != 1 || rows[0] != want {
		t.Errorf("the phases section is %q, want [%q]", rows, want)
	}
	wantClean(t, out)
}

func TestS10JobsShowShowsTheEscapeInTheRunTableAsText(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	_, job := ranJob(t, l)
	runPhase := "execute" + pwned
	logPath := filepath.Join(l.state, "coding-owl", "logs", "second"+pwned+".jsonl")
	var second int64
	inDatabase(t, l, func(ctx context.Context, st *store.Store) error {
		r, err := st.StartRun(ctx, store.Run{JobID: ids(t, job), Started: time.Now(), LogPath: logPath, Phase: runPhase})
		if err != nil {
			return err
		}
		second = r.ID
		return st.FinishRun(ctx, r.ID, time.Now(), "succeeded", "", 0)
	})

	out := mustOwl(t, l, "jobs", "show", job).stdout

	rows := runRows(t, out)
	if len(rows) != 2 || rows[1].id != strconv.FormatInt(second, 10) {
		t.Fatalf("owl jobs show reports runs %+v, want the finished run and run %d after it:\n%s", rows, second, out)
	}
	for _, c := range []struct{ what, got, want string }{
		{"phase", rows[1].phase, shown(runPhase)},
		{"state", rows[1].outcome, "succeeded"},
		{"log", rows[1].log, shown(logPath)},
	} {
		if c.got != c.want {
			t.Errorf("the run's %s is %q, want %q", c.what, c.got, c.want)
		}
	}
	wantClean(t, out)
}

// plainHandoff is a handoff with nothing to escape in it, and everything a
// document is printed with on purpose: lines, a tab, a blank line, and text
// that is not ASCII.
const plainHandoff = "step one: read the tests\n\tthen the parser\n\nžluťoučký kůň úpěl ďábelské ódy\n"

func TestS11JobsShowPrintsAReportWithNothingToEscapeAsItWasWritten(t *testing.T) {
	l, s := planningLayout(t, plainHandoff)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	printed := "first line\n\tsecond line, indented\nčtvrtá řádka\n"
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\n"+
		"unattendedClauses:\n  - "+yamlString("keep\tthe tab and the háček")+"\n"+
		"checks:\n  - name: lint\n"+
		"    run: |\n      printf 'first line\\n\\tsecond line, indented\\nčtvrtá řádka\\n'; exit 1\n",
		"configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	phase(t, l)
	_, job := phase(t, l)
	out := mustOwl(t, l, "jobs", "show", job).stdout

	want := strings.TrimRight(plainHandoff, "\n")
	for _, heading := range []string{"plan", "handoff"} {
		if got := document(t, out, heading); got != want {
			t.Errorf("the %s section is\n%q\nwant exactly the handoff\n%q", heading, got, want)
		}
	}
	if got, given := document(t, out, "system prompt"), s.invoked(t).flag(t, "--append-system-prompt"); got != given {
		t.Errorf("the system prompt section is\n%q\nwant exactly what the agent was given\n%q", got, given)
	}
	if row := wantCheck(t, checks(t, out), "lint", "failed"); row.output != printed {
		t.Errorf("the check's output is\n%q\nwant exactly what its command printed\n%q", row.output, printed)
	}
	// Each line indented as a check's output is, the tab after the indent.
	indented := "    " + strings.ReplaceAll(strings.TrimRight(printed, "\n"), "\n", "\n    ") + "\n"
	if !strings.Contains(out, indented) {
		t.Errorf("owl jobs show does not print the check's output as\n%q\nin:\n%s", indented, out)
	}
	wantClean(t, out)
}

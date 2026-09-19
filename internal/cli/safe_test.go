package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/vojtechmares/coding-owl/internal/client"
)

func TestTerminalSafeShowsAnEscapeAsTheBytesItIs(t *testing.T) {
	// What a filename or an issue title can carry: an escape that retitles
	// the terminal window, and one that clears the line.
	got := terminalSafe("work.txt\x1b]0;pwned\a and \x1b[2K")

	if got != `work.txt\x1b]0;pwned\x07 and \x1b[2K` {
		t.Errorf("terminalSafe = %q, want the escapes made visible", got)
	}
}

func TestTerminalSafeLeavesTextAndDocumentsAlone(t *testing.T) {
	for _, s := range []string{"", "rebasing onto main conflicts in one.txt, two.txt", "a plan\n\twith a tab\n", "žluťoučký kůň"} {
		if got := terminalSafe(s); got != s {
			t.Errorf("terminalSafe(%q) = %q, want it unchanged", s, got)
		}
	}
}

// reported is a Job as owl jobs show reports one with something in every
// section: a planned Job that ran twice, whose second Run read a Skill and was
// granted permissions and whose checks refused the work, on a Project that
// asked for the agent Verifier. Every document holds what a document is
// printed with on purpose - lines, a tab, a blank line - and text that is not
// ASCII, and nothing that is an instruction to a terminal.
func reported() client.JobDetails {
	at := time.Date(2026, 9, 15, 2, 0, 0, 0, time.UTC)
	const prompt = "You are running unattended.\n\n- Commit as you go.\n\nThis project also asks that you:\n\n- keep\tthe háček"
	return client.JobDetails{
		Job: client.Job{
			ID: 7, Source: "local", SourceRef: "01K5A0000000000000000000JB", Project: "api",
			Prompt: "fix the flaky test", State: "blocked", Branch: "owl/job-7",
			Worktree: "/owl/worktrees/7", Planned: true, TTL: 8, Account: "work", Created: at,
			Plan:   "step one: read the tests\n\tthen the parser\n\nžluťoučký kůň\n",
			Reason: "checks failed: lint",
		},
		Runs: []client.Run{
			{
				ID: 11, JobID: 7, Attempt: 1, Phase: "plan", Outcome: "succeeded", ExitCode: 0,
				Started: at, Ended: at.Add(time.Minute), LogPath: "/owl/logs/11.jsonl",
				SystemPrompt: prompt,
			},
			{
				ID: 12, JobID: 7, Attempt: 2, Phase: "execute", Outcome: "succeeded", ExitCode: 0,
				Started: at.Add(time.Hour), Ended: at.Add(2 * time.Hour), LogPath: "/owl/logs/12.jsonl",
				Skills: []client.Skill{{
					Name: "go-review", Source: "github.com/x/go-review", Ref: "v1.4.0",
					Commit: "0123456789abcdef0123456789abcdef01234567",
				}},
				Permissions:  []string{"Read", "Bash(make test:*)"},
				SystemPrompt: prompt,
			},
		},
		SystemPrompt:         prompt,
		VerifierSystemPrompt: "You are reviewing somebody else's work.\n\n- Say pass or fail.",
		Phases: []client.PhaseSettings{
			{
				Phase: "plan", Model: "anthropic/claude-opus", ModelFrom: "default", Effort: "xhigh", EffortFrom: "default",
				Timeout: time.Hour, TimeoutFrom: "default", Stall: 15 * time.Minute, StallFrom: "default",
			},
			{
				Phase: "execute", Model: "anthropic/claude-sonnet", ModelFrom: "project", Effort: "high", EffortFrom: "job",
				Timeout: 90 * time.Minute, TimeoutFrom: "project", Stall: 15 * time.Minute, StallFrom: "default",
			},
		},
		Checks: []client.CheckResult{
			{Name: "build", Command: "make", Passed: true, Output: "ok\n", Verifier: "command"},
			{
				Name: "lint", Command: "make lint", ExitCode: 1, Reason: "exited 1", Verifier: "command",
				Output: "first line\n\tsecond line, indented\nčtvrtá řádka\n",
			},
			{Name: "agent verifier", Passed: true, Output: "verdict: pass\n\nlooks right", Verifier: "agent"},
		},
		Handoff: "step two: fix the parser\n\tthe tab stays\n",
		Diff: client.DiffSummary{
			Files: []client.DiffFile{{Path: "parser.go", Insertions: 3, Deletions: 1}}, Insertions: 3, Deletions: 1,
		},
	}
}

func TestJobsShowPrintsAReportWithNothingToEscapeAsItWas(t *testing.T) {
	var out bytes.Buffer

	printJob(Env{Stdout: &out}, reported())

	if got := out.String(); got != reportedAsItWas {
		t.Errorf("owl jobs show printed\n%s\nwant it byte for byte as it was\n%s", got, reportedAsItWas)
	}
}

// TestJobsShowShowsAnEscapeFromOutsideOwlAsText puts an escape in one value
// of the report at a time. Wherever it is, the report reaches the terminal as
// text: the escape is shown as the bytes it is, where the value was, and a
// document around it keeps its newlines and tabs.
func TestJobsShowShowsAnEscapeFromOutsideOwlAsText(t *testing.T) {
	const escape, shown = "\x1b]0;pwned\a\x1b[2K", `\x1b]0;pwned\x07\x1b[2K`
	for _, c := range []struct {
		what  string
		carry func(d *client.JobDetails)
		// shows is what the report holds where the value was.
		shows string
	}{
		{
			"a Run's phase", func(d *client.JobDetails) { d.Runs[1].Phase = "execute" + escape },
			"  execute" + shown + "  ",
		},
		{
			"a Run's state", func(d *client.JobDetails) { d.Runs[1].Outcome, d.Runs[1].Stage = "", "verifying"+escape },
			"  verifying" + shown + "  ",
		},
		{
			"a Run's log", func(d *client.JobDetails) { d.Runs[1].LogPath = "/owl/logs/" + escape + ".jsonl" },
			"  /owl/logs/" + shown + ".jsonl\n",
		},
		{
			// A label is the user's own word, and reaches the report through the
			// daemon and the database like anything else outside Owl.
			"a label", func(d *client.JobDetails) { d.Job.Labels = []string{"bug" + escape, "urgent"} },
			"\nlabels: bug" + shown + ", urgent\n",
		},
		{
			"the plan", func(d *client.JobDetails) { d.Job.Plan = "read the tests " + escape + "\n\tthen fix them\n" },
			"\nplan:\nread the tests " + shown + "\n\tthen fix them\n\n",
		},
		{
			"the handoff", func(d *client.JobDetails) { d.Handoff = "step two " + escape + "\n\tthe tab stays\n" },
			"\nhandoff:\nstep two " + shown + "\n\tthe tab stays\n\n",
		},
		{
			// What is printed is the prompt each Run was given, so that is where
			// the escape goes.
			"the system prompt", func(d *client.JobDetails) {
				for i := range d.Runs {
					d.Runs[i].SystemPrompt += "\n- run gofmt " + escape
				}
				d.SystemPrompt += "\n- run gofmt " + escape
			},
			"\n- keep\tthe háček\n- run gofmt " + shown + "\n\n",
		},
		{
			"the verifier system prompt", func(d *client.JobDetails) { d.VerifierSystemPrompt += "\n- " + escape },
			"\n- Say pass or fail.\n- " + shown + "\n",
		},
		{
			"a check's name", func(d *client.JobDetails) { d.Checks[1].Name = "lint " + escape },
			"\n- lint " + shown + ": failed (exited 1)\n",
		},
		{
			"a check's reason", func(d *client.JobDetails) { d.Checks[1].Reason = "could not be run: " + escape },
			"\n- lint: failed (could not be run: " + shown + ")\n",
		},
		{
			"a check's output", func(d *client.JobDetails) { d.Checks[1].Output = "lint is unhappy " + escape + "\n\tat line two\n" },
			"\n    lint is unhappy " + shown + "\n    \tat line two\n",
		},
		{
			"a Skill's name", func(d *client.JobDetails) { d.Runs[1].Skills[0].Name = "go-review" + escape },
			"\ngo-review" + shown + "  ",
		},
		{
			"a Skill's source", func(d *client.JobDetails) { d.Runs[1].Skills[0].Source = "github.com/x/go-review" + escape },
			"  github.com/x/go-review" + shown + "  ",
		},
		{
			"a Skill's ref", func(d *client.JobDetails) { d.Runs[1].Skills[0].Ref = "v1.4.0" + escape },
			"  v1.4.0" + shown + "  ",
		},
		{
			// A commit is cut short, so the escape is one that fits in what is
			// printed of it.
			"a Skill's commit", func(d *client.JobDetails) { d.Runs[1].Skills[0].Commit = "\x1b]0;x\a0123456789" },
			"  " + `\x1b]0;x\x07012345` + "\n",
		},
		{
			"a Run's permissions", func(d *client.JobDetails) { d.Runs[1].Permissions[1] = "Bash(make " + escape + ":*)" },
			"\nRUN 12: Read, Bash(make " + shown + ":*)\n",
		},
		{
			"a phase's name", func(d *client.JobDetails) { d.Phases[1].Phase = "execute" + escape },
			"\nexecute" + shown + "  ",
		},
		{
			"a phase's model", func(d *client.JobDetails) { d.Phases[1].Model = "sonnet" + escape },
			"  sonnet" + shown + "  ",
		},
		{
			"where a phase's model came from", func(d *client.JobDetails) { d.Phases[1].ModelFrom = "project" + escape },
			"  project" + shown + "  ",
		},
		{
			"a phase's effort", func(d *client.JobDetails) { d.Phases[1].Effort = "high" + escape },
			"  high" + shown + "  ",
		},
		{
			"where a phase's effort came from", func(d *client.JobDetails) { d.Phases[1].EffortFrom = "job" + escape },
			"  job" + shown + "  ",
		},
		{
			"where a phase's timeout came from", func(d *client.JobDetails) { d.Phases[1].TimeoutFrom = "project" + escape },
			"  project" + shown + "  ",
		},
		{
			"where a phase's stall limit came from", func(d *client.JobDetails) { d.Phases[1].StallFrom = "default" + escape },
			"  default" + shown + "\n",
		},
	} {
		t.Run(c.what, func(t *testing.T) {
			d := reported()
			c.carry(&d)
			var out bytes.Buffer

			printJob(Env{Stdout: &out}, d)

			got := out.String()
			if strings.ContainsFunc(got, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
				t.Errorf("owl jobs show reaches the terminal with a control character in it:\n%q", got)
			}
			if !strings.Contains(got, c.shows) {
				t.Errorf("owl jobs show does not show %q where the value was:\n%s", c.shows, got)
			}
		})
	}
}

// reportedAsItWas is what owl jobs show printed for reported() before anything
// in it was made terminal-safe, with the system prompt headed by the Runs that
// were given it. A report with nothing to escape prints exactly that, byte for
// byte.
const reportedAsItWas = `id: 7
project: api
state: blocked
attempts: 8 left
account: work
labels: (none)
prompt: fix the flaky test
branch: owl/job-7
worktree: /owl/worktrees/7
planned: yes
source: local:01K5A0000000000000000000JB
created: 2026-09-15T02:00:00Z
diff: 1 file changed, 3 added, 1 removed
reason: checks failed: lint
runs:
RUN  ATTEMPT  PHASE    STATE      EXIT  STARTED               ENDED                 LOG
11   1        plan     succeeded  0     2026-09-15T02:00:00Z  2026-09-15T02:01:00Z  /owl/logs/11.jsonl
12   2        execute  succeeded  0     2026-09-15T03:00:00Z  2026-09-15T04:00:00Z  /owl/logs/12.jsonl

skills:
RUN 12     SOURCE                  REF     COMMIT
go-review  github.com/x/go-review  v1.4.0  0123456789ab

permissions:
RUN 12: Read, Bash(make test:*)

checks:
- build: passed
- lint: failed (exited 1)
    first line
    	second line, indented
    čtvrtá řádka
- agent verifier: passed
    verdict: pass
    looks right

phases:
PHASE    MODEL                    FROM     EFFORT  FROM     TIMEOUT  FROM     STALL  FROM
plan     anthropic/claude-opus    default  xhigh   default  1h       default  15m    default
execute  anthropic/claude-sonnet  project  high    job      1h30m    project  15m    default

plan:
step one: read the tests
	then the parser

žluťoučký kůň

handoff:
step two: fix the parser
	the tab stays

runs 11, 12 were given this system prompt:
You are running unattended.

- Commit as you go.

This project also asks that you:

- keep	the háček

verifier system prompt:
You are reviewing somebody else's work.

- Say pass or fail.
`

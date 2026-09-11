package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
)

// chatView is what the chat may read: what Owl already knows, rendered as the
// text a person would read (ADR-0022). Every method is a read, and there is no
// method that is not - the chat never acts.
type chatView struct {
	projects *project.Service
	jobs     *queue.Service
	runs     *run.Service
}

// Projects is every registered Project.
func (v *chatView) Projects(ctx context.Context) (string, error) {
	all, err := v.projects.List(ctx)
	if err != nil {
		return "", err
	}
	if len(all) == 0 {
		return "No Project is registered.", nil
	}
	var b strings.Builder
	for _, p := range all {
		fmt.Fprintf(&b, "%s\tpath=%s\tbase=%s\n", p.Name, p.Path, p.BaseBranch)
	}
	return b.String(), nil
}

// Jobs is the queue, or every Job whatever its state.
func (v *chatView) Jobs(ctx context.Context, all bool) (string, error) {
	jobs, err := v.jobs.List(ctx, all)
	if err != nil {
		return "", err
	}
	if len(jobs) == 0 {
		if all {
			return "Owl has no Jobs.", nil
		}
		return "The queue is empty.", nil
	}
	var b strings.Builder
	for _, j := range jobs {
		fmt.Fprintf(&b, "job %d\tproject=%s\tstate=%s\tprompt=%s\n",
			j.ID, j.Project, j.State, oneLine(j.Prompt))
		if reason := strings.TrimSpace(j.Reason); reason != "" {
			fmt.Fprintf(&b, "\treason: %s\n", oneLine(reason))
		}
	}
	return b.String(), nil
}

// Job is one Job with its Runs, what Verification said, its plan and its
// handoff - the same account of it `owl jobs show` gives.
func (v *chatView) Job(ctx context.Context, id int64) (string, error) {
	d, err := v.runs.Show(ctx, id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	j := d.Job
	fmt.Fprintf(&b, "job %d\nproject: %s\nstate: %s\nprompt: %s\n", j.ID, j.Project, j.State, oneLine(j.Prompt))
	if reason := strings.TrimSpace(j.Reason); reason != "" {
		fmt.Fprintf(&b, "reason: %s\n", oneLine(reason))
	}
	if j.Branch != "" {
		fmt.Fprintf(&b, "branch: %s\n", j.Branch)
	}
	for _, r := range d.Runs {
		fmt.Fprintf(&b, "run %d\tattempt=%d\tphase=%s\toutcome=%s\n",
			r.ID, r.Attempt, orNothing(string(r.Phase)), orRunning(string(r.Outcome)))
		if r.Error != "" {
			fmt.Fprintf(&b, "\terror: %s\n", oneLine(r.Error))
		}
	}
	if len(d.Checks) > 0 {
		b.WriteString("verification:\n")
		for _, c := range d.Checks {
			verdict := "passed"
			if !c.Passed {
				verdict = "failed"
				if c.Reason != "" {
					verdict += " (" + c.Reason + ")"
				}
			}
			fmt.Fprintf(&b, "- %s: %s\n", c.Name, verdict)
			if out := strings.TrimRight(c.Output, "\n"); out != "" && !c.Passed {
				for _, line := range strings.Split(out, "\n") {
					fmt.Fprintf(&b, "    %s\n", line)
				}
			}
		}
	}
	if plan := strings.TrimSpace(d.Job.Plan); plan != "" {
		fmt.Fprintf(&b, "plan:\n%s\n", plan)
	}
	if handoff := strings.TrimSpace(d.Handoff); handoff != "" {
		fmt.Fprintf(&b, "handoff:\n%s\n", handoff)
	}
	return b.String(), nil
}

// RunLog is the end of a Run's captured output. The end rather than the
// beginning: what a Run was doing when it stopped is what a question about it
// is usually about.
func (v *chatView) RunLog(ctx context.Context, id int64, max int) (string, error) {
	var lines []string
	var held int
	err := v.runs.Log(ctx, id, false, func(l run.Line) error {
		lines = append(lines, l.Text)
		held += len(l.Text) + 1
		// Only as much as will be carried is kept, so a long log is read
		// rather than held.
		for held > max && len(lines) > 1 {
			held -= len(lines[0]) + 1
			lines = lines[1:]
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return fmt.Sprintf("run %d printed nothing.", id), nil
	}
	return strings.Join(lines, "\n"), nil
}

// JobDiff is what a Job's branch changed against its Project's base branch.
func (v *chatView) JobDiff(ctx context.Context, id int64, max int) (string, error) {
	d, err := v.runs.Show(ctx, id)
	if err != nil {
		return "", err
	}
	if d.Job.Branch == "" {
		return fmt.Sprintf("job %d has no branch yet, so it has changed nothing.", id), nil
	}
	p, err := v.projects.Show(ctx, d.Job.Project)
	if err != nil {
		return "", err
	}
	patch, complete, err := git.DiffPatch(p.Path, p.BaseBranch, d.Job.Branch, max)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(patch) == "" {
		return fmt.Sprintf("job %d changed no file.", id), nil
	}
	if !complete {
		patch += "\n[the rest of the diff is not carried]"
	}
	return patch, nil
}

// oneLine is text as a single line, for a listing.
func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
}

func orNothing(s string) string {
	if strings.TrimSpace(s) == "" {
		return "none"
	}
	return s
}

func orRunning(outcome string) string {
	if strings.TrimSpace(outcome) == "" {
		return "running"
	}
	return outcome
}

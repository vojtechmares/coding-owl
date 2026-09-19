package behavior_test

// Behavior tests for issue #4. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-4.md. They drive the built owl binary from the outside
// against a daemon started as a subprocess, except S18, which drives that
// daemon through the shared client package, and S19 and S20, which drive the
// queue Service in-process over a temporary database.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// queueRow is one row of owl queue list output.
type queueRow struct {
	position, id, project, state, labels, prompt string
}

// queueRowRE splits a row into its five fixed columns and the prompt, which
// is last because it is the only column that may contain spaces. Labels are a
// fixed column because a label carries no whitespace (issue #132).
var queueRowRE = regexp.MustCompile(`^(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(.*)$`)

// emptyQueue is what owl queue list prints when nothing is queued.
const emptyQueue = "queue is empty"

// queueList runs owl queue list with any extra flags and parses its rows.
func queueList(t *testing.T, l *layout, flags ...string) []queueRow {
	t.Helper()
	out := mustOwl(t, l, append([]string{"queue", "list"}, flags...)...).stdout
	var rows []queueRow
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(ln, "POSITION") || strings.Contains(ln, emptyQueue) {
			continue
		}
		m := queueRowRE.FindStringSubmatch(ln)
		if m == nil {
			t.Fatalf("cannot parse queue row %q in:\n%s", ln, out)
		}
		rows = append(rows, queueRow{m[1], m[2], m[3], m[4], m[5], strings.TrimSpace(m[6])})
	}
	return rows
}

// summarise renders the columns that describe a Job rather than identify it,
// so an expectation can be written without knowing the ids.
func summarise(rows []queueRow) string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, strings.Join([]string{r.position, r.project, r.state, r.prompt}, "|"))
	}
	return strings.Join(out, "\n")
}

// wantQueue fails unless owl queue list shows exactly these rows, in order.
func wantQueue(t *testing.T, l *layout, want ...string) {
	t.Helper()
	if got := summarise(queueList(t, l)); got != strings.Join(want, "\n") {
		t.Errorf("owl queue list =\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
}

// wantQueueAll is wantQueue for owl queue list --all.
func wantQueueAll(t *testing.T, l *layout, want ...string) {
	t.Helper()
	if got := summarise(queueList(t, l, "--all")); got != strings.Join(want, "\n") {
		t.Errorf("owl queue list --all =\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
}

// wantEmptyQueue fails unless owl queue list says the queue is empty and
// prints no row.
func wantEmptyQueue(t *testing.T, l *layout) {
	t.Helper()
	res := mustOwl(t, l, "queue", "list")
	if !strings.Contains(res.stdout, emptyQueue) {
		t.Errorf("owl queue list does not say the queue is empty:\n%s", res.stdout)
	}
	if rows := queueList(t, l); len(rows) != 0 {
		t.Errorf("owl queue list printed %d rows, want none:\n%s", len(rows), res.stdout)
	}
}

// addJob runs owl add in dir and fails on a non-zero exit.
func addJob(t *testing.T, l *layout, dir, prompt string, flags ...string) result {
	t.Helper()
	res := runOwlIn(t, l, dir, append([]string{"add", prompt}, flags...)...)
	if res.code != 0 {
		t.Fatalf("owl add %q in %s exited %d\nstdout:\n%s\nstderr:\n%s", prompt, dir, res.code, res.stdout, res.stderr)
	}
	return res
}

// jobID returns the id of the Job with that prompt, from the given rows.
func jobID(t *testing.T, rows []queueRow, prompt string) string {
	t.Helper()
	for _, r := range rows {
		if r.prompt == prompt {
			return r.id
		}
	}
	t.Fatalf("no job with prompt %q in %v", prompt, rows)
	return ""
}

// project registers a repository named rel and returns it.
func project(t *testing.T, l *layout, rel string) *repo {
	t.Helper()
	r := newRepo(t, l, rel)
	addProject(t, l, r)
	return r
}

// outside returns a directory under the layout that is inside no repository.
func outside(t *testing.T, l *layout) string {
	t.Helper()
	dir := filepath.Join(l.root, "elsewhere")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// namesPath reports whether out names dir, in either the spelling the test
// used or the one symlinks resolve it to.
func namesPath(out, dir string) bool {
	if strings.Contains(out, dir) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(dir)
	return err == nil && strings.Contains(out, resolved)
}

// threeJobs queues first, second and third in one Project and returns it.
func threeJobs(t *testing.T, l *layout) *repo {
	t.Helper()
	r := project(t, l, "api")
	for _, prompt := range []string{"first", "second", "third"} {
		addJob(t, l, r.dir, prompt)
	}
	return r
}

func TestS1QueueAddInsideAProjectNeedsNoFlag(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")

	addJob(t, l, r.dir, "fix the flaky test")

	wantQueue(t, l, "1|api|pending|fix the flaky test")
}

func TestS2QueueAddFromASubdirectory(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	sub := filepath.Join(r.dir, "sub", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	addJob(t, l, sub, "work")

	wantQueue(t, l, "1|api|pending|work")
}

func TestS3QueueAddOutsideAnyProjectIsRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	project(t, l, "api")
	dir := outside(t, l)

	res := runOwlIn(t, l, dir, "add", "work")

	if res.code == 0 {
		t.Fatalf("owl add outside a project exited 0\nstdout:\n%s", res.stdout)
	}
	if !namesPath(res.stderr, dir) {
		t.Errorf("stderr does not name %s:\n%s", dir, res.stderr)
	}
	if !strings.Contains(res.stderr, "--project") {
		t.Errorf("stderr does not name the --project flag:\n%s", res.stderr)
	}
	if !strings.Contains(res.stderr, "not inside a registered project") {
		t.Errorf("stderr does not say the directory is inside no project:\n%s", res.stderr)
	}
	wantEmptyQueue(t, l)
}

func TestS4QueueAddWithProjectFlagFromAnywhere(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	project(t, l, "api")

	addJob(t, l, outside(t, l), "work", "--project", "api")

	wantQueue(t, l, "1|api|pending|work")
}

func TestS5QueueAddWithUnknownProjectIsRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")

	res := runOwlIn(t, l, r.dir, "add", "work", "--project", "ghost")

	if res.code == 0 {
		t.Fatalf("owl add --project ghost exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "ghost") {
		t.Errorf("stderr does not name ghost:\n%s", res.stderr)
	}
	wantEmptyQueue(t, l)
}

func TestS6QueueAddWithAnEmptyPromptIsRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")

	res := runOwlIn(t, l, r.dir, "add", "")

	if res.code == 0 {
		t.Fatalf("owl add with an empty prompt exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "prompt") || !strings.Contains(res.stderr, "empty") {
		t.Errorf("stderr does not say the prompt is empty:\n%s", res.stderr)
	}
	wantEmptyQueue(t, l)
}

func TestS7QueueAddPicksTheInnermostProject(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	project(t, l, "outer")
	inner := project(t, l, filepath.Join("outer", "inner"))

	addJob(t, l, inner.dir, "work")

	wantQueue(t, l, "1|inner|pending|work")
}

func TestS8QueueListShowsThePositionProjectStateAndPrompt(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	api := project(t, l, "api")
	web := project(t, l, "web")

	addJob(t, l, api.dir, "first")
	addJob(t, l, web.dir, "second")
	addJob(t, l, api.dir, "third")

	out := mustOwl(t, l, "queue", "list").stdout
	header := strings.Fields(strings.Split(out, "\n")[0])
	if strings.Join(header, " ") != "POSITION ID PROJECT STATE PROMPT" {
		t.Errorf("header = %v, want POSITION ID PROJECT STATE PROMPT\n%s", header, out)
	}
	wantQueue(t, l,
		"1|api|pending|first",
		"2|web|pending|second",
		"3|api|pending|third",
	)
}

func TestS9QueueListOnAnEmptyQueueSaysSo(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)

	wantEmptyQueue(t, l)
}

func TestS10QueueRemoveCancelsAndClosesTheGap(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	threeJobs(t, l)
	id := jobID(t, queueList(t, l), "second")

	mustOwl(t, l, "queue", "remove", id)

	wantQueue(t, l, "1|api|pending|first", "2|api|pending|third")
	wantQueueAll(t, l, "1|api|pending|first", "2|api|pending|third", "-|api|cancelled|second")
	if got := jobID(t, queueList(t, l, "--all"), "second"); got != id {
		t.Errorf("cancelled job has id %s, want the id it had, %s", got, id)
	}
}

func TestS11QueueRemoveRefusesAJobThatIsNotPending(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	threeJobs(t, l)
	id := jobID(t, queueList(t, l), "second")
	mustOwl(t, l, "queue", "remove", id)

	res := runOwl(t, l, "queue", "remove", id)

	if res.code == 0 {
		t.Fatalf("removing a cancelled job exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, id) || !strings.Contains(res.stderr, "not pending") {
		t.Errorf("stderr does not say job %s is not pending:\n%s", id, res.stderr)
	}
	rows := queueList(t, l, "--all")
	if len(rows) != 3 {
		t.Fatalf("owl queue list --all shows %d jobs, want 3: %v", len(rows), rows)
	}
	if got := rows[2]; got.id != id || got.state != "cancelled" {
		t.Errorf("the job that left the queue is %+v, want %s cancelled", got, id)
	}
}

func TestS12QueueRemoveRefusesAnUnknownID(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)

	res := runOwl(t, l, "queue", "remove", "999")

	if res.code == 0 {
		t.Fatalf("removing an unknown job exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "999") {
		t.Errorf("stderr does not name 999:\n%s", res.stderr)
	}
}

func TestS13QueueReorderMovesAJobAndPersistsIt(t *testing.T) {
	l := newLayout(t)
	p := daemonUp(t, l)
	threeJobs(t, l)
	id := jobID(t, queueList(t, l), "third")

	mustOwl(t, l, "queue", "reorder", id, "1")

	wantQueue(t, l, "1|api|pending|third", "2|api|pending|first", "3|api|pending|second")

	stopDaemon(t, p)
	daemonUp(t, l)
	wantQueue(t, l, "1|api|pending|third", "2|api|pending|first", "3|api|pending|second")
}

func TestS14QueueReorderMovesAJobToTheEnd(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	threeJobs(t, l)
	id := jobID(t, queueList(t, l), "first")

	mustOwl(t, l, "queue", "reorder", id, "3")

	wantQueue(t, l, "1|api|pending|second", "2|api|pending|third", "3|api|pending|first")
}

func TestS15QueueReorderRefusesAPositionOutsideTheQueue(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	threeJobs(t, l)
	id := jobID(t, queueList(t, l), "first")

	// 4294967297 is 2^32 + 1: a position that truncates to 1 if it is ever
	// carried in a narrower number than it was typed in.
	for _, position := range []string{"0", "4", "4294967297"} {
		res := runOwl(t, l, "queue", "reorder", id, position)
		if res.code == 0 {
			t.Fatalf("reorder to position %s exited 0\nstdout:\n%s", position, res.stdout)
		}
		if !strings.Contains(res.stderr, "between 1 and 3") && !strings.Contains(res.stderr, "is not one") {
			t.Errorf("stderr neither names the range of positions nor refuses the position:\n%s", res.stderr)
		}
		if !strings.Contains(res.stderr, position) {
			t.Errorf("stderr reports a position other than the %s that was asked for:\n%s", position, res.stderr)
		}
		wantQueue(t, l, "1|api|pending|first", "2|api|pending|second", "3|api|pending|third")
	}
}

func TestS16QueueReorderRefusesUnknownAndNotPendingJobs(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	threeJobs(t, l)
	cancelled := jobID(t, queueList(t, l), "third")
	mustOwl(t, l, "queue", "remove", cancelled)

	unknown := runOwl(t, l, "queue", "reorder", "999", "1")
	if unknown.code == 0 {
		t.Fatalf("reordering an unknown job exited 0\nstdout:\n%s", unknown.stdout)
	}
	if !strings.Contains(unknown.stderr, "999") {
		t.Errorf("stderr does not name 999:\n%s", unknown.stderr)
	}

	res := runOwl(t, l, "queue", "reorder", cancelled, "1")
	if res.code == 0 {
		t.Fatalf("reordering a cancelled job exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, cancelled) || !strings.Contains(res.stderr, "not pending") {
		t.Errorf("stderr does not say job %s is not pending:\n%s", cancelled, res.stderr)
	}
	wantQueue(t, l, "1|api|pending|first", "2|api|pending|second")
}

func TestS17QueueSurvivesADaemonRestart(t *testing.T) {
	l := newLayout(t)
	p := daemonUp(t, l)
	api := project(t, l, "api")
	web := project(t, l, "web")
	addJob(t, l, api.dir, "first")
	addJob(t, l, web.dir, "second")
	before := queueList(t, l)

	stopDaemon(t, p)
	daemonUp(t, l)

	after := queueList(t, l)
	if len(after) != len(before) {
		t.Fatalf("queue has %d jobs after the restart, want %d", len(after), len(before))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("row %d is %+v after the restart, want %+v", i, after[i], before[i])
		}
	}
}

// ulidRE is Crockford base32 without the letters I, L, O and U.
var ulidRE = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func TestS18QueueJobsCarryTheLocalSourceAndAULIDReference(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	c := client.New(l.socket())
	ctx := context.Background()

	first, err := c.AddJob(ctx, client.AddJobRequest{Prompt: "first", WorkingDir: r.dir})
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	second, err := c.AddJob(ctx, client.AddJobRequest{Project: "api", Prompt: "second"})
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	for _, j := range []client.Job{first, second} {
		if j.Source != "local" {
			t.Errorf("job %d has source %q, want %q", j.ID, j.Source, "local")
		}
		if !ulidRE.MatchString(j.SourceRef) {
			t.Errorf("job %d has source_ref %q, which is not a ulid", j.ID, j.SourceRef)
		}
	}
	if first.SourceRef == second.SourceRef {
		t.Errorf("both jobs carry the reference %q; each add mints its own", first.SourceRef)
	}
}

// fixedSource stands in for a producer whose work has a natural identity, so
// that producing twice means producing the same reference twice (ADR-0032).
type fixedSource struct{ ref string }

func (fixedSource) Name() string           { return "test" }
func (s fixedSource) Ref() (string, error) { return s.ref, nil }

// queueService opens a temporary database holding one Project and returns a
// queue Service producing from src.
func queueService(t *testing.T, src queue.Source) *queue.Service {
	t.Helper()
	st, _, err := store.Open(filepath.Join(t.TempDir(), "owl.db"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	p := store.Project{Name: "api", Path: filepath.Join(t.TempDir(), "api"), BaseBranch: "main", Registered: time.Now().UTC()}
	if err := st.AddProject(context.Background(), p); err != nil {
		t.Fatalf("registering a project: %v", err)
	}
	return queue.NewService(st, src)
}

func TestS19QueueProducingTheSameReferenceTwiceYieldsOneJob(t *testing.T) {
	ctx := context.Background()
	svc := queueService(t, fixedSource{ref: "issue:1"})

	first, err := svc.Add(ctx, queue.AddRequest{Project: "api", Prompt: "first"})
	if err != nil {
		t.Fatalf("first production: %v", err)
	}
	if _, err := svc.Add(ctx, queue.AddRequest{Project: "api", Prompt: "second"}); err != nil {
		t.Fatalf("second production: %v", err)
	}

	jobs, err := svc.List(ctx, true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("the queue holds %d jobs, want 1: %+v", len(jobs), jobs)
	}
	got := jobs[0]
	if got.ID != first.ID {
		t.Errorf("job id = %d, want the id of the first production, %d", got.ID, first.ID)
	}
	if got.Position != first.Position {
		t.Errorf("position = %d, want %d", got.Position, first.Position)
	}
	if got.Prompt != "second" {
		t.Errorf("prompt = %q, want %q: production is an upsert", got.Prompt, "second")
	}
	if got.Source != "test" || got.SourceRef != "issue:1" {
		t.Errorf("job carries source %q ref %q, want test / issue:1", got.Source, got.SourceRef)
	}
}

func TestS20QueueReproducingACancelledReferenceDoesNotReviveIt(t *testing.T) {
	ctx := context.Background()
	svc := queueService(t, fixedSource{ref: "issue:1"})
	first, err := svc.Add(ctx, queue.AddRequest{Project: "api", Prompt: "first"})
	if err != nil {
		t.Fatalf("first production: %v", err)
	}
	if _, err := svc.Cancel(ctx, first.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if _, err := svc.Add(ctx, queue.AddRequest{Project: "api", Prompt: "third"}); err != nil {
		t.Fatalf("producing again: %v", err)
	}

	jobs, err := svc.List(ctx, true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("the queue holds %d jobs, want 1: %+v", len(jobs), jobs)
	}
	got := jobs[0]
	if got.State != queue.StateCancelled {
		t.Errorf("state = %q, want %q", got.State, queue.StateCancelled)
	}
	if got.Position != 0 {
		t.Errorf("position = %d, want none", got.Position)
	}
	if got.Prompt != "first" {
		t.Errorf("prompt = %q, want %q: a job that has left the queue is not rewritten", got.Prompt, "first")
	}
}

func TestS21QueueCommandsReportAStoppedDaemon(t *testing.T) {
	l := newLayout(t)

	for _, args := range [][]string{
		{"add", "work", "--project", "api"},
		{"queue", "list"},
		{"queue", "remove", "1"},
		{"queue", "reorder", "1", "1"},
	} {
		res := runOwl(t, l, args...)
		if res.code == 0 {
			t.Errorf("owl %v exited 0 with no daemon running\nstdout:\n%s", args, res.stdout)
		}
		if !strings.Contains(res.stderr, "daemon not running") || !strings.Contains(res.stderr, l.socket()) {
			t.Errorf("owl %v stderr does not report a stopped daemon at %s:\n%s", args, l.socket(), res.stderr)
		}
	}
}

func TestS22QueueRemovingAProjectTakesItsJobsWithIt(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	api := project(t, l, "api")
	web := project(t, l, "web")
	addJob(t, l, api.dir, "first")
	addJob(t, l, web.dir, "second")
	addJob(t, l, api.dir, "third")
	addJob(t, l, api.dir, "fourth")
	mustOwl(t, l, "queue", "remove", jobID(t, queueList(t, l), "fourth"))

	res := mustOwl(t, l, "project", "remove", "api")

	if !strings.Contains(res.stdout, "3 jobs") {
		t.Errorf("owl project remove does not say three jobs went with the project:\n%s", res.stdout)
	}
	wantQueue(t, l, "1|web|pending|second")
	for _, row := range queueList(t, l, "--all") {
		if row.project == "api" {
			t.Errorf("owl queue list --all still shows %+v for the removed project", row)
		}
	}
}

func TestS23QueueListCutsALongPromptShort(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	prompt := strings.Repeat("long prompt ", 9)[:100]
	addJob(t, l, r.dir, prompt)

	rows := queueList(t, l)

	if len(rows) != 1 {
		t.Fatalf("owl queue list shows %d rows, want 1: %v", len(rows), rows)
	}
	want := string([]rune(prompt)[:57]) + "..."
	if rows[0].prompt != want {
		t.Errorf("prompt column = %q, want %q", rows[0].prompt, want)
	}
	if rows[0].position != "1" || rows[0].project != "api" || rows[0].state != "pending" {
		t.Errorf("row = %+v, want position 1 in api, pending", rows[0])
	}
}

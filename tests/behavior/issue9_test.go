package behavior_test

// Behavior tests for issue #9. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-9.md. They drive the desktop app's Go side, the
// internal/desktop package, in-process over the daemon's socket, and confirm
// what it did by asking the owl binary; the frontend is checked on disk.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/desktop"
)

// event is one thing the app told its frontend.
type event struct {
	name string
	data any
}

// events collects what the app emits, so a scenario can read it back in order.
type events struct{ ch chan event }

func newEvents() *events { return &events{ch: make(chan event, 256)} }

func (e *events) Emit(name string, data any) { e.ch <- event{name, data} }

// next returns the next event emitted, failing if none arrives in time.
func (e *events) next(t *testing.T) event {
	t.Helper()
	select {
	case ev := <-e.ch:
		return ev
	case <-time.After(15 * time.Second):
		t.Fatal("no event from the app within 15s")
		return event{}
	}
}

// none fails if the app emits anything within a short window.
func (e *events) none(t *testing.T) {
	t.Helper()
	select {
	case ev := <-e.ch:
		t.Fatalf("unexpected event %q: %v", ev.name, ev.data)
	case <-time.After(300 * time.Millisecond):
	}
}

// desktopApp is the app as cmd/owl-desktop creates it, for this layout.
func desktopApp(t *testing.T, l *layout) (*desktop.App, *events) {
	t.Helper()
	ev := newEvents()
	app := desktop.New(l.socket(), ev)
	t.Cleanup(app.Shutdown)
	return app, ev
}

// id parses a Job or Run id the owl binary printed.
func id(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("not an id: %q", s)
	}
	return n
}

func TestS1DaemonDownIsReportedWithTheSocket(t *testing.T) {
	l := newLayout(t)
	app, ev := desktopApp(t, l)

	st := app.Status()
	if st.Running {
		t.Fatalf("status = %+v, want not running", st)
	}
	if st.SocketPath != l.socket() {
		t.Errorf("socket = %q, want %q", st.SocketPath, l.socket())
	}
	if !strings.Contains(st.Error, l.socket()) {
		t.Errorf("error = %q, want it to name the socket %s", st.Error, l.socket())
	}

	if _, err := app.Projects(); !errors.Is(err, client.ErrDaemonNotRunning) {
		t.Errorf("Projects() err = %v, want ErrDaemonNotRunning", err)
	}
	if _, err := app.Jobs(true); !errors.Is(err, client.ErrDaemonNotRunning) {
		t.Errorf("Jobs() err = %v, want ErrDaemonNotRunning", err)
	}
	if _, err := app.Overview(); !errors.Is(err, client.ErrDaemonNotRunning) {
		t.Errorf("Overview() err = %v, want ErrDaemonNotRunning", err)
	}
	if err := app.FollowLog(1); !errors.Is(err, client.ErrDaemonNotRunning) {
		t.Errorf("FollowLog() err = %v, want ErrDaemonNotRunning", err)
	}
	ev.none(t)
}

func TestS2StatusReportsTheDaemon(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	app, _ := desktopApp(t, l)

	st := app.Status()
	if !st.Running {
		t.Fatalf("status = %+v, want running", st)
	}
	if st.Version != testVersion {
		t.Errorf("version = %q, want %q", st.Version, testVersion)
	}
	if st.SocketPath != l.socket() {
		t.Errorf("socket = %q, want %q", st.SocketPath, l.socket())
	}
	if st.Error != "" {
		t.Errorf("error = %q, want none", st.Error)
	}
}

// wireImport is the wire, which only the shared client may speak (ADR-0009).
func wireImport(imp string) bool {
	return strings.Contains(imp, "/gen/") ||
		strings.HasPrefix(imp, "connectrpc.com/") ||
		strings.HasPrefix(imp, "google.golang.org/protobuf")
}

// stateImport is the daemon's own state, which no client reaches even
// through a helper: the database, the repositories, the daemon itself.
func stateImport(imp string) bool {
	return strings.HasSuffix(imp, "/internal/store") ||
		strings.HasSuffix(imp, "/internal/git") ||
		strings.HasSuffix(imp, "/internal/daemon") ||
		strings.HasSuffix(imp, "/internal/run") ||
		imp == "database/sql" ||
		strings.Contains(imp, "sqlite")
}

// goList runs go list over pkg with the given format and returns its lines.
func goList(t *testing.T, format, pkg string, deps bool) []string {
	t.Helper()
	args := []string{"list"}
	if deps {
		args = append(args, "-deps")
	}
	args = append(args, "-f", format, pkg)
	cmd := exec.Command("go", args...)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pkg, err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func TestS3AppUsesOnlySharedClient(t *testing.T) {
	pkgs := []string{"./internal/desktop"}
	if runtime.GOOS == "darwin" {
		pkgs = append(pkgs, "./cmd/owl-desktop")
	}
	for _, pkg := range pkgs {
		for _, imp := range goList(t, `{{join .Imports "\n"}}`, pkg, false) {
			if wireImport(imp) {
				t.Errorf("%s imports %s directly; the app speaks to the daemon through the shared client", pkg, imp)
			}
		}
		for _, imp := range goList(t, "{{.ImportPath}}", pkg, true) {
			if stateImport(imp) {
				t.Errorf("%s reaches %s; the app is a pure view with no database or git access", pkg, imp)
			}
		}
	}
	hasClient := false
	for _, imp := range goList(t, `{{join .Imports "\n"}}`, "./internal/desktop", false) {
		if imp == "github.com/vojtechmares/coding-owl/internal/client" {
			hasClient = true
		}
	}
	if !hasClient {
		t.Errorf("internal/desktop does not use the shared client package")
	}
}

func TestS4ProjectsReflectDaemonState(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	app, _ := desktopApp(t, l)
	r := project(t, l, "api")
	show := mustOwl(t, l, "project", "show", "api").stdout

	ps, err := app.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 {
		t.Fatalf("projects = %+v, want the one registered", ps)
	}
	if ps[0].Name != "api" || ps[0].Path != line(t, show, "path") || ps[0].BaseBranch != line(t, show, "base branch") {
		t.Errorf("project = %+v, want name api, path %s, base branch %s", ps[0], line(t, show, "path"), line(t, show, "base branch"))
	}
	_ = r

	mustOwl(t, l, "project", "remove", "api")
	ps, err = app.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 0 {
		t.Errorf("after remove, projects = %+v, want none", ps)
	}
}

func TestS5QueueAndJobsReflectDaemonState(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	app, _ := desktopApp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "first")
	addJob(t, l, r.dir, "second")
	rows := queueList(t, l)

	jobs, err := app.Jobs(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %+v, want two", jobs)
	}
	for i, want := range []string{"first", "second"} {
		j := jobs[i]
		if j.Prompt != want || j.State != "pending" || j.Position != i+1 || j.Project != "api" {
			t.Errorf("job %d = %+v, want prompt %q pending at position %d", i, j, want, i+1)
		}
		if j.ID != id(t, jobID(t, rows, want)) {
			t.Errorf("job %d id = %d, want %s as owl queue list reports", i, j.ID, jobID(t, rows, want))
		}
	}

	o, err := app.Overview()
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Counts) != 1 || o.Counts[0].State != "pending" || o.Counts[0].Count != 2 {
		t.Errorf("overview counts = %+v, want two pending", o.Counts)
	}

	mustOwl(t, l, "queue", "remove", jobID(t, rows, "first"))
	jobs, err = app.Jobs(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Prompt != "second" || jobs[0].Position != 1 {
		t.Errorf("after remove, jobs = %+v, want second at position one", jobs)
	}
}

// twoChecks is one passing and one failing check, so a detail view has both
// verdicts and some output to show.
const twoChecks = `apiVersion: codingowl.dev/v1
checks:
  - name: passing
    run: echo all good
  - name: failing
    run: echo not good; exit 1
`

const plan = "1. write the work\n2. commit it\n"

// detailedJob runs a planned Job through both its phases with an Agent that
// writes a handoff and commits a file, against a Project with two checks, and
// returns the Job's id.
func detailedJob(t *testing.T) (*layout, string) {
	t.Helper()
	l, _ := writingLayout(t, map[string]string{handoffPath: plan, "work.txt": "the agent's work\n"}, true, agentScript)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", twoChecks, "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")
	_, job := phase(t, l)
	_, job2 := phase(t, l)
	if job != job2 {
		t.Fatalf("the second phase ran job %s, not %s", job2, job)
	}
	return l, job
}

func TestS6JobDetailCarriesPlanHandoffChecksAndDiff(t *testing.T) {
	l, job := detailedJob(t)
	app, _ := desktopApp(t, l)

	d, err := app.Job(id(t, job))
	if err != nil {
		t.Fatal(err)
	}
	if d.Job.Plan != plan {
		t.Errorf("plan = %q, want %q", d.Job.Plan, plan)
	}
	if d.Handoff != plan {
		t.Errorf("handoff = %q, want %q as it stands on the branch", d.Handoff, plan)
	}
	got := map[string]client.CheckResult{}
	for _, c := range d.Checks {
		got[c.Name] = c
	}
	if c, ok := got["passing"]; !ok || !c.Passed || !strings.Contains(c.Output, "all good") {
		t.Errorf("passing check = %+v, want passed with its output", c)
	}
	if c, ok := got["failing"]; !ok || c.Passed || !strings.Contains(c.Output, "not good") {
		t.Errorf("failing check = %+v, want failed with its output", c)
	}
	files := map[string]client.DiffFile{}
	for _, f := range d.Diff.Files {
		files[f.Path] = f
	}
	if f, ok := files["work.txt"]; !ok || f.Insertions != 1 || f.Deletions != 0 {
		t.Errorf("diff for work.txt = %+v, want one line added", f)
	}
	if f, ok := files[handoffPath]; !ok || f.Insertions != 2 || f.Deletions != 0 {
		t.Errorf("diff for the handoff = %+v, want two lines added", f)
	}
	if d.Diff.Insertions != 3 || d.Diff.Deletions != 0 || len(d.Diff.Files) != 2 {
		t.Errorf("diff totals = %+v, want two files, three added, none removed", d.Diff)
	}
}

func TestS7JobsShowPrintsHandoffAndDiff(t *testing.T) {
	l, job := detailedJob(t)

	out := mustOwl(t, l, "jobs", "show", job).stdout
	if !strings.Contains(out, "\nhandoff:\n"+strings.TrimRight(plan, "\n")+"\n") {
		t.Errorf("owl jobs show does not print the handoff:\n%s", out)
	}
	if got := line(t, out, "diff"); got != "2 files changed, 3 added, 0 removed" {
		t.Errorf("diff = %q, want %q", got, "2 files changed, 3 added, 0 removed")
	}
}

func TestS8LogViewStreamsARunningRun(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, s := agentLayout(t, script, 0)
	daemonUp(t, l)
	app, ev := desktopApp(t, l)
	runnableJob(t, l, "work")
	run, job := startRun(t, l)

	if err := app.FollowLog(id(t, run)); err != nil {
		t.Fatal(err)
	}
	first := ev.next(t)
	if first.name != desktop.EventLogLine {
		t.Fatalf("first event = %q, want %q", first.name, desktop.EventLogLine)
	}
	if ln, ok := first.data.(desktop.LogLine); !ok || ln.RunID != id(t, run) || ln.Line != agentScript[0] {
		t.Fatalf("first line = %+v, want %q for run %s", first.data, agentScript[0], run)
	}
	ev.none(t)

	s.let(t)

	second := ev.next(t)
	if ln, ok := second.data.(desktop.LogLine); second.name != desktop.EventLogLine || !ok || ln.Line != agentScript[2] {
		t.Errorf("second event = %q %+v, want line %q", second.name, second.data, agentScript[2])
	}
	end := ev.next(t)
	if e, ok := end.data.(desktop.LogEnd); end.name != desktop.EventLogEnd || !ok || e.RunID != id(t, run) || e.Error != "" {
		t.Errorf("end event = %q %+v, want a clean end for run %s", end.name, end.data, run)
	}
	finished(t, l, job)

	err := app.FollowLog(999999)
	var se *client.StatusError
	if !errors.As(err, &se) || se.Kind != client.KindNotFound {
		t.Errorf("following a run that does not exist: err = %v, want not found", err)
	}
	ev.none(t)
}

func TestS9StartAcceptAndDropAreReflectedInOwlStatus(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	app, _ := desktopApp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "first", "--no-plan")
	addJob(t, l, r.dir, "second", "--no-plan")

	res, err := app.Start()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Started || res.Job.Prompt != "first" {
		t.Fatalf("start = %+v, want the first job started", res)
	}
	first := strconv.FormatInt(res.Job.ID, 10)
	waitRun(t, l, first, strconv.FormatInt(res.Run.ID, 10))
	if got := jobState(t, l, first); got != "review" {
		t.Fatalf("state = %q, want review", got)
	}
	if _, err := app.Accept(res.Job.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := jobState(t, l, first); got != "done" {
		t.Errorf("after accept, state = %q, want done", got)
	}

	res, err = app.Start()
	if err != nil || !res.Started || res.Job.Prompt != "second" {
		t.Fatalf("start = %+v, %v, want the second job started", res, err)
	}
	second := strconv.FormatInt(res.Job.ID, 10)
	waitRun(t, l, second, strconv.FormatInt(res.Run.ID, 10))
	branch := line(t, mustOwl(t, l, "jobs", "show", second).stdout, "branch")
	if _, err := app.Drop(res.Job.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := jobState(t, l, second); got != "cancelled" {
		t.Errorf("after drop, state = %q, want cancelled", got)
	}
	if hasBranch(t, r, branch) {
		t.Errorf("after drop, the branch %s is still there", branch)
	}

	res, err = app.Start()
	if err != nil || res.Started {
		t.Errorf("start on an empty queue = %+v, %v, want nothing started and no error", res, err)
	}
}

var (
	hexColour  = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	funcColour = regexp.MustCompile(`\b(rgba?|hsla?)\(`)
	hslDecl    = regexp.MustCompile(`hsla?\(\s*(\d+(?:\.\d+)?)`)
)

func TestS10ThemeIsOneTokensFile(t *testing.T) {
	src := filepath.Join(repoDir, "cmd", "owl-desktop", "frontend", "src")
	tokens, err := os.ReadFile(filepath.Join(src, "theme", "tokens.css"))
	if err != nil {
		t.Fatalf("no tokens file: %v", err)
	}
	for _, want := range []string{"--color-", "--blur-", "--radius-", "--space-"} {
		if !strings.Contains(string(tokens), want) {
			t.Errorf("tokens.css declares no %s* custom properties", want)
		}
	}
	if hexColour.Match(tokens) {
		t.Errorf("tokens.css uses hex colours; write them as hsl() so the hue can be checked")
	}
	for _, m := range hslDecl.FindAllStringSubmatch(string(tokens), -1) {
		hue, _ := strconv.ParseFloat(m[1], 64)
		if hue != 0 && (hue < 200 || hue > 260) {
			t.Errorf("tokens.css has hue %v; the palette is night-sky and navy blues (200 to 260) or grey", hue)
		}
	}

	err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) == "tokens.css" {
			return nil
		}
		switch filepath.Ext(path) {
		case ".css", ".tsx", ".ts", ".html":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if hexColour.Match(data) || funcColour.Match(data) {
			t.Errorf("%s carries a colour literal; colours come from theme/tokens.css", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestS11AppBuilds(t *testing.T) {
	if os.Getenv("OWL_DESKTOP_BUILD") == "" {
		t.Skip("set OWL_DESKTOP_BUILD=1 to build the desktop app; it downloads the frontend's dependencies")
	}
	cmd := exec.Command("make", "desktop")
	cmd.Dir = repoDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("make desktop: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(repoDir, "cmd", "owl-desktop", "build", "bin", "*.app"))
	if len(matches) == 0 {
		t.Errorf("make desktop left no app bundle under cmd/owl-desktop/build/bin")
	}
}

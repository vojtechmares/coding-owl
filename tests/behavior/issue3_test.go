package behavior_test

// Behavior tests for issue #3. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-3.md. They drive the built owl binary from the outside
// against a daemon started as a subprocess, over temporary git repositories
// with real commits on their base branch.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// repo is a temporary git repository whose base branch carries commits.
type repo struct {
	t   *testing.T
	dir string
	env []string
}

// newRepo initialises a repository at <layout root>/repos/<rel> on branch main
// with one commit. rel may contain separators, so a test can control the
// basename owl project add proposes.
func newRepo(t *testing.T, l *layout, rel string) *repo {
	t.Helper()
	dir := filepath.Join(l.root, "repos", rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &repo{t: t, dir: dir, env: append(append([]string{}, l.env...),
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)}
	r.git("init", "-b", "main")
	r.commit("README.md", "# test\n", "initial commit")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v in %s: %v\n%s", args, r.dir, err, out)
	}
	return string(out)
}

// write puts content in the working tree without committing it.
func (r *repo) write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(path, content, msg string) {
	r.t.Helper()
	r.write(path, content)
	r.git("add", "--", path)
	r.git("commit", "-m", msg)
}

func (r *repo) removeCommitted(path string) {
	r.t.Helper()
	r.git("rm", "-q", "--", path)
	r.git("commit", "-m", "remove "+path)
}

// owlConfig is a minimal valid Project configuration file.
func owlConfig(branchPrefix string) string {
	return "apiVersion: codingowl.dev/v1\nbranchPrefix: " + branchPrefix + "\n"
}

// daemonUp starts a daemon for the layout and waits for its socket. It also
// checks that the daemon is keeping credentials in a file: a test that put a
// secret in the user's own keychain would be a bug in the harness, and this is
// where it would be caught (ADR-0019).
func daemonUp(t *testing.T, l *layout) *daemonProc {
	t.Helper()
	p := startDaemon(t, l)
	waitForSocket(t, l.socket())
	if !strings.Contains(p.out(), `store="file `) {
		t.Fatalf("the daemon is not keeping credentials in a file of its own:\n%s", p.out())
	}
	return p
}

func stopDaemon(t *testing.T, p *daemonProc) {
	t.Helper()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := p.exit(t, 5*time.Second); code != 0 {
		t.Fatalf("daemon exited %d:\n%s", code, p.out())
	}
}

func mustOwl(t *testing.T, l *layout, args ...string) result {
	t.Helper()
	res := runOwl(t, l, args...)
	if res.code != 0 {
		t.Fatalf("owl %v exited %d\nstdout:\n%s\nstderr:\n%s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

// listedNames returns the Project names in the order owl project list printed
// them, skipping the header and the empty-list line.
func listedNames(t *testing.T, l *layout) []string {
	t.Helper()
	var names []string
	for _, f := range listFields(t, l) {
		names = append(names, f[0])
	}
	return names
}

// listRows keys the path column by name, for checks that do not care about
// the order the rows were printed in.
func listRows(t *testing.T, l *layout) map[string]string {
	t.Helper()
	rows := map[string]string{}
	for _, f := range listFields(t, l) {
		rows[f[0]] = f[1]
	}
	return rows
}

func listFields(t *testing.T, l *layout) [][]string {
	t.Helper()
	out := mustOwl(t, l, "project", "list").stdout
	var rows [][]string
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 || f[0] == "NAME" || strings.HasPrefix(ln, "no projects") {
			continue
		}
		if len(f) < 2 {
			t.Fatalf("owl project list row %q has no path column", ln)
		}
		rows = append(rows, f)
	}
	return rows
}

// listNames returns the Project names shown by owl project list, in order.
func listNames(t *testing.T, l *layout) []string {
	t.Helper()
	return listedNames(t, l)
}

func wantNames(t *testing.T, l *layout, want ...string) {
	t.Helper()
	got := listNames(t, l)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("owl project list = %v, want %v", got, want)
	}
}

// addProject registers r under the given extra flags and fails on error. A
// Project whose layout can run an Agent is also given an Account to run on,
// since every Job runs on its Project's (ADR-0023); a scenario that is about
// Accounts registers its Projects itself.
func addProject(t *testing.T, l *layout, r *repo, flags ...string) {
	t.Helper()
	res := mustOwl(t, l, append([]string{"project", "add", r.dir}, flags...)...)
	if l.agent {
		runsOn(t, l, r, registeredName(t, res.stdout))
	}
}

var registeredRE = regexp.MustCompile(`registered (\S+) at `)

// registeredName is the name owl project add gave the Project it registered.
func registeredName(t *testing.T, out string) string {
	t.Helper()
	m := registeredRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("owl project add did not say what it registered:\n%s", out)
	}
	return m[1]
}

// wantLine fails unless the key line of out has the wanted value.
func wantLine(t *testing.T, out, key, want string) {
	t.Helper()
	if got := line(t, out, key); got != want {
		t.Errorf("%q line = %q, want %q\nfull output:\n%s", key, got, want, out)
	}
}

func TestS1AddProposesBasename(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")

	addProject(t, l, r)

	rows := listRows(t, l)
	if got, ok := rows["api"]; !ok || got != r.dir {
		t.Errorf("owl project list = %v, want one row api -> %s", rows, r.dir)
	}
	show := mustOwl(t, l, "project", "show", "api").stdout
	wantLine(t, show, "name", "api")
	wantLine(t, show, "path", r.dir)
	wantNames(t, l, "api")
}

func TestS2NameFlagOverrides(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")

	addProject(t, l, r, "--name", "backend")

	wantNames(t, l, "backend")
	if res := runOwl(t, l, "project", "show", "api"); res.code == 0 {
		t.Errorf("owl project show api succeeded after --name backend:\n%s", res.stdout)
	}
}

func TestS3DuplicateNameRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	first := newRepo(t, l, "api")
	second := newRepo(t, l, "other/api")
	addProject(t, l, first)

	res := runOwl(t, l, "project", "add", second.dir)

	if res.code == 0 {
		t.Fatalf("duplicate name accepted:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "api") || !strings.Contains(res.stderr, "--name") {
		t.Errorf("stderr does not name the project and the --name flag:\n%s", res.stderr)
	}
	wantNames(t, l, "api")
}

func TestS4NonRepositoryRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	plain := filepath.Join(l.root, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "project", "add", plain)

	if res.code == 0 {
		t.Fatalf("non-repository accepted:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "not a git repository") || !strings.Contains(res.stderr, plain) {
		t.Errorf("stderr does not say the path is not a git repository:\n%s", res.stderr)
	}
	wantNames(t, l)
}

func TestS5InvalidNameRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")

	res := runOwl(t, l, "project", "add", r.dir, "--name", "a/b")

	if res.code == 0 {
		t.Fatalf("invalid name accepted:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "invalid") {
		t.Errorf("stderr does not say the name is invalid:\n%s", res.stderr)
	}
	wantNames(t, l)
}

func TestS6ShowNamesRootConfig(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("root/"), "add config")
	addProject(t, l, r)

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "name", "api")
	wantLine(t, show, "path", r.dir)
	wantLine(t, show, "base branch", "main")
	wantLine(t, show, "config", "main:.coding-owl.yaml")
	wantLine(t, show, "branch prefix", "root/")
}

func TestS7DiscoveryOrderFirstMatchWins(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	order := []struct{ path, prefix string }{
		{".coding-owl.yaml", "a/"},
		{".config/coding-owl.yaml", "b/"},
		{".config/.coding-owl.yaml", "c/"},
		{".meta/coding-owl.yaml", "d/"},
		{".meta/.coding-owl.yaml", "e/"},
	}
	for _, c := range order {
		r.commit(c.path, owlConfig(c.prefix), "add "+c.path)
	}
	addProject(t, l, r)

	for _, c := range order {
		show := mustOwl(t, l, "project", "show", "api").stdout
		wantLine(t, show, "config", "main:"+c.path)
		wantLine(t, show, "branch prefix", c.prefix)
		r.removeCommitted(c.path)
	}
}

func TestS8ConfigHomeFallback(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	fallback := filepath.Join(l.config, "coding-owl", "api", "config.yaml")
	writeFile(t, fallback, owlConfig("fallback/"))

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "config", fallback)
	wantLine(t, show, "branch prefix", "fallback/")
}

func TestS9InRepoOutranksFallback(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("root/"), "add config")
	addProject(t, l, r)
	writeFile(t, filepath.Join(l.config, "coding-owl", "api", "config.yaml"), owlConfig("fallback/"))

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "config", "main:.coding-owl.yaml")
	wantLine(t, show, "branch prefix", "root/")
}

func TestS10NoConfigUsesDefaults(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	addProject(t, l, r)

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "config", "(none)")
	wantLine(t, show, "branch prefix", "owl/")
}

func TestS11ConfigReadFromBaseBranchNotWorktree(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("committed/"), "add config")
	addProject(t, l, r)
	r.write(".coding-owl.yaml", owlConfig("dirty/"))

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "branch prefix", "committed/")
}

func TestS12ConfigOnAnotherBranchIgnored(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("committed/"), "add config")
	addProject(t, l, r)
	r.git("checkout", "-q", "-b", "feature")
	r.commit(".coding-owl.yaml", owlConfig("feature/"), "change config on feature")

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "branch prefix", "committed/")
	wantLine(t, show, "config", "main:.coding-owl.yaml")
}

func TestS13UnknownAPIVersionRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v99\nbranchPrefix: nope/\n", "add config")
	addProject(t, l, r)

	res := runOwl(t, l, "project", "show", "api")

	if res.code == 0 {
		t.Fatalf("unknown apiVersion accepted:\n%s", res.stdout)
	}
	for _, want := range []string{".coding-owl.yaml", "apiVersion", "codingowl.dev/v99"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not mention %q:\n%s", want, res.stderr)
		}
	}
}

func TestS14MissingAPIVersionRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "branchPrefix: nope/\n", "add config")
	addProject(t, l, r)

	res := runOwl(t, l, "project", "show", "api")

	if res.code == 0 {
		t.Fatalf("missing apiVersion accepted:\n%s", res.stdout)
	}
	for _, want := range []string{".coding-owl.yaml", "apiVersion"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not mention %q:\n%s", want, res.stderr)
		}
	}
}

func TestS15MoveKeepsIdentity(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	before := mustOwl(t, l, "project", "show", "api").stdout

	moved := filepath.Join(l.root, "repos", "moved")
	if err := os.Rename(r.dir, moved); err != nil {
		t.Fatal(err)
	}
	mustOwl(t, l, "project", "move", "api", moved)

	after := mustOwl(t, l, "project", "show", "api").stdout
	wantLine(t, after, "path", moved)
	wantLine(t, after, "name", line(t, before, "name"))
	wantLine(t, after, "registered", line(t, before, "registered"))
}

func TestS16MoveToNonRepositoryRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	plain := filepath.Join(l.root, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "project", "move", "api", plain)

	if res.code == 0 {
		t.Fatalf("move to a non-repository accepted:\n%s", res.stdout)
	}
	wantLine(t, mustOwl(t, l, "project", "show", "api").stdout, "path", r.dir)
}

func TestS17RenameMovesConfigDirectory(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	content := owlConfig("fallback/")
	writeFile(t, filepath.Join(l.config, "coding-owl", "api", "config.yaml"), content)

	mustOwl(t, l, "project", "rename", "api", "backend")

	got, err := os.ReadFile(filepath.Join(l.config, "coding-owl", "backend", "config.yaml"))
	if err != nil {
		t.Fatalf("config directory was not moved: %v", err)
	}
	if string(got) != content {
		t.Errorf("moved config = %q, want %q", got, content)
	}
	if _, err := os.Stat(filepath.Join(l.config, "coding-owl", "api")); !os.IsNotExist(err) {
		t.Errorf("old config directory still exists (err=%v)", err)
	}
	mustOwl(t, l, "project", "show", "backend")
	if res := runOwl(t, l, "project", "show", "api"); res.code == 0 {
		t.Errorf("owl project show api succeeded after rename:\n%s", res.stdout)
	}
}

func TestS18FailedRenameLeavesNothingBehind(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	apiRepo := newRepo(t, l, "api")
	backendRepo := newRepo(t, l, "backend")
	addProject(t, l, apiRepo)
	addProject(t, l, backendRepo)
	apiCfg := filepath.Join(l.config, "coding-owl", "api", "config.yaml")
	backendCfg := filepath.Join(l.config, "coding-owl", "backend", "config.yaml")
	writeFile(t, apiCfg, owlConfig("api/"))
	writeFile(t, backendCfg, owlConfig("backend/"))

	res := runOwl(t, l, "project", "rename", "api", "backend")

	if res.code == 0 {
		t.Fatalf("rename onto an existing name accepted:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "backend") || !strings.Contains(res.stderr, "already registered") {
		t.Errorf("stderr does not say a project named backend already exists:\n%s", res.stderr)
	}
	wantNames(t, l, "api", "backend")
	wantFileContent(t, apiCfg, owlConfig("api/"))
	wantFileContent(t, backendCfg, owlConfig("backend/"))
}

func TestS19RemoveDropsProject(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	addProject(t, l, r)

	mustOwl(t, l, "project", "remove", "api")

	wantNames(t, l)
	res := runOwl(t, l, "project", "show", "api")
	if res.code == 0 {
		t.Fatalf("owl project show succeeded after remove:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "api") || !strings.Contains(res.stderr, "no such project") {
		t.Errorf("stderr does not say there is no such project:\n%s", res.stderr)
	}
}

func TestS20UnknownProjectFailsClearly(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")

	for _, args := range [][]string{
		{"project", "show", "ghost"},
		{"project", "move", "ghost", r.dir},
		{"project", "rename", "ghost", "other"},
		{"project", "remove", "ghost"},
	} {
		res := runOwl(t, l, args...)
		if res.code == 0 {
			t.Errorf("owl %v succeeded for an unknown project:\n%s", args, res.stdout)
		}
		if !strings.Contains(res.stderr, "ghost") {
			t.Errorf("owl %v stderr does not name ghost:\n%s", args, res.stderr)
		}
	}
}

func TestS21MigrationsRunOnStartAndAreIdempotent(t *testing.T) {
	l := newLayout(t)
	db := filepath.Join(l.data, "coding-owl", "owl.db")

	first := daemonUp(t, l)
	schema1, applied1 := databaseLog(t, first)
	if applied1 == "0" {
		t.Errorf("first start applied no migrations (schema=%s)", schema1)
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("database not created at %s: %v", db, err)
	}
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	stopDaemon(t, first)

	second := daemonUp(t, l)
	schema2, applied2 := databaseLog(t, second)
	if schema2 != schema1 {
		t.Errorf("second start moved the schema from %s to %s", schema1, schema2)
	}
	if applied2 != "0" {
		t.Errorf("second start applied %s migrations, want 0", applied2)
	}
	wantNames(t, l, "api")
	stopDaemon(t, second)

	third := daemonUp(t, l)
	schema3, applied3 := databaseLog(t, third)
	if schema3 != schema1 || applied3 != "0" {
		t.Errorf("third start: schema=%s applied=%s, want schema=%s applied=0", schema3, applied3, schema1)
	}
}

// databaseLog waits for the daemon's database line and returns the schema
// version it reports and how many migrations that start applied.
func databaseLog(t *testing.T, p *daemonProc) (schema, applied string) {
	t.Helper()
	waitForLog(t, p, "database ready")
	m := regexp.MustCompile(`schema=(\S+) applied=(\S+)`).FindStringSubmatch(p.out())
	if m == nil {
		t.Fatalf("daemon log has no schema/applied fields:\n%s", p.out())
	}
	return m[1], m[2]
}

func TestS22ProjectCommandsReportStoppedDaemon(t *testing.T) {
	l := newLayout(t)

	res := runOwl(t, l, "project", "list")

	if res.code == 0 {
		t.Fatalf("owl project list succeeded with no daemon:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "daemon not running") || !strings.Contains(res.stderr, l.socket()) {
		t.Errorf("stderr does not report a stopped daemon and its socket:\n%s", res.stderr)
	}
}

func TestS23SubdirectoryRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	sub := filepath.Join(r.dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "project", "add", sub)

	if res.code == 0 {
		t.Fatalf("a subdirectory was accepted as a project:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "root") || !strings.Contains(res.stderr, r.dir) {
		t.Errorf("stderr does not point at the repository root %s:\n%s", r.dir, res.stderr)
	}
}

func TestS24BaseBranchFlag(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("main/"), "config on main")
	r.git("checkout", "-q", "-b", "develop")
	r.commit(".coding-owl.yaml", owlConfig("dev/"), "config on develop")
	r.git("checkout", "-q", "main")

	addProject(t, l, r, "--base-branch", "develop")

	show := mustOwl(t, l, "project", "show", "api").stdout
	wantLine(t, show, "base branch", "develop")
	wantLine(t, show, "config", "develop:.coding-owl.yaml")
	wantLine(t, show, "branch prefix", "dev/")
}

func TestS25UnknownBaseBranchRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")

	res := runOwl(t, l, "project", "add", r.dir, "--base-branch", "nope")

	if res.code == 0 {
		t.Fatalf("unknown base branch accepted:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "nope") {
		t.Errorf("stderr does not name the branch:\n%s", res.stderr)
	}
	wantNames(t, l)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func wantFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}

func TestS26RepositoryWithNoCommitsRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	dir := filepath.Join(l.root, "repos", "empty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	empty := &repo{t: t, dir: dir, env: append(append([]string{}, l.env...),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")}
	empty.git("init", "-b", "main")

	res := runOwl(t, l, "project", "add", dir)

	if res.code == 0 {
		t.Fatalf("a repository with no commits was accepted:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "main") {
		t.Errorf("stderr does not name the branch:\n%s", res.stderr)
	}
	wantNames(t, l)
}

func TestS27VanishedBaseBranchReported(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("committed/"), "add config")
	addProject(t, l, r)
	r.git("checkout", "-q", "-b", "other")
	r.git("branch", "-q", "-D", "main")

	res := runOwl(t, l, "project", "show", "api")

	if res.code == 0 {
		t.Fatalf("show fell back to defaults after the base branch was deleted:\n%s", res.stdout)
	}
	for _, want := range []string{"main", "api"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not mention %q:\n%s", want, res.stderr)
		}
	}
	if strings.Contains(res.stdout, "branch prefix: owl/") {
		t.Errorf("show reported the default configuration anyway:\n%s", res.stdout)
	}
}

func TestS28VanishedRepositoryReported(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	if err := os.RemoveAll(r.dir); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "project", "show", "api")

	if res.code == 0 {
		t.Fatalf("show succeeded for a Project whose repository is gone:\n%s", res.stdout)
	}
	if strings.Contains(res.stdout, "config: (none)") {
		t.Errorf("show reported a missing repository as a Project with no configuration:\n%s", res.stdout)
	}
}

func TestS29MoveRefusesAPathWithoutTheBaseBranch(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	other := newRepo(t, l, "other")
	other.git("branch", "-m", "main", "trunk")

	res := runOwl(t, l, "project", "move", "api", other.dir)

	if res.code == 0 {
		t.Fatalf("move accepted a repository without the base branch:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "main") {
		t.Errorf("stderr does not name the base branch:\n%s", res.stderr)
	}
	wantLine(t, mustOwl(t, l, "project", "show", "api").stdout, "path", r.dir)
}

func TestS30RevisionExpressionIsNotABaseBranch(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("root/"), "add config")

	res := runOwl(t, l, "project", "add", r.dir, "--base-branch", "main:.coding-owl.yaml")

	if res.code == 0 {
		t.Fatalf("a revision expression was accepted as a base branch:\n%s", res.stdout)
	}
	wantNames(t, l)
}

func TestS31MutationsWorkWhenConfigCannotBeLoaded(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "bad")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v99\n", "add unloadable config")
	addProject(t, l, r)
	if res := runOwl(t, l, "project", "show", "bad"); res.code == 0 {
		t.Fatalf("precondition: show should refuse this config:\n%s", res.stdout)
	}

	moved := newRepo(t, l, "moved")
	moved.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v99\n", "add unloadable config")
	if res := runOwl(t, l, "project", "move", "bad", moved.dir); res.code != 0 {
		t.Errorf("move reported failure (exit %d) on a Project whose config cannot be loaded:\n%s", res.code, res.stderr)
	}
	if got := listRows(t, l)["bad"]; got != moved.dir {
		t.Errorf("after move, list shows %q, want %q", got, moved.dir)
	}

	if res := runOwl(t, l, "project", "rename", "bad", "good"); res.code != 0 {
		t.Errorf("rename reported failure (exit %d) on a Project whose config cannot be loaded:\n%s", res.code, res.stderr)
	}
	rows := listRows(t, l)
	if _, stillThere := rows["bad"]; stillThere {
		t.Errorf("rename left the old name behind: %v", rows)
	}
	if got := rows["good"]; got != moved.dir {
		t.Errorf("after rename, list shows good -> %q, want %q", got, moved.dir)
	}
}

func TestListIsPrintedInNameOrder(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	for _, n := range []string{"web", "api", "tools"} {
		addProject(t, l, newRepo(t, l, n))
	}

	if got := strings.Join(listedNames(t, l), ","); got != "api,tools,web" {
		t.Errorf("owl project list order = %s, want api,tools,web", got)
	}
}

func TestS32TagDoesNotShadowTheBaseBranch(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("branch/"), "config on main")
	r.git("checkout", "-q", "-b", "other")
	r.commit(".coding-owl.yaml", owlConfig("tag/"), "config on other")
	r.git("tag", "main", "other")
	r.git("checkout", "-q", "main")
	addProject(t, l, r)

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "branch prefix", "branch/")
	wantLine(t, show, "config", "main:.coding-owl.yaml")
}

func TestS33DaemonEnvironmentCannotRedirectReads(t *testing.T) {
	l := newLayout(t)
	decoy := newRepo(t, l, "decoy")
	decoy.commit(".coding-owl.yaml", owlConfig("decoy/"), "config in the decoy")

	// The daemon is started from an environment that names another
	// repository, as it would be if launched from inside a git hook.
	poisoned := l.withEnv("GIT_DIR="+filepath.Join(decoy.dir, ".git"), "GIT_WORK_TREE="+decoy.dir)
	startDaemon(t, poisoned)
	waitForSocket(t, l.socket())

	own := newRepo(t, l, "api")
	own.commit(".coding-owl.yaml", owlConfig("own/"), "config in the project")
	addProject(t, l, own)

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "branch prefix", "own/")
	wantLine(t, show, "path", own.dir)
}

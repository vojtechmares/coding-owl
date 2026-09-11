package project_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/skill"
	"github.com/vojtechmares/coding-owl/internal/store"
)

var ctx = context.Background()

// fixture is a service over a temporary database and config home.
type fixture struct {
	svc        *project.Service
	configHome string
	root       string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	st, _, err := store.Open(filepath.Join(root, "data", "owl.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	configHome := filepath.Join(root, "config", "coding-owl")
	return &fixture{svc: project.NewService(st, configHome), configHome: configHome, root: root}
}

// repo initialises a repository named rel on branch main with one commit.
func (f *fixture) repo(t *testing.T, rel string) string {
	t.Helper()
	dir := filepath.Join(f.root, "repos", rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f.git(t, dir, "init", "-b", "main")
	f.commit(t, dir, "README.md", "# test\n")
	return dir
}

func (f *fixture) git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (f *fixture) commit(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f.git(t, dir, "add", "--", path)
	f.git(t, dir, "commit", "-m", "add "+path)
}

func owlConfig(prefix string) string {
	return "apiVersion: codingowl.dev/v1\nbranchPrefix: " + prefix + "\n"
}

func TestAddProposesTheBasenameAndShowReportsTheRootConfig(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	f.commit(t, dir, ".coding-owl.yaml", owlConfig("root/"))

	p, err := f.svc.Add(ctx, project.AddRequest{Path: dir})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if p.Name != "api" {
		t.Errorf("Name = %q, want %q", p.Name, "api")
	}
	if p.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q, want %q", p.BaseBranch, "main")
	}

	d, err := f.svc.Show(ctx, "api")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if d.ConfigSource != "main:.coding-owl.yaml" {
		t.Errorf("ConfigSource = %q, want %q", d.ConfigSource, "main:.coding-owl.yaml")
	}
	if d.Config.BranchPrefix != "root/" {
		t.Errorf("BranchPrefix = %q, want %q", d.Config.BranchPrefix, "root/")
	}
}

func TestAddRefusesADuplicateNameAndNamesTheFlag(t *testing.T) {
	f := newFixture(t)
	first := f.repo(t, "api")
	second := f.repo(t, "other/api")
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: first}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	_, err := f.svc.Add(ctx, project.AddRequest{Path: second})

	if err == nil {
		t.Fatal("Add accepted a duplicate name")
	}
	for _, want := range []string{"api", "--name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestAddRefusesANonRepository(t *testing.T) {
	f := newFixture(t)
	plain := filepath.Join(f.root, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.Add(ctx, project.AddRequest{Path: plain})

	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("Add error = %v, want a not-a-repository error", err)
	}
}

func TestAddRefusesASubdirectoryAndNamesTheRoot(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.Add(ctx, project.AddRequest{Path: sub})

	if err == nil || !strings.Contains(err.Error(), "not the root") {
		t.Fatalf("Add error = %v, want a not-the-root error", err)
	}
	root, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(err.Error(), root) {
		t.Errorf("error %q does not name the repository root %s", err, root)
	}
}

func TestAddRefusesAnInvalidName(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")

	for _, name := range []string{"a/b", "..", "-x", " api"} {
		if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir, Name: name}); err == nil {
			t.Errorf("Add accepted the name %q", name)
		} else if !strings.Contains(err.Error(), "invalid") {
			t.Errorf("Add(%q) error = %v, want an invalid-name error", name, err)
		}
	}
}

func TestAddRefusesAnUnknownBaseBranch(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")

	_, err := f.svc.Add(ctx, project.AddRequest{Path: dir, BaseBranch: "nope"})

	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("Add error = %v, want an error naming the branch", err)
	}
}

func TestAddHonoursTheBaseBranch(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	f.commit(t, dir, ".coding-owl.yaml", owlConfig("main/"))
	f.git(t, dir, "checkout", "-q", "-b", "develop")
	f.commit(t, dir, ".coding-owl.yaml", owlConfig("dev/"))
	f.git(t, dir, "checkout", "-q", "main")

	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir, BaseBranch: "develop"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	d, err := f.svc.Show(ctx, "api")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if d.BaseBranch != "develop" || d.ConfigSource != "develop:.coding-owl.yaml" || d.Config.BranchPrefix != "dev/" {
		t.Errorf("Show = base %q, source %q, prefix %q; want develop, develop:.coding-owl.yaml, dev/",
			d.BaseBranch, d.ConfigSource, d.Config.BranchPrefix)
	}
}

func TestDiscoveryOrderFirstMatchWins(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	order := []struct{ path, prefix string }{
		{".coding-owl.yaml", "a/"},
		{".config/coding-owl.yaml", "b/"},
		{".config/.coding-owl.yaml", "c/"},
		{".meta/coding-owl.yaml", "d/"},
		{".meta/.coding-owl.yaml", "e/"},
	}
	for _, c := range order {
		f.commit(t, dir, c.path, owlConfig(c.prefix))
	}
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// The config-home fallback is present throughout and must never win while
	// any in-repo form is left.
	writeFile(t, filepath.Join(f.configHome, "api", "config.yaml"), owlConfig("fallback/"))

	for _, c := range order {
		d, err := f.svc.Show(ctx, "api")
		if err != nil {
			t.Fatalf("Show: %v", err)
		}
		if d.ConfigSource != "main:"+c.path || d.Config.BranchPrefix != c.prefix {
			t.Fatalf("Show = source %q, prefix %q; want main:%s, %s", d.ConfigSource, d.Config.BranchPrefix, c.path, c.prefix)
		}
		f.git(t, dir, "rm", "-q", "--", c.path)
		f.git(t, dir, "commit", "-m", "remove "+c.path)
	}

	d, err := f.svc.Show(ctx, "api")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if d.Config.BranchPrefix != "fallback/" {
		t.Errorf("with no in-repo config the fallback should win, got source %q prefix %q", d.ConfigSource, d.Config.BranchPrefix)
	}
}

func TestShowFallsBackToDefaults(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	d, err := f.svc.Show(ctx, "api")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if d.ConfigSource != "" {
		t.Errorf("ConfigSource = %q, want empty", d.ConfigSource)
	}
	if d.Config.BranchPrefix != "owl/" {
		t.Errorf("BranchPrefix = %q, want the default owl/", d.Config.BranchPrefix)
	}
}

func TestShowReadsTheBaseBranchNotTheWorktree(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	f.commit(t, dir, ".coding-owl.yaml", owlConfig("committed/"))
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".coding-owl.yaml"), []byte(owlConfig("dirty/")), 0o644); err != nil {
		t.Fatal(err)
	}
	f.git(t, dir, "checkout", "-q", "-b", "feature")
	f.commit(t, dir, ".coding-owl.yaml", owlConfig("feature/"))

	d, err := f.svc.Show(ctx, "api")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if d.Config.BranchPrefix != "committed/" {
		t.Errorf("BranchPrefix = %q, want committed/", d.Config.BranchPrefix)
	}
}

func TestShowReadsTheLockBesideTheConfigurationItChose(t *testing.T) {
	// A repository can carry more than one of the places a lockfile is looked
	// for. `owl skills` writes the one beside the configuration, so that is
	// the one a Run has to read: reading the other silently unpins everything.
	f := newFixture(t)
	dir := f.repo(t, "api")
	f.commit(t, dir, filepath.Join(".config", "coding-owl.yaml"), owlConfig("owl/"))
	f.commit(t, dir, filepath.Join(".config", ".coding-owl.lock.yaml"), lockOf("beside"))
	f.commit(t, dir, ".coding-owl.lock.yaml", lockOf("elsewhere"))
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	d, err := f.svc.Show(ctx, "api")

	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if _, ok := d.Lock.Skills["beside"]; !ok {
		t.Errorf("Lock records %v, want the lockfile beside the configuration", d.Lock.Names())
	}
}

// lockOf is a lockfile recording one skill of that name.
func lockOf(name string) string {
	return "apiVersion: codingowl.dev/v1\nskills:\n  " + name + ":\n" +
		"    source: x/" + name + "\n    ref: main\n    commit: 0123456789abcdef\n" +
		"    digest: sha256:ab1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd\n"
}

func TestSkillsAddRefusesWhenTheManifestIsNotInTheCheckout(t *testing.T) {
	// The Project is configured in its own repository, and the file is not in
	// the working tree: another branch, a rebase in flight, a deletion. Owl
	// writing one here would carry the skills and none of the checks, setup or
	// account the base branch's file has.
	f := newFixture(t)
	dir := f.repo(t, "api")
	f.commit(t, dir, ".coding-owl.yaml", owlConfig("owl/"))
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, ".coding-owl.yaml")); err != nil {
		t.Fatal(err)
	}
	skills := project.NewSkillService(f.svc, skill.NewService(skill.NewCache(filepath.Join(f.root, "skills"))))

	_, _, err := skills.Add(ctx, project.SkillRequest{Project: "api"}, "x/go-review", "", false)

	var invalid *project.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Add with no manifest in the checkout = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), ".coding-owl.yaml") {
		t.Errorf("the error %q does not name the file", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".coding-owl.yaml")); statErr == nil {
		t.Error("a manifest was written over a project configured on its base branch")
	}
}

func TestShowRefusesAnUnrecognisedAPIVersion(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	f.commit(t, dir, ".coding-owl.yaml", "apiVersion: codingowl.dev/v99\n")
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	_, err := f.svc.Show(ctx, "api")

	if err == nil {
		t.Fatal("Show accepted an unrecognised apiVersion")
	}
	for _, want := range []string{"main:.coding-owl.yaml", "apiVersion", "codingowl.dev/v99"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestShowUnknownProject(t *testing.T) {
	f := newFixture(t)

	if _, err := f.svc.Show(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Show error = %v, want ErrNotFound", err)
	}
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

func TestList(t *testing.T) {
	f := newFixture(t)
	for _, n := range []string{"web", "api"} {
		if _, err := f.svc.Add(ctx, project.AddRequest{Path: f.repo(t, n)}); err != nil {
			t.Fatalf("Add(%s): %v", n, err)
		}
	}

	got, err := f.svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, p := range got {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "api,web" {
		t.Errorf("List = %v, want api,web", names)
	}
}

func TestMoveUpdatesThePathAndKeepsIdentity(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	before, err := f.svc.Add(ctx, project.AddRequest{Path: dir})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	moved := filepath.Join(f.root, "repos", "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Move(ctx, "api", moved); err != nil {
		t.Fatalf("Move: %v", err)
	}

	after, err := f.svc.Show(ctx, "api")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if after.Path != moved {
		t.Errorf("Path = %q, want %q", after.Path, moved)
	}
	if after.Name != before.Name || !after.Registered.Equal(before.Registered) {
		t.Errorf("identity changed: %+v -> %+v", before, after.Project)
	}
}

func TestMoveRefusesANonRepository(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	plain := filepath.Join(f.root, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Move(ctx, "api", plain); err == nil {
		t.Fatal("Move accepted a directory that is not a repository")
	}

	d, err := f.svc.Show(ctx, "api")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if d.Path != dir {
		t.Errorf("Path = %q, want the original %q", d.Path, dir)
	}
}

func TestMoveUnknownProject(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")

	if _, err := f.svc.Move(ctx, "ghost", dir); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Move error = %v, want ErrNotFound", err)
	}
}

func TestRenameMovesTheConfigurationDirectory(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: f.repo(t, "api")}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	content := owlConfig("fallback/")
	writeFile(t, filepath.Join(f.configHome, "api", "config.yaml"), content)

	if _, err := f.svc.Rename(ctx, "api", "backend"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(f.configHome, "backend", "config.yaml"))
	if err != nil {
		t.Fatalf("configuration directory was not moved: %v", err)
	}
	if string(got) != content {
		t.Errorf("moved config = %q, want %q", got, content)
	}
	if _, err := os.Stat(filepath.Join(f.configHome, "api")); !os.IsNotExist(err) {
		t.Errorf("old configuration directory survived (err=%v)", err)
	}
	if _, err := f.svc.Show(ctx, "backend"); err != nil {
		t.Errorf("Show(backend): %v", err)
	}
}

func TestRenameWithNoConfigurationDirectory(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: f.repo(t, "api")}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if _, err := f.svc.Rename(ctx, "api", "backend"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := f.svc.Show(ctx, "backend"); err != nil {
		t.Errorf("Show(backend): %v", err)
	}
}

func TestRenameOntoATakenNameLeavesNothingBehind(t *testing.T) {
	f := newFixture(t)
	for _, n := range []string{"api", "backend"} {
		if _, err := f.svc.Add(ctx, project.AddRequest{Path: f.repo(t, n)}); err != nil {
			t.Fatalf("Add(%s): %v", n, err)
		}
		writeFile(t, filepath.Join(f.configHome, n, "config.yaml"), owlConfig(n+"/"))
	}

	_, err := f.svc.Rename(ctx, "api", "backend")

	if err == nil || !strings.Contains(err.Error(), "backend") {
		t.Fatalf("Rename error = %v, want an error naming backend", err)
	}
	for _, n := range []string{"api", "backend"} {
		if _, err := f.svc.Show(ctx, n); err != nil {
			t.Errorf("Show(%s) after the refused rename: %v", n, err)
		}
		got, err := os.ReadFile(filepath.Join(f.configHome, n, "config.yaml"))
		if err != nil {
			t.Errorf("reading %s config: %v", n, err)
			continue
		}
		if string(got) != owlConfig(n+"/") {
			t.Errorf("%s config = %q, want %q", n, got, owlConfig(n+"/"))
		}
	}
}

func TestRenameRefusesAnInvalidName(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: f.repo(t, "api")}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if _, err := f.svc.Rename(ctx, "api", "a/b"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("Rename error = %v, want an invalid-name error", err)
	}
}

func TestRenameUnknownProject(t *testing.T) {
	f := newFixture(t)

	if _, err := f.svc.Rename(ctx, "ghost", "other"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Rename error = %v, want ErrNotFound", err)
	}
}

func TestRemove(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: f.repo(t, "api")}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if _, err := f.svc.Remove(ctx, "api"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := f.svc.Show(ctx, "api"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Show after Remove = %v, want ErrNotFound", err)
	}
}

func TestRemoveUnknownProject(t *testing.T) {
	f := newFixture(t)

	if _, err := f.svc.Remove(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Remove error = %v, want ErrNotFound", err)
	}
}

func TestShowReportsAVanishedBaseBranch(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	f.commit(t, dir, ".coding-owl.yaml", owlConfig("committed/"))
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	f.git(t, dir, "checkout", "-q", "-b", "other")
	f.git(t, dir, "branch", "-q", "-D", "main")

	_, err := f.svc.Show(ctx, "api")

	if err == nil {
		t.Fatal("Show fell back to the defaults instead of reporting the missing base branch")
	}
	for _, want := range []string{"main", "api"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestShowReportsAVanishedRepository(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Show(ctx, "api"); err == nil {
		t.Fatal("Show reported defaults for a Project whose repository is gone")
	}
}

func TestAddRefusesARepositoryWithNoCommits(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, "repos", "empty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f.git(t, dir, "init", "-b", "main")

	_, err := f.svc.Add(ctx, project.AddRequest{Path: dir})

	if err == nil || !strings.Contains(err.Error(), "main") {
		t.Errorf("Add error = %v, want an error naming the branch that does not exist yet", err)
	}
}

func TestAddRefusesARevisionExpressionAsBaseBranch(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	f.commit(t, dir, ".coding-owl.yaml", owlConfig("root/"))

	_, err := f.svc.Add(ctx, project.AddRequest{Path: dir, BaseBranch: "main:.coding-owl.yaml"})

	if err == nil {
		t.Fatal("Add accepted a revision expression as a base branch")
	}
}

func TestMoveRefusesAPathWithoutTheBaseBranch(t *testing.T) {
	f := newFixture(t)
	dir := f.repo(t, "api")
	if _, err := f.svc.Add(ctx, project.AddRequest{Path: dir}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	other := f.repo(t, "other")
	f.git(t, other, "branch", "-m", "main", "trunk")

	_, err := f.svc.Move(ctx, "api", other)

	if err == nil || !strings.Contains(err.Error(), "main") {
		t.Fatalf("Move error = %v, want an error naming the missing base branch", err)
	}
	d, err := f.svc.Show(ctx, "api")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if d.Path != dir {
		t.Errorf("Path = %q, want the original %q", d.Path, dir)
	}
}

package skill_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/skill"
)

// sourceRepo is a git repository standing in for a published Skill: a SKILL.md
// with the frontmatter the ecosystem expects, and a tag on an earlier commit
// than the branch.
type sourceRepo struct {
	t    *testing.T
	dir  string
	name string
}

func newSource(t *testing.T, name string) *sourceRepo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &sourceRepo{t: t, dir: dir, name: name}
	s.git("init", "-b", "main")
	s.write("Be kind.\n")
	s.git("tag", "-a", "v1.0.0", "-m", "v1.0.0")
	s.write("Be kinder.\n")
	return s
}

// write puts a SKILL.md in the source and commits it.
func (s *sourceRepo) write(body string) {
	s.t.Helper()
	s.file("SKILL.md", "---\nname: "+s.name+"\ndescription: what it is for\n---\n\n"+body)
}

// file writes any file in the source and commits it.
func (s *sourceRepo) file(path, body string) {
	s.t.Helper()
	full := filepath.Join(s.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		s.t.Fatal(err)
	}
	s.git("add", "-A")
	s.git("commit", "-m", "write "+path)
}

func (s *sourceRepo) git(args ...string) string {
	s.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("git %v in %s: %v\n%s", args, s.dir, err, out)
	}
	return string(out)
}

// commit is what a ref resolves to in the source.
func (s *sourceRepo) commit(ref string) string {
	s.t.Helper()
	return strings.TrimSpace(s.git("rev-parse", ref+"^{commit}"))
}

func TestFetchPutsTheCommitInTheCache(t *testing.T) {
	src := newSource(t, "go-review")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))

	commit, err := c.Resolve(src.dir, "main")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got, err := c.Fetch("go-review", src.dir, "main", commit, "")

	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Commit != src.commit("main") {
		t.Errorf("the fetch is at %s, want the commit main points at %s", got.Commit, src.commit("main"))
	}
	if !strings.HasPrefix(got.Digest, "sha256:") {
		t.Errorf("the digest is %q, want one Owl wrote", got.Digest)
	}
	body, err := os.ReadFile(filepath.Join(got.Path, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading the cached skill: %v", err)
	}
	if !strings.Contains(string(body), "Be kinder.") {
		t.Errorf("the cached skill is %q, want the content at that commit", body)
	}
	if !strings.HasPrefix(got.Path, c.Dir()) {
		t.Errorf("the skill landed at %s, outside the cache %s", got.Path, c.Dir())
	}
}

func TestFetchAtATagIsTheTaggedCommit(t *testing.T) {
	src := newSource(t, "go-review")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))

	commit, err := c.Resolve(src.dir, "v1.0.0")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got, err := c.Fetch("go-review", src.dir, "v1.0.0", commit, "")

	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Commit != src.commit("v1.0.0") {
		t.Errorf("the fetch is at %s, want the tagged commit %s", got.Commit, src.commit("v1.0.0"))
	}
	body, err := os.ReadFile(filepath.Join(got.Path, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Be kinder.") {
		t.Errorf("the cached skill is the branch's content, not the tag's:\n%s", body)
	}
}

func TestFetchTheSameContentTwiceIsOneDirectory(t *testing.T) {
	src := newSource(t, "go-review")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))
	commit, err := c.Resolve(src.dir, "main")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	first, err := c.Fetch("go-review", src.dir, "main", commit, "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	second, err := c.Fetch("go-review", src.dir, "main", commit, first.Digest)
	if err != nil {
		t.Fatalf("Fetch again: %v", err)
	}

	if first.Path != second.Path {
		t.Errorf("the same content landed at %s and %s, want one place", first.Path, second.Path)
	}
	if first.Digest != second.Digest {
		t.Errorf("the same content digested as %s and %s", first.Digest, second.Digest)
	}
}

func TestFetchRefusesContentThatDoesNotMatchTheLockedDigest(t *testing.T) {
	src := newSource(t, "go-review")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))
	commit, err := c.Resolve(src.dir, "main")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// A digest Owl wrote, but not of this content: what the lockfile records
	// is what ran, so anything else is refused rather than accepted quietly.
	_, err = c.Fetch("go-review", src.dir, "main", commit,
		"sha256:"+strings.Repeat("0", 64))

	var refused *skill.InvalidError
	if !errors.As(err, &refused) {
		t.Fatalf("Fetch with a digest that does not match = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "lockfile") {
		t.Errorf("the error %q does not say what did not match", err)
	}
}

func TestFetchRefusesASourceWithNoSkillFile(t *testing.T) {
	src := newSource(t, "empty")
	if err := os.Remove(filepath.Join(src.dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	src.git("add", "-A")
	src.git("commit", "-m", "take the skill away")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))
	commit, err := c.Resolve(src.dir, "main")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	_, err = c.Fetch("empty", src.dir, "main", commit, "")

	var refused *skill.InvalidError
	if !errors.As(err, &refused) {
		t.Fatalf("Fetch of a source with no SKILL.md = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), skill.FileName) {
		t.Errorf("the error %q does not say what is missing", err)
	}
}

func TestResolveRefusesARefTheSourceDoesNotHave(t *testing.T) {
	src := newSource(t, "go-review")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))

	_, err := c.Resolve(src.dir, "nope")

	var refused *skill.InvalidError
	if !errors.As(err, &refused) {
		t.Fatalf("Resolve of a ref that is not there = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("the error %q does not name the ref", err)
	}
}

func TestDigestChangesWithTheContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := skill.Digest(dir)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	again, err := skill.Digest(dir)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := skill.Digest(dir)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	if first != again {
		t.Errorf("the same content digested as %s and %s", first, again)
	}
	if first == changed {
		t.Error("content that changed digested the same")
	}
}

func TestDigestNoticesAFileMovingBetweenNames(t *testing.T) {
	// Two trees whose bytes concatenate the same must not digest the same, or
	// a source could move content between files without the digest saying so.
	first, second := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "a.md"), []byte("xy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "b.md"), []byte("z"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "a.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "b.md"), []byte("yz"), 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := skill.Digest(first)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	b, err := skill.Digest(second)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	if a == b {
		t.Error("two different trees digested the same")
	}
}

func TestNameOfReadsTheSkillsNameFromItsSource(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{"x/go-review", "go-review"},
		{"https://github.com/x/go-review.git", "go-review"},
		{"git@github.com:x/go-review.git", "go-review"},
		{"/repos/skills/house-style", "house-style"},
		{"/repos/skills/house-style/", "house-style"},
	} {
		if got := skill.NameOf(c.source); got != c.want {
			t.Errorf("NameOf(%q) = %q, want %q", c.source, got, c.want)
		}
	}
}

func TestCheckNameRefusesWhatCannotBeADirectory(t *testing.T) {
	for _, name := range []string{"", "..", "with/slash", ".hidden", "with space"} {
		if err := skill.CheckName(name); err == nil {
			t.Errorf("CheckName(%q) = nil, want it refused", name)
		}
	}
	if err := skill.CheckName("go-review"); err != nil {
		t.Errorf("CheckName on an ordinary name = %v", err)
	}
}

func TestCheckSkillRefusesOneWithoutFrontmatter(t *testing.T) {
	for _, body := range []string{
		"no frontmatter at all\n",
		"---\ndescription: but no name\n---\n",
		"---\nname: but no description\n---\n",
		"---\nname:\ndescription:\n---\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, skill.FileName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		err := skill.CheckSkill(dir, "the source")

		if err == nil {
			t.Errorf("CheckSkill of %q = nil, want it refused", body)
		}
	}
}

func TestCheckSkillAcceptsTheEcosystemsShape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, skill.FileName),
		[]byte("---\nname: go-review\ndescription: how we review go\n---\n\nBe kind.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := skill.CheckSkill(dir, "the source"); err != nil {
		t.Errorf("CheckSkill of an ordinary skill = %v", err)
	}
}

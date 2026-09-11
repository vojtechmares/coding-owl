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

func TestParseSourceReadsTheEcosystemsShorthands(t *testing.T) {
	for _, c := range []struct {
		in    string
		url   string
		local bool
	}{
		{in: "x/go-review", url: "https://github.com/x/go-review.git"},
		// What ADR-0024's example declares, and what an address bar gives.
		{in: "github.com/x/go-review", url: "https://github.com/x/go-review.git"},
		{in: "gitlab.com/group/sub/go-review.git", url: "https://gitlab.com/group/sub/go-review.git"},
		{in: "https://github.com/x/go-review.git", url: "https://github.com/x/go-review.git"},
		{in: "ssh://git@github.com/x/go-review.git", url: "ssh://git@github.com/x/go-review.git"},
		{in: "git@github.com:x/go-review.git", url: "git@github.com:x/go-review.git"},
		{in: "/repos/skills/house-style", url: "/repos/skills/house-style", local: true},
	} {
		got, err := skill.ParseSource(c.in)

		if err != nil {
			t.Errorf("ParseSource(%q): %v", c.in, err)
			continue
		}
		if got.URL != c.url {
			t.Errorf("ParseSource(%q).URL = %q, want %q", c.in, got.URL, c.url)
		}
		if got.Local != c.local {
			t.Errorf("ParseSource(%q).Local = %v, want %v", c.in, got.Local, c.local)
		}
		if got.Name() != "go-review" && got.Name() != "house-style" {
			t.Errorf("ParseSource(%q).Name() = %q", c.in, got.Name())
		}
	}
}

func TestParseSourceRefusesWhatGitWouldRunAProgramFor(t *testing.T) {
	// `ext::` makes git run a command the URL names. A source is written in a
	// configuration file, which an Agent could have written (ADR-0024).
	for _, source := range []string{
		"ext::sh -c 'touch /tmp/pwned'",
		"",
		"   ",
		"./relative/path",
		"relative/path/deeper",
		// A source is printed back to a terminal by owl skills list, and comes
		// out of a file a merged pull request can change.
		"x/go-review\x1b]0;pwned\x07",
		"x/go\nreview",
	} {
		_, err := skill.ParseSource(source)

		if err == nil {
			t.Errorf("ParseSource(%q) = nil, want it refused", source)
		}
	}
}

func TestParseLockRefusesWhatOwlWouldNotHaveWritten(t *testing.T) {
	// A lockfile reaches the daemon from a base branch, which a merged pull
	// request writes. What it records is passed to git and printed back.
	const digest = "sha256:ab1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd"
	for what, entry := range map[string]string{
		"a commit that is not an object name": "commit: --upload-pack=touch\n    digest: " + digest,
		"a digest Owl did not write":          "commit: 0123456789abcdef\n    digest: nonsense",
		"a source that would rewrite the terminal": "commit: 0123456789abcdef\n    digest: " + digest +
			"\n    source: \"x/go-review\\e]0;pwned\\a\"",
		"a ref that would rewrite the terminal": "commit: 0123456789abcdef\n    digest: " + digest +
			"\n    ref: \"main\\e]0;pwned\\a\"",
	} {
		lock := "apiVersion: codingowl.dev/v1\nskills:\n  go-review:\n    source: x/go-review\n    " + entry + "\n"

		_, err := skill.ParseLock("lock.yaml", []byte(lock))

		if err == nil {
			t.Errorf("ParseLock with %s = nil, want it refused:\n%s", what, lock)
		}
	}
}

func TestFetchFromARemoteSourceMirrorsIt(t *testing.T) {
	// A source that is not a local path is cloned into the cache and read from
	// there. A file:// URL is a remote as far as git is concerned, which is
	// what makes this exercise the path a github.com source takes.
	src := newSource(t, "go-review")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))
	remote := "file://" + src.dir

	commit, err := c.Resolve(remote, "main")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got, err := c.Fetch("go-review", remote, "main", commit, "")

	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Commit != src.commit("main") {
		t.Errorf("the fetch is at %s, want what the remote's main points at", got.Commit)
	}
	body, err := os.ReadFile(filepath.Join(got.Path, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading the cached skill: %v", err)
	}
	if !strings.Contains(string(body), "Be kinder.") {
		t.Errorf("the cached skill is %q, want the content at that commit", body)
	}
}

func TestResolveFromARemoteSourceSeesItMove(t *testing.T) {
	src := newSource(t, "go-review")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))
	remote := "file://" + src.dir
	was, err := c.Resolve(remote, "main")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	src.write("Be newest.\n")
	now, err := c.Resolve(remote, "main")

	if err != nil {
		t.Fatalf("Resolve again: %v", err)
	}
	// The mirror is brought up to date every time: what a ref points at is the
	// source's to say, and the answer has to be today's.
	if now == was {
		t.Error("a mirrored source that moved still resolves to what it was")
	}
	if now != src.commit("main") {
		t.Errorf("Resolve = %s, want what the source's main points at now", now)
	}
}

func TestDefaultBranchOfARemoteSourceIsWhatItsHeadPointsAt(t *testing.T) {
	src := newSource(t, "go-review")
	c := skill.NewCache(filepath.Join(t.TempDir(), "skills"))

	got, err := c.DefaultBranch("file://" + src.dir)

	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("DefaultBranch = %q, want main", got)
	}
}

func TestFetchReplacesACacheEntrySomethingChanged(t *testing.T) {
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
	// An Agent reaches the cache through the link in its worktree and runs as
	// the same user, so the cache is checked rather than trusted.
	if err := os.WriteFile(filepath.Join(first.Path, "SKILL.md"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	again, err := c.Fetch("go-review", src.dir, "main", commit, first.Digest)

	if err != nil {
		t.Fatalf("Fetch again: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(again.Path, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "tampered") {
		t.Errorf("the cache still holds what something changed it to:\n%s", body)
	}
	if again.Digest != first.Digest {
		t.Errorf("the digest changed from %s to %s", first.Digest, again.Digest)
	}
}

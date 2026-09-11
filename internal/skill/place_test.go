package skill_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/skill"
)

// placement is a repository with a worktree of its own, and a cache to place
// Skills out of.
type placement struct {
	repo     string
	worktree string
	owned    string
	cache    *skill.Cache
}

func newPlacement(t *testing.T) placement {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	src := newSource(t, "repo")
	// The source helper makes a repository with a SKILL.md; a Project is an
	// ordinary repository, so this one is made the same way and then emptied
	// of the skill file.
	src.dir = repo
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	src.git("init", "-b", "main")
	src.file("README.md", "# test\n")
	worktree := filepath.Join(root, "worktrees", "1")
	if err := git.AddWorktree(repo, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	return placement{
		repo: repo, worktree: worktree,
		owned: filepath.Join(root, "owned", "1"),
		cache: skill.NewCache(filepath.Join(root, "skills")),
	}
}

// fetch puts a Skill in the cache, ready to be placed.
func (p placement) fetch(t *testing.T, src *sourceRepo, ref string) skill.Resolved {
	t.Helper()
	commit, err := p.cache.Resolve(src.dir, ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got, err := p.cache.Fetch(src.name, src.dir, ref, commit, "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return got
}

// site is where this placement happens.
func (p placement) site() skill.Site {
	return skill.Site{
		Repo:      p.repo,
		Worktree:  p.worktree,
		SkillsDir: ".claude/skills",
		OwnedDir:  p.owned,
		Cache:     p.cache.Dir(),
	}
}

// place puts skills in the worktree and fails the test if it could not.
func (p placement) place(t *testing.T, skills ...skill.Resolved) skill.Placement {
	t.Helper()
	got, err := skill.Place(p.site(), skills)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	return got
}

// status is what git reports in the worktree.
func (p placement) status(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, p.worktree, "status", "--porcelain"))
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	s := &sourceRepo{t: t, dir: dir}
	return s.git(args...)
}

func TestPlacePutsASkillInTheDriversDirectory(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")

	got := p.place(t, p.fetch(t, src, "main"))

	body, err := os.ReadFile(filepath.Join(p.worktree, ".claude", "skills", "go-review", "SKILL.md"))
	if err != nil {
		t.Fatalf("the skill was not placed: %v", err)
	}
	if !strings.Contains(string(body), "Be kinder.") {
		t.Errorf("the placed skill is %q, want the content at its commit", body)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "go-review" {
		t.Errorf("Place reports %+v, want the one skill", got.Skills)
	}
}

func TestPlaceHidesTheSkillsFromGit(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")

	p.place(t, p.fetch(t, src, "main"))

	if got := p.status(t); got != "" {
		t.Errorf("the worktree reports the skills:\n%s", got)
	}
	// The repository's own checkout is untouched: only the worktree was told
	// anything (ADR-0033).
	if got := strings.TrimSpace(gitIn(t, p.repo, "status", "--porcelain")); got != "" {
		t.Errorf("the repository's own checkout is not clean:\n%s", got)
	}
}

func TestPlaceCarriesForwardAnExcludeFileTheRepositoryAlreadyHad(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")
	theirs := filepath.Join(t.TempDir(), "their-excludes")
	if err := os.WriteFile(theirs, []byte("notes.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, p.repo, "config", "core.excludesFile", theirs)

	p.place(t, p.fetch(t, src, "main"))

	if err := os.WriteFile(filepath.Join(p.worktree, "notes.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := p.status(t); got != "" {
		t.Errorf("the worktree reports what the repository was already told to ignore:\n%s", got)
	}
	if got := strings.TrimSpace(gitIn(t, p.repo, "config", "--get", "core.excludesFile")); got != theirs {
		t.Errorf("the repository's own setting is now %q, want %q", got, theirs)
	}
}

func TestPlaceRefusesAnEntryItDidNotCreate(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")
	theirs := filepath.Join(p.worktree, ".claude", "skills", "go-review")
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("ours\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := skill.Place(p.site(), []skill.Resolved{p.fetch(t, src, "main")})

	var refused *skill.InvalidError
	if !errors.As(err, &refused) {
		t.Fatalf("Place over an entry Owl did not create = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "did not create") {
		t.Errorf("the error %q does not say why", err)
	}
	body, readErr := os.ReadFile(filepath.Join(theirs, "SKILL.md"))
	if readErr != nil || string(body) != "ours\n" {
		t.Errorf("the entry Owl did not create was touched: %q %v", body, readErr)
	}
}

func TestPlaceReplacesWhatAnEarlierPlacementMade(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")
	p.place(t, p.fetch(t, src, "v1.0.0"))

	p.place(t, p.fetch(t, src, "main"))

	body, err := os.ReadFile(filepath.Join(p.worktree, ".claude", "skills", "go-review", "SKILL.md"))
	if err != nil {
		t.Fatalf("the skill was not placed: %v", err)
	}
	if !strings.Contains(string(body), "Be kinder.") {
		t.Errorf("the placed skill is still the old one:\n%s", body)
	}
}

func TestPlaceTakesAwayASkillNoLongerDeclared(t *testing.T) {
	p := newPlacement(t)
	first, second := newSource(t, "go-review"), newSource(t, "house-style")
	p.place(t, p.fetch(t, first, "main"), p.fetch(t, second, "main"))

	p.place(t, p.fetch(t, second, "main"))

	if _, err := os.Lstat(filepath.Join(p.worktree, ".claude", "skills", "go-review")); err == nil {
		t.Error("a skill the project no longer declares is still placed")
	}
	if _, err := os.Stat(filepath.Join(p.worktree, ".claude", "skills", "house-style", "SKILL.md")); err != nil {
		t.Errorf("the skill it still declares went with it: %v", err)
	}
}

func TestPlaceWithNoSkillsLeavesACleanWorktree(t *testing.T) {
	p := newPlacement(t)

	p.place(t)

	if got := p.status(t); got != "" {
		t.Errorf("the worktree is not clean:\n%s", got)
	}
}

func TestPlaceLeavesAnEntryThatTookTheNameOfOneItPlaced(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")
	p.place(t, p.fetch(t, src, "main"))
	// A name Owl used once is no evidence that what is there now is Owl's: a
	// base branch that has since committed a skill of its own, or an Agent
	// that wrote in its place, both arrive this way.
	link := filepath.Join(p.worktree, ".claude", "skills", "go-review")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(link, "SKILL.md"), []byte("ours\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := skill.Place(p.site(), []skill.Resolved{p.fetch(t, src, "main")})

	var refused *skill.InvalidError
	if !errors.As(err, &refused) {
		t.Fatalf("Place over an entry that replaced one of Owl's = %v, want it refused", err)
	}
	body, readErr := os.ReadFile(filepath.Join(link, "SKILL.md"))
	if readErr != nil || string(body) != "ours\n" {
		t.Errorf("what took the name of a placed skill was touched: %q %v", body, readErr)
	}
}

func TestPlaceTakesAwayOnlyWhatItPlaced(t *testing.T) {
	p := newPlacement(t)
	first, second := newSource(t, "go-review"), newSource(t, "house-style")
	p.place(t, p.fetch(t, first, "main"), p.fetch(t, second, "main"))
	// An Agent's own work under a name nothing declares is the Agent's, and a
	// placement that no longer declares go-review is not a licence to delete
	// what replaced it.
	skills := filepath.Join(p.worktree, ".claude", "skills")
	theirs := filepath.Join(skills, "notes")
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theirs, "work.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(skills, "go-review")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skills, "go-review", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}

	p.place(t, p.fetch(t, second, "main"))

	for _, kept := range []string{theirs, filepath.Join(skills, "go-review", "deep")} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was removed by a placement that did not put it there: %v", kept, err)
		}
	}
}

func TestPlaceRefusesASkillsDirectoryThatLeadsOutOfTheWorktree(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "theirs.txt"), []byte("theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p.worktree, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A symlink the base branch could carry, or one an earlier Agent left.
	if err := os.Symlink(outside, filepath.Join(p.worktree, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}

	_, err := skill.Place(p.site(), []skill.Resolved{p.fetch(t, src, "main")})

	var refused *skill.InvalidError
	if !errors.As(err, &refused) {
		t.Fatalf("Place through a symlink out of the worktree = %v, want it refused", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 {
		t.Errorf("the directory outside the worktree holds %d entries, want only what was there", len(entries))
	}
}

func TestPlaceCreatesNothingThroughASymlinkOutOfTheWorktree(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	// The driver's directory itself leads out, so making the skills directory
	// is already a write through it - the check has to come first.
	if err := os.Symlink(outside, filepath.Join(p.worktree, ".claude")); err != nil {
		t.Fatal(err)
	}

	_, err := skill.Place(p.site(), []skill.Resolved{p.fetch(t, src, "main")})

	var refused *skill.InvalidError
	if !errors.As(err, &refused) {
		t.Fatalf("Place through a symlink out of the worktree = %v, want it refused", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Errorf("a placement that refused created %v outside the worktree", entries)
	}
}

func TestPlaceWritesNothingWhenItIsGoingToRefuse(t *testing.T) {
	p := newPlacement(t)
	src := newSource(t, "go-review")
	theirs := filepath.Join(p.worktree, ".claude", "skills", "go-review")
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("ours\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := skill.Place(p.site(), []skill.Resolved{p.fetch(t, src, "main")})

	if err == nil {
		t.Fatal("Place over an entry Owl did not create = nil, want it refused")
	}
	// The repository is not reconfigured on the way to refusing: a Run that
	// does not happen leaves nothing behind. git exits non-zero for a setting
	// nobody has made, so the whole list is what says it is absent.
	if got := gitIn(t, p.repo, "config", "--list", "--local"); strings.Contains(got, "worktreeconfig") {
		t.Errorf("the repository was reconfigured by a placement that refused:\n%s", got)
	}
}

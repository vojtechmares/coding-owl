package behavior_test

// Behavior tests for issue #16. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-16.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// exercise the Skills a Project gives every Agent that works in it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillsPath is where the Claude Code Driver reads Skills from, inside the
// worktree (ADR-0033).
const skillsPath = ".claude/skills"

// lockName is the lockfile beside a Project's configuration.
const lockName = ".coding-owl.lock.yaml"

// source is a git repository standing in for a published Skill: a SKILL.md
// with the frontmatter the ecosystem expects, a tag on an earlier commit than
// the branch, so that a pinned Skill and a tracking one differ.
type source struct {
	t    *testing.T
	name string
	repo *repo
}

// newSource makes a Skill source under the layout, named after its directory.
func newSource(t *testing.T, l *layout, name string) *source {
	t.Helper()
	r := newRepo(t, l, filepath.Join("skills", name))
	s := &source{t: t, name: name, repo: r}
	s.write("Be kind.\n")
	r.git("tag", "-a", "v1.0.0", "-m", "v1.0.0")
	s.write("Be kinder.\n")
	return s
}

// write puts a SKILL.md carrying body in the source and commits it.
func (s *source) write(body string) {
	s.t.Helper()
	s.repo.commit("SKILL.md",
		"---\nname: "+s.name+"\ndescription: what "+s.name+" is for\n---\n\n"+body,
		"write the skill")
}

// commit is what a ref resolves to in the source.
func (s *source) commit(ref string) string {
	s.t.Helper()
	return strings.TrimSpace(s.repo.git("rev-parse", ref+"^{commit}"))
}

// path is the source's own directory, which is what owl skills add is given.
func (s *source) path() string { return s.repo.dir }

// skillLayout is a layout whose daemon can run an Agent, with a Project and a
// Skill source ready to add.
func skillLayout(t *testing.T) (*layout, *repo, *source) {
	t.Helper()
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	return l, r, newSource(t, l, "go-review")
}

// addSkill adds a Skill to the Project the working directory is in.
func addSkill(t *testing.T, l *layout, dir string, args ...string) result {
	t.Helper()
	res := runOwlIn(t, l, dir, append([]string{"skills", "add"}, args...)...)
	if res.code != 0 {
		t.Fatalf("owl skills add %v exited %d\nstdout:\n%s\nstderr:\n%s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

// manifest is the Project's configuration as it stands on its base branch,
// which is where the daemon reads it from (ADR-0014).
func manifest(t *testing.T, r *repo) string {
	t.Helper()
	return r.git("show", "HEAD:.coding-owl.yaml")
}

// lockfile is the lockfile as it stands on the Project's base branch.
func lockfile(t *testing.T, r *repo) string {
	t.Helper()
	return r.git("show", "HEAD:"+lockName)
}

// skillRow is one row of owl skills list.
type skillRow struct{ name, source, ref, commit, pinned string }

// skillRows parses owl skills list, skipping its header.
func skillRows(t *testing.T, l *layout, dir string) []skillRow {
	t.Helper()
	out := mustOwlIn(t, l, dir, "skills", "list").stdout
	var rows []skillRow
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(ln)
		if len(f) != 5 || f[0] == "NAME" {
			continue
		}
		rows = append(rows, skillRow{name: f[0], source: f[1], ref: f[2], commit: f[3], pinned: f[4]})
	}
	return rows
}

// mustOwlIn runs owl in a directory and fails the test if it could not.
func mustOwlIn(t *testing.T, l *layout, dir string, args ...string) result {
	t.Helper()
	res := runOwlIn(t, l, dir, args...)
	if res.code != 0 {
		t.Fatalf("owl %v exited %d\nstdout:\n%s\nstderr:\n%s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

// materialised is the SKILL.md of a Skill as it stands in a Job's worktree.
func materialised(t *testing.T, worktree, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktree, skillsPath, name, "SKILL.md"))
	if err != nil {
		t.Fatalf("the skill %s was not materialised: %v", name, err)
	}
	return string(data)
}

func TestS1SkillsAddRecordsTheSourceAndWhatItResolvedTo(t *testing.T) {
	l, r, src := skillLayout(t)

	res := addSkill(t, l, r.dir, src.path())

	head := src.commit("main")
	for _, want := range []string{"go-review", head[:12]} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("owl skills add does not name %q:\n%s", want, res.stdout)
		}
	}
	got := manifest(t, r)
	for _, want := range []string{"skills:", src.path(), "main"} {
		if !strings.Contains(got, want) {
			t.Errorf("the manifest does not record %q:\n%s", want, got)
		}
	}
	lock := lockfile(t, r)
	if !strings.Contains(lock, head) {
		t.Errorf("the lockfile does not record the resolved commit %s:\n%s", head, lock)
	}
	if !strings.Contains(lock, "digest") {
		t.Errorf("the lockfile records no digest of what was fetched:\n%s", lock)
	}
}

func TestS2SkillsAddPinsToTheRefItIsGiven(t *testing.T) {
	l, r, src := skillLayout(t)

	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")

	if got := manifest(t, r); !strings.Contains(got, "v1.0.0") {
		t.Errorf("the manifest does not record the ref it was given:\n%s", got)
	}
	tagged, head := src.commit("v1.0.0"), src.commit("main")
	if tagged == head {
		t.Fatal("the source's tag and branch point at the same commit, so this proves nothing")
	}
	lock := lockfile(t, r)
	if !strings.Contains(lock, tagged) {
		t.Errorf("the lockfile does not record the tagged commit %s:\n%s", tagged, lock)
	}
	if strings.Contains(lock, head) {
		t.Errorf("the lockfile records the branch's commit for a pinned skill:\n%s", lock)
	}
}

func TestS3SkillsListTellsPinnedFromTracking(t *testing.T) {
	l, r, src := skillLayout(t)
	other := newSource(t, l, "house-style")
	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")
	addSkill(t, l, r.dir, other.path(), "--auto-update")

	rows := skillRows(t, l, r.dir)

	if len(rows) != 2 {
		t.Fatalf("owl skills list reports %d skills, want two:\n%+v", len(rows), rows)
	}
	byName := map[string]skillRow{}
	for _, row := range rows {
		byName[row.name] = row
	}
	pinned, ok := byName["go-review"]
	if !ok {
		t.Fatalf("owl skills list does not report go-review:\n%+v", rows)
	}
	if pinned.ref != "v1.0.0" || !strings.HasPrefix(src.commit("v1.0.0"), pinned.commit) {
		t.Errorf("go-review is %+v, want it at v1.0.0 and the commit that resolves to", pinned)
	}
	if pinned.pinned != "pinned" {
		t.Errorf("go-review is reported as %q, want pinned", pinned.pinned)
	}
	tracking, ok := byName["house-style"]
	if !ok {
		t.Fatalf("owl skills list does not report house-style:\n%+v", rows)
	}
	if tracking.pinned != "tracking" {
		t.Errorf("house-style is reported as %q, want tracking", tracking.pinned)
	}
}

func TestS4SkillsListWithNoneSaysSo(t *testing.T) {
	l, r, _ := skillLayout(t)

	out := mustOwlIn(t, l, r.dir, "skills", "list").stdout

	if !strings.Contains(out, "no skills") {
		t.Errorf("owl skills list for a project with none says %q", strings.TrimSpace(out))
	}
}

func TestS5SkillsAddRefusesASourceItCannotRead(t *testing.T) {
	l, r, _ := skillLayout(t)
	nowhere := filepath.Join(l.root, "not-a-repository")
	if err := os.MkdirAll(nowhere, 0o755); err != nil {
		t.Fatal(err)
	}

	res := runOwlIn(t, l, r.dir, "skills", "add", nowhere)

	if res.code == 0 {
		t.Fatalf("owl skills add for a source that is not a repository exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, nowhere) {
		t.Errorf("stderr does not name the source:\n%s", res.stderr)
	}
	if got := manifest(t, r); strings.Contains(got, "skills:") {
		t.Errorf("the manifest gained a skills section:\n%s", got)
	}
	if got := r.git("ls-tree", "--name-only", "HEAD"); strings.Contains(got, lockName) {
		t.Errorf("a lockfile was written for a source that could not be read:\n%s", got)
	}
}

func TestS6SkillsAddRefusesARefTheSourceDoesNotHave(t *testing.T) {
	l, r, src := skillLayout(t)

	res := runOwlIn(t, l, r.dir, "skills", "add", src.path(), "--ref", "nope")

	if res.code == 0 {
		t.Fatalf("owl skills add for a ref that is not there exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "nope") {
		t.Errorf("stderr does not name the ref:\n%s", res.stderr)
	}
	if got := manifest(t, r); strings.Contains(got, "skills:") {
		t.Errorf("the manifest gained a skills section:\n%s", got)
	}
}

func TestS7SkillsAddRefusesASourceWithNoSkillFile(t *testing.T) {
	l, r, _ := skillLayout(t)
	bare := newRepo(t, l, filepath.Join("skills", "empty"))

	res := runOwlIn(t, l, r.dir, "skills", "add", bare.dir)

	if res.code == 0 {
		t.Fatalf("owl skills add for a source with no SKILL.md exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "SKILL.md") {
		t.Errorf("stderr does not say what a skill needs:\n%s", res.stderr)
	}
	if got := manifest(t, r); strings.Contains(got, "skills:") {
		t.Errorf("the manifest gained a skills section:\n%s", got)
	}
}

func TestS8ARunMaterialisesTheSkills(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path())
	addJob(t, l, r.dir, "work", "--no-plan")

	out, _ := finishedJob(t, l)

	worktree := line(t, out, "worktree")
	got := materialised(t, worktree, "go-review")
	if !strings.Contains(got, "Be kinder.") {
		t.Errorf("the skill in the worktree is not what its commit says:\n%s", got)
	}
	if !strings.Contains(got, "name: go-review") {
		t.Errorf("the skill in the worktree has no frontmatter:\n%s", got)
	}
}

func TestS9TheSkillsAreInvisibleToGit(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path())
	addJob(t, l, r.dir, "work", "--no-plan")

	out, _ := finishedJob(t, l)

	worktree := line(t, out, "worktree")
	if got := gitIn(t, r, worktree, "status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Errorf("the job's worktree is not clean:\n%s", got)
	}
	if got := r.git("status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Errorf("the project's own checkout is not clean:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(r.dir, skillsPath)); err == nil {
		t.Errorf("the project's own checkout gained a %s directory", skillsPath)
	}
}

func TestS10AGlobalExcludesFileStillApplies(t *testing.T) {
	l, r, src := skillLayout(t)
	theirs := filepath.Join(l.root, "their-excludes")
	if err := os.WriteFile(theirs, []byte("notes.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.git("config", "core.excludesFile", theirs)
	addSkill(t, l, r.dir, src.path())
	addJob(t, l, r.dir, "work", "--no-plan")

	out, _ := finishedJob(t, l)

	worktree := line(t, out, "worktree")
	if err := os.WriteFile(filepath.Join(worktree, "notes.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, r, worktree, "status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Errorf("the worktree reports something it was told to ignore:\n%s", got)
	}
	if got := strings.TrimSpace(r.git("config", "--get", "core.excludesFile")); got != theirs {
		t.Errorf("the repository's own excludes file is now %q, want %q", got, theirs)
	}
}

func TestS11EditingTheLockfileInTheWorktreeChangesNothing(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")
	addJob(t, l, r.dir, "work")
	out, job := finishedJob(t, l)
	worktree := line(t, out, "worktree")
	pinned := src.commit("v1.0.0")

	// The Agent rewrites the lock in its own worktree to name another commit.
	lock := filepath.Join(worktree, lockName)
	data, err := os.ReadFile(lock)
	if err != nil {
		t.Fatalf("reading the lockfile in the worktree: %v", err)
	}
	if err := os.WriteFile(lock, []byte(strings.ReplaceAll(string(data), pinned, src.commit("main"))), 0o644); err != nil {
		t.Fatal(err)
	}
	run, _ := startRun(t, l)
	waitRun(t, l, job, run)

	if got := materialised(t, worktree, "go-review"); !strings.Contains(got, "Be kind.\n") || strings.Contains(got, "Be kinder.") {
		t.Errorf("the second run used the lockfile the agent edited:\n%s", got)
	}
}

func TestS12ARunRecordsTheSkillVersionsItRanWith(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path())
	addJob(t, l, r.dir, "work", "--no-plan")

	out, _ := finishedJob(t, l)

	skills := section(t, out, "skills:")
	for _, want := range []string{"go-review", "main", src.commit("main")[:12]} {
		if !strings.Contains(skills, want) {
			t.Errorf("owl jobs show does not record %q for the run:\n%s", want, out)
		}
	}
}

func TestS13SkillsUpdateResolvesTrackingAndLeavesPinned(t *testing.T) {
	l, r, src := skillLayout(t)
	other := newSource(t, l, "house-style")
	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")
	addSkill(t, l, r.dir, other.path(), "--auto-update")
	pinned := src.commit("v1.0.0")
	src.write("Be kindest.\n")
	other.write("In our house.\n")

	res := mustOwlIn(t, l, r.dir, "skills", "update")

	lock := lockfile(t, r)
	if !strings.Contains(lock, other.commit("main")) {
		t.Errorf("the lockfile does not record house-style's new commit:\n%s", lock)
	}
	if !strings.Contains(lock, pinned) {
		t.Errorf("the lockfile no longer records go-review's pinned commit:\n%s", lock)
	}
	if !strings.Contains(res.stdout, "house-style") {
		t.Errorf("owl skills update does not say what it updated:\n%s", res.stdout)
	}
}

func TestS14SkillsUpdateTakesASkillByName(t *testing.T) {
	l, r, src := skillLayout(t)
	other := newSource(t, l, "house-style")
	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")
	addSkill(t, l, r.dir, other.path(), "--auto-update")
	was := other.commit("main")
	other.write("In our house.\n")

	res := mustOwlIn(t, l, r.dir, "skills", "update", "go-review")

	lock := lockfile(t, r)
	if !strings.Contains(lock, src.commit("v1.0.0")) {
		t.Errorf("the lockfile does not record what go-review's ref resolves to:\n%s", lock)
	}
	if !strings.Contains(lock, was) {
		t.Errorf("house-style was updated too:\n%s", lock)
	}
	if !strings.Contains(res.stdout, "go-review") {
		t.Errorf("owl skills update does not name what it looked at:\n%s", res.stdout)
	}
}

func TestS15AnAutoUpdateSkillIsResolvedAtRunStart(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path(), "--auto-update")
	addJob(t, l, r.dir, "work")
	out, job := finishedJob(t, l)
	worktree := line(t, out, "worktree")

	src.write("Be newest.\n")
	run, _ := startRun(t, l)
	waitRun(t, l, job, run)

	if got := materialised(t, worktree, "go-review"); !strings.Contains(got, "Be newest.") {
		t.Errorf("the second run did not pick up the new commit:\n%s", got)
	}
	shown := mustOwl(t, l, "jobs", "show", job).stdout
	if !strings.Contains(section(t, shown, "skills:"), src.commit("main")[:12]) {
		t.Errorf("owl jobs show does not record the new commit:\n%s", shown)
	}
}

func TestS16APinnedSkillIsNotResolvedAtRunStart(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")
	addJob(t, l, r.dir, "work")
	out, job := finishedJob(t, l)
	worktree := line(t, out, "worktree")

	src.write("Be newest.\n")
	run, _ := startRun(t, l)
	waitRun(t, l, job, run)

	if got := materialised(t, worktree, "go-review"); strings.Contains(got, "Be newest.") {
		t.Errorf("a pinned skill moved when the source did:\n%s", got)
	}
}

func TestS17OwlRefusesToOverwriteASkillsEntryItDidNotCreate(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path())
	// The Project keeps a skill of its own at the same name, committed.
	r.commit(filepath.Join(skillsPath, "go-review", "SKILL.md"),
		"---\nname: go-review\ndescription: the project's own\n---\n\nOurs.\n",
		"keep our own skill")
	addJob(t, l, r.dir, "work", "--no-plan")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start over a skills entry Owl did not create exited 0\nstdout:\n%s", res.stdout)
	}
	for _, want := range []string{"go-review", "did not create"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not say %q:\n%s", want, res.stderr)
		}
	}
	if got := r.git("show", "HEAD:"+filepath.Join(skillsPath, "go-review", "SKILL.md")); !strings.Contains(got, "Ours.") {
		t.Errorf("the project's own skill was touched:\n%s", got)
	}
	out := mustOwl(t, l, "jobs", "show", "1").stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job left pending", got)
	}
	if !strings.Contains(out, "runs: none") {
		t.Errorf("a run was started for a job that could not run:\n%s", out)
	}
}

func TestS18SkillsRemoveTakesItOutOfBoth(t *testing.T) {
	l, r, src := skillLayout(t)
	other := newSource(t, l, "house-style")
	addSkill(t, l, r.dir, src.path())
	addSkill(t, l, r.dir, other.path())

	mustOwlIn(t, l, r.dir, "skills", "remove", "go-review")

	if got := manifest(t, r); strings.Contains(got, "go-review") {
		t.Errorf("the manifest still mentions go-review:\n%s", got)
	}
	if got := lockfile(t, r); strings.Contains(got, "go-review") {
		t.Errorf("the lockfile still mentions go-review:\n%s", got)
	}
	if got := manifest(t, r); !strings.Contains(got, "house-style") {
		t.Errorf("the other skill went with it:\n%s", got)
	}
	addJob(t, l, r.dir, "work", "--no-plan")
	out, _ := finishedJob(t, l)
	worktree := line(t, out, "worktree")
	if _, err := os.Stat(filepath.Join(worktree, skillsPath, "go-review")); err == nil {
		t.Error("the removed skill was materialised anyway")
	}
	materialised(t, worktree, "house-style")
}

func TestS19SkillsRemoveRefusesASkillThatIsNotThere(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path())

	res := runOwlIn(t, l, r.dir, "skills", "remove", "nothing")

	if res.code == 0 {
		t.Fatalf("owl skills remove for a skill that is not there exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "nothing") {
		t.Errorf("stderr does not name the skill:\n%s", res.stderr)
	}
}

func TestS20SkillsReportsAStoppedDaemon(t *testing.T) {
	l := newLayout(t)

	res := runOwl(t, l, "skills", "list")

	if res.code == 0 {
		t.Fatalf("owl skills list with no daemon exited 0\nstdout:\n%s", res.stdout)
	}
	for _, want := range []string{"not running", l.socket()} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not say %q:\n%s", want, res.stderr)
		}
	}
}

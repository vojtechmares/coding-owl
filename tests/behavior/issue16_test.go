package behavior_test

// Behavior tests for issue #16. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-16.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// exercise the Skills a Project gives every Agent that works in it.

import (
	"os"
	"path/filepath"
	"regexp"
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

// moveTag points the source's tag at what its branch has come to, which is
// what a publisher does when a release moves.
func (s *source) moveTag() {
	s.t.Helper()
	s.repo.git("tag", "-f", "-a", "v1.0.0", "-m", "v1.0.0 again")
}

// path is the source's own directory, which is what owl skills add is given.
func (s *source) path() string { return s.repo.dir }

// skillLayout is a layout whose daemon can run an Agent, with a Project
// configured in its own repository - which is where `owl skills` then writes -
// and a Skill source ready to add.
func skillLayout(t *testing.T) (*layout, *repo, *source) {
	t.Helper()
	l, _ := agentLayout(t, agentScript, 0)
	return withSkillProject(t, l)
}

// twoRunLayout is skillLayout for the scenarios that run a Job twice: the stub
// writes a handoff, so the planning Run succeeds and the Job comes back to the
// queue to be carried out (ADR-0026).
func twoRunLayout(t *testing.T) (*layout, *repo, *source) {
	t.Helper()
	l, _ := planningLayout(t, "# Handoff\n\nstep one done\n")
	return withSkillProject(t, l)
}

// withSkillProject starts a daemon for a layout and gives it a Project whose
// configuration is in its own repository, plus a Skill source to add.
func withSkillProject(t *testing.T, l *layout) (*layout, *repo, *source) {
	t.Helper()
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\n", "configure owl")
	addProject(t, l, r)
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

// manifest is the Project's configuration file as `owl skills` left it. For a
// Project configured in its own repository that is the working tree: the
// daemon reads the base branch, so a Skill takes effect once it is committed.
func manifest(t *testing.T, r *repo) string {
	t.Helper()
	return readFile(t, filepath.Join(r.dir, ".coding-owl.yaml"))
}

// lockfile is the lockfile beside it, also as it stands in the working tree.
func lockfile(t *testing.T, r *repo) string {
	t.Helper()
	return readFile(t, filepath.Join(r.dir, lockName))
}

// readFile is a file's contents, empty for one that is not there.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// commitSkills commits the manifest and the lockfile, which is what a user
// does before a Run reads them from the base branch (ADR-0014).
func commitSkills(t *testing.T, r *repo) {
	t.Helper()
	r.git("add", "--", ".coding-owl.yaml", lockName)
	r.git("commit", "-m", "declare skills")
}

// digestRE is the digest Owl writes: an algorithm and the hash of what was
// fetched, rather than a word that could be anything.
var digestRE = regexp.MustCompile(`digest: sha256:[0-9a-f]{64}`)

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

// runSkillCommit is the commit owl jobs show records for one Skill of one Run.
// The section carries a block per Run, headed by RUN and its id, so a scenario
// about a second Run can ask what that Run ran with rather than what any Run
// did.
func runSkillCommit(t *testing.T, out, run, name string) string {
	t.Helper()
	var at string
	for _, ln := range strings.Split(section(t, out, "skills:"), "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		if f[0] == "RUN" {
			at = f[1]
			continue
		}
		if at == run && f[0] == name {
			return f[len(f)-1]
		}
	}
	t.Fatalf("owl jobs show records no %s for run %s:\n%s", name, run, out)
	return ""
}

// setDigest rewrites the digest a lockfile records, so that an edit naming
// another commit is not given away by the digest beside it.
func setDigest(lock, digest string) string {
	lines := strings.Split(lock, "\n")
	for i, ln := range lines {
		if before, _, ok := strings.Cut(ln, "digest:"); ok {
			lines[i] = before + "digest: " + digest
		}
	}
	return strings.Join(lines, "\n")
}

// digestIn is the digest a lockfile with one Skill records.
func digestIn(t *testing.T, lock string) string {
	t.Helper()
	for _, ln := range strings.Split(lock, "\n") {
		if _, value, ok := strings.Cut(ln, "digest:"); ok {
			return strings.TrimSpace(value)
		}
	}
	t.Fatalf("no digest in:\n%s", lock)
	return ""
}

// digestOf is what Owl records for a source at a ref, taken from a Project of
// its own so that a scenario can write a lockfile Owl would believe.
func digestOf(t *testing.T, l *layout, src *source, ref string) string {
	t.Helper()
	r := newRepo(t, l, "elsewhere")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\n", "configure owl")
	addProject(t, l, r)
	addSkill(t, l, r.dir, src.path(), "--ref", ref)
	return digestIn(t, lockfile(t, r))
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
	if !digestRE.MatchString(lock) {
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
	if tracking.ref != "main" || !strings.HasPrefix(other.commit("main"), tracking.commit) {
		t.Errorf("house-style is %+v, want it at main and the commit that resolves to", tracking)
	}
	for _, row := range rows {
		if !strings.Contains(row.source, row.name) {
			t.Errorf("%s is reported with the source %q, which is not where it came from", row.name, row.source)
		}
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
	if got := lockfile(t, r); got != "" {
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
	commitSkills(t, r)
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
	commitSkills(t, r)
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
	commitSkills(t, r)
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
	l, r, src := twoRunLayout(t)
	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")
	commitSkills(t, r)
	addJob(t, l, r.dir, "work")
	out, job := finishedJob(t, l)
	worktree := line(t, out, "worktree")
	pinned, forged := src.commit("v1.0.0"), src.commit("main")

	// The Agent rewrites the lock in its own worktree to name another commit,
	// with the digest Owl itself records for it: an edit the digest gives away
	// would prove only that the digest is checked.
	credible := digestOf(t, l, src, "main")
	lock := filepath.Join(worktree, lockName)
	data, err := os.ReadFile(lock)
	if err != nil {
		t.Fatalf("reading the lockfile in the worktree: %v", err)
	}
	edited := setDigest(strings.ReplaceAll(string(data), pinned, forged), credible)
	if err := os.WriteFile(lock, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	run, _ := startRun(t, l)
	waitRun(t, l, job, run)

	if got := materialised(t, worktree, "go-review"); !strings.Contains(got, "Be kind.\n") || strings.Contains(got, "Be kinder.") {
		t.Errorf("the second run used the lockfile the agent edited:\n%s", got)
	}
	shown := mustOwl(t, l, "jobs", "show", job).stdout
	if got := runSkillCommit(t, shown, run, "go-review"); !strings.HasPrefix(pinned, got) {
		t.Errorf("run %s records %s for go-review, want the base branch's %s:\n%s",
			run, got, pinned[:12], shown)
	}
	if strings.Contains(section(t, shown, "skills:"), forged[:12]) {
		t.Errorf("a run records the commit the agent wrote into its own worktree:\n%s", shown)
	}
}

func TestS12ARunRecordsTheSkillVersionsItRanWith(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path())
	commitSkills(t, r)
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
	// The release go-review is pinned at moves too, so leaving a pinned skill
	// alone is something that can be seen rather than something that follows
	// from the tag never having moved.
	src.moveTag()
	other.write("In our house.\n")
	if src.commit("v1.0.0") == pinned {
		t.Fatal("the tag did not move, so leaving go-review alone proves nothing")
	}

	res := mustOwlIn(t, l, r.dir, "skills", "update")

	lock := lockfile(t, r)
	if !strings.Contains(lock, other.commit("main")) {
		t.Errorf("the lockfile does not record house-style's new commit:\n%s", lock)
	}
	if !strings.Contains(lock, pinned) {
		t.Errorf("the lockfile no longer records go-review's pinned commit:\n%s", lock)
	}
	if strings.Contains(lock, src.commit("v1.0.0")) {
		t.Errorf("a pinned skill followed its tag:\n%s", lock)
	}
	updated := section(t, res.stdout, "updated:")
	if !strings.Contains(updated, "house-style") || strings.Contains(updated, "go-review") {
		t.Errorf("owl skills update reports updating %q:\n%s", updated, res.stdout)
	}
}

func TestS14SkillsUpdateTakesASkillByName(t *testing.T) {
	l, r, src := skillLayout(t)
	other := newSource(t, l, "house-style")
	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")
	addSkill(t, l, r.dir, other.path(), "--auto-update")
	was, untouched := src.commit("v1.0.0"), other.commit("main")
	// The release go-review is pinned at moves, so naming it has something to
	// do: a pinned skill that is already where its ref points would prove
	// nothing about the command.
	src.write("Be kindest.\n")
	src.moveTag()
	other.write("In our house.\n")
	moved := src.commit("v1.0.0")
	if moved == was {
		t.Fatal("the tag did not move, so this proves nothing")
	}

	res := mustOwlIn(t, l, r.dir, "skills", "update", "go-review")

	lock := lockfile(t, r)
	if !strings.Contains(lock, moved) {
		t.Errorf("the lockfile does not record what go-review's ref now resolves to (%s):\n%s", moved, lock)
	}
	if strings.Contains(lock, was) {
		t.Errorf("the lockfile still records go-review's old commit %s:\n%s", was, lock)
	}
	if !strings.Contains(lock, untouched) {
		t.Errorf("house-style was updated too:\n%s", lock)
	}
	updated := section(t, res.stdout, "updated:")
	if !strings.Contains(updated, "go-review") || strings.Contains(updated, "house-style") {
		t.Errorf("owl skills update go-review reports updating %q:\n%s", updated, res.stdout)
	}
}

func TestS15AnAutoUpdateSkillIsResolvedAtRunStart(t *testing.T) {
	l, r, src := twoRunLayout(t)
	addSkill(t, l, r.dir, src.path(), "--auto-update")
	commitSkills(t, r)
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
	if got := runSkillCommit(t, shown, run, "go-review"); !strings.HasPrefix(src.commit("main"), got) {
		t.Errorf("run %s records %s for go-review, want the new %s:\n%s",
			run, got, src.commit("main")[:12], shown)
	}
}

func TestS16APinnedSkillIsNotResolvedAtRunStart(t *testing.T) {
	l, r, src := twoRunLayout(t)
	// Neither --ref nor --auto-update: it follows main, and a moving branch is
	// what tells pinning apart from re-resolving at the start of every Run.
	addSkill(t, l, r.dir, src.path())
	commitSkills(t, r)
	locked := src.commit("main")
	addJob(t, l, r.dir, "work")
	out, job := finishedJob(t, l)
	worktree := line(t, out, "worktree")
	first := runRows(t, out)[0].id

	src.write("Be newest.\n")
	if src.commit("main") == locked {
		t.Fatal("the source's main did not move, so this proves nothing")
	}
	run, _ := startRun(t, l)
	waitRun(t, l, job, run)

	if got := materialised(t, worktree, "go-review"); !strings.Contains(got, "Be kinder.") || strings.Contains(got, "Be newest.") {
		t.Errorf("a skill nobody asked to move moved when its branch did:\n%s", got)
	}
	shown := mustOwl(t, l, "jobs", "show", job).stdout
	for _, id := range []string{first, run} {
		if got := runSkillCommit(t, shown, id, "go-review"); !strings.HasPrefix(locked, got) {
			t.Errorf("run %s records %s for go-review, want the locked %s:\n%s",
				id, got, locked[:12], shown)
		}
	}
	if strings.Contains(section(t, shown, "skills:"), src.commit("main")[:12]) {
		t.Errorf("a run records a commit that was made after it was locked:\n%s", shown)
	}
}

func TestS17OwlRefusesToOverwriteASkillsEntryItDidNotCreate(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path())
	commitSkills(t, r)
	// The Project keeps a skill of its own at the same name, committed.
	r.commit(filepath.Join(skillsPath, "go-review", "SKILL.md"),
		"---\nname: go-review\ndescription: the project's own\n---\n\nOurs.\n",
		"keep our own skill")
	addJob(t, l, r.dir, "work", "--no-plan")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start over a skills entry Owl did not create exited 0\nstdout:\n%s", res.stdout)
	}
	out := mustOwl(t, l, "jobs", "show", "1").stdout
	worktree := line(t, out, "worktree")
	// The path, not only the name: what a user has to look at to fix this is
	// the entry in their worktree.
	for _, want := range []string{filepath.Join(worktree, skillsPath, "go-review"), "did not create"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not say %q:\n%s", want, res.stderr)
		}
	}
	if got := r.git("show", "HEAD:"+filepath.Join(skillsPath, "go-review", "SKILL.md")); !strings.Contains(got, "Ours.") {
		t.Errorf("the commit the project's own skill came from was touched:\n%s", got)
	}
	// The refusal happens with the worktree already checked out, so the file
	// the project carries is there to be taken - and must still be theirs.
	theirs := filepath.Join(worktree, skillsPath, "go-review", "SKILL.md")
	if got := readFile(t, theirs); !strings.Contains(got, "Ours.") {
		t.Errorf("the project's own skill in the worktree is now %q", got)
	}
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
	locked := other.commit("main")

	mustOwlIn(t, l, r.dir, "skills", "remove", "go-review")
	commitSkills(t, r)

	if got := manifest(t, r); strings.Contains(got, "go-review") {
		t.Errorf("the manifest still mentions go-review:\n%s", got)
	}
	if got := lockfile(t, r); strings.Contains(got, "go-review") {
		t.Errorf("the lockfile still mentions go-review:\n%s", got)
	}
	if got := manifest(t, r); !strings.Contains(got, "house-style") {
		t.Errorf("the other skill went with it:\n%s", got)
	}
	// Untouched means in both files: a lockfile that lost house-style would
	// leave it to be resolved afresh, which is the opposite of pinning.
	if got := lockfile(t, r); !strings.Contains(got, locked) {
		t.Errorf("the other skill is no longer locked at %s:\n%s", locked, got)
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

func TestS21AWithdrawnSkillIsGoneFromTheNextRun(t *testing.T) {
	l, r, src := twoRunLayout(t)
	addSkill(t, l, r.dir, src.path())
	commitSkills(t, r)
	addJob(t, l, r.dir, "work")
	out, job := finishedJob(t, l)
	worktree := line(t, out, "worktree")
	materialised(t, worktree, "go-review")

	mustOwlIn(t, l, r.dir, "skills", "remove", "go-review")
	commitSkills(t, r)
	run, _ := startRun(t, l)
	waitRun(t, l, job, run)

	if _, err := os.Lstat(filepath.Join(worktree, skillsPath, "go-review")); err == nil {
		t.Error("a skill the project withdrew is still in the worktree")
	}
	shown := mustOwl(t, l, "jobs", "show", job).stdout
	if got := section(t, shown, "skills:"); strings.Contains(got, "RUN "+run) {
		t.Errorf("run %s is recorded as having read skills:\n%s", run, got)
	}
}

func TestS22ARunIsRefusedWhenTheLockDoesNotAnswer(t *testing.T) {
	l, r, src := skillLayout(t)
	addSkill(t, l, r.dir, src.path(), "--ref", "v1.0.0")
	// The user edits the ref by hand and commits, without running
	// owl skills update: the lockfile now answers a question nobody is asking.
	manifest := filepath.Join(r.dir, ".coding-owl.yaml")
	edited := strings.ReplaceAll(readFile(t, manifest), "ref: v1.0.0", "ref: main")
	if err := os.WriteFile(manifest, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	commitSkills(t, r)
	addJob(t, l, r.dir, "work", "--no-plan")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start with a lockfile that does not answer exited 0\nstdout:\n%s", res.stdout)
	}
	for _, want := range []string{"go-review", "v1.0.0", "main", "owl skills update"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not say %q:\n%s", want, res.stderr)
		}
	}
	out := mustOwl(t, l, "jobs", "show", "1").stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job left pending", got)
	}
	if !strings.Contains(out, "runs: none") {
		t.Errorf("a run was started for a job that could not run:\n%s", out)
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

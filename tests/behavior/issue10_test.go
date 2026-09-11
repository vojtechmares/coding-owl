package behavior_test

// Behavior tests for issue #10. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-10.md. The release scenarios drive the repository's own
// scripts, with a bare repository standing in for the git remote and for the
// Homebrew tap; the daemon install scenarios drive the built owl binary with a
// stub launchctl first on its PATH, except S20, which asks the real launchd.

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// runScript runs one of the repository's scripts with dir as its working
// directory and env added to the test process's own environment.
func runScript(t *testing.T, script, dir string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(script, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !asExit(err, &exitErr) {
			t.Fatalf("running %s %v: %v\n%s", script, args, err, errb.String())
		}
		code = exitErr.ExitCode()
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

func asExit(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

func repoScript(name string) string { return filepath.Join(repoDir, "scripts", name) }

// releaseArchive builds the release once, since a cross-compiled build is slow
// and three scenarios read the same archive.
var (
	releaseOnce sync.Once
	releaseDist string
	releaseOut  result
	releaseErr  error
)

const releaseVersion = "v0.1.0"

func builtRelease(t *testing.T) (dist string, res result) {
	t.Helper()
	releaseOnce.Do(func() {
		dir, err := os.MkdirTemp("", "owlrelease")
		if err != nil {
			releaseErr = err
			return
		}
		releaseDist = dir
		releaseOut = runScript(t, repoScript("build-release.sh"), repoDir,
			[]string{"DIST=" + dir}, releaseVersion)
	})
	if releaseErr != nil {
		t.Fatal(releaseErr)
	}
	return releaseDist, releaseOut
}

// untar extracts one member of a gzipped tar archive to dst and returns its
// mode.
func untar(t *testing.T, archive, member, dst string) os.FileMode {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatalf("opening the archive: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("the archive is not gzipped: %v", err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading the archive: %v", err)
		}
		names = append(names, h.Name)
		if filepath.Base(h.Name) != member {
			continue
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&os.ModePerm)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			t.Fatal(err)
		}
		if err := out.Close(); err != nil {
			t.Fatal(err)
		}
		return os.FileMode(h.Mode) & os.ModePerm
	}
	t.Fatalf("the archive holds no %s, only %v", member, names)
	return 0
}

func sha256Of(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestS1ReleaseBuildProducesTheArchiveAndItsChecksum(t *testing.T) {
	dist, res := builtRelease(t)

	if res.code != 0 {
		t.Fatalf("build-release.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	archive := filepath.Join(dist, "coding-owl_"+releaseVersion+"_darwin_arm64.tar.gz")
	if _, err := os.Stat(archive); err != nil {
		entries, _ := os.ReadDir(dist)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("no %s; the release directory holds %v", filepath.Base(archive), names)
	}
	owl := filepath.Join(t.TempDir(), "owl")
	mode := untar(t, archive, "owl", owl)
	if mode&0o111 == 0 {
		t.Errorf("owl in the archive has mode %v, which nobody can execute", mode)
	}
	f, err := macho.Open(owl)
	if err != nil {
		t.Fatalf("the archived owl is not a Mach-O binary: %v", err)
	}
	defer func() { _ = f.Close() }()
	if f.Cpu != macho.CpuArm64 {
		t.Errorf("the archived owl is built for %v, want arm64", f.Cpu)
	}

	sums, err := os.ReadFile(filepath.Join(dist, "checksums.txt"))
	if err != nil {
		t.Fatalf("no checksums.txt: %v", err)
	}
	want := sha256Of(t, archive) + "  " + filepath.Base(archive)
	if !strings.Contains(strings.Join(strings.Fields(string(sums)), " "), strings.Join(strings.Fields(want), " ")) {
		t.Errorf("checksums.txt does not carry the archive's own sha256\nwant: %s\ngot:\n%s", want, sums)
	}
}

func TestS2ReleaseBinaryReportsItsVersion(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skipf("the release binary is darwin/arm64 and this is %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	dist, res := builtRelease(t)
	if res.code != 0 {
		t.Fatalf("build-release.sh exited %d\n%s", res.code, res.stderr)
	}
	owl := filepath.Join(t.TempDir(), "owl")
	untar(t, filepath.Join(dist, "coding-owl_"+releaseVersion+"_darwin_arm64.tar.gz"), "owl", owl)

	out, err := exec.Command(owl, "--version").CombinedOutput()

	if err != nil {
		t.Fatalf("running the released owl: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), strings.TrimPrefix(releaseVersion, "v")) {
		t.Errorf("owl --version says %q, want the version it was built for", strings.TrimSpace(string(out)))
	}
	if strings.Contains(string(out), "dev") {
		t.Errorf("owl --version says %q, which is the unstamped default", strings.TrimSpace(string(out)))
	}
}

func TestS3ReleaseBuildRefusesAVersionThatIsNotOne(t *testing.T) {
	for _, args := range [][]string{{"0.1"}, {"nightly"}, {}} {
		dist := t.TempDir()
		res := runScript(t, repoScript("build-release.sh"), repoDir, []string{"DIST=" + dist}, args...)
		if res.code == 0 {
			t.Errorf("build-release.sh %v exited 0\nstdout:\n%s", args, res.stdout)
		}
		if strings.TrimSpace(res.stderr) == "" {
			t.Errorf("build-release.sh %v said nothing about what was wrong", args)
		}
		entries, err := os.ReadDir(dist)
		if err == nil && len(entries) > 0 {
			t.Errorf("build-release.sh %v left %d files behind", args, len(entries))
		}
	}
}

// tap is a temporary Homebrew tap: a checkout on its branch whose origin is a
// bare repository, so a scenario can see what was pushed.
type tap struct {
	dir, bare, branch string
	env               []string
}

const tapBranch = "master"

func newTap(t *testing.T) *tap {
	t.Helper()
	root := shortTempDir(t)
	tp := &tap{
		dir:    filepath.Join(root, "tap"),
		bare:   filepath.Join(root, "tap.git"),
		branch: tapBranch,
		env: []string{
			"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
			"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		},
	}
	tp.run(t, root, "git", "init", "--bare", "--initial-branch="+tp.branch, tp.bare)
	// An empty remote has no branch to clone, so the checkout is made first
	// and the remote given to it.
	if err := os.MkdirAll(tp.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tp.git(t, "init", "--quiet", "--initial-branch="+tp.branch)
	tp.git(t, "remote", "add", "origin", tp.bare)
	if err := os.WriteFile(filepath.Join(tp.dir, "README.md"), []byte("# tap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tp.git(t, "add", "--", "README.md")
	tp.git(t, "commit", "-m", "initial commit")
	tp.git(t, "push", "--quiet", "origin", "HEAD:refs/heads/"+tp.branch)
	return tp
}

func (tp *tap) run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), tp.env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
	return string(out)
}

func (tp *tap) git(t *testing.T, args ...string) string {
	t.Helper()
	return tp.run(t, tp.dir, "git", args...)
}

// bareGit runs git in the tap's remote.
func (tp *tap) bareGit(t *testing.T, args ...string) string {
	t.Helper()
	return tp.run(t, tp.bare, "git", args...)
}

func (tp *tap) formula() string { return filepath.Join(tp.dir, "Formula", "coding-owl.rb") }

// servedAsset answers the asset check with status, and reports how many times
// it was asked.
func servedAsset(t *testing.T, status int) (url string, hits *int) {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/coding-owl.tar.gz", &n
}

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// bumpEnv is what scripts/bump-formula.sh needs to work on a temporary tap.
func bumpEnv(tp *tap, version, sha, assetURL string, extra ...string) []string {
	env := []string{
		"VERSION=" + version,
		"SHA256=" + sha,
		"TAP_DIR=" + tp.dir,
		"TAP_BRANCH=" + tp.branch,
		"ASSET_URL=" + assetURL,
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	}
	return append(env, extra...)
}

func TestS4FormulaPointsAtTheAssetWithItsChecksum(t *testing.T) {
	tp := newTap(t)

	res := runScript(t, repoScript("bump-formula.sh"), repoDir,
		bumpEnv(tp, "0.1.0", testDigest, "", "DRY_RUN=1"))

	if res.code != 0 {
		t.Fatalf("bump-formula.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	body, err := os.ReadFile(tp.formula())
	if err != nil {
		t.Fatalf("no rendered formula: %v", err)
	}
	formula := string(body)
	for _, want := range []string{
		"class CodingOwl < Formula",
		`version "0.1.0"`,
		`sha256 "` + testDigest + `"`,
		`url "https://github.com/vojtechmares/coding-owl/releases/download/v0.1.0/`,
		`bin.install "owl"`,
		"service do",
		`run [opt_bin/"owl", "daemon", "run"]`,
		"keep_alive true",
	} {
		if !strings.Contains(formula, want) {
			t.Errorf("the formula does not carry %q:\n%s", want, formula)
		}
	}
	if out := tp.git(t, "log", "--oneline"); strings.Count(out, "\n") != 1 {
		t.Errorf("a dry run committed to the tap:\n%s", out)
	}
}

func TestS5FormulaIsCommittedAndPushedToTheTap(t *testing.T) {
	tp := newTap(t)
	url, hits := servedAsset(t, http.StatusOK)

	res := runScript(t, repoScript("bump-formula.sh"), repoDir, bumpEnv(tp, "0.1.0", testDigest, url))

	if res.code != 0 {
		t.Fatalf("bump-formula.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	if *hits == 0 {
		t.Errorf("the asset was never checked before the formula was written")
	}
	if body := tp.bareGit(t, "show", tp.branch+":Formula/coding-owl.rb"); !strings.Contains(body, `version "0.1.0"`) {
		t.Errorf("the tap's remote does not carry the formula for 0.1.0:\n%s", body)
	}
	if msg := tp.bareGit(t, "log", "-1", "--format=%B", tp.branch); !strings.Contains(msg, "Signed-off-by:") {
		t.Errorf("the tap commit is not signed off:\n%s", msg)
	}
}

func TestS6FormulaAtTheSameVersionChangesNothing(t *testing.T) {
	tp := newTap(t)
	url, _ := servedAsset(t, http.StatusOK)
	if res := runScript(t, repoScript("bump-formula.sh"), repoDir, bumpEnv(tp, "0.1.0", testDigest, url)); res.code != 0 {
		t.Fatalf("the first bump exited %d\n%s", res.code, res.stderr)
	}
	before := tp.bareGit(t, "rev-parse", tp.branch)

	res := runScript(t, repoScript("bump-formula.sh"), repoDir, bumpEnv(tp, "0.1.0", testDigest, url))

	if res.code != 0 {
		t.Fatalf("the second bump exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "already") {
		t.Errorf("the second bump does not say the formula already points at 0.1.0:\n%s", res.stdout)
	}
	if after := tp.bareGit(t, "rev-parse", tp.branch); after != before {
		t.Errorf("the tap moved from %s to %s with nothing to change", strings.TrimSpace(before), strings.TrimSpace(after))
	}
}

func TestS7FormulaIsNotWrittenForAnAssetThatIsNotThere(t *testing.T) {
	tp := newTap(t)
	url, _ := servedAsset(t, http.StatusNotFound)

	res := runScript(t, repoScript("bump-formula.sh"), repoDir, bumpEnv(tp, "0.1.0", testDigest, url))

	if res.code == 0 {
		t.Fatalf("bump-formula.sh exited 0 for an asset that answers 404\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "download") {
		t.Errorf("stderr does not say the asset cannot be downloaded:\n%s", res.stderr)
	}
	if _, err := os.Stat(tp.formula()); err == nil {
		t.Errorf("the formula was written anyway")
	}
}

func TestS8FormulaRefusesAVersionOrChecksumThatIsNotOne(t *testing.T) {
	for _, bad := range []struct{ version, sha, what string }{
		{"nightly", testDigest, "version"},
		{"0.1.0", "not-a-digest", "checksum"},
	} {
		tp := newTap(t)
		url, _ := servedAsset(t, http.StatusOK)

		res := runScript(t, repoScript("bump-formula.sh"), repoDir, bumpEnv(tp, bad.version, bad.sha, url))

		if res.code == 0 {
			t.Errorf("bump-formula.sh accepted a bad %s\nstdout:\n%s", bad.what, res.stdout)
		}
		if strings.TrimSpace(res.stderr) == "" {
			t.Errorf("bump-formula.sh said nothing about the bad %s", bad.what)
		}
		if _, err := os.Stat(tp.formula()); err == nil {
			t.Errorf("a bad %s still wrote a formula", bad.what)
		}
	}
}

func TestS21FormulaThatWasNeverPushedIsPushedNextTime(t *testing.T) {
	tp := newTap(t)
	url, _ := servedAsset(t, http.StatusOK)
	// A remote that is not there is a push that fails after the commit.
	tp.git(t, "remote", "set-url", "origin", filepath.Join(tp.dir, "..", "gone.git"))
	if res := runScript(t, repoScript("bump-formula.sh"), repoDir, bumpEnv(tp, "0.1.0", testDigest, url)); res.code == 0 {
		t.Fatalf("bump-formula.sh reported success although the push could not work:\n%s", res.stdout)
	}
	tp.git(t, "remote", "set-url", "origin", tp.bare)

	res := runScript(t, repoScript("bump-formula.sh"), repoDir, bumpEnv(tp, "0.1.0", testDigest, url))

	if res.code != 0 {
		t.Fatalf("bump-formula.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	if body := tp.bareGit(t, "show", tp.branch+":Formula/coding-owl.rb"); !strings.Contains(body, `version "0.1.0"`) {
		t.Errorf("the tap's remote still does not carry the formula:\n%s", body)
	}
}

// releaseClone is a repository carrying the release script, on main, whose
// origin is a bare repository. It stands in for the project itself, so that
// tagging and pushing can be watched without touching the real remote.
type releaseClone struct {
	dir, bare string
	env       []string
}

func newReleaseClone(t *testing.T) *releaseClone {
	t.Helper()
	root := shortTempDir(t)
	rc := &releaseClone{
		dir:  filepath.Join(root, "work"),
		bare: filepath.Join(root, "origin.git"),
		env: []string{
			"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
			"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		},
	}
	rc.run(t, root, "git", "init", "--bare", "--initial-branch=main", rc.bare)
	// The script works out its own repository from where it lives, so it has
	// to live in the clone.
	if err := os.MkdirAll(filepath.Join(rc.dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	rc.git(t, "init", "--quiet", "--initial-branch=main")
	rc.git(t, "remote", "add", "origin", rc.bare)
	body, err := os.ReadFile(repoScript("release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rc.dir, "scripts", "release.sh"), body, 0o755); err != nil {
		t.Fatal(err)
	}
	rc.git(t, "add", "--", "scripts/release.sh")
	rc.git(t, "commit", "-m", "add the release script")
	rc.git(t, "push", "--quiet", "--set-upstream", "origin", "main")
	return rc
}

func (rc *releaseClone) run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), rc.env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
	return string(out)
}

func (rc *releaseClone) git(t *testing.T, args ...string) string {
	t.Helper()
	return rc.run(t, rc.dir, "git", args...)
}

func (rc *releaseClone) release(t *testing.T, args ...string) result {
	t.Helper()
	return runScript(t, filepath.Join(rc.dir, "scripts", "release.sh"), rc.dir, rc.env, args...)
}

// tags returns the tags the remote carries.
func (rc *releaseClone) remoteTags(t *testing.T) string {
	t.Helper()
	return rc.run(t, rc.bare, "git", "tag", "--list")
}

func TestS9ReleaseRefusesADirtyWorkingTree(t *testing.T) {
	rc := newReleaseClone(t)
	if err := os.WriteFile(filepath.Join(rc.dir, "scratch.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := rc.release(t, "--yes", "v0.1.0")

	if res.code == 0 {
		t.Fatalf("release.sh tagged a dirty tree\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "dirty") {
		t.Errorf("stderr does not say the tree is dirty:\n%s", res.stderr)
	}
	if tags := rc.git(t, "tag", "--list"); strings.TrimSpace(tags) != "" {
		t.Errorf("a tag was created anyway: %s", tags)
	}
}

func TestS10ReleaseRefusesABranchThatIsNotMain(t *testing.T) {
	rc := newReleaseClone(t)
	rc.git(t, "checkout", "-q", "-b", "wip")

	res := rc.release(t, "--yes", "v0.1.0")

	if res.code == 0 {
		t.Fatalf("release.sh tagged from a feature branch\nstdout:\n%s", res.stdout)
	}
	for _, want := range []string{"wip", "main"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not name %q:\n%s", want, res.stderr)
		}
	}
	if tags := rc.git(t, "tag", "--list"); strings.TrimSpace(tags) != "" {
		t.Errorf("a tag was created anyway: %s", tags)
	}
}

func TestS11ReleaseDryRunChangesNothing(t *testing.T) {
	rc := newReleaseClone(t)

	res := rc.release(t, "--dry-run", "v0.2.0")

	if res.code != 0 {
		t.Fatalf("release.sh --dry-run exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "v0.2.0") {
		t.Errorf("a dry run does not report the version it would tag:\n%s", res.stdout)
	}
	if tags := rc.git(t, "tag", "--list"); strings.TrimSpace(tags) != "" {
		t.Errorf("a dry run created the tag %s", tags)
	}
	if tags := rc.remoteTags(t); strings.TrimSpace(tags) != "" {
		t.Errorf("a dry run pushed the tag %s", tags)
	}
}

func TestS12ReleaseTagsTheCommitAndPushesIt(t *testing.T) {
	rc := newReleaseClone(t)
	head := strings.TrimSpace(rc.git(t, "rev-parse", "HEAD"))
	before := strings.Fields(rc.run(t, rc.bare, "git", "for-each-ref", "--format=%(refname) %(objectname)"))

	res := rc.release(t, "--yes", "v0.2.0")

	if res.code != 0 {
		t.Fatalf("release.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	if got := strings.TrimSpace(rc.remoteTags(t)); got != "v0.2.0" {
		t.Errorf("the remote carries the tags %q, want v0.2.0", got)
	}
	if got := strings.TrimSpace(rc.run(t, rc.bare, "git", "rev-parse", "v0.2.0^{commit}")); got != head {
		t.Errorf("the tag points at %s, want the commit that was checked out, %s", got, head)
	}
	if kind := strings.TrimSpace(rc.run(t, rc.bare, "git", "cat-file", "-t", "v0.2.0")); kind != "tag" {
		t.Errorf("the tag is a %s, want an annotated tag", kind)
	}
	// Nothing but the tag: the remote gained one ref, and every ref it had
	// before is still pointing where it was.
	after := strings.Fields(rc.run(t, rc.bare, "git", "for-each-ref", "--format=%(refname) %(objectname)"))
	var gained []string
	for i := 0; i < len(after); i += 2 {
		ref, object := after[i], after[i+1]
		kept := false
		for j := 0; j < len(before); j += 2 {
			if before[j] == ref {
				kept = true
				if before[j+1] != object {
					t.Errorf("%s moved from %s to %s; releasing pushes the tag and nothing else",
						ref, before[j+1], object)
				}
			}
		}
		if !kept {
			gained = append(gained, ref)
		}
	}
	if len(gained) != 1 || gained[0] != "refs/tags/v0.2.0" {
		t.Errorf("the remote gained %v, want only refs/tags/v0.2.0", gained)
	}
}

func TestS13ReleaseRefusesAVersionThatAlreadyExists(t *testing.T) {
	rc := newReleaseClone(t)
	if res := rc.release(t, "--yes", "v0.2.0"); res.code != 0 {
		t.Fatalf("the first release exited %d\n%s", res.code, res.stderr)
	}
	// Gone from this checkout, still on the remote - which is what a second
	// machine, or a tag somebody deleted locally, looks like.
	rc.git(t, "tag", "-d", "v0.2.0")

	res := rc.release(t, "--yes", "v0.2.0")

	if res.code == 0 {
		t.Fatalf("release.sh tagged v0.2.0 twice\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "exists") {
		t.Errorf("stderr does not say the tag already exists:\n%s", res.stderr)
	}
	// Refused, rather than let git fail on the tag it was told to create.
	if strings.Contains(res.stdout, "Tagging") {
		t.Errorf("release.sh went as far as tagging before it noticed:\n%s", res.stdout)
	}
	if got := strings.Fields(rc.remoteTags(t)); len(got) != 1 {
		t.Errorf("the remote carries %v, want exactly one v0.2.0", got)
	}
}

// launchctlStub puts a stub launchctl first on the PATH and returns the file it
// records its arguments in.
func launchctlStub(t *testing.T, l *layout, failOn string) (*layout, string) {
	t.Helper()
	dir := filepath.Join(l.root, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(l.root, "launchctl.log")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ -n %q ] && [ "$1" = %q ]; then
  echo "launchctl refused: $*" >&2
  exit 65
fi
exit 0
`, log, failOn, failOn)
	if err := os.WriteFile(filepath.Join(dir, "launchctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return l.withEnv("PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")), log
}

func launchctlCalls(t *testing.T, log string) []string {
	t.Helper()
	body, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	var calls []string
	for _, ln := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if ln != "" {
			calls = append(calls, ln)
		}
	}
	return calls
}

func requireDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skipf("launchd is macOS's, and this is %s", runtime.GOOS)
	}
}

// launchAgent is a launch agent read back the way launchd reads it.
type launchAgent struct {
	Label             string            `json:"Label"`
	ProgramArguments  []string          `json:"ProgramArguments"`
	KeepAlive         bool              `json:"KeepAlive"`
	RunAtLoad         bool              `json:"RunAtLoad"`
	StandardOutPath   string            `json:"StandardOutPath"`
	StandardErrorPath string            `json:"StandardErrorPath"`
	EnvironmentVars   map[string]string `json:"EnvironmentVariables"`
}

// readAgent parses a property list with the tool macOS itself parses them
// with, so a scenario reads what launchd would read rather than the text.
func readAgent(t *testing.T, path string) launchAgent {
	t.Helper()
	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).CombinedOutput()
	if err != nil {
		body, _ := os.ReadFile(path)
		t.Fatalf("plutil refuses the launch agent: %v\n%s\n%s", err, out, body)
	}
	var a launchAgent
	if err := json.Unmarshal(out, &a); err != nil {
		t.Fatalf("reading the launch agent: %v\n%s", err, out)
	}
	return a
}

func agentPath(l *layout) string {
	return filepath.Join(l.home, "Library", "LaunchAgents", "dev.codingowl.owld.plist")
}

func TestS15DaemonInstallWritesTheLaunchAgent(t *testing.T) {
	requireDarwin(t)
	l, _ := launchctlStub(t, newLayout(t), "")
	// A state directory that is already there, and readable by everybody.
	if err := os.MkdirAll(filepath.Join(l.state, "coding-owl"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(l.state, "coding-owl"), 0o777); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "daemon", "install")

	if res.code != 0 {
		t.Fatalf("owl daemon install exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	agent := readAgent(t, agentPath(l))
	if agent.Label != "dev.codingowl.owld" {
		t.Errorf("label = %q, want dev.codingowl.owld", agent.Label)
	}
	if want := []string{owlBin, "daemon", "run"}; strings.Join(agent.ProgramArguments, " ") != strings.Join(want, " ") {
		t.Errorf("the agent runs %v, want %v", agent.ProgramArguments, want)
	}
	// Both of these are what makes launchd start the daemon at login and start
	// it again after it dies.
	if !agent.KeepAlive {
		t.Errorf("KeepAlive is not set, so launchd would not restart the daemon")
	}
	if !agent.RunAtLoad {
		t.Errorf("RunAtLoad is not set, so the daemon would not start until something asked")
	}
	stateDir := filepath.Join(l.state, "coding-owl")
	for what, path := range map[string]string{"stdout": agent.StandardOutPath, "stderr": agent.StandardErrorPath} {
		if !strings.HasPrefix(path, stateDir+string(os.PathSeparator)) {
			t.Errorf("the agent writes its %s to %q, want a file under %s", what, path, stateDir)
		}
	}
	// The agent's log goes in the daemon's state directory, which is nobody
	// else's business - the daemon creates it that way itself.
	st, err := os.Stat(filepath.Join(l.state, "coding-owl"))
	if err != nil {
		t.Fatalf("the state directory the agent logs into was not created: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o700 {
		t.Errorf("the state directory is %v, want 0700", got)
	}
}

func TestS16DaemonInstallHandsTheAgentToLaunchctl(t *testing.T) {
	requireDarwin(t)
	l, log := launchctlStub(t, newLayout(t), "")

	res := runOwl(t, l, "daemon", "install")

	if res.code != 0 {
		t.Fatalf("owl daemon install exited %d\n%s", res.code, res.stderr)
	}
	calls := launchctlCalls(t, log)
	bootstrapped := false
	for _, c := range calls {
		if strings.HasPrefix(c, "bootstrap ") && strings.Contains(c, agentPath(l)) && strings.Contains(c, fmt.Sprintf("gui/%d", os.Getuid())) {
			bootstrapped = true
		}
	}
	if !bootstrapped {
		t.Errorf("launchctl was never asked to bootstrap the agent into the user's domain:\n%v", calls)
	}
	if !strings.Contains(res.stdout, agentPath(l)) {
		t.Errorf("the command does not say where it wrote the agent:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "owl daemon status") {
		t.Errorf("the command does not say how to check on the daemon:\n%s", res.stdout)
	}
}

func TestS17DaemonInstallTwiceReplacesTheAgent(t *testing.T) {
	requireDarwin(t)
	l, log := launchctlStub(t, newLayout(t), "")
	if res := runOwl(t, l, "daemon", "install"); res.code != 0 {
		t.Fatalf("the first install exited %d\n%s", res.code, res.stderr)
	}

	res := runOwl(t, l, "daemon", "install")

	if res.code != 0 {
		t.Fatalf("the second install exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	calls := launchctlCalls(t, log)
	if len(calls) < 2 {
		t.Fatalf("launchctl was called %d times over two installs: %v", len(calls), calls)
	}
	last := calls[len(calls)-2:]
	if !strings.HasPrefix(last[0], "bootout ") || !strings.Contains(last[0], "dev.codingowl.owld") {
		t.Errorf("the second install did not remove the old agent first: %v", calls)
	}
	if !strings.HasPrefix(last[1], "bootstrap ") {
		t.Errorf("the second install did not bootstrap the new agent: %v", calls)
	}
	if agent := readAgent(t, agentPath(l)); agent.Label != "dev.codingowl.owld" {
		t.Errorf("the reinstalled agent is %q", agent.Label)
	}
}

func TestS18DaemonInstallPrintWritesNothing(t *testing.T) {
	requireDarwin(t)
	l, log := launchctlStub(t, newLayout(t), "")

	res := runOwl(t, l, "daemon", "install", "--print")

	if res.code != 0 {
		t.Fatalf("owl daemon install --print exited %d\n%s", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "dev.codingowl.owld") {
		t.Errorf("--print does not print the agent:\n%s", res.stdout)
	}
	if _, err := os.Stat(filepath.Join(l.home, "Library", "LaunchAgents")); err == nil {
		t.Errorf("--print created the LaunchAgents directory")
	}
	if calls := launchctlCalls(t, log); len(calls) != 0 {
		t.Errorf("--print called launchctl: %v", calls)
	}
}

func TestS19DaemonInstallReportsALaunchdFailure(t *testing.T) {
	requireDarwin(t)
	l, _ := launchctlStub(t, newLayout(t), "bootstrap")

	res := runOwl(t, l, "daemon", "install")

	if res.code == 0 {
		t.Fatalf("owl daemon install exited 0 although launchctl refused\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "launchctl refused") {
		t.Errorf("stderr does not hold what launchctl printed:\n%s", res.stderr)
	}
}

func TestS20DaemonInstallStartsADaemonStatusCanReach(t *testing.T) {
	requireDarwin(t)
	if os.Getenv("OWL_SMOKE_LAUNCHD") == "" {
		t.Skip("set OWL_SMOKE_LAUNCHD=1 to register a real launchd agent on this machine")
	}
	l := newLayout(t)
	t.Cleanup(func() {
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/dev.codingowl.owld", os.Getuid())).Run()
	})

	if res := runOwl(t, l, "daemon", "install"); res.code != 0 {
		t.Fatalf("owl daemon install exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}

	var status result
	waitFor(t, "the daemon the launch agent starts", func() bool {
		status = runOwl(t, l, "daemon", "status")
		return status.code == 0
	})
	for _, want := range []string{"version:", "uptime:", l.socket()} {
		if !strings.Contains(status.stdout, want) {
			t.Errorf("owl daemon status does not report %q:\n%s", want, status.stdout)
		}
	}

	out, err := exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/dev.codingowl.owld", os.Getuid())).CombinedOutput()
	if err != nil {
		t.Fatalf("unloading the agent: %v\n%s", err, out)
	}
	waitFor(t, "the daemon to stop once the agent is unloaded", func() bool {
		return runOwl(t, l, "daemon", "status").code != 0
	})
}

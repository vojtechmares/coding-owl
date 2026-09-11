package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseNumstatReadsRecordsRenamesAndBinaries(t *testing.T) {
	// Two plain records, a rename, and a binary file, as `--numstat -z`
	// writes them.
	out := []byte("3\t1\ta.txt\x00" +
		"0\t2\tdir/b.txt\x00" +
		"1\t1\t\x00old.txt\x00new.txt\x00" +
		"-\t-\timg.png\x00")
	got, err := parseNumstat(out)
	if err != nil {
		t.Fatal(err)
	}
	want := DiffSummary{
		Files: []DiffFile{
			{Path: "a.txt", Insertions: 3, Deletions: 1},
			{Path: "dir/b.txt", Insertions: 0, Deletions: 2},
			{Path: "new.txt", Insertions: 1, Deletions: 1},
			{Path: "img.png"},
		},
		Insertions: 4,
		Deletions:  4,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNumstat = %+v, want %+v", got, want)
	}
}

func TestParseNumstatRefusesGarbage(t *testing.T) {
	for _, in := range []string{"3\ta.txt\x00", "x\t1\ta.txt\x00", "1\t1\t\x00old\x00"} {
		if _, err := parseNumstat([]byte(in)); err == nil {
			t.Errorf("parseNumstat(%q) = nil error, want one", in)
		}
	}
	if got, err := parseNumstat(nil); err != nil || len(got.Files) != 0 {
		t.Errorf("parseNumstat(nil) = %+v, %v; want empty", got, err)
	}
}

// git runs git in dir for the test's own setup.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiffStatCountsOnlyWhatTheBranchAdded(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "base.txt")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "job")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "work")
	// main moves on after the branch left it; that must not count against
	// the branch.
	gitIn(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "base moves")

	got, err := DiffStat(dir, "main", "job")
	if err != nil {
		t.Fatal(err)
	}
	want := DiffSummary{Files: []DiffFile{{Path: "work.txt", Insertions: 2}}, Insertions: 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffStat = %+v, want %+v", got, want)
	}

	if _, err := DiffStat(dir, "main", "nope"); err == nil {
		t.Error("DiffStat against a branch that does not exist returned no error")
	}
}

func TestDiffPatchIsWhatTheBranchChangedAndIsBounded(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "base.txt")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "job")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("a line the agent added\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "work")
	// What landed on main after the branch left it is not the branch's doing,
	// so it is not in the branch's patch either.
	gitIn(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "base moves")

	got, complete, err := DiffPatch(dir, "main", "job", 1<<20)

	if err != nil {
		t.Fatal(err)
	}
	if !complete {
		t.Error("DiffPatch reports a patch that did not fit, for one that is a few lines")
	}
	for _, want := range []string{"work.txt", "+a line the agent added"} {
		if !strings.Contains(got, want) {
			t.Errorf("the patch does not carry %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "two") {
		t.Errorf("the patch carries what landed on main after the branch left it:\n%s", got)
	}

	cut, complete, err := DiffPatch(dir, "main", "job", 40)

	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Error("DiffPatch reports a patch as whole when it was cut")
	}
	if len(cut) > 40 {
		t.Errorf("the patch is %d bytes, want no more than the 40 it was given", len(cut))
	}
}

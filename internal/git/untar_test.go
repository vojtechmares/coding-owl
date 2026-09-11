package git

// An internal test: unpacking is about what an archive may contain, and the
// archives that matter here are ones git would not write.

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openArchive reads back an archive written for a test.
func openArchive(t *testing.T, path string) io.Reader {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// archiveOf writes a tar holding those entries and returns its path.
func archiveOf(t *testing.T, entries ...*tar.Header) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.tar")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := tar.NewWriter(f)
	for _, h := range entries {
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg && h.Size > 0 {
			if _, err := w.Write([]byte(strings.Repeat("x", int(h.Size)))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUntarRefusesAPathThatWouldLandOutside(t *testing.T) {
	into := t.TempDir()
	outside := filepath.Join(t.TempDir(), "theirs")
	if err := os.WriteFile(outside, []byte("theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"../escaped.txt", "/etc/escaped", "a/../../escaped.txt"} {
		err := untar(openArchive(t, archiveOf(t, &tar.Header{Name: name, Typeflag: tar.TypeReg, Size: 1})), into)

		if err == nil {
			t.Errorf("untar of %q = nil, want it refused", name)
		}
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "theirs\n" {
		t.Errorf("something outside the directory was written: %q %v", got, err)
	}
}

func TestUntarRefusesASymlink(t *testing.T) {
	// A symlink in a skill is a way of reaching whatever it points at from
	// inside a worktree an Agent works in.
	err := untar(openArchive(t, archiveOf(t, &tar.Header{
		Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd",
	})), t.TempDir())

	if err == nil {
		t.Fatal("untar of a symlink = nil, want it refused")
	}
	if !strings.Contains(err.Error(), "link") {
		t.Errorf("the error %q does not name the entry", err)
	}
}

func TestUntarRefusesAFileLargerThanASkillsMayBe(t *testing.T) {
	err := untar(openArchive(t, archiveOf(t, &tar.Header{
		Name: "huge", Typeflag: tar.TypeReg, Size: maxEntry + 1,
	})), t.TempDir())

	if err == nil {
		t.Fatal("untar of an oversized file = nil, want it refused")
	}
}

func TestUntarWritesFilesOnlyItsOwnerCanRead(t *testing.T) {
	into := t.TempDir()

	if err := untar(openArchive(t, archiveOf(t, &tar.Header{
		Name: "SKILL.md", Typeflag: tar.TypeReg, Size: 4, Mode: 0o777,
	})), into); err != nil {
		t.Fatalf("untar: %v", err)
	}

	info, err := os.Stat(filepath.Join(into, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	// What a skill's author marked executable is not what Owl runs.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the file is %o, want 600 whatever the archive said", perm)
	}
}

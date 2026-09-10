package project

// The rename rollback cannot be reached from the public interface without
// losing a race with a concurrent add, so the move and its undo are exercised
// here directly. Rename wires them together; these tests are what say the undo
// actually restores the directory.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return &Service{configHome: filepath.Join(t.TempDir(), "coding-owl")}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMoveConfigDirUndoRestoresTheDirectory(t *testing.T) {
	s := newTestService(t)
	writeConfig(t, s.configPath("api"), "apiVersion: codingowl.dev/v1\n")

	undo, err := s.moveConfigDir("api", "backend")
	if err != nil {
		t.Fatalf("moveConfigDir: %v", err)
	}
	if undo == nil {
		t.Fatal("moveConfigDir reported nothing to undo after moving a directory")
	}
	if _, err := os.Stat(s.configPath("backend")); err != nil {
		t.Fatalf("directory was not moved: %v", err)
	}

	if err := undo(); err != nil {
		t.Fatalf("undo: %v", err)
	}

	got, err := os.ReadFile(s.configPath("api"))
	if err != nil {
		t.Fatalf("undo did not put the directory back: %v", err)
	}
	if string(got) != "apiVersion: codingowl.dev/v1\n" {
		t.Errorf("restored config = %q", got)
	}
	if _, err := os.Stat(s.configDir("backend")); !os.IsNotExist(err) {
		t.Errorf("the destination directory survived the undo (err=%v)", err)
	}
}

func TestMoveConfigDirWithNothingToMove(t *testing.T) {
	s := newTestService(t)

	undo, err := s.moveConfigDir("api", "backend")
	if err != nil {
		t.Fatalf("moveConfigDir: %v", err)
	}
	if undo != nil {
		t.Error("moveConfigDir returned an undo for a directory that does not exist")
	}
}

func TestMoveConfigDirRefusesAnExistingDestination(t *testing.T) {
	s := newTestService(t)
	writeConfig(t, s.configPath("api"), "api\n")
	writeConfig(t, s.configPath("backend"), "backend\n")

	undo, err := s.moveConfigDir("api", "backend")

	if err == nil {
		t.Fatal("moveConfigDir overwrote an existing destination")
	}
	if undo != nil {
		t.Error("moveConfigDir returned an undo after refusing")
	}
	if !strings.Contains(err.Error(), s.configDir("backend")) {
		t.Errorf("error %q does not name the destination", err)
	}
	for name, want := range map[string]string{"api": "api\n", "backend": "backend\n"} {
		got, err := os.ReadFile(s.configPath(name))
		if err != nil {
			t.Errorf("reading %s: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s config = %q, want %q", name, got, want)
		}
	}
}

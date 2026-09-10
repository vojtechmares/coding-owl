package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	s, _, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenCreatesTheDatabaseAndAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "owl.db")

	s, m, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if m.Applied == 0 {
		t.Errorf("Applied = 0 on a fresh database")
	}
	if m.Schema != m.Applied {
		t.Errorf("Schema = %d, Applied = %d; a fresh database should reach the version it applied", m.Schema, m.Applied)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file was not created: %v", err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owl.db")
	first, m1, err := store.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	_ = first.Close()

	second, m2, err := store.Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = second.Close() }()

	if m2.Applied != 0 {
		t.Errorf("second Open applied %d migrations, want 0", m2.Applied)
	}
	if m2.Schema != m1.Schema {
		t.Errorf("second Open moved the schema from %d to %d", m1.Schema, m2.Schema)
	}
}

var ctx = context.Background()

func TestAddAndGetProject(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	want := store.Project{
		Name:       "api",
		Path:       "/src/api",
		BaseBranch: "main",
		Registered: time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC),
	}

	if err := s.AddProject(ctx, want); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	got, err := s.GetProject(ctx, "api")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != want.Name || got.Path != want.Path || got.BaseBranch != want.BaseBranch || !got.Registered.Equal(want.Registered) {
		t.Errorf("GetProject = %+v, want %+v", got, want)
	}
}

func TestGetUnknownProject(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))

	_, err := s.GetProject(ctx, "ghost")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetProject error = %v, want ErrNotFound", err)
	}
}

func addProject(t *testing.T, s *store.Store, name string) store.Project {
	t.Helper()
	p := store.Project{Name: name, Path: "/src/" + name, BaseBranch: "main", Registered: time.Now()}
	if err := s.AddProject(ctx, p); err != nil {
		t.Fatalf("AddProject(%s): %v", name, err)
	}
	return p
}

func TestAddProjectRefusesADuplicateName(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	addProject(t, s, "api")

	err := s.AddProject(ctx, store.Project{Name: "api", Path: "/elsewhere", BaseBranch: "main", Registered: time.Now()})

	if !errors.Is(err, store.ErrNameTaken) {
		t.Errorf("AddProject error = %v, want ErrNameTaken", err)
	}
}

func TestListProjectsIsOrderedByName(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	for _, n := range []string{"web", "api", "tools"} {
		addProject(t, s, n)
	}

	got, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	var names []string
	for _, p := range got {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "api,tools,web" {
		t.Errorf("ListProjects = %v, want api,tools,web", names)
	}
}

func TestSetProjectPathKeepsIdentity(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	before := addProject(t, s, "api")

	if err := s.SetProjectPath(ctx, "api", "/moved/api"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}

	got, err := s.GetProject(ctx, "api")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Path != "/moved/api" {
		t.Errorf("Path = %q, want %q", got.Path, "/moved/api")
	}
	if !got.Registered.Equal(before.Registered.UTC().Truncate(time.Nanosecond)) {
		t.Errorf("Registered changed from %s to %s", before.Registered, got.Registered)
	}
}

func TestSetProjectPathOnUnknownProject(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))

	if err := s.SetProjectPath(ctx, "ghost", "/x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SetProjectPath error = %v, want ErrNotFound", err)
	}
}

func TestRenameProject(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	addProject(t, s, "api")

	if err := s.RenameProject(ctx, "api", "backend"); err != nil {
		t.Fatalf("RenameProject: %v", err)
	}

	if _, err := s.GetProject(ctx, "backend"); err != nil {
		t.Errorf("GetProject(backend): %v", err)
	}
	if _, err := s.GetProject(ctx, "api"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetProject(api) error = %v, want ErrNotFound", err)
	}
}

func TestRenameProjectOntoATakenName(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	addProject(t, s, "api")
	addProject(t, s, "backend")

	if err := s.RenameProject(ctx, "api", "backend"); !errors.Is(err, store.ErrNameTaken) {
		t.Errorf("RenameProject error = %v, want ErrNameTaken", err)
	}
	if _, err := s.GetProject(ctx, "api"); err != nil {
		t.Errorf("api disappeared after a refused rename: %v", err)
	}
}

func TestRemoveProject(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	addProject(t, s, "api")

	if err := s.RemoveProject(ctx, "api"); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}

	if _, err := s.GetProject(ctx, "api"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetProject after remove = %v, want ErrNotFound", err)
	}
	if err := s.RemoveProject(ctx, "api"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second RemoveProject = %v, want ErrNotFound", err)
	}
}

func TestRenameUnknownProjectOntoAnExistingName(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	addProject(t, s, "backend")

	if err := s.RenameProject(ctx, "ghost", "backend"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("RenameProject error = %v, want ErrNotFound", err)
	}
}

func TestOpenRestrictsTheDatabaseAndItsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	// A directory that already exists and is world-readable: MkdirAll would
	// leave it alone, but the write-ahead log lands in it.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "owl.db")

	s := openStore(t, path)
	if err := s.AddProject(ctx, store.Project{Name: "api", Path: "/src/api", BaseBranch: "main", Registered: time.Now()}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	for target, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		st, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat %s: %v", target, err)
		}
		if got := st.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", target, got, want)
		}
	}
}

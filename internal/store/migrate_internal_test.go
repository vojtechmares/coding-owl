package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// migrateBefore brings a new database up to the schema it had before the named
// migration, which is how a test gets hold of a database an older Owl left.
func migrateBefore(t *testing.T, path, migration string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer func() { _ = db.Close() }()
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < migration {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		text, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(text)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(names))); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO projects (name, path, base_branch, registered) VALUES ('api', '/repo', 'main', '2026-09-01T00:00:00Z')`,
		`INSERT INTO jobs (source, source_ref, project, prompt, state, created) VALUES ('local', 'a', 'api', 'work', 'review', '2026-09-01T00:00:00Z')`,
		`INSERT INTO runs (job_id, attempt, started, log_path) VALUES (1, 1, '2026-09-01T00:00:00Z', '/logs/1.jsonl')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func TestARunFromBeforePromptsWereRecordedHasNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owl.db")
	migrateBefore(t, path, "0013_run_system_prompt.sql")

	s, m, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	r, err := s.GetRun(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}

	if m.Applied == 0 {
		t.Fatal("opening the older database applied no migrations")
	}
	// Nothing recorded what that Run was given, and nothing may make it up.
	if r.SystemPrompt != "" {
		t.Errorf("a run from before prompts were recorded reports %q, want none", r.SystemPrompt)
	}
}

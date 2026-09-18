package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
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
	// The schema version is the index of the named migration in the sorted
	// list, not the number of files that happen to sort before it: a test that
	// asks to stop before a migration that is not there must say so rather than
	// quietly test some other schema.
	all := migrationNames(t)
	idx := -1
	for i, name := range all {
		if name == migration {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("migrateBefore: no migration named %s, so it cannot stop before it", migration)
	}
	names := all[:idx]
	for _, name := range names {
		text, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(text)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", idx)); err != nil {
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

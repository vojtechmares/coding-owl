package behavior_test

// Behavior tests for issue #108. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-108.md. The append-only migration policy is enforced by
// a test inside internal/store, so each scenario mutates a throwaway copy of
// the repository and runs that test there, exactly as CI would.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// policyTest108 is the check the issue asks for; migrateBeforeTest108 is the
	// existing test whose helper must stop counting files (S8).
	policyTest108        = "TestMigrationsAreAppendOnly"
	migrateBeforeTest108 = "TestARunFromBeforePromptsWereRecordedHasNone"

	manifestPath108     = "internal/store/migrations.sha256"
	migrationsDir108    = "internal/store/migrations"
	storeTestTimeout108 = 5 * time.Minute
)

// copyRepoFor108 copies the repository's tracked and untracked-but-not-ignored
// files into a fresh temporary directory, so a scenario can mutate the
// migrations without touching the checkout. Untracked files are copied too:
// during the red-green loop the check and the manifest are not committed yet.
func copyRepoFor108(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoDir, "ls-files", "-zco", "--exclude-standard").Output()
	if err != nil {
		t.Fatalf("listing the repository's files: %v", err)
	}
	dst := t.TempDir()
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		src := filepath.Join(repoDir, rel)
		info, err := os.Stat(src)
		if err != nil {
			// git lists paths it has staged but that are gone from disk.
			continue
		}
		if info.IsDir() {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	return dst
}

// runStoreTestFor108 runs one test of internal/store in dir and returns what CI
// would see: the exit code and the combined output.
func runStoreTestFor108(t *testing.T, dir, name string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), storeTestTimeout108)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-v", "-run", name, "./internal/store/")
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !asExit(err, &exitErr) {
			t.Fatalf("running go test -run %s in %s: %v\n%s", name, dir, err, out.String())
		}
		code = exitErr.ExitCode()
	}
	return result{stdout: out.String(), code: code}
}

func sha256Of108(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// recordedSHAFor108 returns the sha256 the committed manifest records for a
// migration. It reads the manifest as the check's own parser must: blank lines
// and comments are skipped, and a line is "<sha256>  <name>".
func recordedSHAFor108(t *testing.T, repo, migration string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, manifestPath108))
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == migration {
			return fields[0]
		}
	}
	t.Fatalf("the manifest records no line for %s", migration)
	return ""
}

// editedMigration108 is shared by S2 and S7, which assert different things
// about the same failure, so the nested go test runs once for the pair.
type edited108 struct {
	run      result
	recorded string
	computed string
}

// editedCopy108 makes a copy with a line appended to 0005_checks.sql and runs
// the policy check in it.
var (
	editedOnce108   sync.Once
	editedResult108 edited108
)

func editedCopy108(t *testing.T) edited108 {
	t.Helper()
	editedOnce108.Do(func() {
		dir := copyRepoFor108(t)
		path := filepath.Join(dir, migrationsDir108, "0005_checks.sql")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading 0005_checks.sql: %v", err)
		}
		edit := append(data, []byte("\n-- an edit an installed database will never see\n")...)
		if err := os.WriteFile(path, edit, 0o644); err != nil {
			t.Fatalf("editing 0005_checks.sql: %v", err)
		}
		editedResult108 = edited108{
			run:      runStoreTestFor108(t, dir, policyTest108),
			recorded: recordedSHAFor108(t, repoDir, "0005_checks.sql"),
			computed: sha256Of108(t, path),
		}
	})
	return editedResult108
}

func failedFor108(t *testing.T, res result, test string) {
	t.Helper()
	if res.code == 0 {
		t.Fatalf("go test -run %s exited 0, want non-zero\n%s", test, res.stdout)
	}
	if !strings.Contains(res.stdout, "--- FAIL: "+test) {
		t.Fatalf("the output carries no %q line\n%s", "--- FAIL: "+test, res.stdout)
	}
}

// lineNaming108 returns the first line of out that names every one of parts.
func lineNaming108(out string, parts ...string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		all := true
		for _, p := range parts {
			if !strings.Contains(line, p) {
				all = false
				break
			}
		}
		if all {
			return line, true
		}
	}
	return "", false
}

func TestS1TheCommittedMigrationsSatisfyThePolicy(t *testing.T) {
	res := runStoreTestFor108(t, repoDir, policyTest108)
	if res.code != 0 {
		t.Fatalf("go test -run %s exited %d, want 0\n%s", policyTest108, res.code, res.stdout)
	}
	if !strings.Contains(res.stdout, "--- PASS: "+policyTest108) {
		t.Fatalf("the output carries no %q line, so the check does not exist\n%s",
			"--- PASS: "+policyTest108, res.stdout)
	}
}

func TestS2EditingAnExistingMigrationFails(t *testing.T) {
	e := editedCopy108(t)
	failedFor108(t, e.run, policyTest108)
	if !strings.Contains(e.run.stdout, "0005_checks.sql") {
		t.Errorf("the output does not name 0005_checks.sql\n%s", e.run.stdout)
	}
	if !strings.Contains(e.run.stdout, e.recorded) {
		t.Errorf("the output does not carry the recorded sha256 %s\n%s", e.recorded, e.run.stdout)
	}
	if !strings.Contains(e.run.stdout, e.computed) {
		t.Errorf("the output does not carry the computed sha256 %s\n%s", e.computed, e.run.stdout)
	}
}

func TestS3InsertingAMigrationBetweenTwoExistingOnesFails(t *testing.T) {
	dir := copyRepoFor108(t)
	inserted := filepath.Join(dir, migrationsDir108, "0005a_inserted.sql")
	if err := os.WriteFile(inserted,
		[]byte("CREATE TABLE _issue108_inserted (id INTEGER PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatalf("inserting 0005a_inserted.sql: %v", err)
	}

	res := runStoreTestFor108(t, dir, policyTest108)
	failedFor108(t, res, policyTest108)
	if _, ok := lineNaming108(res.stdout, "0005a_inserted.sql", "sorts before"); !ok {
		t.Errorf("no line names 0005a_inserted.sql and says it sorts before a recorded migration\n%s",
			res.stdout)
	}
}

func TestS4RenamingAnExistingMigrationFails(t *testing.T) {
	dir := copyRepoFor108(t)
	from := filepath.Join(dir, migrationsDir108, "0011_usage.sql")
	to := filepath.Join(dir, migrationsDir108, "0011a_usage.sql")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("renaming 0011_usage.sql: %v", err)
	}

	res := runStoreTestFor108(t, dir, policyTest108)
	failedFor108(t, res, policyTest108)
	if _, ok := lineNaming108(res.stdout, "0011_usage.sql", "no longer present"); !ok {
		t.Errorf("no line names 0011_usage.sql and says it is no longer present\n%s", res.stdout)
	}
}

func TestS5DeletingAnExistingMigrationFails(t *testing.T) {
	dir := copyRepoFor108(t)
	if err := os.Remove(filepath.Join(dir, migrationsDir108, "0009_verifier.sql")); err != nil {
		t.Fatalf("deleting 0009_verifier.sql: %v", err)
	}

	res := runStoreTestFor108(t, dir, policyTest108)
	failedFor108(t, res, policyTest108)
	if _, ok := lineNaming108(res.stdout, "0009_verifier.sql", "no longer present"); !ok {
		t.Errorf("no line names 0009_verifier.sql and says it is no longer present\n%s", res.stdout)
	}
}

func TestS6AppendingAMigrationAndUpdatingTheManifestPasses(t *testing.T) {
	dir := copyRepoFor108(t)
	body := []byte("CREATE TABLE _issue108_probe (id INTEGER PRIMARY KEY);\n")
	probe := filepath.Join(dir, migrationsDir108, "0014_probe.sql")
	if err := os.WriteFile(probe, body, 0o644); err != nil {
		t.Fatalf("appending 0014_probe.sql: %v", err)
	}
	manifest := filepath.Join(dir, manifestPath108)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	line := fmt.Sprintf("%s  %s\n", sha256Of108(t, probe), "0014_probe.sql")
	if err := os.WriteFile(manifest, append(data, []byte(line)...), 0o644); err != nil {
		t.Fatalf("updating the manifest: %v", err)
	}

	res := runStoreTestFor108(t, dir, policyTest108)
	if res.code != 0 {
		t.Fatalf("go test -run %s exited %d, want 0\n%s", policyTest108, res.code, res.stdout)
	}
	if !strings.Contains(res.stdout, "--- PASS: "+policyTest108) {
		t.Fatalf("the output carries no %q line\n%s", "--- PASS: "+policyTest108, res.stdout)
	}
}

func TestS7TheFailureNamesThePolicyAndTheFix(t *testing.T) {
	e := editedCopy108(t)
	for _, want := range []string{
		"Migrations are append-only",
		"add a new migration at the end and append its line to internal/store/migrations.sha256",
		"never edit the manifest to match",
	} {
		if !strings.Contains(e.run.stdout, want) {
			t.Errorf("the failure does not say %q\n%s", want, e.run.stdout)
		}
	}
}

func TestS8ADatabaseIsBroughtToANamedMigrationNotToAFileCount(t *testing.T) {
	dir := copyRepoFor108(t)
	from := filepath.Join(dir, migrationsDir108, "0013_run_system_prompt.sql")
	to := filepath.Join(dir, migrationsDir108, "0013a_run_system_prompt.sql")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("renaming 0013_run_system_prompt.sql: %v", err)
	}

	res := runStoreTestFor108(t, dir, migrateBeforeTest108)
	failedFor108(t, res, migrateBeforeTest108)
	if _, ok := lineNaming108(res.stdout, "0013_run_system_prompt.sql", "cannot stop before"); !ok {
		t.Errorf("no line names 0013_run_system_prompt.sql as a migration it cannot stop before\n%s",
			res.stdout)
	}
}

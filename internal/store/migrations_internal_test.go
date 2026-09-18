package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// manifestFile records the sha256 of every migration that has shipped. It is
// read relative to the package directory, which is a Go test's working
// directory.
const manifestFile = "migrations.sha256"

// appendOnlyPolicy is what a developer who broke the policy needs to read: why
// the rule exists and which way out is the correct one. It is the last thing
// the failure prints.
const appendOnlyPolicy = `Migrations are append-only: a migration is added at the end and never inserted
between others, renamed, deleted or edited. internal/store.migrate tracks
progress as the index into the sorted file list (PRAGMA user_version), so a
database that has already applied a migration never sees a later change to it,
and a file inserted mid-sequence shifts every index after it.

The fix is to add a new migration at the end and append its line to internal/store/migrations.sha256.
Do that even for a one-character correction, and never edit the manifest to match a
change made to a migration that has already shipped: that only hides the divergence
from the databases that already ran it.`

// manifestEntry is one line of the manifest: a migration that has shipped and
// the sha256 it shipped with.
type manifestEntry struct {
	name string
	sum  string
}

// manifestLine matches "<64 hex sha256><spaces><file name>", the format
// shasum -a 256 prints, so a line can be pasted straight in.
var manifestLine = regexp.MustCompile(`^([0-9a-f]{64})\s+(\S+)$`)

// readManifest parses migrations.sha256, skipping blank lines and comments.
func readManifest(t *testing.T) []manifestEntry {
	t.Helper()
	data, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatalf("reading the migration manifest: %v\n\n%s", err, appendOnlyPolicy)
	}
	var entries []manifestEntry
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := manifestLine.FindStringSubmatch(trimmed)
		if m == nil {
			t.Fatalf("%s:%d: %q is not \"<sha256>  <file name>\"\n\n%s",
				manifestFile, i+1, trimmed, appendOnlyPolicy)
		}
		entries = append(entries, manifestEntry{name: m[2], sum: m[1]})
	}
	if len(entries) == 0 {
		t.Fatalf("%s records no migrations\n\n%s", manifestFile, appendOnlyPolicy)
	}
	return entries
}

// migrationNames is the sorted list of embedded migrations, exactly as migrate
// builds it.
func migrationNames(t *testing.T) []string {
	t.Helper()
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("reading the embedded migrations: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TestMigrationsAreAppendOnly holds the policy migrate's correctness rests on.
// It fails when a migration that has already shipped was edited, renamed,
// deleted or pushed down the list by an insertion.
func TestMigrationsAreAppendOnly(t *testing.T) {
	recorded := readManifest(t)
	if !sort.SliceIsSorted(recorded, func(i, j int) bool { return recorded[i].name < recorded[j].name }) {
		t.Fatalf("%s is not in sorted order, so it does not describe the order migrate applies\n\n%s",
			manifestFile, appendOnlyPolicy)
	}
	names := migrationNames(t)
	index := make(map[string]int, len(names))
	for i, n := range names {
		index[n] = i
	}

	var problems []string
	for i, e := range recorded {
		at, ok := index[e.name]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s is recorded in %s but is no longer present in migrations/: a migration that has shipped is never renamed or deleted",
				e.name, manifestFile))
			continue
		}
		data, err := migrationsFS.ReadFile("migrations/" + e.name)
		if err != nil {
			t.Fatalf("reading migrations/%s: %v", e.name, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != e.sum {
			problems = append(problems, fmt.Sprintf(
				"%s has been edited: %s records sha256 %s, its contents are sha256 %s",
				e.name, manifestFile, e.sum, got))
		}
		if at != i {
			problems = append(problems, fmt.Sprintf(
				"%s is recorded at index %d but is now at index %d: something was inserted ahead of it, and every database at an older user_version would skip it",
				e.name, i, at))
		}
	}

	last := recorded[len(recorded)-1].name
	known := make(map[string]bool, len(recorded))
	for _, e := range recorded {
		known[e.name] = true
	}
	for _, n := range names {
		if known[n] {
			continue
		}
		if n <= last {
			problems = append(problems, fmt.Sprintf(
				"%s is not in %s and sorts before %s, which is: a migration may only be added after every one already recorded",
				n, manifestFile, last))
		}
	}

	if len(problems) > 0 {
		t.Fatalf("the append-only migration policy is broken:\n  %s\n\n%s",
			strings.Join(problems, "\n  "), appendOnlyPolicy)
	}
}

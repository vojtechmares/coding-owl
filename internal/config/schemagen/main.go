// Command schemagen writes the JSON schemas of Owl's two configuration file
// kinds into the website, where the site deploy publishes them (issue #129).
//
// It is run by `make generate`, beside `buf generate`, and CI runs it again
// and fails on a difference, so a struct that changes without the schemas
// being regenerated is caught in the pull request rather than by a user whose
// editor is quietly wrong.
//
// It writes into the repository it is run from, so it is run from the
// repository root.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vojtechmares/coding-owl/internal/config"
)

// schemaDir is where the schemas are written, relative to the repository
// root. The website copies `website/public` verbatim, so this is also the
// path they are served under.
const schemaDir = "website/public/schema"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

func run() error {
	type target struct {
		name  string
		build func() ([]byte, error)
	}
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", schemaDir, err)
	}
	for _, t := range []target{
		{"project.json", config.ProjectSchema},
		{"daemon.json", config.DaemonSchema},
	} {
		data, err := t.build()
		if err != nil {
			return err
		}
		path := filepath.Join(schemaDir, t.name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Println("wrote", path)
	}
	return nil
}

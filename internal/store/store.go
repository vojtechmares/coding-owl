// Package store is the daemon's SQLite database: Projects now, Jobs and Runs
// later (ADR-0008). Only the daemon opens it; the CLI and the desktop app
// reach it through the API.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration reports what Open did to the schema.
type Migration struct {
	// Schema is the schema version the database is at afterwards.
	Schema int
	// Applied is how many migrations this Open ran.
	Applied int
}

// Store is an open database.
type Store struct {
	db *sql.DB
}

// Open opens the database at path, creating the file and its directory if
// needed, and brings the schema up to date. Running it against a database that
// is already current applies nothing.
func Open(path string) (*Store, Migration, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, Migration{}, fmt.Errorf("creating %s: %w", dir, err)
	}
	// SQLite creates owl.db-wal and owl.db-shm alongside the database at the
	// process umask, and the write-ahead log holds committed rows. MkdirAll
	// does not tighten a directory that already exists, so the mode is set
	// rather than assumed.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, Migration{}, fmt.Errorf("restricting %s: %w", dir, err)
	}
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, Migration{}, fmt.Errorf("opening %s: %w", path, err)
	}
	// The daemon is the only writer and SQLite takes one writer at a time;
	// a single connection keeps that true without lock contention.
	db.SetMaxOpenConns(1)
	m, err := migrate(db)
	if err != nil {
		_ = db.Close()
		return nil, Migration{}, fmt.Errorf("migrating %s: %w", path, err)
	}
	// The database holds only this user's Projects; SQLite creates it with the
	// process umask, so the mode is set rather than inherited.
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, Migration{}, fmt.Errorf("restricting %s: %w", path, err)
	}
	return &Store{db: db}, m, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// migrate applies every embedded migration the database has not seen, in file
// name order, tracking progress in SQLite's own user_version.
func migrate(db *sql.DB) (Migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return Migration{}, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return Migration{}, err
	}
	m := Migration{Schema: version}
	for i, name := range names {
		if i < version {
			continue
		}
		sqlText, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return m, err
		}
		tx, err := db.Begin()
		if err != nil {
			return m, err
		}
		if _, err := tx.Exec(string(sqlText)); err != nil {
			_ = tx.Rollback()
			return m, fmt.Errorf("%s: %w", name, err)
		}
		// user_version takes no placeholder, and i+1 is not user input.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()
			return m, fmt.Errorf("%s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return m, fmt.Errorf("%s: %w", name, err)
		}
		m.Schema = i + 1
		m.Applied++
	}
	return m, nil
}

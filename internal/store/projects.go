package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrNotFound is returned when no Project carries the name asked for.
var ErrNotFound = errors.New("no such project")

// ErrNameTaken is returned when a name is already in use. A Project is
// identified by its name (ADR-0031), so this is a collision of identity.
var ErrNameTaken = errors.New("project name already in use")

// Project is a registered git repository.
type Project struct {
	// Name identifies the Project and names its configuration directory.
	Name string
	// Path is where the repository sits now; it may change (ADR-0031).
	Path string
	// BaseBranch is the branch Jobs branch from and configuration is read
	// from (ADR-0014).
	BaseBranch string
	// Registered is when the Project was first added.
	Registered time.Time
}

// timeFormat keeps timestamps sortable as text.
const timeFormat = time.RFC3339Nano

// AddProject registers p. It returns ErrNameTaken when the name is in use.
func (s *Store) AddProject(ctx context.Context, p Project) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (name, path, base_branch, registered) VALUES (?, ?, ?, ?)`,
		p.Name, p.Path, p.BaseBranch, p.Registered.UTC().Format(timeFormat))
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: %s", ErrNameTaken, p.Name)
	}
	return err
}

// GetProject returns the Project of that name, or ErrNotFound.
func (s *Store) GetProject(ctx context.Context, name string) (Project, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT name, path, base_branch, registered FROM projects WHERE name = ?`, name)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return p, err
}

type scanner interface{ Scan(dest ...any) error }

func scanProject(sc scanner) (Project, error) {
	var p Project
	var registered string
	if err := sc.Scan(&p.Name, &p.Path, &p.BaseBranch, &registered); err != nil {
		return Project{}, err
	}
	t, err := time.Parse(timeFormat, registered)
	if err != nil {
		return Project{}, fmt.Errorf("project %s has an unreadable registered time %q: %w", p.Name, registered, err)
	}
	p.Registered = t
	return p, nil
}

// isUniqueViolation reports whether err is SQLite refusing a duplicate key.
func isUniqueViolation(err error) bool {
	var serr *sqlite.Error
	if !errors.As(err, &serr) {
		return false
	}
	return serr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
		serr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

// ListProjects returns every Project, ordered by name.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, path, base_branch, registered FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetProjectPath points an existing Project at a new path, leaving its name
// and registration untouched.
func (s *Store) SetProjectPath(ctx context.Context, name, path string) error {
	return s.affectOne(ctx, name, `UPDATE projects SET path = ? WHERE name = ?`, path, name)
}

// RenameProject changes a Project's name. It returns ErrNameTaken when the new
// name is in use and ErrNotFound when the old one is not.
func (s *Store) RenameProject(ctx context.Context, from, to string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET name = ? WHERE name = ?`, to, from)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: %s", ErrNameTaken, to)
	}
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, from)
	}
	return nil
}

// RemoveProject deletes a Project, or returns ErrNotFound. The Jobs queued
// against it go with it - a Job whose Project is gone has nowhere to run - and
// the queue is renumbered so the Jobs left in it still count from one. How
// many Jobs went is returned - every one of them, queued or not - because
// losing work silently is worse than the removal itself.
func (s *Store) RemoveProject(ctx context.Context, name string) (int, error) {
	var jobs int
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM jobs WHERE project = ?`, name).Scan(&jobs); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE name = ?`, name)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", ErrNotFound, name)
		}
		return renumber(ctx, tx)
	})
	if err != nil {
		return 0, err
	}
	return jobs, nil
}

// affectOne runs a statement that must touch exactly one Project and turns
// touching none into ErrNotFound.
func (s *Store) affectOne(ctx context.Context, name, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return nil
}

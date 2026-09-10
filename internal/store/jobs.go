package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
)

// ErrJobNotFound is returned when no Job carries the id asked for.
var ErrJobNotFound = errors.New("no such job")

// ErrNotQueued is returned when a Job the caller wanted to move or take out
// of the queue is not in it.
var ErrNotQueued = errors.New("job is not in the queue")

// Job is one standing intent to do a piece of work in one Project
// (ADR-0027).
type Job struct {
	// ID identifies the Job and is what the owl queue commands take.
	ID int64
	// Source names the producer the Job came from (ADR-0008).
	Source string
	// SourceRef is the Job's reference within that Source; the two are unique
	// together (ADR-0032).
	SourceRef string
	// Project is the name of the Project the Job is queued against.
	Project string
	// Prompt is the work to do.
	Prompt string
	// State is where the Job is in its lifecycle.
	State string
	// Position is the Job's place in the queue, counting from one, and zero
	// for a Job that is not in the queue.
	Position int
	// Created is when the Job was first produced.
	Created time.Time
}

// jobColumns is the select list every Job read shares, in scanJob's order.
const jobColumns = `id, source, source_ref, project, prompt, state, position, created`

// UpsertJob produces j. A Job with that source and reference is not made
// twice: the second production rewrites the prompt of the Job already in the
// queue, and leaves a Job that has left the queue exactly as it is (ADR-0032).
// The Job as it stands afterwards is returned.
func (s *Store) UpsertJob(ctx context.Context, j Job) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// A queued Job takes the position after the last one. MAX ignores the
	// NULLs of the Jobs that have left the queue, so their positions are not
	// held against the ones still in it.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO jobs (source, source_ref, project, prompt, state, position, created)
		 VALUES (?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM jobs), ?)
		 ON CONFLICT (source, source_ref) DO UPDATE SET prompt = excluded.prompt
		   WHERE jobs.position IS NOT NULL`,
		j.Source, j.SourceRef, j.Project, j.Prompt, j.State,
		j.Created.UTC().Format(timeFormat)); err != nil {
		return Job{}, err
	}
	row := tx.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE source = ? AND source_ref = ?`,
		j.Source, j.SourceRef)
	out, err := scanJob(row)
	if err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return out, nil
}

// GetJob returns the Job of that id, or ErrJobNotFound.
func (s *Store) GetJob(ctx context.Context, id int64) (Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, fmt.Errorf("%w: %d", ErrJobNotFound, id)
	}
	return j, err
}

// ListJobs returns the queue - the Jobs that have a position - in order.
// With all, every other Job follows it, oldest first.
func (s *Store) ListJobs(ctx context.Context, all bool) ([]Job, error) {
	query := `SELECT ` + jobColumns + ` FROM jobs WHERE position IS NOT NULL ORDER BY position, id`
	if all {
		query = `SELECT ` + jobColumns + ` FROM jobs ORDER BY position IS NULL, position, id`
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func scanJob(sc scanner) (Job, error) {
	var j Job
	var position sql.NullInt64
	var created string
	if err := sc.Scan(&j.ID, &j.Source, &j.SourceRef, &j.Project, &j.Prompt, &j.State, &position, &created); err != nil {
		return Job{}, err
	}
	j.Position = int(position.Int64)
	t, err := time.Parse(timeFormat, created)
	if err != nil {
		return Job{}, fmt.Errorf("job %d has an unreadable created time %q: %w", j.ID, created, err)
	}
	j.Created = t
	return j, nil
}

// DequeueJob takes a Job out of the queue, giving it state and no position,
// and closes the gap behind it so the queue keeps counting from one. It
// returns ErrNotQueued for a Job that has already left.
func (s *Store) DequeueJob(ctx context.Context, id int64, state string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE jobs SET state = ?, position = NULL WHERE id = ? AND position IS NOT NULL`,
			state, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return notQueued(ctx, tx, id)
		}
		return renumber(ctx, tx)
	})
}

// MoveJob puts a queued Job at position, counting from one, shifting the Jobs
// it passes. A position past the end of the queue puts it last.
func (s *Store) MoveJob(ctx context.Context, id int64, position int) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		order, err := queuedIDs(ctx, tx)
		if err != nil {
			return err
		}
		at := slices.Index(order, id)
		if at < 0 {
			return notQueued(ctx, tx, id)
		}
		order = slices.Delete(order, at, at+1)
		to := min(max(position-1, 0), len(order))
		order = slices.Insert(order, to, id)
		return writePositions(ctx, tx, order)
	})
}

// CountQueued returns how many Jobs are waiting in the queue.
func (s *Store) CountQueued(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE position IS NOT NULL`).Scan(&n)
	return n, err
}

// inTx runs fn in a transaction, rolling back unless it returns nil.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// notQueued says why a Job could not be moved: it is not in the queue, or
// there is no such Job at all.
func notQueued(ctx context.Context, tx *sql.Tx, id int64) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE id = ?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("%w: %d", ErrJobNotFound, id)
	}
	return fmt.Errorf("%w: %d", ErrNotQueued, id)
}

// renumber rewrites the queue's positions so they run from one upwards in the
// order they already stand in.
func renumber(ctx context.Context, tx *sql.Tx) error {
	order, err := queuedIDs(ctx, tx)
	if err != nil {
		return err
	}
	return writePositions(ctx, tx, order)
}

// queuedIDs returns the ids in the queue, in queue order.
func queuedIDs(ctx context.Context, tx *sql.Tx) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM jobs WHERE position IS NOT NULL ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// writePositions numbers ids from one in the order given.
func writePositions(ctx context.Context, tx *sql.Tx, ids []int64) error {
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return nil
}

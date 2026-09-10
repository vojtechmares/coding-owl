package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrRunNotFound is returned when no Run carries the id asked for.
var ErrRunNotFound = errors.New("no such run")

// Run is one attempt to carry out a Job (ADR-0027).
type Run struct {
	// ID identifies the Run and is what owl logs takes.
	ID int64
	// JobID is the Job this Run is an attempt at.
	JobID int64
	// Attempt counts this Job's Runs, from one.
	Attempt int
	// Started is when the Agent was launched.
	Started time.Time
	// Ended is when it exited, and is zero while the Run is still going.
	Ended time.Time
	// Outcome is succeeded, failed or interrupted, and is empty while the Run
	// is still going.
	Outcome string
	// Error is why a Run did not succeed, in the words the user reads.
	Error string
	// LogPath is the file the Run's structured output was captured to.
	LogPath string
}

// runColumns is the select list every Run read shares, in scanRun's order.
const runColumns = `id, job_id, attempt, started, ended, outcome, error, log_path`

// StartRun records the beginning of an attempt at a Job, numbering it after
// the attempts already made.
func (s *Store) StartRun(ctx context.Context, r Run) (Run, error) {
	var out Run
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx,
			`INSERT INTO runs (job_id, attempt, started, log_path)
			 VALUES (?, (SELECT COUNT(*) + 1 FROM runs WHERE job_id = ?), ?, ?)
			 RETURNING `+runColumns,
			r.JobID, r.JobID, r.Started.UTC().Format(timeFormat), r.LogPath)
		var err error
		out, err = scanRun(row)
		return err
	})
	return out, err
}

// FinishRun records how an attempt ended.
func (s *Store) FinishRun(ctx context.Context, id int64, ended time.Time, outcome, reason string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET ended = ?, outcome = ?, error = ? WHERE id = ?`,
		ended.UTC().Format(timeFormat), outcome, reason, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrRunNotFound, id)
	}
	return nil
}

// SetRunLog records where a Run's output is being captured, which is only
// known once the Run has an id to name the file after.
func (s *Store) SetRunLog(ctx context.Context, id int64, path string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET log_path = ? WHERE id = ?`, path, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrRunNotFound, id)
	}
	return nil
}

// GetRun returns the Run of that id, or ErrRunNotFound.
func (s *Store) GetRun(ctx context.Context, id int64) (Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, id)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: %d", ErrRunNotFound, id)
	}
	return r, err
}

// ListRuns returns a Job's attempts, oldest first.
func (s *Store) ListRuns(ctx context.Context, jobID int64) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runColumns+` FROM runs WHERE job_id = ? ORDER BY attempt, id`, jobID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunInProgress returns the Run that has not ended yet, if there is one. Only
// one Agent runs at a time in this milestone (ADR-0029).
func (s *Store) RunInProgress(ctx context.Context) (Run, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM runs WHERE outcome = '' ORDER BY id LIMIT 1`)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, err
	}
	return r, true, nil
}

func scanRun(sc scanner) (Run, error) {
	var r Run
	var started, ended string
	if err := sc.Scan(&r.ID, &r.JobID, &r.Attempt, &started, &ended, &r.Outcome, &r.Error, &r.LogPath); err != nil {
		return Run{}, err
	}
	t, err := time.Parse(timeFormat, started)
	if err != nil {
		return Run{}, fmt.Errorf("run %d has an unreadable start time %q: %w", r.ID, started, err)
	}
	r.Started = t
	if ended != "" {
		t, err := time.Parse(timeFormat, ended)
		if err != nil {
			return Run{}, fmt.Errorf("run %d has an unreadable end time %q: %w", r.ID, ended, err)
		}
		r.Ended = t
	}
	return r, nil
}

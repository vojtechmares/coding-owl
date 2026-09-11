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
	// ExitCode is what the Agent exited with, and NoExitCode when it never got
	// far enough to have one.
	ExitCode int
	// LogPath is the file the Run's structured output was captured to.
	LogPath string
	// Phase is what the Run was carrying out: planning the Job, or executing
	// it (ADR-0026).
	Phase string
}

// NoExitCode is the exit status of a Run whose Agent never exited: one that
// could not be started, or one that was stopped.
const NoExitCode = -1

// runColumns is the select list every Run read shares, in scanRun's order.
const runColumns = `id, job_id, attempt, started, ended, outcome, error, exit_code, log_path, phase`

// StartRun records the beginning of an attempt at a Job, numbering it after
// the attempts already made.
func (s *Store) StartRun(ctx context.Context, r Run) (Run, error) {
	var out Run
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx,
			`INSERT INTO runs (job_id, attempt, started, log_path, exit_code, phase)
			 VALUES (?, (SELECT COUNT(*) + 1 FROM runs WHERE job_id = ?), ?, ?, ?, ?)
			 RETURNING `+runColumns,
			r.JobID, r.JobID, r.Started.UTC().Format(timeFormat), r.LogPath, NoExitCode, r.Phase)
		var err error
		out, err = scanRun(row)
		return err
	})
	return out, err
}

// FinishRun records how an attempt ended, and what the Agent exited with. It
// spends one of the Job's attempts, which is what a Run costs whatever became
// of it (ADR-0025). A Run only ends once, so an attempt is only spent once.
func (s *Store) FinishRun(ctx context.Context, id int64, ended time.Time, outcome, reason string, exitCode int) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var jobID int64
		err := tx.QueryRowContext(ctx,
			`UPDATE runs SET ended = ?, outcome = ?, error = ?, exit_code = ?
			 WHERE id = ? AND outcome = '' RETURNING job_id`,
			ended.UTC().Format(timeFormat), outcome, reason, exitCode, id).Scan(&jobID)
		if errors.Is(err, sql.ErrNoRows) {
			return alreadyEnded(ctx, tx, id)
		}
		if err != nil {
			return err
		}
		return spendAttempt(ctx, tx, jobID)
	})
}

// alreadyEnded says why a Run could not be ended: either there is no such Run,
// or it has ended already and its attempt is already spent.
func alreadyEnded(ctx context.Context, tx *sql.Tx, id int64) error {
	var outcome string
	err := tx.QueryRowContext(ctx, `SELECT outcome FROM runs WHERE id = ?`, id).Scan(&outcome)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %d", ErrRunNotFound, id)
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("run %d has already ended as %s", id, outcome)
}

// spendAttempt takes one of a Job's attempts, what one Run costs (ADR-0025).
// A Job with none left has nothing to spend: the count stops at zero rather
// than going negative, so that what owl reports is always a number of Runs the
// Job could still take.
func spendAttempt(ctx context.Context, tx *sql.Tx, jobID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE jobs SET ttl = ttl - 1 WHERE id = ? AND ttl > 0`, jobID)
	return err
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

// InterruptRunsInProgress ends the Runs that were still going when the daemon
// last stopped, and returns how many there were. Every Agent is a child of the
// daemon (ADR-0012), so a Run still open at startup is one nothing survived to
// record.
func (s *Store) InterruptRunsInProgress(ctx context.Context, at time.Time, outcome, reason string) (int, error) {
	var n int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		// The attempts go first, while the Runs that cost them are still the
		// ones in progress. An interrupted Run costs an attempt exactly as a
		// finished one does (ADR-0025).
		if _, err := tx.ExecContext(ctx,
			`UPDATE jobs SET ttl = ttl - 1
			 WHERE ttl > 0 AND id IN (SELECT job_id FROM runs WHERE outcome = '')`); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE runs SET ended = ?, outcome = ?, error = ? WHERE outcome = ''`,
			at.UTC().Format(timeFormat), outcome, reason)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return int(n), err
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

// ListRunsInProgress returns every Run that has not ended, oldest first.
func (s *Store) ListRunsInProgress(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runColumns+` FROM runs WHERE outcome = '' ORDER BY id`)
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

func scanRun(sc scanner) (Run, error) {
	var r Run
	var started, ended string
	if err := sc.Scan(&r.ID, &r.JobID, &r.Attempt, &started, &ended, &r.Outcome, &r.Error,
		&r.ExitCode, &r.LogPath, &r.Phase); err != nil {
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

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
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
	// Branch is the branch the Job's work lands on, empty until it has one.
	Branch string
	// Worktree is where that branch is checked out, empty until it has one.
	Worktree string
	// Planned is whether the Job is planned before it is executed (ADR-0026).
	Planned bool
	// Plan is what its planning Run decided, empty until there is one.
	Plan string
	// Model and Effort are the Job's own overrides, empty when it has none
	// and the Project or the defaults decide (ADR-0028).
	Model  string
	Effort string
	// Reason is why the Job is where it is when no Run explains it: a setup
	// command that failed before an Agent started, or the checks that refused
	// the work.
	Reason string
	// TTL is how many Runs the Job may still take. One is spent at the end of
	// every Run whatever its outcome, and a Job with none left is exhausted
	// rather than pending (ADR-0025).
	TTL int
	// Account is the Account the Job ran on, taken from its Project's
	// configuration at its first Run and unchanged afterwards (ADR-0023). It
	// is empty until the Job has run.
	Account string
	// Labels are the names the Job carries, in order and each one once. They
	// live in a table of their own, so they are read alongside the Job rather
	// than scanned out of its row.
	Labels []string
	// Position is the Job's place in the queue, counting from one, and zero
	// for a Job that is not in the queue.
	Position int
	// Created is when the Job was first produced.
	Created time.Time
}

// jobColumns is the select list every Job read shares, in scanJob's order.
const jobColumns = `id, source, source_ref, project, prompt, state, branch, worktree,
	planned, plan, model, effort, reason, ttl, account, position, created`

// querier is what reading labels needs of a database or a transaction, so
// that one read serves a plain query and one taken inside a write.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// attachLabels fills in the Labels of every Job in jobs. It is one query for
// the whole page rather than one per Job, so that listing the queue costs the
// same two queries however long it is.
func attachLabels(ctx context.Context, q querier, jobs []Job) error {
	if len(jobs) == 0 {
		return nil
	}
	at := make(map[int64]int, len(jobs))
	args := make([]any, 0, len(jobs))
	for i := range jobs {
		jobs[i].Labels = nil
		at[jobs[i].ID] = i
		args = append(args, jobs[i].ID)
	}
	rows, err := q.QueryContext(ctx,
		`SELECT job_id, label FROM job_labels WHERE job_id IN (`+placeholders(len(args))+`) ORDER BY label`,
		args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var label string
		if err := rows.Scan(&id, &label); err != nil {
			return err
		}
		if i, ok := at[id]; ok {
			jobs[i].Labels = append(jobs[i].Labels, label)
		}
	}
	return rows.Err()
}

// placeholders renders n bind markers for an IN clause. The values are always
// numbers this package produced, but they go on the wire as parameters all
// the same: no query in here is built by pasting a value into it.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// AddJobLabels gives a Job labels it does not already carry, and leaves the
// ones it does exactly as they are: carrying a label is a fact about the Job,
// not a count of how often it was asked for.
func (s *Store) AddJobLabels(ctx context.Context, id int64, labels []string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		return insertLabels(ctx, tx, id, labels)
	})
}

// RemoveJobLabels takes labels off a Job, and returns the ones it did not
// carry. It removes nothing at all unless it carries every one of them: a
// caller that refuses the request must not find the others already gone. The
// reading and the removal are one transaction, so what is removed is what was
// looked at.
func (s *Store) RemoveJobLabels(ctx context.Context, id int64, labels []string) ([]string, error) {
	var missing []string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		jobs := []Job{{ID: id}}
		if err := attachLabels(ctx, tx, jobs); err != nil {
			return err
		}
		missing = nil
		for _, l := range labels {
			if !slices.Contains(jobs[0].Labels, l) {
				missing = append(missing, l)
			}
		}
		if len(missing) > 0 || len(labels) == 0 {
			return nil
		}
		args := make([]any, 0, len(labels)+1)
		args = append(args, id)
		for _, l := range labels {
			args = append(args, l)
		}
		_, err := tx.ExecContext(ctx,
			`DELETE FROM job_labels WHERE job_id = ? AND label IN (`+placeholders(len(labels))+`)`, args...)
		return err
	})
	return missing, err
}

// insertLabels writes a Job's labels inside a transaction, ignoring the ones
// it already carries.
func insertLabels(ctx context.Context, tx *sql.Tx, id int64, labels []string) error {
	for _, label := range labels {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO job_labels (job_id, label) VALUES (?, ?)`, id, label); err != nil {
			return err
		}
	}
	return nil
}

// UpsertJob produces j. A Job with that source and reference is not made
// twice: the second production rewrites the prompt of the Job already in the
// queue, and leaves a Job that has left the queue exactly as it is (ADR-0032).
// Labels the production carries follow the same rule as the prompt: they are
// added to a Job still in the queue, on top of whatever owl jobs label add
// may have put there since, and a Job that has left it is not touched. The
// Job as it stands afterwards is returned.
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
		`INSERT INTO jobs (source, source_ref, project, prompt, state, planned, model, effort, ttl, position, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM jobs), ?)
		 ON CONFLICT (source, source_ref) DO UPDATE SET prompt = excluded.prompt
		   WHERE jobs.position IS NOT NULL`,
		j.Source, j.SourceRef, j.Project, j.Prompt, j.State, boolToInt(j.Planned), j.Model, j.Effort,
		j.TTL, j.Created.UTC().Format(timeFormat)); err != nil {
		return Job{}, err
	}
	row := tx.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE source = ? AND source_ref = ?`,
		j.Source, j.SourceRef)
	out, err := scanJob(row)
	if err != nil {
		return Job{}, err
	}
	// Only while the Job is still in the queue, which is the condition the
	// upsert above puts on the prompt.
	if out.Position > 0 {
		if err := insertLabels(ctx, tx, out.ID, j.Labels); err != nil {
			return Job{}, err
		}
	}
	labelled := []Job{out}
	if err := attachLabels(ctx, tx, labelled); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return labelled[0], nil
}

// GetJob returns the Job of that id, or ErrJobNotFound.
func (s *Store) GetJob(ctx context.Context, id int64) (Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, fmt.Errorf("%w: %d", ErrJobNotFound, id)
	}
	if err != nil {
		return Job{}, err
	}
	jobs := []Job{j}
	if err := attachLabels(ctx, s.db, jobs); err != nil {
		return Job{}, err
	}
	return jobs[0], nil
}

// ListQueue returns the Jobs waiting in that state, in queue order. A Job
// being run keeps its position (ADR-0025) but is no longer waiting, so the
// state is what decides membership of the queue rather than the position.
// Jobs carrying every one of labels, when any are given: repeating a label
// narrows the answer rather than widening it.
func (s *Store) ListQueue(ctx context.Context, state string, labels ...string) ([]Job, error) {
	where, args := carryingAll(labels)
	return s.listJobs(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE position IS NOT NULL AND state = ?`+where+
			` ORDER BY position, id`,
		append([]any{state}, args...)...)
}

// ListAllJobs returns every Job whatever its state, the queue first, and
// narrows to the Jobs carrying every one of labels when any are given.
func (s *Store) ListAllJobs(ctx context.Context, labels ...string) ([]Job, error) {
	where, args := carryingAll(labels)
	return s.listJobs(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE 1 = 1`+where+
			` ORDER BY position IS NULL, position, id`, args...)
}

// carryingAll is the clause that keeps only the Jobs carrying every one of
// labels, with the arguments it binds. No label means no clause, so an
// unfiltered listing pays nothing for the feature.
func carryingAll(labels []string) (string, []any) {
	if len(labels) == 0 {
		return "", nil
	}
	args := make([]any, 0, len(labels))
	for _, l := range labels {
		args = append(args, l)
	}
	// Counting the distinct labels matched is what makes this "all of them"
	// rather than "any of them", and what stops a label given twice from
	// counting twice.
	return ` AND (SELECT COUNT(DISTINCT label) FROM job_labels
	           WHERE job_id = jobs.id AND label IN (` + placeholders(len(labels)) + `)) = ` +
		strconv.Itoa(len(distinct(labels))), args
}

// distinct is labels without repeats, in the order they were given.
func distinct(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// SetJobState moves a Job to another state, leaving its place in the queue
// alone: a re-attempt never changes a Job's position (ADR-0025). A Job on its
// way somewhere carries no reason for having stopped.
func (s *Store) SetJobState(ctx context.Context, id int64, state string) error {
	return s.affectOneJob(ctx, id, `UPDATE jobs SET state = ?, reason = '' WHERE id = ?`, state, id)
}

// MoveJobState moves a Job from one state to another, and reports whether it
// was in the state to move from. It is what a caller uses when the state it
// read and the state it is acting on have to be the same one: a Job somebody
// else has already decided about is left exactly as they left it.
func (s *Store) MoveJobState(ctx context.Context, id int64, from, to string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET state = ?, reason = '' WHERE id = ? AND state = ?`, to, id, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetJobAccount records the Account a Job runs on. It is set at the Job's
// first Run and does not change afterwards, so what a Job drew on stays
// knowable for its whole life (ADR-0023).
func (s *Store) SetJobAccount(ctx context.Context, id int64, account string) error {
	return s.affectOneJob(ctx, id, `UPDATE jobs SET account = ? WHERE id = ?`, account, id)
}

// SetJobPlan records what a Job's planning Run decided, which is also what is
// committed as the handoff on its branch (ADR-0026).
func (s *Store) SetJobPlan(ctx context.Context, id int64, plan string) error {
	return s.affectOneJob(ctx, id, `UPDATE jobs SET plan = ? WHERE id = ?`, plan, id)
}

// boolToInt renders a flag for a STRICT table, which has no boolean type.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SetJobWorkspace records the branch a Job's work lands on and the worktree it
// is checked out in. Both belong to the Job, not to one Run (ADR-0007).
func (s *Store) SetJobWorkspace(ctx context.Context, id int64, branch, worktree string) error {
	return s.affectOneJob(ctx, id,
		`UPDATE jobs SET branch = ?, worktree = ? WHERE id = ?`, branch, worktree, id)
}

// affectOneJob runs a statement that must touch exactly one Job and turns
// touching none into ErrJobNotFound.
func (s *Store) affectOneJob(ctx context.Context, id int64, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrJobNotFound, id)
	}
	return nil
}

func (s *Store) listJobs(ctx context.Context, query string, args ...any) ([]Job, error) {
	jobs, err := scanJobs(s.db.QueryContext(ctx, query, args...))
	if err != nil {
		return nil, err
	}
	if err := attachLabels(ctx, s.db, jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// scanJobs reads a query's Jobs, taking the query's own error so that a caller
// can hand it straight through.
func scanJobs(rows *sql.Rows, err error) ([]Job, error) {
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
	var planned int
	var created string
	if err := sc.Scan(&j.ID, &j.Source, &j.SourceRef, &j.Project, &j.Prompt, &j.State,
		&j.Branch, &j.Worktree, &planned, &j.Plan, &j.Model, &j.Effort, &j.Reason,
		&j.TTL, &j.Account, &position, &created); err != nil {
		return Job{}, err
	}
	j.Position = int(position.Int64)
	j.Planned = planned != 0
	t, err := time.Parse(timeFormat, created)
	if err != nil {
		return Job{}, fmt.Errorf("job %d has an unreadable created time %q: %w", j.ID, created, err)
	}
	j.Created = t
	return j, nil
}

// DequeueJob takes a Job out of the queue, giving it state to and no position,
// and closes the gap behind it so the queue keeps counting from one. from is
// the state the caller saw it in, and the departure applies only while the Job
// is still in it: a Job somebody else has decided about since is left exactly
// as they left it. reason says why, for the states no Run explains; pass an
// empty one otherwise. It returns ErrNotQueued for a Job that has already
// left, or that is not in the state it was seen in.
func (s *Store) DequeueJob(ctx context.Context, id int64, from, to, reason string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE jobs SET state = ?, reason = ?, position = NULL
			 WHERE id = ? AND position IS NOT NULL AND state = ?`,
			to, reason, id, from)
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
// it passes. A position past the end of the queue puts it last. from is the
// state the caller saw the Job in, and the move applies only while it is
// still in it: a Job that has since been claimed for a Run keeps the place it
// held (ADR-0025) rather than being shuffled by a decision taken before the
// claim. It returns ErrNotQueued otherwise.
func (s *Store) MoveJob(ctx context.Context, id int64, position int, from string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var state string
		err := tx.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id = ? AND position IS NOT NULL`, id).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && state != from) {
			return notQueued(ctx, tx, id)
		}
		if err != nil {
			return err
		}
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

// ReturnJobToQueue puts a Job back in the queue at the place it kept while it
// ran (ADR-0025), or leaves it exhausted when it has no attempts left: a Job
// that has run out is not stuck on anything, it has simply had its Runs. The
// state it ends up in is returned. The choice and the write are one statement,
// so an extension arriving at the same moment cannot be overwritten by a
// decision taken before it.
func (s *Store) ReturnJobToQueue(ctx context.Context, id int64, pending, exhausted string) (string, error) {
	var state string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`UPDATE jobs SET state = CASE WHEN ttl > 0 THEN ? ELSE ? END, reason = ''
			 WHERE id = ? RETURNING state`, pending, exhausted, id).Scan(&state)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: %d", ErrJobNotFound, id)
	}
	return state, err
}

// maxAttempts caps what a Job can be extended to. The count is reported as a
// 32-bit number, so letting it grow past that would mean showing the user a
// number of attempts the Job does not have.
const maxAttempts = 1<<31 - 1

// ExtendJob gives a Job add more attempts, and returns a Job that had run out
// to the queue at the place it kept (ADR-0025). A Job in any other state keeps
// it: more attempts are not what a blocked Job is waiting for. The Job as it
// stands afterwards is returned. The two states are taken in the order
// ReturnJobToQueue takes them, so that no call site has to remember two
// orders.
func (s *Store) ExtendJob(ctx context.Context, id int64, add int, pending, exhausted string) (Job, error) {
	var j Job
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx,
			`UPDATE jobs SET ttl = MIN(ttl + ?, ?),
			   state = CASE WHEN state = ? THEN ? ELSE state END
			 WHERE id = ? RETURNING `+jobColumns,
			add, maxAttempts, exhausted, pending, id)
		var err error
		if j, err = scanJob(row); err != nil {
			return err
		}
		extended := []Job{j}
		if err := attachLabels(ctx, tx, extended); err != nil {
			return err
		}
		j = extended[0]
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, fmt.Errorf("%w: %d", ErrJobNotFound, id)
	}
	return j, err
}

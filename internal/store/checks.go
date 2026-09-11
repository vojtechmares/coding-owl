package store

import (
	"context"
	"database/sql"
)

// CheckResult is what one Verification check said about a Run.
type CheckResult struct {
	// Name identifies the check.
	Name string
	// Command is what it ran.
	Command string
	// Passed is whether it was satisfied.
	Passed bool
	// ExitCode is what the command exited with.
	ExitCode int
	// Output is what it printed.
	Output string
	// Reason is why it failed, empty when it passed.
	Reason string
	// Verifier is which Verifier said it: the Project's own checks, or the
	// review a Project asked for (ADR-0013).
	Verifier string
}

// SaveCheckResults records what Verification said about a Run, replacing
// anything an earlier Verification of the same Run had said.
func (s *Store) SaveCheckResults(ctx context.Context, runID int64, results []CheckResult) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM check_results WHERE run_id = ?`, runID); err != nil {
			return err
		}
		for i, r := range results {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO check_results (run_id, position, name, command, passed, exit_code, output, reason, verifier)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				runID, i+1, r.Name, r.Command, boolToInt(r.Passed), r.ExitCode, r.Output, r.Reason,
				r.Verifier); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListCheckResults returns what Verification said about a Run, in the order
// the checks were configured.
func (s *Store) ListCheckResults(ctx context.Context, runID int64) ([]CheckResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, command, passed, exit_code, output, reason, verifier
		 FROM check_results WHERE run_id = ? ORDER BY position, id`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CheckResult
	for rows.Next() {
		var r CheckResult
		var passed int
		if err := rows.Scan(&r.Name, &r.Command, &passed, &r.ExitCode, &r.Output, &r.Reason,
			&r.Verifier); err != nil {
			return nil, err
		}
		r.Passed = passed != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

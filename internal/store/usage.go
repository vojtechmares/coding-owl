package store

import (
	"context"
	"time"
)

// AccountUsage is what a Run's stream last said about one of an Account's
// windows (ADR-0020).
type AccountUsage struct {
	// Account is whose window it is.
	Account string
	// Window is the window, in the tool's own words for it.
	Window string
	// Utilization is how much of it was spent, as a percentage.
	Utilization float64
	// Resets is when the window starts again, and Observed when this was read.
	Resets   time.Time
	Observed time.Time
}

// RecordAccountUsage writes down what was read about one window, replacing
// what was known about it: only the last reading is worth anything, because
// the next one counts everything the one before it did.
func (s *Store) RecordAccountUsage(ctx context.Context, u AccountUsage) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO account_usage (account, window_name, utilization, resets_unix, observed_unix)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(account, window_name) DO UPDATE SET
    utilization   = excluded.utilization,
    resets_unix   = excluded.resets_unix,
    observed_unix = excluded.observed_unix`,
		u.Account, u.Window, u.Utilization, u.Resets.UTC().Unix(), u.Observed.UTC().Unix())
	return err
}

// ForgetResetWindows drops every reading whose window has started again. A
// figure about a window that is over says nothing about the one that replaced
// it, and keeping it would hold work back for a reason that has passed
// (ADR-0020).
func (s *Store) ForgetResetWindows(ctx context.Context, now time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM account_usage WHERE resets_unix <= ?`, now.UTC().Unix())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ListAccountUsage is every reading still about a window that has not started
// again, oldest window first for a steady report.
func (s *Store) ListAccountUsage(ctx context.Context) ([]AccountUsage, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT account, window_name, utilization, resets_unix, observed_unix
FROM account_usage
ORDER BY account, window_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AccountUsage
	for rows.Next() {
		var u AccountUsage
		var resets, observed int64
		if err := rows.Scan(&u.Account, &u.Window, &u.Utilization, &resets, &observed); err != nil {
			return nil, err
		}
		u.Resets = time.Unix(resets, 0).UTC()
		u.Observed = time.Unix(observed, 0).UTC()
		out = append(out, u)
	}
	return out, rows.Err()
}

// ForgetAccountUsage drops everything known about one Account's windows, which
// is what removing the Account leaves behind.
func (s *Store) ForgetAccountUsage(ctx context.Context, account string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM account_usage WHERE account = ?`, account)
	return err
}

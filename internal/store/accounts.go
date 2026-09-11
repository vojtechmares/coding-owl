package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrAccountNotFound is returned when no Account carries the name asked for.
var ErrAccountNotFound = errors.New("no such account")

// Account is a subscription Owl runs work on, with a tool configuration
// directory of its own (ADR-0019).
type Account struct {
	// Name identifies the Account and is what a Project's configuration names.
	Name string
	// Driver is the coding tool this Account belongs to (ADR-0018).
	Driver string
	// ConfigDir is the Account's own tool configuration directory, which Owl
	// owns: an Account is never the user's own setup.
	ConfigDir string
	// CredentialRef says where the Account's secret is kept. The secret itself
	// is never here.
	CredentialRef string
	// FailoverAllowed is recorded and unused: failing over to another Account
	// is out of scope for the MVP (ADR-0019).
	FailoverAllowed bool
	// Created is when the Account was added.
	Created time.Time
}

// accountColumns is the select list every Account read shares, in scanAccount's
// order.
const accountColumns = `name, driver, config_dir, credential_ref, failover_allowed, created`

// AddAccount records an Account. A name already taken is ErrNameTaken: an
// Account's name is how a Project asks for it, so two cannot share one.
func (s *Store) AddAccount(ctx context.Context, a Account) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO accounts (`+accountColumns+`) VALUES (?, ?, ?, ?, ?, ?)`,
		a.Name, a.Driver, a.ConfigDir, a.CredentialRef, boolToInt(a.FailoverAllowed),
		a.Created.UTC().Format(timeFormat))
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: %s", ErrNameTaken, a.Name)
	}
	return err
}

// GetAccount returns the Account of that name, or ErrAccountNotFound.
func (s *Store) GetAccount(ctx context.Context, name string) (Account, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE name = ?`, name)
	a, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, fmt.Errorf("%w: %s", ErrAccountNotFound, name)
	}
	return a, err
}

// ListAccounts returns every Account, oldest first, which is the order they
// were added in.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+accountColumns+` FROM accounts ORDER BY created, name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAccount removes an Account, or reports that there is no such one.
// Whether anything still references it is the caller's business: a Job names
// the Account it ran on for its whole life (ADR-0023), and that has to be
// refused with a message rather than by a constraint.
func (s *Store) DeleteAccount(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrAccountNotFound, name)
	}
	return nil
}

// CountJobsOnAccount is how many Jobs record having run on that Account.
func (s *Store) CountJobsOnAccount(ctx context.Context, name string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE account = ?`, name).Scan(&n)
	return n, err
}

func scanAccount(sc scanner) (Account, error) {
	var a Account
	var failover int
	var created string
	if err := sc.Scan(&a.Name, &a.Driver, &a.ConfigDir, &a.CredentialRef, &failover, &created); err != nil {
		return Account{}, err
	}
	a.FailoverAllowed = failover != 0
	t, err := time.Parse(timeFormat, created)
	if err != nil {
		return Account{}, fmt.Errorf("account %s has an unreadable created time %q: %w", a.Name, created, err)
	}
	a.Created = t
	return a, nil
}

// Package account keeps the subscriptions Owl runs work on. An Account has a
// tool configuration directory of its own, so that a night of Owl's work never
// disturbs the user's own setup, and a secret that lives in the credential
// store rather than in the database (ADR-0019).
package account

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/credential"
	"github.com/vojtechmares/coding-owl/internal/drivers"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// accountsDir is where the Account directories live under the data home
// (ADR-0014).
const accountsDir = "accounts"

// dirMode is what an Account's directory is created with. It holds a coding
// tool's whole credential state, so nobody but its owner may read it.
const dirMode = 0o700

// maxName bounds a name, which becomes a directory of its own.
const maxName = 64

// nameRE is what an Account may be called. It is a directory name, a keychain
// item and a word in a Project's configuration file, so it is kept to the
// characters all three read the same way.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// InvalidError marks a failure the user can fix by asking for something
// different: a name Owl cannot use, a Driver it does not have, an empty token.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }

func invalid(format string, a ...any) error {
	return &InvalidError{Err: fmt.Errorf(format, a...)}
}

// InUseError marks an Account that cannot be removed because Jobs still name
// it.
type InUseError struct{ Err error }

func (e *InUseError) Error() string { return e.Err.Error() }
func (e *InUseError) Unwrap() error { return e.Err }

// Account is a subscription Owl runs work on.
type Account struct {
	// Name identifies the Account and is what a Project's configuration names.
	Name string
	// Driver is the coding tool this Account belongs to.
	Driver string
	// ConfigDir is the Account's own tool configuration directory.
	ConfigDir string
	// HasCredential is whether the credential store still holds its secret.
	HasCredential bool
	// FailoverAllowed is recorded and unused (ADR-0019).
	FailoverAllowed bool
	// Created is when the Account was added.
	Created time.Time
}

// DirFor is where an Account's own tool configuration lives. It is derived
// rather than stored so that the command that walks a user through the tool's
// token setup and the daemon that records the Account name the same directory.
func DirFor(dataDir, name string) string {
	return filepath.Join(dataDir, accountsDir, name)
}

// RefFor is the credential reference an Account's secret is kept under.
func RefFor(name string) string { return "account/" + name }

// CheckName refuses a name Owl could not make a directory of its own for. It
// is exported because the command tree builds the directory's path before the
// daemon has seen the name, and must refuse the same names for the same
// reasons.
func CheckName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return invalid("an account needs a name")
	case len(name) > maxName:
		return invalid("the account name is %d characters; keep it to %d", len(name), maxName)
	case !nameRE.MatchString(name):
		return invalid("the account name %q is not one Owl can use: it becomes a directory of its own, "+
			"so it must start with a letter or a digit and hold only letters, digits, dots, dashes and underscores", name)
	}
	return nil
}

// Service adds, lists and removes Accounts.
type Service struct {
	store   *store.Store
	creds   credential.Store
	dataDir string
	now     func() time.Time
}

// NewService returns a Service recording Accounts in st, keeping their secrets
// in creds, and giving each a directory under dataDir.
func NewService(st *store.Store, creds credential.Store, dataDir string) *Service {
	return &Service{store: st, creds: creds, dataDir: dataDir, now: time.Now}
}

// AddRequest is what owl account add carries.
type AddRequest struct {
	// Name is what the Account is called, and what a Project's configuration
	// names to run on it.
	Name string
	// Driver is the coding tool it belongs to. Empty is the default one.
	Driver string
	// Token is the long-lived token the tool's own setup printed.
	Token string
	// FailoverAllowed is recorded and unused (ADR-0019).
	FailoverAllowed bool
}

// Add records an Account, makes its configuration directory and puts its token
// in the credential store. Nothing is left behind by a request that is
// refused: the name is checked, then the directory is made, then the secret is
// written, and the row is what makes the Account real.
func (s *Service) Add(ctx context.Context, req AddRequest) (Account, error) {
	if err := CheckName(req.Name); err != nil {
		return Account{}, err
	}
	name := req.Driver
	if name == "" {
		name = drivers.Default
	}
	if _, ok := drivers.Lookup(name); !ok {
		return Account{}, &InvalidError{Err: drivers.Unknown(name)}
	}
	if strings.TrimSpace(req.Token) == "" {
		return Account{}, invalid("account %s has no token; run the tool's own token setup and paste what it prints", req.Name)
	}
	if _, err := s.store.GetAccount(ctx, req.Name); err == nil {
		return Account{}, fmt.Errorf("%w: %s", store.ErrNameTaken, req.Name)
	} else if !errors.Is(err, store.ErrAccountNotFound) {
		return Account{}, err
	}

	dir := DirFor(s.dataDir, req.Name)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return Account{}, fmt.Errorf("making the account's configuration directory: %w", err)
	}
	// MkdirAll leaves a directory that was already there as it was, and this
	// one holds a tool's credentials.
	if err := os.Chmod(dir, dirMode); err != nil {
		return Account{}, fmt.Errorf("securing the account's configuration directory: %w", err)
	}
	ref := RefFor(req.Name)
	if err := s.creds.Set(ctx, ref, req.Token); err != nil {
		return Account{}, err
	}
	row := store.Account{
		Name:            req.Name,
		Driver:          name,
		ConfigDir:       dir,
		CredentialRef:   ref,
		FailoverAllowed: req.FailoverAllowed,
		Created:         s.now().UTC(),
	}
	if err := s.store.AddAccount(ctx, row); err != nil {
		// The secret was written for an Account that does not exist, so it is
		// taken back rather than left in the keychain under a name nothing
		// refers to.
		_ = s.creds.Delete(ctx, ref)
		return Account{}, err
	}
	return Account{
		Name: row.Name, Driver: row.Driver, ConfigDir: row.ConfigDir,
		HasCredential: true, FailoverAllowed: row.FailoverAllowed, Created: row.Created,
	}, nil
}

// List returns every Account, oldest first, and says of each whether the
// credential store still holds its secret.
func (s *Service) List(ctx context.Context) ([]Account, error) {
	rows, err := s.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, Account{
			Name: r.Name, Driver: r.Driver, ConfigDir: r.ConfigDir,
			HasCredential:   s.holds(ctx, r.CredentialRef),
			FailoverAllowed: r.FailoverAllowed, Created: r.Created,
		})
	}
	return out, nil
}

// holds reports whether the credential store still has that secret. A store
// that cannot answer is reported as not holding it: the report is about what
// Owl can be sure of.
func (s *Service) holds(ctx context.Context, ref string) bool {
	_, err := s.creds.Get(ctx, ref)
	return err == nil
}

// Remove takes an Account away with its secret, and refuses while any Job
// records having run on it: what a Job drew on stays knowable for its whole
// life (ADR-0023). The Account's configuration directory is left where it is -
// removing an Account is not a reason to destroy a tool's state - and the
// Account as it last stood is returned so the caller can say where that is.
func (s *Service) Remove(ctx context.Context, name string) (Account, error) {
	row, err := s.store.GetAccount(ctx, name)
	if err != nil {
		return Account{}, err
	}
	jobs, err := s.store.CountJobsOnAccount(ctx, name)
	if err != nil {
		return Account{}, err
	}
	if jobs > 0 {
		return Account{}, &InUseError{Err: fmt.Errorf(
			"account %s is what %s ran on; removing it would leave them naming an account that is not there",
			name, plural(jobs, "job"))}
	}
	if err := s.store.DeleteAccount(ctx, name); err != nil {
		return Account{}, err
	}
	// The row is gone, so the secret has nothing left referring to it.
	if err := s.creds.Delete(ctx, row.CredentialRef); err != nil {
		return Account{}, err
	}
	return Account{
		Name: row.Name, Driver: row.Driver, ConfigDir: row.ConfigDir,
		FailoverAllowed: row.FailoverAllowed, Created: row.Created,
	}, nil
}

// Credential returns an Account with the secret to run it on, which is what a
// Run needs and nothing else does.
func (s *Service) Credential(ctx context.Context, name string) (Account, string, error) {
	row, err := s.store.GetAccount(ctx, name)
	if err != nil {
		return Account{}, "", err
	}
	token, err := s.creds.Get(ctx, row.CredentialRef)
	if errors.Is(err, credential.ErrNotFound) {
		return Account{}, "", fmt.Errorf(
			"account %s has no credential in %s; add it again to authenticate it", name, s.creds.Name())
	}
	if err != nil {
		return Account{}, "", err
	}
	return Account{
		Name: row.Name, Driver: row.Driver, ConfigDir: row.ConfigDir,
		HasCredential: true, FailoverAllowed: row.FailoverAllowed, Created: row.Created,
	}, token, nil
}

// plural renders a count with its noun, so a message can say one job and two
// jobs without saying "job(s)".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

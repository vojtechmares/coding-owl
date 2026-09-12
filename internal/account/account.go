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
// in the credential store.
//
// The row goes in before the secret, so that a name is claimed by the database
// rather than by a check: two callers adding the same name at once would
// otherwise both pass the check, and the one that lost would take the winner's
// secret back out of the keychain on its way to reporting the collision.
// Nothing is left behind by a request that is refused, and an Account whose
// secret could not be written is taken out again rather than left unable to
// run.
func (s *Service) Add(ctx context.Context, req AddRequest) (Account, error) {
	if err := CheckName(req.Name); err != nil {
		return Account{}, err
	}
	driverName := req.Driver
	if driverName == "" {
		driverName = drivers.Default
	}
	if _, ok := drivers.Lookup(driverName); !ok {
		return Account{}, &InvalidError{Err: drivers.Unknown(driverName)}
	}
	if strings.TrimSpace(req.Token) == "" {
		return Account{}, invalid("account %s has no token; run the tool's own token setup and paste what it prints", req.Name)
	}
	dir := DirFor(s.dataDir, req.Name)
	ref := RefFor(req.Name)
	row := store.Account{
		Name:            req.Name,
		Driver:          driverName,
		ConfigDir:       dir,
		CredentialRef:   ref,
		FailoverAllowed: req.FailoverAllowed,
		Created:         s.now().UTC(),
	}
	if err := s.store.AddAccount(ctx, row); err != nil {
		return Account{}, err
	}
	if err := ensureDir(dir); err != nil {
		return Account{}, s.undo(ctx, req.Name, err)
	}
	if err := s.creds.Set(ctx, ref, req.Token); err != nil {
		// An Account with no secret can run nothing, so it is taken out again
		// rather than left for the user to find at the next Run.
		return Account{}, s.undo(ctx, req.Name, err)
	}
	return Account{
		Name: row.Name, Driver: row.Driver, ConfigDir: row.ConfigDir,
		HasCredential: true, FailoverAllowed: row.FailoverAllowed, Created: row.Created,
	}, nil
}

// undoTimeout bounds taking back an Account that could not be finished.
const undoTimeout = 10 * time.Second

// undo takes back the row of an Account that could not be finished, and says
// so when it cannot: an Account left behind that way can run nothing, and the
// user has to hear about it rather than meet it at the next Run. It does not
// run under the request's own context, because the likeliest reason to be here
// is that the request was cancelled.
func (s *Service) undo(ctx context.Context, name string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), undoTimeout)
	defer cancel()
	if err := s.store.DeleteAccount(ctx, name); err != nil {
		return fmt.Errorf("%w (and account %s could not be taken back out, so it is recorded without a credential: %v)",
			cause, name, err)
	}
	return cause
}

// ensureDir makes an Account's configuration directory, readable by nobody but
// its owner: it holds a coding tool's whole credential state. It is exported
// through EnsureDir because the command that walks a user through the tool's
// token setup has to make it before the tool writes into it, which is before
// the daemon has heard of the Account.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("making the account's configuration directory: %w", err)
	}
	// MkdirAll leaves a directory that was already there as it was.
	if err := os.Chmod(dir, dirMode); err != nil {
		return fmt.Errorf("securing the account's configuration directory: %w", err)
	}
	return nil
}

// EnsureDir makes the configuration directory of the Account that will be
// called name under dataDir, and secures it. The tool's own token setup writes
// its credential state there before there is an Account to record, so the
// directory has to exist, and be the user's alone, before it runs.
func EnsureDir(dataDir, name string) error {
	if err := CheckName(name); err != nil {
		return err
	}
	return ensureDir(DirFor(dataDir, name))
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
	// The count is against the name as it is recorded, not as it was asked
	// for: an Account can be named in any case, and a Job records the one the
	// Account carries.
	jobs, err := s.store.CountJobsOnAccount(ctx, row.Name)
	if err != nil {
		return Account{}, err
	}
	if jobs > 0 {
		return Account{}, &InUseError{Err: fmt.Errorf(
			"account %s is what %s ran on; removing it would leave them naming an account that is not there",
			row.Name, plural(jobs, "job"))}
	}
	if err := s.store.DeleteAccount(ctx, row.Name); err != nil {
		return Account{}, err
	}
	// What was read about its windows goes with it: a figure about an Account
	// nobody has is about nothing (ADR-0020). The Account is removed either
	// way, which the message has to say - and forgetting first would leave an
	// Account that could not be removed with no figure to hold it to.
	if err := s.store.ForgetAccountUsage(ctx, row.Name); err != nil {
		return Account{}, fmt.Errorf(
			"account %s was removed, but what was read about its windows is still in the database: %w",
			row.Name, err)
	}
	// The row is gone, so the secret has nothing left referring to it. The
	// Account is removed either way, which the message has to say: leaving a
	// caller to think it is still there would be worse than the secret that is
	// still in the store.
	if err := s.creds.Delete(ctx, row.CredentialRef); err != nil {
		return Account{}, fmt.Errorf(
			"account %s was removed, but its credential is still in %s and has to be taken out by hand: %w",
			row.Name, s.creds.Name(), err)
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

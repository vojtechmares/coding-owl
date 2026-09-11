package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

func account(name string) store.Account {
	return store.Account{
		Name:          name,
		Driver:        "claude-code",
		ConfigDir:     "/data/accounts/" + name,
		CredentialRef: "account/" + name,
		Created:       time.Now().UTC(),
	}
}

// accountStore opens a database to hang Accounts off.
func accountStore(t *testing.T) *store.Store {
	t.Helper()
	return openStore(t, filepath.Join(t.TempDir(), "owl.db"))
}

func TestAddAccountReadsBackWhatItWasGiven(t *testing.T) {
	ctx := context.Background()
	s := accountStore(t)
	a := account("work")
	a.FailoverAllowed = true

	if err := s.AddAccount(ctx, a); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	got, err := s.GetAccount(ctx, "work")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.Name != a.Name || got.Driver != a.Driver || got.ConfigDir != a.ConfigDir {
		t.Errorf("GetAccount = %+v, want %+v", got, a)
	}
	if got.CredentialRef != a.CredentialRef {
		t.Errorf("the account references %q, want %q", got.CredentialRef, a.CredentialRef)
	}
	if !got.FailoverAllowed {
		t.Error("the account does not allow failover, though it was added allowing it")
	}
	if !got.Created.Equal(a.Created) {
		t.Errorf("the account was created %v, want the %v it was given", got.Created, a.Created)
	}
}

func TestAddAccountRefusesANameThatIsTaken(t *testing.T) {
	ctx := context.Background()
	s := accountStore(t)
	if err := s.AddAccount(ctx, account("work")); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	err := s.AddAccount(ctx, account("work"))

	if !errors.Is(err, store.ErrAccountNameTaken) {
		t.Errorf("adding an account twice = %v, want ErrAccountNameTaken", err)
	}
	if !strings.Contains(err.Error(), "account") {
		t.Errorf("the error %q does not say what was already there", err)
	}
}

func TestAddAccountRefusesANameThatIsTakenInAnotherCase(t *testing.T) {
	ctx := context.Background()
	s := accountStore(t)
	if err := s.AddAccount(ctx, account("work")); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	err := s.AddAccount(ctx, account("Work"))

	// The name becomes a directory, and macOS does not tell the two apart.
	if !errors.Is(err, store.ErrAccountNameTaken) {
		t.Errorf("adding Work beside work = %v, want ErrAccountNameTaken", err)
	}
}

func TestGetAccountFindsItWhateverTheCase(t *testing.T) {
	ctx := context.Background()
	s := accountStore(t)
	if err := s.AddAccount(ctx, account("work")); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	got, err := s.GetAccount(ctx, "WORK")

	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.Name != "work" {
		t.Errorf("GetAccount = %q, want the account as it was named", got.Name)
	}
}

func TestGetAccountReportsOneThatIsNotThere(t *testing.T) {
	_, err := accountStore(t).GetAccount(context.Background(), "nothing")

	if !errors.Is(err, store.ErrAccountNotFound) {
		t.Errorf("GetAccount on an unknown account = %v, want ErrAccountNotFound", err)
	}
}

func TestListAccountsReturnsThemInTheOrderTheyWereAdded(t *testing.T) {
	ctx := context.Background()
	s := accountStore(t)
	// The two times differ only in a fraction, and as text the later one sorts
	// first: ".55Z" is less than ".5Z", because '5' comes before 'Z'. The
	// order asked for is the order they were added, whatever the text does.
	first := account("work")
	first.Created = time.Date(2026, 9, 11, 10, 0, 0, 500000000, time.UTC)
	if err := s.AddAccount(ctx, first); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	second := account("personal")
	second.Created = time.Date(2026, 9, 11, 10, 0, 0, 550000000, time.UTC)
	if err := s.AddAccount(ctx, second); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	got, err := s.ListAccounts(ctx)

	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(got) != 2 || got[0].Name != "work" || got[1].Name != "personal" {
		t.Errorf("ListAccounts = %+v, want work then personal", got)
	}
}

func TestDeleteAccountTakesItAway(t *testing.T) {
	ctx := context.Background()
	s := accountStore(t)
	if err := s.AddAccount(ctx, account("work")); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	if err := s.DeleteAccount(ctx, "work"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	if _, err := s.GetAccount(ctx, "work"); !errors.Is(err, store.ErrAccountNotFound) {
		t.Errorf("GetAccount after DeleteAccount = %v, want ErrAccountNotFound", err)
	}
}

func TestDeleteAccountReportsOneThatIsNotThere(t *testing.T) {
	err := accountStore(t).DeleteAccount(context.Background(), "nothing")

	if !errors.Is(err, store.ErrAccountNotFound) {
		t.Errorf("DeleteAccount on an unknown account = %v, want ErrAccountNotFound", err)
	}
}

func TestCountJobsOnAccountCountsOnlyThatAccountsJobs(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	mine := queuedJob(t, s, "mine", "a")
	theirs := queuedJob(t, s, "theirs", "b")
	queuedJob(t, s, "unrun", "c")
	if err := s.SetJobAccount(ctx, mine.ID, "work"); err != nil {
		t.Fatalf("SetJobAccount: %v", err)
	}
	if err := s.SetJobAccount(ctx, theirs.ID, "personal"); err != nil {
		t.Fatalf("SetJobAccount: %v", err)
	}

	n, err := s.CountJobsOnAccount(ctx, "work")

	if err != nil {
		t.Fatalf("CountJobsOnAccount: %v", err)
	}
	if n != 1 {
		t.Errorf("CountJobsOnAccount = %d, want the one job that ran on it", n)
	}
}

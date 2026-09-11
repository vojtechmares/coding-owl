package account_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/account"
	"github.com/vojtechmares/coding-owl/internal/credential"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// fixture is a Service over a temporary database and a credential file, with
// the data directory its Accounts get their directories under.
type fixture struct {
	svc     *account.Service
	store   *store.Store
	creds   credential.Store
	dataDir string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	st, _, err := store.Open(filepath.Join(root, "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	creds := credential.NewFile(filepath.Join(root, "credentials.json"))
	return fixture{
		svc:     account.NewService(st, creds, root),
		store:   st,
		creds:   creds,
		dataDir: root,
	}
}

// added adds an Account and fails the test if it could not be added.
func (f fixture) added(t *testing.T, name string) account.Account {
	t.Helper()
	a, err := f.svc.Add(context.Background(), account.AddRequest{Name: name, Token: "sk-ant-oat01-" + name})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return a
}

func TestAddMakesTheDirectoryAndKeepsTheTokenInTheStore(t *testing.T) {
	f := newFixture(t)

	a := f.added(t, "work")

	if a.ConfigDir != filepath.Join(f.dataDir, "accounts", "work") {
		t.Errorf("the account's directory is %s, want one of its own under the data home", a.ConfigDir)
	}
	info, err := os.Stat(a.ConfigDir)
	if err != nil {
		t.Fatalf("the account's directory: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("the account's directory is %o, want 700", perm)
	}
	got, err := f.creds.Get(context.Background(), account.RefFor("work"))
	if err != nil {
		t.Fatalf("the credential store holds nothing for the account: %v", err)
	}
	if got != "sk-ant-oat01-work" {
		t.Errorf("the credential store holds %q, want the token the account was added with", got)
	}
	if !a.HasCredential {
		t.Error("the account reports no credential, though one was just written")
	}
}

func TestAddKeepsTheSecretOutOfTheDatabase(t *testing.T) {
	f := newFixture(t)

	f.added(t, "work")

	row, err := f.store.GetAccount(context.Background(), "work")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if strings.Contains(row.CredentialRef, "sk-ant") {
		t.Errorf("the database holds %q, want a reference rather than the secret", row.CredentialRef)
	}
	if row.CredentialRef != account.RefFor("work") {
		t.Errorf("the account references %q, want %q", row.CredentialRef, account.RefFor("work"))
	}
}

func TestAddRefusesANameThatIsTaken(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")

	_, err := f.svc.Add(context.Background(), account.AddRequest{Name: "work", Token: "another"})

	if !errors.Is(err, store.ErrAccountNameTaken) {
		t.Fatalf("adding an account twice = %v, want ErrAccountNameTaken", err)
	}
	if !strings.Contains(err.Error(), "account") {
		t.Errorf("the error %q does not say what was already there", err)
	}
	if got, err := f.creds.Get(context.Background(), account.RefFor("work")); err != nil {
		t.Fatalf("the first account lost its credential: %v", err)
	} else if got != "sk-ant-oat01-work" {
		t.Errorf("the credential store holds %q, want the first account's token", got)
	}
}

func TestAddDoesNotTakeTheSecretOfTheAccountThatIsAlreadyThere(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")

	// The second add replaces the secret before the name collides, so an
	// Account that refuses to be added twice must not take the first one's
	// credential with it.
	_, _ = f.svc.Add(context.Background(), account.AddRequest{Name: "work", Token: "another"})

	got, err := f.creds.Get(context.Background(), account.RefFor("work"))
	if err != nil {
		t.Fatalf("the account that was already there lost its credential: %v", err)
	}
	if got != "sk-ant-oat01-work" {
		t.Errorf("the credential store holds %q, want the first account's token", got)
	}
}

func TestRemoveFindsTheAccountWhateverTheCase(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")
	ctx := context.Background()
	if err := f.store.AddProject(ctx, store.Project{
		Name: "api", Path: "/repos/api", BaseBranch: "main", Registered: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	j, err := f.store.UpsertJob(ctx, store.Job{
		Source: "local", SourceRef: "a", Project: "api", Prompt: "work",
		State: "pending", TTL: 10, Created: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if err := f.store.SetJobAccount(ctx, j.ID, "work"); err != nil {
		t.Fatalf("SetJobAccount: %v", err)
	}

	_, err = f.svc.Remove(ctx, "WORK")

	// The Jobs are counted against the name the Account carries, not the one
	// the caller typed, or a differently-cased name would remove an Account
	// Jobs still name.
	var inUse *account.InUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("removing WORK, which a job ran on as work = %v, want it refused", err)
	}
}

func TestAddRefusesANameThatIsNotADirectoryOfItsOwn(t *testing.T) {
	f := newFixture(t)

	for _, name := range []string{"", "..", "../elsewhere", "work/nested", ".hidden", "with space", strings.Repeat("x", 65)} {
		_, err := f.svc.Add(context.Background(), account.AddRequest{Name: name, Token: "token"})

		var invalid *account.InvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("adding the account %q = %v, want it refused", name, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(f.dataDir, "accounts"))
	if err == nil && len(entries) > 0 {
		t.Errorf("a refused name left %d directories behind", len(entries))
	}
}

func TestAddRefusesADriverOwlDoesNotHave(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.Add(context.Background(), account.AddRequest{Name: "work", Driver: "codex", Token: "token"})

	var invalid *account.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("adding an account on an unknown driver = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "claude-code") {
		t.Errorf("the error %q does not name the drivers Owl has", err)
	}
}

func TestAddRefusesAnEmptyToken(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.Add(context.Background(), account.AddRequest{Name: "work", Token: "  "})

	var invalid *account.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("adding an account with no token = %v, want it refused", err)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "accounts", "work")); err == nil {
		t.Error("a directory was left behind for an account that was refused")
	}
}

func TestListReportsAccountsOldestFirst(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")
	f.added(t, "personal")

	got, err := f.svc.List(context.Background())

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List = %+v, want two accounts", got)
	}
	if got[0].Name != "work" || got[1].Name != "personal" {
		t.Errorf("List = %s then %s, want them in the order they were added", got[0].Name, got[1].Name)
	}
}

func TestListSaysWhenAnAccountHasLostItsCredential(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")
	if err := f.creds.Delete(context.Background(), account.RefFor("work")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := f.svc.List(context.Background())

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].HasCredential {
		t.Errorf("List = %+v, want the account reported as having no credential", got)
	}
}

func TestRemoveTakesTheAccountAndItsSecret(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")

	removed, err := f.svc.Remove(context.Background(), "work")

	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed.ConfigDir != a.ConfigDir {
		t.Errorf("Remove reports the directory %s, want %s", removed.ConfigDir, a.ConfigDir)
	}
	if _, err := f.store.GetAccount(context.Background(), "work"); !errors.Is(err, store.ErrAccountNotFound) {
		t.Errorf("the account is still recorded: %v", err)
	}
	if _, err := f.creds.Get(context.Background(), account.RefFor("work")); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("the credential store still holds the token: %v", err)
	}
	// Removing an Account is not a reason to destroy a tool's state.
	if _, err := os.Stat(a.ConfigDir); err != nil {
		t.Errorf("the account's configuration directory went with it: %v", err)
	}
}

func TestRemoveRefusesWhileAJobRecordsTheAccount(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")
	ctx := context.Background()
	if err := f.store.AddProject(ctx, store.Project{
		Name: "api", Path: "/repos/api", BaseBranch: "main", Registered: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	j, err := f.store.UpsertJob(ctx, store.Job{
		Source: "local", SourceRef: "a", Project: "api", Prompt: "work",
		State: "pending", TTL: 10, Created: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if err := f.store.SetJobAccount(ctx, j.ID, "work"); err != nil {
		t.Fatalf("SetJobAccount: %v", err)
	}

	_, err = f.svc.Remove(ctx, "work")

	var inUse *account.InUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("removing an account a job ran on = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "1 job") {
		t.Errorf("the error %q does not say how many jobs ran on it", err)
	}
	if _, err := f.creds.Get(ctx, account.RefFor("work")); err != nil {
		t.Errorf("a refused removal took the credential anyway: %v", err)
	}
}

func TestRemoveReportsAnAccountThatIsNotThere(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.Remove(context.Background(), "nothing")

	if !errors.Is(err, store.ErrAccountNotFound) {
		t.Errorf("removing an account that is not there = %v, want ErrAccountNotFound", err)
	}
}

func TestCredentialReturnsTheTokenToRunOn(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")

	a, token, err := f.svc.Credential(context.Background(), "work")

	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if a.ConfigDir == "" {
		t.Error("the account has no configuration directory to run in")
	}
	if token != "sk-ant-oat01-work" {
		t.Errorf("Credential = %q, want the account's token", token)
	}
}

func TestCredentialSaysWhenTheSecretIsGone(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")
	if err := f.creds.Delete(context.Background(), account.RefFor("work")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, _, err := f.svc.Credential(context.Background(), "work")

	if err == nil {
		t.Fatal("Credential with no secret = nil, want an error")
	}
	if !strings.Contains(err.Error(), "work") {
		t.Errorf("the error %q does not name the account", err)
	}
}

func TestDirForIsUnderTheDataHome(t *testing.T) {
	if got := account.DirFor("/data", "work"); got != filepath.Join("/data", "accounts", "work") {
		t.Errorf("DirFor = %s, want the account's own directory under the data home", got)
	}
}

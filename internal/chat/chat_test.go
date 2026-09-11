package chat_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/chat"
	"github.com/vojtechmares/coding-owl/internal/credential"
	"github.com/vojtechmares/coding-owl/internal/store"
)

var ctx = context.Background()

// fixture is a chat over a temporary database and credential store, with a
// view of what Owl knows that the test decides.
type fixture struct {
	chat  *chat.Service
	store *store.Store
	creds credential.Store
	view  *fakeView
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	st, _, err := store.Open(filepath.Join(root, "owl.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	creds := credential.NewFile(filepath.Join(root, "credentials.json"))
	view := &fakeView{}
	return &fixture{chat: chat.NewService(st, creds, chat.NewTools(view)), store: st, creds: creds, view: view}
}

// fakeView answers what the tools ask of Owl's state.
type fakeView struct {
	projects string
	jobs     string
	job      string
	log      string
	diff     string
	asked    []string
	err      error
}

func (v *fakeView) Projects(context.Context) (string, error) {
	v.asked = append(v.asked, "projects")
	return v.projects, v.err
}

func (v *fakeView) Jobs(_ context.Context, all bool) (string, error) {
	v.asked = append(v.asked, "jobs")
	_ = all
	return v.jobs, v.err
}

func (v *fakeView) Job(_ context.Context, id int64) (string, error) {
	v.asked = append(v.asked, "job")
	_ = id
	return v.job, v.err
}

func (v *fakeView) RunLog(_ context.Context, id int64, max int) (string, error) {
	v.asked = append(v.asked, "log")
	_, _ = id, max
	return v.log, v.err
}

func (v *fakeView) JobDiff(_ context.Context, id int64, max int) (string, error) {
	v.asked = append(v.asked, "diff")
	_, _ = id, max
	return v.diff, v.err
}

func TestAddProviderKeepsTheKeyOutOfTheDatabase(t *testing.T) {
	f := newFixture(t)

	got, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret", "", nil)

	if err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	if len(got.Models) == 0 {
		t.Error("a provider Owl knows the models of was configured with none")
	}
	row, err := f.store.GetChatProvider(ctx, "anthropic")
	if err != nil {
		t.Fatalf("GetChatProvider: %v", err)
	}
	if strings.Contains(row.CredentialRef, "sk-ant-secret") {
		t.Errorf("the row carries the key itself: %+v", row)
	}
	secret, err := f.creds.Get(ctx, row.CredentialRef)
	if err != nil || secret != "sk-ant-secret" {
		t.Errorf("the credential store holds %q, %v; want the key", secret, err)
	}
}

func TestAddProviderRefusesWhatOwlCannotUse(t *testing.T) {
	f := newFixture(t)
	for what, add := range map[string]func() error{
		"a provider Owl does not drive": func() error {
			_, err := f.chat.AddProvider(ctx, "some-llm", "k", "", []string{"m"})
			return err
		},
		"no key at all": func() error {
			_, err := f.chat.AddProvider(ctx, "anthropic", "  ", "", nil)
			return err
		},
		"openrouter with no model": func() error {
			_, err := f.chat.AddProvider(ctx, "openrouter", "k", "", nil)
			return err
		},
		"a base url that is not one": func() error {
			_, err := f.chat.AddProvider(ctx, "anthropic", "k", "ftp://models", nil)
			return err
		},
		"a model name Owl cannot use": func() error {
			_, err := f.chat.AddProvider(ctx, "openrouter", "k", "", []string{"--upload-pack=evil"})
			return err
		},
	} {
		err := add()

		var invalid *chat.InvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("%s = %v, want it refused", what, err)
		}
	}
	if all, err := f.chat.ListProviders(ctx); err != nil || len(all) != 0 {
		t.Errorf("ListProviders = %+v, %v; want nothing configured", all, err)
	}
}

func TestRemoveProviderTakesTheKeyWithIt(t *testing.T) {
	f := newFixture(t)
	if _, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret", "", nil); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	if err := f.chat.RemoveProvider(ctx, "anthropic"); err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}

	if _, err := f.creds.Get(ctx, "chat/anthropic"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("the key is still in the credential store: %v", err)
	}
	var invalid *chat.InvalidError
	if err := f.chat.RemoveProvider(ctx, "anthropic"); !errors.As(err, &invalid) {
		t.Errorf("removing one that is not there = %v, want it refused", err)
	}
}

func TestSendRefusesAModelNobodyConfigured(t *testing.T) {
	f := newFixture(t)

	_, err := f.chat.Send(ctx, chat.SendRequest{Model: "claude-opus-5", Text: "hello"}, nil)

	var invalid *chat.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Send with no provider = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "claude-opus-5") {
		t.Errorf("the error %q does not name the model", err)
	}
	// Nothing was started: a conversation is what a message makes, and there
	// was no message.
	if all, err := f.chat.Conversations(ctx); err != nil || len(all) != 0 {
		t.Errorf("Conversations = %+v, %v; want none", all, err)
	}
}

func TestSendRefusesAMessageWithNothingInIt(t *testing.T) {
	f := newFixture(t)

	_, err := f.chat.Send(ctx, chat.SendRequest{Model: "claude-opus-5", Text: "   "}, nil)

	var invalid *chat.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Send with nothing to say = %v, want it refused", err)
	}
}

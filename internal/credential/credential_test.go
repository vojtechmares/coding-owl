package credential_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/credential"
)

// stores is every Store worth running the same behaviour against. The keychain
// adds itself on the platform that has one.
func stores(t *testing.T) map[string]credential.Store {
	t.Helper()
	out := map[string]credential.Store{
		"file": credential.NewFile(filepath.Join(t.TempDir(), "credentials.json")),
	}
	if kc, ok := testKeychain(t); ok {
		out["keychain"] = kc
	}
	return out
}

func TestAStoreReadsBackWhatItWasGiven(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := s.Set(ctx, "account/work", "sk-ant-oat01-one"); err != nil {
				t.Fatalf("Set: %v", err)
			}

			got, err := s.Get(ctx, "account/work")

			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got != "sk-ant-oat01-one" {
				t.Errorf("Get = %q, want the secret it was given", got)
			}
		})
	}
}

func TestAStoreKeepsItsSecretsApart(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := s.Set(ctx, "account/work", "one"); err != nil {
				t.Fatalf("Set: %v", err)
			}
			if err := s.Set(ctx, "account/personal", "two"); err != nil {
				t.Fatalf("Set: %v", err)
			}

			first, err := s.Get(ctx, "account/work")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			second, err := s.Get(ctx, "account/personal")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}

			if first != "one" || second != "two" {
				t.Errorf("the store holds %q and %q, want each reference its own secret", first, second)
			}
		})
	}
}

func TestSetReplacesASecretAlreadyThere(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := s.Set(ctx, "account/work", "old"); err != nil {
				t.Fatalf("Set: %v", err)
			}

			if err := s.Set(ctx, "account/work", "new"); err != nil {
				t.Fatalf("Set again: %v", err)
			}

			got, err := s.Get(ctx, "account/work")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got != "new" {
				t.Errorf("Get = %q, want the secret that replaced it", got)
			}
		})
	}
}

func TestGetReportsASecretThatIsNotThere(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			_, err := s.Get(context.Background(), "account/never-written")

			if !errors.Is(err, credential.ErrNotFound) {
				t.Errorf("Get on a reference nobody wrote = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestDeleteTakesASecretAway(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := s.Set(ctx, "account/work", "one"); err != nil {
				t.Fatalf("Set: %v", err)
			}

			if err := s.Delete(ctx, "account/work"); err != nil {
				t.Fatalf("Delete: %v", err)
			}

			if _, err := s.Get(ctx, "account/work"); !errors.Is(err, credential.ErrNotFound) {
				t.Errorf("Get after Delete = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestDeleteDoesNotMindASecretThatIsNotThere(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			if err := s.Delete(context.Background(), "account/never-written"); err != nil {
				t.Errorf("Delete on a reference nobody wrote = %v, want nothing to do", err)
			}
		})
	}
}

func TestAStoreRefusesAReferenceItCouldNotTellApart(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for _, ref := range []string{"", "-w", "account/with\nnewline"} {
				if err := s.Set(ctx, ref, "one"); err == nil {
					t.Errorf("Set with the reference %q = nil, want it refused", ref)
				}
			}
		})
	}
}

func TestTheFileStoreIsReadableOnlyByItsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	s := credential.NewFile(path)

	if err := s.Set(context.Background(), "account/work", "one"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the credential file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the credential file is %o, want 600: it holds secrets", perm)
	}
}

func TestTheFileStoreMakesItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials.json")

	if err := credential.NewFile(path).Set(context.Background(), "account/work", "one"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the credential file was not made: %v", err)
	}
}

func TestParseKindReadsWhatTheConfigurationSays(t *testing.T) {
	for _, c := range []struct {
		in   string
		want credential.Kind
	}{
		{in: "keychain", want: credential.KindKeychain},
		{in: "file", want: credential.KindFile},
		{in: "", want: credential.Default()},
		{in: "  file  ", want: credential.KindFile},
	} {
		got, err := credential.ParseKind(c.in)
		if err != nil {
			t.Errorf("ParseKind(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseKind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseKindRefusesAStoreOwlDoesNotHave(t *testing.T) {
	_, err := credential.ParseKind("1password")

	if err == nil {
		t.Fatal("ParseKind on a store Owl does not have = nil, want an error")
	}
	for _, want := range []string{"1password", "keychain", "file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not name %q", err, want)
		}
	}
}

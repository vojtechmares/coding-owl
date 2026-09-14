package xdg

import (
	"errors"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveFromDefaultsToHome(t *testing.T) {
	p, err := ResolveFrom(env(map[string]string{"HOME": "/Users/o"}))
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		ConfigDir:  "/Users/o/.config/coding-owl",
		DataDir:    "/Users/o/.local/share/coding-owl",
		StateDir:   "/Users/o/.local/state/coding-owl",
		SocketPath: "/Users/o/.local/state/coding-owl/owld.sock",
	}
	if p != want {
		t.Errorf("got %+v, want %+v", p, want)
	}
}

func TestResolveFromHonoursXDGVariables(t *testing.T) {
	p, err := ResolveFrom(env(map[string]string{
		"HOME":            "/Users/o",
		"XDG_CONFIG_HOME": "/c",
		"XDG_DATA_HOME":   "/d",
		"XDG_STATE_HOME":  "/s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		ConfigDir:  "/c/coding-owl",
		DataDir:    "/d/coding-owl",
		StateDir:   "/s/coding-owl",
		SocketPath: "/s/coding-owl/owld.sock",
	}
	if p != want {
		t.Errorf("got %+v, want %+v", p, want)
	}
}

func TestResolveFromPrefersRuntimeDirForSocket(t *testing.T) {
	p, err := ResolveFrom(env(map[string]string{"HOME": "/Users/o", "XDG_RUNTIME_DIR": "/run/u/501"}))
	if err != nil {
		t.Fatal(err)
	}
	if p.SocketPath != "/run/u/501/coding-owl/owld.sock" {
		t.Errorf("socket = %q", p.SocketPath)
	}
	if p.StateDir != "/Users/o/.local/state/coding-owl" {
		t.Errorf("state dir = %q", p.StateDir)
	}
}

func TestResolveFromIgnoresRelativeValues(t *testing.T) {
	p, err := ResolveFrom(env(map[string]string{
		"HOME":            "/Users/o",
		"XDG_STATE_HOME":  "state",
		"XDG_RUNTIME_DIR": "run",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.SocketPath != "/Users/o/.local/state/coding-owl/owld.sock" {
		t.Errorf("relative XDG values must be ignored, got socket %q", p.SocketPath)
	}
}

func TestResolveFromWithoutHomeFails(t *testing.T) {
	if _, err := ResolveFrom(env(map[string]string{})); err == nil {
		t.Fatal("expected an error when HOME is unset")
	}
}

func TestCheckSocketPathLength(t *testing.T) {
	ok := "/tmp/" + strings.Repeat("a", 98)
	if len(ok) != 103 {
		t.Fatalf("test setup: len = %d", len(ok))
	}
	if err := CheckSocketPath(ok); err != nil {
		t.Errorf("103-byte path should be accepted: %v", err)
	}
	long := ok + "b"
	err := CheckSocketPath(long)
	if err == nil {
		t.Fatal("104-byte path should be refused")
	}
	if !strings.Contains(err.Error(), "too long") || !strings.Contains(err.Error(), long) {
		t.Errorf("error should say too long and print the path: %v", err)
	}
	if !errors.Is(err, ErrSocketPathTooLong) {
		t.Errorf("error %v does not carry ErrSocketPathTooLong, which is what a client tells it apart by", err)
	}
}

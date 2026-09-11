package credential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// fileMode is what a credential file is created with: the secrets in it are
// the user's own, and nobody else's business.
const fileMode fs.FileMode = 0o600

// File keeps secrets in one JSON file only its owner can read. It is what Owl
// uses where there is no OS keychain to drive, and what the tests use so that
// no test touches a real keychain.
type File struct {
	path string
	// mu serialises the read-modify-write below: one daemon owns the file, so
	// a lock in this process is the whole story.
	mu sync.Mutex
}

// NewFile returns a Store keeping secrets in the file at path. The file is
// made when the first secret is written.
func NewFile(path string) *File { return &File{path: path} }

// Name says where the secrets are, which the daemon logs at startup.
func (f *File) Name() string { return string(KindFile) + " " + f.path }

// Set writes a secret under ref.
func (f *File) Set(_ context.Context, ref, secret string) error {
	if err := checkRef(ref); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	held, err := f.read()
	if err != nil {
		return err
	}
	held[ref] = secret
	return f.write(held)
}

// Get reads one back, or ErrNotFound.
func (f *File) Get(_ context.Context, ref string) (string, error) {
	if err := checkRef(ref); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	held, err := f.read()
	if err != nil {
		return "", err
	}
	secret, ok := held[ref]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	return secret, nil
}

// Delete removes one, and does not mind one that was never there.
func (f *File) Delete(_ context.Context, ref string) error {
	if err := checkRef(ref); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	held, err := f.read()
	if err != nil {
		return err
	}
	if _, ok := held[ref]; !ok {
		return nil
	}
	delete(held, ref)
	return f.write(held)
}

// read returns what the file holds, and an empty set when there is no file
// yet: a store nobody has written to holds nothing.
func (f *File) read() (map[string]string, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the credential file: %w", err)
	}
	held := map[string]string{}
	if err := json.Unmarshal(data, &held); err != nil {
		return nil, fmt.Errorf("the credential file at %s is unreadable: %w", f.path, err)
	}
	return held, nil
}

// write replaces the file, through a temporary one in the same directory, so
// that an interrupted write cannot leave a user with half their credentials.
// The temporary file is created with the same mode as the real one, because
// for the moment it exists it holds exactly the same secrets.
func (f *File) write(held map[string]string) error {
	data, err := json.Marshal(held)
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}

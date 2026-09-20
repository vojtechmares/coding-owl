package project

// Writing the one setting a Project cannot run without (issue #126).
//
// A Project that names no Account cannot run a single Job (ADR-0023), and the
// only way to give it one was to know that a configuration file exists, where
// ADR-0014 looks for one, and what to write in it. `owl project setup` asks
// the two questions instead, and this is what it writes when they are
// answered.
//
// It only ever creates a file. A Project that already has one is refused
// rather than rewritten: what else that file carries is the user's, and a
// setup command is not the thing to merge into it.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/config"
)

// inRepoSetupCandidate is the file `owl project setup` writes in a
// repository: the first of ADR-0014's in-repo forms, which is the one a reader
// of the repository will look for.
const inRepoSetupCandidate = ".coding-owl.yaml"

// SetupRequest is what `owl project setup` carries: which Project, which
// Account it runs on, and which of ADR-0014's two homes its file goes in.
type SetupRequest struct {
	// Project is the Project by name, or a path inside one. Empty falls back
	// to WorkingDir.
	Project string
	// WorkingDir is the directory owl was run in, used when Project names
	// neither a Project nor a path.
	WorkingDir string
	// Account is the Account its Jobs will run on.
	Account string
	// InRepo asks for the committed form rather than the config-home
	// fallback.
	InRepo bool
}

// ConfigFile is the file setup wrote, and whether it is one the user has to
// commit before it means anything.
type ConfigFile struct {
	// Path is where it is on disk.
	Path string
	// InRepo is true for a file in the Project's repository, which a Run
	// reads from the base branch and not from the working tree (ADR-0014), so
	// it does nothing until it is committed.
	InRepo bool
}

// Setup writes a Project's first configuration file: its apiVersion and the
// Account it runs on, and nothing else. What the rest of a file can carry is
// documented rather than asked about, because a setup command that guessed at
// a project's checks would be wrong for something (ADR-0014 considered and
// rejected detecting them).
func (s *Service) Setup(ctx context.Context, req SetupRequest) (ConfigFile, error) {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return ConfigFile{}, invalid("no account was named for the project to run on")
	}
	name, err := s.resolveSetupProject(ctx, req)
	if err != nil {
		return ConfigFile{}, err
	}
	d, err := s.Show(ctx, name)
	if err != nil {
		return ConfigFile{}, err
	}
	// Refused rather than rewritten. Discovery is what decides, so a file in
	// any of ADR-0014's places counts, not only the one setup would write.
	if d.ConfigSource != "" {
		return ConfigFile{}, invalid(
			"project %s is already configured by %s; edit that file rather than having setup overwrite it",
			name, d.ConfigSource)
	}
	path := s.configPath(name)
	if req.InRepo {
		path = filepath.Join(d.Path, inRepoSetupCandidate)
	}
	if err := writeFirstConfig(path, account); err != nil {
		return ConfigFile{}, err
	}
	return ConfigFile{Path: path, InRepo: req.InRepo}, nil
}

// resolveSetupProject reads which Project the request is for: the one it names,
// the one whose directory holds the path it names, or the one the caller ran
// in. Named first, because a Project's name is what every other project
// command takes.
func (s *Service) resolveSetupProject(ctx context.Context, req SetupRequest) (string, error) {
	given := strings.TrimSpace(req.Project)
	if given != "" {
		if p, err := s.store.GetProject(ctx, given); err == nil {
			return p.Name, nil
		}
	}
	dir := given
	if dir == "" {
		dir = req.WorkingDir
	}
	if dir == "" {
		return "", invalid("no project was named and there is no working directory to take one from")
	}
	projects, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	if p, ok := containingProject(dir, projects); ok {
		return p.Name, nil
	}
	if given != "" {
		return "", invalid(
			"no project is named %q, and %s is not inside one either; owl project list says what there is",
			given, dir)
	}
	return "", invalid(
		"%s is not inside a registered project; run owl project setup from a project directory, or name one",
		dir)
}

// writeFirstConfig writes a configuration file carrying the apiVersion and the
// Account. It refuses to write one that is already there, so that a file
// discovery somehow missed is still not overwritten, and it refuses to write
// through a symlink, because a Project's own directory is one an Agent works
// in and a link there points wherever it says (the rule
// internal/skill.notThroughALink keeps for the same files).
func writeFirstConfig(path, account string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return invalid("%s is a symlink; setup will not write through one", path)
		}
		return invalid("%s already exists; setup will not overwrite it", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("apiVersion: %s\naccount: %s\n", config.APIVersion, account)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

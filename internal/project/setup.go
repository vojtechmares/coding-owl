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

	accountpkg "github.com/vojtechmares/coding-owl/internal/account"
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
	// Path is Project made absolute by the caller, for when it names a path
	// rather than a Project. Only the caller knows what a relative path is
	// relative to, so the daemon resolves none of it itself.
	Path string
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
	// The name is written into a YAML file, so it is held to the same shape
	// an Account's name is held to everywhere else rather than taken as
	// written: a caller reaching this without going through the picker does
	// not get to decide what else the file says.
	if err := accountpkg.CheckName(account); err != nil {
		return ConfigFile{}, invalid("%s", err)
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
	if err := writeFirstConfig(path, account, req.InRepo); err != nil {
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
	// The caller's own absolute form of the argument, because a relative path
	// means nothing to a daemon that was started somewhere else.
	dir := strings.TrimSpace(req.Path)
	if given == "" {
		dir = req.WorkingDir
	}
	if dir == "" {
		if given != "" {
			return "", invalid(
				"no project is named %q, and no path came with it to look for one at; owl project list says what there is",
				given)
		}
		return "", invalid("no project was named and there is no working directory to take one from")
	}
	// A path that is not there is not a path. Without this, a mistyped name
	// typed inside a Project reads as a path under it, which every containing
	// test then matches - and setup would quietly configure whichever Project
	// the user happened to be standing in rather than saying it had never
	// heard of what they asked for.
	if _, err := os.Stat(dir); err != nil {
		if given != "" {
			return "", invalid(
				"no project is named %q, and there is nothing at %s to find one from; owl project list says what there is",
				given, dir)
		}
		return "", invalid("%s is not a directory to look for a project in", dir)
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
// Account. It creates the file or it fails: O_EXCL refuses one that is already
// there, so a file discovery could not see is still not overwritten, and
// refuses a symlink, dangling or not, because a Project's own directory is one
// an Agent works in and a link there points wherever it says (the rule
// internal/skill.notThroughALink keeps for the same files). Asking and writing
// in one call is what keeps the two from being decided at different moments.
func writeFirstConfig(path, account string, inRepo bool) error {
	// A file in the repository is meant to be read by whoever clones it; one
	// in the configuration home is Owl's own, and everything else Owl keeps
	// there is the user's alone (the modes internal/project's own
	// moveConfigDir, internal/store and internal/account use).
	dirMode, fileMode := os.FileMode(0o700), os.FileMode(0o600)
	if inRepo {
		dirMode, fileMode = 0o755, 0o644
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if errors.Is(err, os.ErrExist) {
		return invalid("%s is already there; setup will not overwrite it", path)
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	body := fmt.Sprintf("apiVersion: %s\naccount: %s\n", config.APIVersion, account)
	if _, err := f.WriteString(body); err == nil {
		err = f.Close()
		if err == nil {
			return nil
		}
	} else {
		_ = f.Close()
	}
	// The file this call created is taken away again, so that a write that
	// failed leaves the place as it found it. Left behind, the empty file
	// would meet the next run's O_EXCL and be reported as one somebody else
	// had put there.
	_ = os.Remove(path)
	return fmt.Errorf("writing %s: %w", path, err)
}

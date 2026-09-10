// Package project registers git repositories as Projects and answers what
// each one is configured to do. It owns the discovery order of ADR-0014 and
// the name-is-identity rule of ADR-0031.
package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// configFileName is the per-Project fallback file under the config home.
const configFileName = "config.yaml"

// sepChars are the bytes a Project name may not contain, because the name is
// used verbatim as a directory name under the config home.
const sepChars = `/\` + "\x00"

// inRepoCandidates is the discovery order for the in-repo forms of ADR-0014.
// First match wins.
var inRepoCandidates = []string{
	".coding-owl.yaml",
	".config/coding-owl.yaml",
	".config/.coding-owl.yaml",
	".meta/coding-owl.yaml",
	".meta/.coding-owl.yaml",
}

// InvalidError marks a failure the user can fix by asking for something
// different: a path that is not a repository, a name that cannot be a
// directory, a branch that does not exist, a configuration file Owl will not
// load. Everything else is Owl's problem, not theirs.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }

// invalid builds an InvalidError, keeping the message the caller wrote.
func invalid(format string, a ...any) error {
	return &InvalidError{Err: fmt.Errorf(format, a...)}
}

// ConflictError marks a name that is already in use. Names are the identity
// of a Project (ADR-0031), so a collision is a conflict, not bad input.
type ConflictError struct{ Err error }

func (e *ConflictError) Error() string { return e.Err.Error() }
func (e *ConflictError) Unwrap() error { return e.Err }

func conflict(format string, a ...any) error {
	return &ConflictError{Err: fmt.Errorf(format, a...)}
}

// Service is the Project half of the daemon's state.
type Service struct {
	store      *store.Store
	configHome string
	now        func() time.Time
}

// NewService returns a Service storing Projects in st and per-Project
// configuration directories under configHome, which is
// `<XDG_CONFIG_HOME>/coding-owl`.
func NewService(st *store.Store, configHome string) *Service {
	return &Service{store: st, configHome: configHome, now: time.Now}
}

// Project is a registered repository.
type Project struct {
	Name       string
	Path       string
	BaseBranch string
	Registered time.Time
}

// Details is a Project together with the configuration in force for it and
// where that configuration was read from.
type Details struct {
	Project
	// ConfigSource names the file the configuration came from: `<branch>:<path>`
	// for an in-repo form, an absolute path for the config-home fallback, and
	// empty when no file was found.
	ConfigSource string
	Config       config.Config
}

// AddRequest is what owl project add carries.
type AddRequest struct {
	// Path is the repository root to register.
	Path string
	// Name overrides the directory basename when set.
	Name string
	// BaseBranch overrides the repository's current branch when set.
	BaseBranch string
}

// Add registers a repository as a Project.
func (s *Service) Add(ctx context.Context, req AddRequest) (Project, error) {
	path, err := repoRoot(req.Path)
	if err != nil {
		return Project{}, err
	}
	name := req.Name
	if name == "" {
		name = filepath.Base(path)
	}
	if err := validateName(name); err != nil {
		return Project{}, err
	}
	base := req.BaseBranch
	if base == "" {
		if base, err = git.CurrentBranch(path); err != nil {
			return Project{}, &InvalidError{Err: err}
		}
	}
	// The base branch is where Jobs branch from and where configuration is
	// read (ADR-0007, ADR-0014), so a Project without one is not registrable.
	ok, err := git.HasBranch(path, base)
	if err != nil {
		return Project{}, err
	}
	if !ok {
		return Project{}, invalid("%s has no branch %q", path, base)
	}
	p := Project{Name: name, Path: path, BaseBranch: base, Registered: s.now().UTC()}
	if err := s.store.AddProject(ctx, store.Project(p)); err != nil {
		if errors.Is(err, store.ErrNameTaken) {
			return Project{}, conflict("a project named %q is already registered; pass --name to choose another", name)
		}
		return Project{}, err
	}
	return p, nil
}

// Show returns a Project with its effective configuration.
func (s *Service) Show(ctx context.Context, name string) (Details, error) {
	p, err := s.store.GetProject(ctx, name)
	if err != nil {
		return Details{}, err
	}
	source, cfg, err := s.discover(Project(p))
	if err != nil {
		return Details{}, err
	}
	return Details{Project: Project(p), ConfigSource: source, Config: cfg}, nil
}

// discover walks the ADR-0014 order and returns the first configuration file
// it finds, or the defaults when there is none. In-repo forms are read from
// the Project's base branch, never from a working tree.
func (s *Service) discover(p Project) (string, config.Config, error) {
	hasBase, err := git.HasBranch(p.Path, p.BaseBranch)
	if err != nil {
		// The path came from the caller's own registration, so a repository
		// that has moved or stopped being one is theirs to fix.
		return "", config.Config{}, &InvalidError{Err: err}
	}
	if !hasBase {
		// Falling through to the defaults here would let a Project's
		// configuration stop applying without anyone being told, which is
		// exactly what reading from the base branch is meant to prevent.
		return "", config.Config{}, invalid("%s has no branch %q, so no configuration can be read for project %s", p.Path, p.BaseBranch, p.Name)
	}
	for _, candidate := range inRepoCandidates {
		data, found, err := git.ShowFile(p.Path, p.BaseBranch, candidate)
		if err != nil {
			return "", config.Config{}, &InvalidError{Err: err}
		}
		if !found {
			continue
		}
		source := p.BaseBranch + ":" + candidate
		cfg, err := config.Parse(source, data)
		if err != nil {
			return "", config.Config{}, &InvalidError{Err: err}
		}
		return source, cfg, nil
	}
	fallback := s.configPath(p.Name)
	data, err := os.ReadFile(fallback)
	if err == nil {
		cfg, err := config.Parse(fallback, data)
		if err != nil {
			return "", config.Config{}, &InvalidError{Err: err}
		}
		return fallback, cfg, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", config.Config{}, fmt.Errorf("reading %s: %w", fallback, err)
	}
	return "", config.Default(), nil
}

// configDir is the Project's configuration directory under the config home.
func (s *Service) configDir(name string) string { return filepath.Join(s.configHome, name) }

// configPath is the per-Project fallback configuration file.
func (s *Service) configPath(name string) string {
	return filepath.Join(s.configDir(name), configFileName)
}

// repoRoot resolves path and checks it is the root of a git repository.
func repoRoot(path string) (string, error) {
	if path == "" {
		return "", invalid("a path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	switch st, err := os.Stat(abs); {
	case errors.Is(err, os.ErrNotExist):
		return "", invalid("no such directory: %s", abs)
	case err != nil:
		return "", &InvalidError{Err: err}
	case !st.IsDir():
		return "", invalid("not a directory: %s", abs)
	}
	root, err := git.Root(abs)
	if err != nil {
		return "", &InvalidError{Err: err}
	}
	// Root resolves symlinks, so compare like with like before deciding the
	// caller pointed at a subdirectory - and report both paths in the resolved
	// form, so the one to register is not a different spelling of the one
	// that was refused.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolved = abs
	}
	if resolved != root {
		return "", invalid("%s is not the root of its git repository; register %s instead", resolved, root)
	}
	return abs, nil
}

// validateName refuses names that could not safely name a directory under the
// config home, since that is exactly what a Project name does (ADR-0031).
func validateName(name string) error {
	switch {
	case name == "":
		return invalid("invalid project name: it is empty")
	case name == "." || name == "..":
		return invalid("invalid project name %q: it is a directory reference", name)
	case strings.ContainsAny(name, sepChars):
		return invalid("invalid project name %q: it may not contain a path separator", name)
	case strings.HasPrefix(name, "-"):
		return invalid("invalid project name %q: it may not start with a dash", name)
	case strings.TrimSpace(name) != name:
		return invalid("invalid project name %q: it has leading or trailing whitespace", name)
	}
	return nil
}

// List returns every Project, ordered by name.
func (s *Service) List(ctx context.Context) ([]Project, error) {
	rows, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(rows))
	for _, r := range rows {
		out = append(out, Project(r))
	}
	return out, nil
}

// Move points a Project at a new path. Its name, and everything attached to
// that name, is unaffected (ADR-0031).
func (s *Service) Move(ctx context.Context, name, path string) (Project, error) {
	p, err := s.store.GetProject(ctx, name)
	if err != nil {
		return Project{}, err
	}
	root, err := repoRoot(path)
	if err != nil {
		return Project{}, err
	}
	// The Project keeps its base branch across a move, so the repository at
	// the new path has to carry it or nothing could be read there.
	ok, err := git.HasBranch(root, p.BaseBranch)
	if err != nil {
		return Project{}, &InvalidError{Err: err}
	}
	if !ok {
		return Project{}, invalid("%s has no branch %q, which is project %s's base branch", root, p.BaseBranch, name)
	}
	if err := s.store.SetProjectPath(ctx, name, root); err != nil {
		return Project{}, err
	}
	p.Path = root
	return Project(p), nil
}

// Rename changes a Project's name and moves its configuration directory with
// it. Either both happen or neither does: the directory is moved first and
// moved back if the database refuses the new name.
func (s *Service) Rename(ctx context.Context, from, to string) (Project, error) {
	if err := validateName(to); err != nil {
		return Project{}, err
	}
	p, err := s.store.GetProject(ctx, from)
	if err != nil {
		return Project{}, err
	}
	if _, err := s.store.GetProject(ctx, to); err == nil {
		return Project{}, conflict("a project named %q is already registered", to)
	} else if !errors.Is(err, store.ErrNotFound) {
		return Project{}, err
	}

	undo, err := s.moveConfigDir(from, to)
	if err != nil {
		return Project{}, err
	}
	if err := s.store.RenameProject(ctx, from, to); err != nil {
		if undo != nil {
			// Put the directory back, so a refused rename leaves nothing
			// behind. Losing the race to a concurrent add lands here.
			if undoErr := undo(); undoErr != nil {
				return Project{}, fmt.Errorf("%w (and the configuration directory was left at %s: %v)", err, s.configDir(to), undoErr)
			}
		}
		if errors.Is(err, store.ErrNameTaken) {
			return Project{}, conflict("a project named %q is already registered", to)
		}
		return Project{}, err
	}
	p.Name = to
	return Project(p), nil
}

// moveConfigDir moves the per-Project configuration directory and returns the
// action that puts it back. undo is nil when there was no directory to move.
// It refuses to overwrite an existing destination.
func (s *Service) moveConfigDir(from, to string) (undo func() error, err error) {
	src, dst := s.configDir(from), s.configDir(to)
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dst); err == nil {
		return nil, invalid("%s already exists; move or remove it before renaming", dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return nil, err
	}
	if err := os.Rename(src, dst); err != nil {
		return nil, fmt.Errorf("moving %s to %s: %w", src, dst, err)
	}
	return func() error { return os.Rename(dst, src) }, nil
}

// Remove deregisters a Project. Its configuration directory is left on disk:
// it is hand-written, and nothing else can put it back.
func (s *Service) Remove(ctx context.Context, name string) error {
	return s.store.RemoveProject(ctx, name)
}

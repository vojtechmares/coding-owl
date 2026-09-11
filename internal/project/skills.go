package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/skill"
)

// Skill is one of a Project's Skills as the daemon reports it: what the
// manifest declares, and what the lockfile records it resolved to.
type Skill struct {
	Name       string
	Source     string
	Ref        string
	Commit     string
	Digest     string
	AutoUpdate bool
}

// SkillRequest names the Project a Skills command is about: by name, or by the
// directory the caller ran in.
type SkillRequest struct {
	Project    string
	WorkingDir string
}

// SkillService manages a Project's Skills: what it declares, what that resolved
// to, and the two files that say so (ADR-0024, ADR-0033).
//
// It writes the manifest and the lockfile where the Project's configuration
// was found. For a Project configured in its own repository that is the user's
// own checkout, and committing them is theirs to do: the daemon reads the base
// branch, so a Skill only takes effect once it is committed.
type SkillService struct {
	projects *Service
	skills   *skill.Service
}

// NewSkillService returns a SkillService over a Project service and a Skill
// service.
func NewSkillService(projects *Service, skills *skill.Service) *SkillService {
	return &SkillService{projects: projects, skills: skills}
}

// Add resolves a source at a ref, fetches it, and records it in both files.
func (s *SkillService) Add(ctx context.Context, req SkillRequest, source, ref string, autoUpdate bool) (Skill, Files, error) {
	name, err := s.resolveProject(ctx, req)
	if err != nil {
		return Skill{}, Files{}, err
	}
	d, files, err := s.declared(ctx, name)
	if err != nil {
		return Skill{}, Files{}, err
	}
	got, added, err := s.skills.Add(ctx, source, ref, autoUpdate)
	if err != nil {
		return Skill{}, Files{}, err
	}
	if slices.ContainsFunc(d, func(x skill.Declared) bool { return x.Name() == added.Name() }) {
		return Skill{}, Files{}, &InvalidError{Err: fmt.Errorf(
			"project %s already has a skill called %s; remove it first, or add one under another name",
			name, added.Name())}
	}
	lock, err := skill.ReadLock(files.Lock)
	if err != nil {
		return Skill{}, Files{}, err
	}
	if lock.Skills == nil {
		lock.Skills = map[string]skill.Locked{}
	}
	lock.Skills[got.Name] = got.Locked
	if err := s.write(files, append(d, added), lock); err != nil {
		return Skill{}, Files{}, err
	}
	return asSkill(added, got.Locked), files, nil
}

// List is what the Project declares, with what each resolved to.
func (s *SkillService) List(ctx context.Context, req SkillRequest) ([]Skill, Files, error) {
	name, err := s.resolveProject(ctx, req)
	if err != nil {
		return nil, Files{}, err
	}
	d, files, err := s.declared(ctx, name)
	if err != nil {
		return nil, Files{}, err
	}
	lock, err := skill.ReadLock(files.Lock)
	if err != nil {
		return nil, Files{}, err
	}
	out := make([]Skill, 0, len(d))
	for _, declared := range d {
		out = append(out, asSkill(declared, lock.Skills[declared.Name()]))
	}
	return out, files, nil
}

// Remove takes a Skill out of both files.
func (s *SkillService) Remove(ctx context.Context, req SkillRequest, name string) (Skill, Files, error) {
	projectName, err := s.resolveProject(ctx, req)
	if err != nil {
		return Skill{}, Files{}, err
	}
	d, files, err := s.declared(ctx, projectName)
	if err != nil {
		return Skill{}, Files{}, err
	}
	at := slices.IndexFunc(d, func(x skill.Declared) bool { return x.Name() == name })
	if at < 0 {
		return Skill{}, Files{}, &InvalidError{Err: fmt.Errorf(
			"project %s has no skill called %s", projectName, name)}
	}
	lock, err := skill.ReadLock(files.Lock)
	if err != nil {
		return Skill{}, Files{}, err
	}
	gone := asSkill(d[at], lock.Skills[name])
	delete(lock.Skills, name)
	if err := s.write(files, slices.Delete(slices.Clone(d), at, at+1), lock); err != nil {
		return Skill{}, Files{}, err
	}
	return gone, files, nil
}

// Update re-resolves the Skills that may move on their own, and any named
// outright - which is how a pinned Skill is moved deliberately (ADR-0024).
func (s *SkillService) Update(ctx context.Context, req SkillRequest, names []string) (updated, all []Skill, files Files, err error) {
	projectName, err := s.resolveProject(ctx, req)
	if err != nil {
		return nil, nil, Files{}, err
	}
	d, files, err := s.declared(ctx, projectName)
	if err != nil {
		return nil, nil, Files{}, err
	}
	for _, name := range names {
		if !slices.ContainsFunc(d, func(x skill.Declared) bool { return x.Name() == name }) {
			return nil, nil, Files{}, &InvalidError{Err: fmt.Errorf(
				"project %s has no skill called %s", projectName, name)}
		}
	}
	lock, err := skill.ReadLock(files.Lock)
	if err != nil {
		return nil, nil, Files{}, err
	}
	if lock.Skills == nil {
		lock.Skills = map[string]skill.Locked{}
	}
	for _, declared := range d {
		name := declared.Name()
		was := lock.Skills[name]
		// A Skill nobody named is only re-resolved when it asks to move on its
		// own; pinning is the default, and an update is otherwise deliberate.
		if len(names) > 0 && !slices.Contains(names, name) {
			all = append(all, asSkill(declared, was))
			continue
		}
		if len(names) == 0 && !declared.AutoUpdate && was.Commit != "" {
			all = append(all, asSkill(declared, was))
			continue
		}
		got, _, err := s.skills.Add(ctx, declared.Source, declared.Ref, declared.AutoUpdate)
		if err != nil {
			return nil, nil, Files{}, err
		}
		lock.Skills[name] = got.Locked
		all = append(all, asSkill(declared, got.Locked))
		if was.Commit != got.Commit {
			updated = append(updated, asSkill(declared, got.Locked))
		}
	}
	if err := s.write(files, d, lock); err != nil {
		return nil, nil, Files{}, err
	}
	return updated, all, files, nil
}

// declared is what a Project's configuration declares, and where the files
// that declare it are.
func (s *SkillService) declared(ctx context.Context, name string) ([]skill.Declared, Files, error) {
	d, err := s.projects.Show(ctx, name)
	if err != nil {
		return nil, Files{}, err
	}
	files, err := s.projects.FilesFor(ctx, name)
	if err != nil {
		return nil, Files{}, err
	}
	// What the manifest on disk says, rather than what the base branch says:
	// these commands edit the working copies, and two `owl skills add` in a
	// row must not lose the first.
	declared, err := declaredOnDisk(files.Manifest)
	if err != nil {
		return nil, Files{}, err
	}
	if declared == nil {
		if files.InRepo {
			// The Project is configured in its own repository and that file is
			// not in the checkout: another branch, a rebase in flight, or a
			// deletion. Writing one here would be a manifest carrying the
			// skills and none of the checks, setup or account the Project has.
			return nil, Files{}, &InvalidError{Err: fmt.Errorf(
				"%s configures project %s, and it is not in your checkout; "+
					"check out the branch that carries it, or restore it, and run this again",
				files.Manifest, name)}
		}
		declared = skill.Declare(d.Config)
	}
	return declared, files, nil
}

// declaredOnDisk reads the `skills` of a manifest as it stands on disk, and is
// nil for a file that is not there.
func declaredOnDisk(path string) ([]skill.Declared, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := config.Parse(path, data)
	if err != nil {
		return nil, err
	}
	out := skill.Declare(cfg)
	if out == nil {
		// A file that declares no Skills is still a file: an empty list rather
		// than nothing at all, so the caller does not fall back on the base
		// branch.
		return []skill.Declared{}, nil
	}
	return out, nil
}

// write puts the manifest and the lockfile where they belong.
func (s *SkillService) write(files Files, declared []skill.Declared, lock skill.Lock) error {
	if err := skill.WriteManifest(files.Manifest, declared); err != nil {
		return err
	}
	return skill.WriteLock(files.Lock, lock)
}

// resolveProject names the Project a request is about: the one it names, or
// the one its working directory sits in.
func (s *SkillService) resolveProject(ctx context.Context, req SkillRequest) (string, error) {
	if strings.TrimSpace(req.Project) != "" {
		p, err := s.projects.Show(ctx, req.Project)
		if err != nil {
			return "", err
		}
		return p.Name, nil
	}
	if req.WorkingDir == "" {
		return "", &InvalidError{Err: fmt.Errorf(
			"no project was named and there is no working directory to take one from; pass --project")}
	}
	projects, err := s.projects.List(ctx)
	if err != nil {
		return "", err
	}
	p, ok := containingProject(req.WorkingDir, projects)
	if !ok {
		return "", &InvalidError{Err: fmt.Errorf(
			"%s is not inside a registered project; run owl skills from a project directory, or name one with --project",
			req.WorkingDir)}
	}
	return p.Name, nil
}

// containingProject is the Project whose directory holds dir - the innermost
// one, when a Project is registered inside another.
func containingProject(dir string, projects []Project) (Project, bool) {
	target := resolvePath(dir)
	var best Project
	var bestRoot string
	var found bool
	for _, p := range projects {
		root := resolvePath(p.Path)
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			continue
		}
		if !found || len(root) > len(bestRoot) {
			best, bestRoot, found = p, root, true
		}
	}
	return best, found
}

func resolvePath(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(path)
}

// asSkill is one Skill as the daemon reports it: what the manifest declares,
// and what the lock records for it.
func asSkill(d skill.Declared, locked skill.Locked) Skill {
	return Skill{
		Name:       d.Name(),
		Source:     d.Source,
		Ref:        d.Ref,
		Commit:     locked.Commit,
		Digest:     locked.Digest,
		AutoUpdate: d.AutoUpdate,
	}
}

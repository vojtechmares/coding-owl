package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/git"
)

// excludesName is the exclude file Owl owns for one worktree, kept beside the
// worktree rather than in it so that it is not something an Agent can edit.
const excludesName = "excludes"

// Site is where a placement happens: the repository the worktree belongs to,
// the worktree itself, the Driver's own relative skills directory, the
// directory Owl owns for this worktree, and the cache the Skills are in.
type Site struct {
	// Repo is the Project's own repository, which the worktree belongs to.
	Repo string
	// Worktree is the Job's worktree, and the only place anything is written.
	Worktree string
	// SkillsDir is the Driver's own relative directory, `.claude/skills` for
	// Claude Code.
	SkillsDir string
	// OwnedDir is where Owl keeps the exclude file for this worktree.
	OwnedDir string
	// Cache is where fetched Skills live. An entry in the skills directory is
	// Owl's to replace when it is a link into it, and nobody else's.
	Cache string
}

// Placement is what a Run placed, so that it can be recorded and reported.
type Placement struct {
	// Skills are the Skills as they were placed.
	Skills []Resolved
	// Dir is the Driver's skills directory inside the worktree.
	Dir string
}

// Place puts the Skills in the Driver's skills directory inside a worktree and
// hides that directory from git, leaving the user's own checkout alone.
//
// An entry Owl did not create is never overwritten: a repository that keeps
// skills of its own at that path still works, and Owl says so rather than
// taking it (ADR-0033). What Owl created is a symlink into its own cache, so
// that is what it recognises - a name is not evidence, because the base branch
// and the Agent can both put anything at one.
func Place(site Site, skills []Resolved) (Placement, error) {
	into, err := skillsIn(site.Worktree, site.SkillsDir)
	if err != nil {
		return Placement{}, err
	}

	wanted := map[string]bool{}
	for _, s := range skills {
		wanted[s.Name] = true
		link := filepath.Join(into, s.Name)
		owned, err := ours(link, site.Cache)
		if err != nil {
			return Placement{}, err
		}
		if !owned {
			return Placement{}, invalid(
				"%s is already there and Owl did not create it; remove it, or rename the skill, and Owl will leave it alone",
				link)
		}
	}
	// Nothing is written until every entry has been found to be Owl's: a Run
	// that is going to be refused leaves the worktree exactly as it was.
	if err := hide(site); err != nil {
		return Placement{}, err
	}
	if err := os.MkdirAll(into, 0o755); err != nil {
		return Placement{}, fmt.Errorf("creating %s: %w", into, err)
	}
	for _, s := range skills {
		if err := relink(filepath.Join(into, s.Name), s.Path); err != nil {
			return Placement{}, err
		}
	}
	// A Skill the Project no longer declares goes, so that a worktree carries
	// what the manifest says and nothing else. Only Owl's own links go: an
	// Agent's work under a name Owl once used is the Agent's.
	stale, err := placedIn(into, site.Cache)
	if err != nil {
		return Placement{}, err
	}
	for _, name := range stale {
		if wanted[name] {
			continue
		}
		if err := os.Remove(filepath.Join(into, name)); err != nil {
			return Placement{}, err
		}
	}
	return Placement{Skills: skills, Dir: into}, nil
}

// ours reports whether a path is Owl's to replace: nothing there at all, or a
// link into the cache, which is the only thing a placement makes.
func ours(link, cache string) (bool, error) {
	info, err := os.Lstat(link)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return placedLink(link, info, cache)
}

// placedLink reports whether an entry is one a placement made: a symlink whose
// target is inside the cache. A dangling one still counts - the cache can be
// reclaimed under a worktree, and what the link says is what it was made for.
func placedLink(link string, info fs.FileInfo, cache string) (bool, error) {
	if info.Mode()&fs.ModeSymlink == 0 {
		return false, nil
	}
	target, err := os.Readlink(link)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	return within(cache, target), nil
}

// placedIn is every entry of a skills directory that a placement made.
func placedIn(into, cache string) ([]string, error) {
	entries, err := os.ReadDir(into)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		placed, err := placedLink(filepath.Join(into, e.Name()), info, cache)
		if err != nil {
			return nil, err
		}
		if placed {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// relink points a name at a cached Skill, replacing a link a previous
// placement left. The link is made beside its place and moved onto it, so that
// a worktree never has a moment with no Skill where one should be.
func relink(link, target string) error {
	staged := link + ".owl-linking"
	_ = os.RemoveAll(staged)
	if err := os.Symlink(target, staged); err != nil {
		return fmt.Errorf("linking %s: %w", link, err)
	}
	if err := os.RemoveAll(link); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	if err := os.Rename(staged, link); err != nil {
		_ = os.RemoveAll(staged)
		return fmt.Errorf("linking %s: %w", link, err)
	}
	return nil
}

// skillsIn is the Driver's skills directory inside a worktree, checked to be
// really inside it. A path that resolves outside - through a symlink the base
// branch carries, or one an Agent left in an earlier Run - is refused:
// everything below this point writes and deletes.
//
// Nothing is created here. The check has to happen before any directory is
// made, or the making is itself the write through the symlink.
func skillsIn(worktree, skillsDir string) (string, error) {
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", err
	}
	into := filepath.Join(worktree, filepath.FromSlash(skillsDir))
	real, err := resolved(into)
	if err != nil {
		return "", err
	}
	if !within(root, real) {
		return "", invalid(
			"%s leads to %s, which is outside the job's worktree; Owl will not place skills through it",
			into, real)
	}
	return into, nil
}

// resolved is a path with every symlink along the part of it that exists
// followed, so that a path nothing has created yet can still be judged by
// where it would be created.
func resolved(path string) (string, error) {
	rest := ""
	for {
		real, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent, name := filepath.Dir(path), filepath.Base(path)
		if parent == path {
			return "", err
		}
		path, rest = parent, filepath.Join(name, rest)
	}
}

// within reports whether a path is root or inside it.
func within(root, path string) bool {
	root, path = filepath.Clean(root), filepath.Clean(path)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// hide keeps the Skills out of the review diff: per-worktree configuration is
// enabled on the repository, and this worktree alone is pointed at an exclude
// file of Owl's that carries forward whatever the repository was already told
// (ADR-0033).
func hide(site Site) error {
	if err := git.EnableWorktreeConfig(site.Repo); err != nil {
		return err
	}
	theirs, err := git.GlobalExcludes(site.Repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(site.OwnedDir, dirMode); err != nil {
		return err
	}
	excludes := filepath.Join(site.OwnedDir, excludesName)
	body := "# Written by Owl. The skills it places are not the job's work.\n"
	if theirs != "" {
		// Setting this shadows whatever the user had, so their file is read
		// from here rather than silently stopping to apply.
		body += "# What this repository was already told to ignore:\n" +
			"# " + theirs + "\n"
	}
	body += "/" + strings.Trim(site.SkillsDir, "/") + "/\n"
	if theirs != "" {
		data, err := git.ReadExcludes(theirs)
		if err != nil {
			return err
		}
		if data != "" {
			body += "\n" + data
		}
	}
	if err := os.WriteFile(excludes, []byte(body), 0o600); err != nil {
		return err
	}
	return git.SetWorktreeExcludes(site.Worktree, excludes)
}

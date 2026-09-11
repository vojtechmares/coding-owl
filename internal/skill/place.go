package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/git"
)

// markerName is the file Owl leaves beside the Skills it placed, naming them.
// It is what lets a later Run tell an entry Owl made from one the repository
// carries itself, which Owl will not touch (ADR-0033).
const markerName = ".owl-skills"

// excludesName is the exclude file Owl owns for one worktree, kept beside the
// worktree rather than in it so that it is not something an Agent can edit.
const excludesName = "excludes"

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
// skillsDir is the Driver's own relative directory, `.claude/skills` for Claude
// Code. ownedDir is where Owl keeps the exclude file for this worktree.
//
// An entry Owl did not create is never overwritten: a repository that keeps
// skills of its own at that path still works, and Owl says so rather than
// taking it (ADR-0033).
func Place(repo, worktree, skillsDir, ownedDir string, skills []Resolved) (Placement, error) {
	into := filepath.Join(worktree, filepath.FromSlash(skillsDir))
	if err := hide(repo, worktree, skillsDir, ownedDir); err != nil {
		return Placement{}, err
	}
	mine, err := placedHere(into)
	if err != nil {
		return Placement{}, err
	}
	if err := os.MkdirAll(into, 0o755); err != nil {
		return Placement{}, fmt.Errorf("creating %s: %w", into, err)
	}

	wanted := map[string]bool{}
	for _, s := range skills {
		wanted[s.Name] = true
		link := filepath.Join(into, s.Name)
		owned, err := ours(link, mine)
		if err != nil {
			return Placement{}, err
		}
		if !owned {
			return Placement{}, invalid(
				"%s is already there and Owl did not create it; remove it, or rename the skill, and Owl will leave it alone",
				link)
		}
		if err := relink(link, s.Path); err != nil {
			return Placement{}, err
		}
	}
	// A Skill the Project no longer declares goes, so that a worktree carries
	// what the manifest says and nothing else.
	for _, name := range mine {
		if wanted[name] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(into, name)); err != nil {
			return Placement{}, err
		}
	}
	if err := writeMarker(into, skills); err != nil {
		return Placement{}, err
	}
	return Placement{Skills: skills, Dir: into}, nil
}

// ours reports whether a path is Owl's to replace: nothing there at all, or an
// entry a previous placement made.
func ours(link string, mine []string) (bool, error) {
	_, err := os.Lstat(link)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	name := filepath.Base(link)
	for _, placed := range mine {
		if placed == name {
			return true, nil
		}
	}
	return false, nil
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

// placedHere is the Skills a previous placement made in a directory, read from
// the marker Owl leaves. A directory with no marker holds nothing of Owl's.
func placedHere(into string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(into, markerName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ln := range strings.Split(string(data), "\n") {
		if name := strings.TrimSpace(ln); name != "" && !strings.HasPrefix(name, "#") {
			names = append(names, name)
		}
	}
	return names, nil
}

// writeMarker records what this placement made, so the next one knows what is
// Owl's to replace.
func writeMarker(into string, skills []Resolved) error {
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	body := "# Written by Owl: the skills it placed here, which it may replace.\n" +
		strings.Join(names, "\n")
	if len(names) > 0 {
		body += "\n"
	}
	return os.WriteFile(filepath.Join(into, markerName), []byte(body), 0o600)
}

// hide keeps the Skills out of the review diff: per-worktree configuration is
// enabled on the repository, and this worktree alone is pointed at an exclude
// file of Owl's that carries forward whatever the repository was already told
// (ADR-0033).
func hide(repo, worktree, skillsDir, ownedDir string) error {
	if err := git.EnableWorktreeConfig(repo); err != nil {
		return err
	}
	theirs, err := git.GlobalExcludes(repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ownedDir, dirMode); err != nil {
		return err
	}
	excludes := filepath.Join(ownedDir, excludesName)
	body := "# Written by Owl. The skills it places are not the job's work.\n"
	if theirs != "" {
		// Setting this shadows whatever the user had, so their file is read
		// from here rather than silently stopping to apply.
		body += "# What this repository was already told to ignore:\n" +
			"# " + theirs + "\n"
	}
	body += "/" + strings.Trim(skillsDir, "/") + "/\n"
	if theirs != "" {
		data, err := os.ReadFile(theirs)
		if err == nil {
			body += "\n" + string(data)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("reading the exclude file this repository was told to use: %w", err)
		}
	}
	if err := os.WriteFile(excludes, []byte(body), 0o600); err != nil {
		return err
	}
	return git.SetWorktreeExcludes(worktree, excludes)
}

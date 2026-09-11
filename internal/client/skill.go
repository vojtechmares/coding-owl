package client

import (
	"context"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
)

// Skill is one of a Project's Skills as the daemon reports it (ADR-0024).
type Skill struct {
	// Name is what it is called, and the directory it is placed in inside a
	// worktree.
	Name string
	// Source is the repository it comes from.
	Source string
	// Ref is the branch or tag it follows.
	Ref string
	// Commit is what that ref resolved to, empty for one nothing has resolved.
	Commit string
	// Digest is of the content fetched at that commit.
	Digest string
	// AutoUpdate is whether it is re-resolved at the start of every Run. A
	// Skill without it is pinned, which is the default.
	AutoUpdate bool
}

// SkillFiles is where a Project's manifest and lockfile are, so a caller can
// say what to commit.
type SkillFiles struct {
	Manifest string
	Lock     string
	// InRepo is whether they are in the Project's own repository.
	InRepo bool
}

// SkillRequest names the Project a Skills command is about: by name, or by the
// directory the caller ran in.
type SkillRequest struct {
	Project    string
	WorkingDir string
}

// AddSkill resolves a source at a ref, fetches it, and records it in the
// Project's manifest and lockfile.
func (c *Client) AddSkill(ctx context.Context, req SkillRequest, source, ref string, autoUpdate bool) (Skill, SkillFiles, error) {
	res, err := c.skills.AddSkill(ctx, connect.NewRequest(&codingowlv1.AddSkillRequest{
		Project: req.Project, WorkingDir: req.WorkingDir,
		Source: source, Ref: ref, AutoUpdate: autoUpdate,
	}))
	if err != nil {
		return Skill{}, SkillFiles{}, c.wrap(err)
	}
	return skillFromProto(res.Msg.GetSkill()), filesFromProto(res.Msg.GetFiles()), nil
}

// ListSkills is what a Project declares, with what each resolved to.
func (c *Client) ListSkills(ctx context.Context, req SkillRequest) ([]Skill, SkillFiles, error) {
	res, err := c.skills.ListSkills(ctx, connect.NewRequest(&codingowlv1.ListSkillsRequest{
		Project: req.Project, WorkingDir: req.WorkingDir,
	}))
	if err != nil {
		return nil, SkillFiles{}, c.wrap(err)
	}
	return skillsFromProto(res.Msg.GetSkills()), filesFromProto(res.Msg.GetFiles()), nil
}

// RemoveSkill takes a Skill out of both files.
func (c *Client) RemoveSkill(ctx context.Context, req SkillRequest, name string) (Skill, SkillFiles, error) {
	res, err := c.skills.RemoveSkill(ctx, connect.NewRequest(&codingowlv1.RemoveSkillRequest{
		Project: req.Project, WorkingDir: req.WorkingDir, Name: name,
	}))
	if err != nil {
		return Skill{}, SkillFiles{}, c.wrap(err)
	}
	return skillFromProto(res.Msg.GetSkill()), filesFromProto(res.Msg.GetFiles()), nil
}

// UpdateSkills re-resolves the Skills that may move on their own, and any
// named outright. It returns what changed and everything as it now stands.
func (c *Client) UpdateSkills(ctx context.Context, req SkillRequest, names []string) (updated, all []Skill, files SkillFiles, err error) {
	res, err := c.skills.UpdateSkill(ctx, connect.NewRequest(&codingowlv1.UpdateSkillRequest{
		Project: req.Project, WorkingDir: req.WorkingDir, Names: names,
	}))
	if err != nil {
		return nil, nil, SkillFiles{}, c.wrap(err)
	}
	return skillsFromProto(res.Msg.GetUpdated()), skillsFromProto(res.Msg.GetSkills()),
		filesFromProto(res.Msg.GetFiles()), nil
}

func skillFromProto(s *codingowlv1.Skill) Skill {
	return Skill{
		Name:       s.GetName(),
		Source:     s.GetSource(),
		Ref:        s.GetRef(),
		Commit:     s.GetCommit(),
		Digest:     s.GetDigest(),
		AutoUpdate: s.GetAutoUpdate(),
	}
}

func skillsFromProto(skills []*codingowlv1.Skill) []Skill {
	out := make([]Skill, 0, len(skills))
	for _, s := range skills {
		out = append(out, skillFromProto(s))
	}
	return out
}

func filesFromProto(f *codingowlv1.SkillFiles) SkillFiles {
	return SkillFiles{Manifest: f.GetManifest(), Lock: f.GetLock(), InRepo: f.GetInRepo()}
}

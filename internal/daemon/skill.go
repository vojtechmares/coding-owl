package daemon

import (
	"context"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/skill"
)

// skillService exposes a Project's Skills over ConnectRPC. It holds no logic of
// its own; everything lives in internal/skill and internal/project.
type skillService struct {
	codingowlv1connect.UnimplementedSkillServiceHandler
	skills *project.SkillService
}

func (s *skillService) AddSkill(ctx context.Context, req *connect.Request[codingowlv1.AddSkillRequest]) (*connect.Response[codingowlv1.AddSkillResponse], error) {
	got, files, err := s.skills.Add(ctx, project.SkillRequest{
		Project:    req.Msg.GetProject(),
		WorkingDir: req.Msg.GetWorkingDir(),
	}, req.Msg.GetSource(), req.Msg.GetRef(), req.Msg.GetAutoUpdate())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.AddSkillResponse{
		Skill: toSkillProto(got), Files: toFilesProto(files),
	}), nil
}

func (s *skillService) ListSkills(ctx context.Context, req *connect.Request[codingowlv1.ListSkillsRequest]) (*connect.Response[codingowlv1.ListSkillsResponse], error) {
	got, files, err := s.skills.List(ctx, project.SkillRequest{
		Project:    req.Msg.GetProject(),
		WorkingDir: req.Msg.GetWorkingDir(),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.ListSkillsResponse{
		Skills: toSkillsProto(got), Files: toFilesProto(files),
	}), nil
}

func (s *skillService) RemoveSkill(ctx context.Context, req *connect.Request[codingowlv1.RemoveSkillRequest]) (*connect.Response[codingowlv1.RemoveSkillResponse], error) {
	got, files, err := s.skills.Remove(ctx, project.SkillRequest{
		Project:    req.Msg.GetProject(),
		WorkingDir: req.Msg.GetWorkingDir(),
	}, req.Msg.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.RemoveSkillResponse{
		Skill: toSkillProto(got), Files: toFilesProto(files),
	}), nil
}

func (s *skillService) UpdateSkill(ctx context.Context, req *connect.Request[codingowlv1.UpdateSkillRequest]) (*connect.Response[codingowlv1.UpdateSkillResponse], error) {
	updated, all, files, err := s.skills.Update(ctx, project.SkillRequest{
		Project:    req.Msg.GetProject(),
		WorkingDir: req.Msg.GetWorkingDir(),
	}, req.Msg.GetNames())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.UpdateSkillResponse{
		Updated: toSkillsProto(updated), Skills: toSkillsProto(all), Files: toFilesProto(files),
	}), nil
}

func toSkillProto(s project.Skill) *codingowlv1.Skill {
	return &codingowlv1.Skill{
		Name:       s.Name,
		Source:     s.Source,
		Ref:        s.Ref,
		Commit:     s.Commit,
		Digest:     s.Digest,
		AutoUpdate: s.AutoUpdate,
	}
}

func toSkillsProto(skills []project.Skill) []*codingowlv1.Skill {
	out := make([]*codingowlv1.Skill, 0, len(skills))
	for _, s := range skills {
		out = append(out, toSkillProto(s))
	}
	return out
}

func toFilesProto(f project.Files) *codingowlv1.SkillFiles {
	return &codingowlv1.SkillFiles{Manifest: f.Manifest, Lock: f.Lock, InRepo: f.InRepo}
}

// toRunSkillsProto puts what a Run read on the wire.
func toRunSkillsProto(skills []skill.Locked) []*codingowlv1.Skill {
	out := make([]*codingowlv1.Skill, 0, len(skills))
	for _, s := range skills {
		out = append(out, &codingowlv1.Skill{
			Name: s.Name, Source: s.Source, Ref: s.Ref, Commit: s.Commit, Digest: s.Digest,
		})
	}
	return out
}

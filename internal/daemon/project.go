package daemon

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/project"
)

// projectService exposes the Project half of the daemon's state over
// ConnectRPC. It holds no logic of its own; everything lives in
// internal/project.
type projectService struct {
	codingowlv1connect.UnimplementedProjectServiceHandler
	projects *project.Service
}

func (s *projectService) AddProject(ctx context.Context, req *connect.Request[codingowlv1.AddProjectRequest]) (*connect.Response[codingowlv1.AddProjectResponse], error) {
	p, err := s.projects.Add(ctx, project.AddRequest{
		Path:       req.Msg.GetPath(),
		Name:       req.Msg.GetName(),
		BaseBranch: req.Msg.GetBaseBranch(),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.AddProjectResponse{Project: toProto(p)}), nil
}

func (s *projectService) ListProjects(ctx context.Context, _ *connect.Request[codingowlv1.ListProjectsRequest]) (*connect.Response[codingowlv1.ListProjectsResponse], error) {
	ps, err := s.projects.List(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.ListProjectsResponse{Projects: make([]*codingowlv1.Project, 0, len(ps))}
	for _, p := range ps {
		res.Projects = append(res.Projects, toProto(p))
	}
	return connect.NewResponse(res), nil
}

func (s *projectService) GetProject(ctx context.Context, req *connect.Request[codingowlv1.GetProjectRequest]) (*connect.Response[codingowlv1.GetProjectResponse], error) {
	d, err := s.projects.Show(ctx, req.Msg.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.GetProjectResponse{
		Project: toProto(d.Project),
		Config: &codingowlv1.ProjectConfig{
			Source:       d.ConfigSource,
			BranchPrefix: d.Config.BranchPrefix,
			Account:      d.Config.Account,
		},
	}), nil
}

func (s *projectService) MoveProject(ctx context.Context, req *connect.Request[codingowlv1.MoveProjectRequest]) (*connect.Response[codingowlv1.MoveProjectResponse], error) {
	// The response is built from the row the move wrote, never by re-running
	// configuration discovery: a Project whose configuration cannot be loaded
	// must not be told its move failed after the move happened.
	p, err := s.projects.Move(ctx, req.Msg.GetName(), req.Msg.GetPath())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.MoveProjectResponse{Project: toProto(p)}), nil
}

func (s *projectService) RenameProject(ctx context.Context, req *connect.Request[codingowlv1.RenameProjectRequest]) (*connect.Response[codingowlv1.RenameProjectResponse], error) {
	p, err := s.projects.Rename(ctx, req.Msg.GetName(), req.Msg.GetNewName())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.RenameProjectResponse{Project: toProto(p)}), nil
}

func (s *projectService) RemoveProject(ctx context.Context, req *connect.Request[codingowlv1.RemoveProjectRequest]) (*connect.Response[codingowlv1.RemoveProjectResponse], error) {
	jobs, err := s.projects.Remove(ctx, req.Msg.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.RemoveProjectResponse{JobsRemoved: int32(jobs)}), nil
}

func (s *projectService) SetupProject(ctx context.Context, req *connect.Request[codingowlv1.SetupProjectRequest]) (*connect.Response[codingowlv1.SetupProjectResponse], error) {
	file, err := s.projects.Setup(ctx, project.SetupRequest{
		Project:    req.Msg.GetProject(),
		Path:       req.Msg.GetPath(),
		WorkingDir: req.Msg.GetWorkingDir(),
		Account:    req.Msg.GetAccount(),
		InRepo:     req.Msg.GetInRepo(),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.SetupProjectResponse{
		File: &codingowlv1.ConfigFile{Path: file.Path, InRepo: file.InRepo},
	}), nil
}

func toProto(p project.Project) *codingowlv1.Project {
	return &codingowlv1.Project{
		Name:       p.Name,
		Path:       p.Path,
		BaseBranch: p.BaseBranch,
		Registered: timestamppb.New(p.Registered),
	}
}

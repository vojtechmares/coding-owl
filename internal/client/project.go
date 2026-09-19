package client

import (
	"context"
	"time"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
)

// Project is a registered repository as the daemon reports it.
type Project struct {
	Name       string
	Path       string
	BaseBranch string
	Registered time.Time
}

// ProjectConfig is the configuration in force for a Project, and where it was
// read from. Source is empty when no configuration file was found.
type ProjectConfig struct {
	Source       string
	BranchPrefix string
	// Account names the Account every Job in the Project runs on (ADR-0023),
	// empty for a Project that names none.
	Account string
	// StrayConfig is the absolute path of a file in the Project's
	// configuration directory that looks like it was meant to be its
	// configuration but is not the name that location reads (ADR-0014
	// form 4), so nothing loaded it. Empty when there is none, and never set
	// while Source names a file that was found.
	StrayConfig string
}

// ProjectDetails is what owl project show reports.
type ProjectDetails struct {
	Project Project
	Config  ProjectConfig
}

// AddProject registers the repository at path. name and baseBranch are
// optional overrides; empty means "let the daemon decide".
func (c *Client) AddProject(ctx context.Context, path, name, baseBranch string) (Project, error) {
	res, err := c.projects.AddProject(ctx, connect.NewRequest(&codingowlv1.AddProjectRequest{
		Path: path, Name: name, BaseBranch: baseBranch,
	}))
	if err != nil {
		return Project{}, c.wrap(err)
	}
	return fromProto(res.Msg.GetProject()), nil
}

// ListProjects returns every Project, ordered by name.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	res, err := c.projects.ListProjects(ctx, connect.NewRequest(&codingowlv1.ListProjectsRequest{}))
	if err != nil {
		return nil, c.wrap(err)
	}
	out := make([]Project, 0, len(res.Msg.GetProjects()))
	for _, p := range res.Msg.GetProjects() {
		out = append(out, fromProto(p))
	}
	return out, nil
}

// GetProject returns one Project with its effective configuration.
func (c *Client) GetProject(ctx context.Context, name string) (ProjectDetails, error) {
	res, err := c.projects.GetProject(ctx, connect.NewRequest(&codingowlv1.GetProjectRequest{Name: name}))
	if err != nil {
		return ProjectDetails{}, c.wrap(err)
	}
	return ProjectDetails{
		Project: fromProto(res.Msg.GetProject()),
		Config: ProjectConfig{
			Source:       res.Msg.GetConfig().GetSource(),
			BranchPrefix: res.Msg.GetConfig().GetBranchPrefix(),
			Account:      res.Msg.GetConfig().GetAccount(),
			StrayConfig:  res.Msg.GetConfig().GetStrayConfig(),
		},
	}, nil
}

// MoveProject points a Project at a new path.
func (c *Client) MoveProject(ctx context.Context, name, path string) (Project, error) {
	res, err := c.projects.MoveProject(ctx, connect.NewRequest(&codingowlv1.MoveProjectRequest{
		Name: name, Path: path,
	}))
	if err != nil {
		return Project{}, c.wrap(err)
	}
	return fromProto(res.Msg.GetProject()), nil
}

// RenameProject changes a Project's name.
func (c *Client) RenameProject(ctx context.Context, name, newName string) (Project, error) {
	res, err := c.projects.RenameProject(ctx, connect.NewRequest(&codingowlv1.RenameProjectRequest{
		Name: name, NewName: newName,
	}))
	if err != nil {
		return Project{}, c.wrap(err)
	}
	return fromProto(res.Msg.GetProject()), nil
}

// RemoveProject deregisters a Project, returning how many Jobs went with it,
// queued or not.
func (c *Client) RemoveProject(ctx context.Context, name string) (int, error) {
	res, err := c.projects.RemoveProject(ctx, connect.NewRequest(&codingowlv1.RemoveProjectRequest{Name: name}))
	if err != nil {
		return 0, c.wrap(err)
	}
	return int(res.Msg.GetJobsRemoved()), nil
}

func fromProto(p *codingowlv1.Project) Project {
	return Project{
		Name:       p.GetName(),
		Path:       p.GetPath(),
		BaseBranch: p.GetBaseBranch(),
		Registered: p.GetRegistered().AsTime(),
	}
}

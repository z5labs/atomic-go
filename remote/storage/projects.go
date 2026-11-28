package storage

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ProjectInfo is a project as the server reports it
// (docs/remote/endpoints/storage.md#projectinfo).
//
// It is read from camelCase members, and workspaceId, defaultView,
// createdAt and updatedAt may also be spelled workspace_id, default_view,
// created_at and updated_at. Every member but description is required; a
// member written twice, or under both spellings, is refused.
type ProjectInfo struct {
	// ID is the project's ID.
	ID uuid.UUID `json:"id"`

	// WorkspaceID is the workspace that holds the project.
	WorkspaceID uuid.UUID `json:"workspaceId"`

	// Name is the display name.
	Name string `json:"name"`

	// Slug is the project's name in paths. With the workspace's slug it
	// gives the project URL: serverurl.NewProject(orgBase, workspace,
	// info.Slug).
	Slug string `json:"slug"`

	// Description is the description, or nil when the server sent none or
	// null.
	Description *string `json:"description"`

	// DefaultView is the name of the project's default view.
	DefaultView string `json:"defaultView"`

	// Visibility is who can see the project.
	Visibility Visibility `json:"visibility"`

	// CreatedAt is when the project was created.
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the project was last changed.
	UpdatedAt time.Time `json:"updatedAt"`
}

// projectAlias returns the member a snake_case spelling ProjectInfo also
// accepts stands for.
func projectAlias(name string) (string, bool) {
	switch name {
	case "workspace_id":
		return "workspaceId", true
	case "default_view":
		return "defaultView", true
	case "created_at":
		return "createdAt", true
	case "updated_at":
		return "updatedAt", true
	}
	return "", false
}

// UnmarshalJSON decodes a project with the aliases and required members
// [ProjectInfo] describes.
func (p *ProjectInfo) UnmarshalJSON(b []byte) error {
	type plain ProjectInfo
	return unmarshalAliased(b, (*plain)(p), projectAlias,
		"id", "workspaceId", "name", "slug", "defaultView", "visibility", "createdAt", "updatedAt")
}

// CreateProjectRequest is the body of [Projects.Create]
// (docs/remote/endpoints/storage.md#createprojectrequest). Unlike
// [ProjectInfo], it is written in snake_case.
type CreateProjectRequest struct {
	// Name is the project's name.
	Name string `json:"name"`

	// Description is the description; an empty one is not sent.
	Description string `json:"description,omitzero"`

	// DefaultView is the name of the project's default view. It is sent
	// even when empty. The Rust CLI sends "dev".
	DefaultView string `json:"default_view"`

	// Kind is a language hint, such as "go" or "rust"; an empty one is not
	// sent.
	Kind string `json:"kind,omitzero"`

	// Visibility is who can see the project. It is required, and must be
	// [VisibilityPublic] or [VisibilityPrivate].
	Visibility Visibility `json:"visibility"`
}

// UpdateProjectRequest is the body of [Projects.Update]
// (docs/remote/endpoints/storage.md#updateprojectrequest), written in
// snake_case. Only the fields that are not nil are sent, so a request with
// none set is {}, and a Description pointing at "" is sent as "".
type UpdateProjectRequest struct {
	// Name is the new name.
	Name *string `json:"name,omitzero"`

	// Description is the new description.
	Description *string `json:"description,omitzero"`

	// DefaultView is the name of the new default view.
	DefaultView *string `json:"default_view,omitzero"`

	// Visibility is the new visibility, which must be [VisibilityPublic]
	// or [VisibilityPrivate].
	Visibility *Visibility `json:"visibility,omitzero"`
}

// Projects sends the project requests of the management API
// (docs/remote/endpoints/storage.md#projects). Get one from
// [Client.Projects]; its zero value is not usable.
//
// Its [Client]'s base must be an org base, which it does not check. Every
// project is named by its workspace's slug and its own. A slug is sent as
// one path segment and refused, with an error wrapping
// [ErrInvalidArgument] and nothing sent, when it would address another
// path; see the package documentation.
type Projects struct {
	c *Client
}

// Projects returns the project requests sent through c.
func (c *Client) Projects() Projects {
	return Projects{c: c}
}

// List returns the projects in the workspace ws:
// GET /workspaces/{ws}/projects.
func (p Projects) List(ctx context.Context, ws string) ([]ProjectInfo, error) {
	path, err := projectsPath(ws)
	if err != nil {
		return nil, err
	}
	return call[[]ProjectInfo](ctx, p.c, request{
		op:     "list_projects",
		method: http.MethodGet,
		path:   path,
	})
}

// Create creates a project in the workspace ws and returns it:
// POST /workspaces/{ws}/projects. A req.Visibility that is not exactly
// "public" or "private" is refused, with an error wrapping
// [ErrInvalidArgument], before anything is sent.
func (p Projects) Create(ctx context.Context, ws string, req CreateProjectRequest) (ProjectInfo, error) {
	path, err := projectsPath(ws)
	if err != nil {
		return ProjectInfo{}, err
	}
	if err := req.Visibility.check(); err != nil {
		return ProjectInfo{}, err
	}
	return call[ProjectInfo](ctx, p.c, request{
		op:     "create_project",
		method: http.MethodPost,
		path:   path,
		body:   req,
	})
}

// Get returns the project named project in the workspace ws:
// GET /workspaces/{ws}/projects/{project}. A project that does not exist
// is the server's 404, which matches [remote.ErrNotFound].
func (p Projects) Get(ctx context.Context, ws, project string) (ProjectInfo, error) {
	path, err := projectPath(ws, project)
	if err != nil {
		return ProjectInfo{}, err
	}
	return call[ProjectInfo](ctx, p.c, request{
		op:     "get_project",
		method: http.MethodGet,
		path:   path,
	})
}

// Update changes the fields req sets on the project named project in the
// workspace ws and returns the project:
// PUT /workspaces/{ws}/projects/{project}. A req.Visibility that is set
// but not exactly "public" or "private" is refused, with an error wrapping
// [ErrInvalidArgument], before anything is sent.
func (p Projects) Update(ctx context.Context, ws, project string, req UpdateProjectRequest) (ProjectInfo, error) {
	path, err := projectPath(ws, project)
	if err != nil {
		return ProjectInfo{}, err
	}
	if req.Visibility != nil {
		if err := req.Visibility.check(); err != nil {
			return ProjectInfo{}, err
		}
	}
	return call[ProjectInfo](ctx, p.c, request{
		op:     "update_project",
		method: http.MethodPut,
		path:   path,
		body:   req,
	})
}

// Delete deletes the project named project in the workspace ws:
// DELETE /workspaces/{ws}/projects/{project}. Any 2xx response is success,
// whatever its body.
func (p Projects) Delete(ctx context.Context, ws, project string) error {
	path, err := projectPath(ws, project)
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, p.c, request{
		op:     "delete_project",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}

// projectsPath returns the escaped path /workspaces/{ws}/projects.
func projectsPath(ws string) (string, error) {
	base, err := workspacePath(ws)
	if err != nil {
		return "", err
	}
	return base + "/projects", nil
}

// projectPath returns the escaped path
// /workspaces/{ws}/projects/{project}.
func projectPath(ws, project string) (string, error) {
	base, err := projectsPath(ws)
	if err != nil {
		return "", err
	}
	ps, err := segment(project)
	if err != nil {
		return "", err
	}
	return base + "/" + ps, nil
}

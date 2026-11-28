package storage

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// WorkspaceInfo is a workspace as the server reports it
// (docs/remote/endpoints/storage.md#workspaceinfo).
//
// It is read from camelCase members, and tenantId, ownerId, createdAt and
// updatedAt may also be spelled tenant_id, owner_id, created_at and
// updated_at. Every member but description is required; a member written
// twice, or under both spellings, is refused.
type WorkspaceInfo struct {
	// ID is the workspace's ID.
	ID uuid.UUID `json:"id"`

	// TenantID is the tenant that owns the workspace.
	TenantID uuid.UUID `json:"tenantId"`

	// Name is the display name.
	Name string `json:"name"`

	// Slug is the workspace's name in paths: the workspace argument of
	// [Projects] methods and of serverurl.NewProject.
	Slug string `json:"slug"`

	// Description is the description, or nil when the server sent none or
	// null.
	Description *string `json:"description"`

	// OwnerID is the identity that owns the workspace.
	OwnerID uuid.UUID `json:"ownerId"`

	// Visibility is who can see the workspace.
	Visibility Visibility `json:"visibility"`

	// CreatedAt is when the workspace was created.
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the workspace was last changed.
	UpdatedAt time.Time `json:"updatedAt"`
}

// workspaceAlias returns the member a snake_case spelling WorkspaceInfo
// also accepts stands for.
func workspaceAlias(name string) (string, bool) {
	switch name {
	case "tenant_id":
		return "tenantId", true
	case "owner_id":
		return "ownerId", true
	case "created_at":
		return "createdAt", true
	case "updated_at":
		return "updatedAt", true
	}
	return "", false
}

// UnmarshalJSON decodes a workspace with the aliases and required members
// [WorkspaceInfo] describes.
func (w *WorkspaceInfo) UnmarshalJSON(b []byte) error {
	type plain WorkspaceInfo
	return unmarshalAliased(b, (*plain)(w), workspaceAlias,
		"id", "tenantId", "name", "slug", "ownerId", "visibility", "createdAt", "updatedAt")
}

// CreateWorkspaceRequest is the body of [Workspaces.Create]
// (docs/remote/endpoints/storage.md#createworkspacerequest). The server
// picks the new workspace's slug and returns it in the [WorkspaceInfo].
type CreateWorkspaceRequest struct {
	// Name is the workspace's name.
	Name string `json:"name"`

	// Description is the description; an empty one is not sent.
	Description string `json:"description,omitzero"`

	// Visibility is who can see the workspace. It is required, and must be
	// [VisibilityPublic] or [VisibilityPrivate].
	Visibility Visibility `json:"visibility"`
}

// UpdateWorkspaceRequest is the body of [Workspaces.Update]
// (docs/remote/endpoints/storage.md#updateworkspacerequest). Only the
// fields that are not nil are sent, so a request with none set is {}, and
// a Description pointing at "" is sent as "".
type UpdateWorkspaceRequest struct {
	// Name is the new name.
	Name *string `json:"name,omitzero"`

	// Description is the new description.
	Description *string `json:"description,omitzero"`

	// Visibility is the new visibility, which must be [VisibilityPublic]
	// or [VisibilityPrivate].
	Visibility *Visibility `json:"visibility,omitzero"`
}

// Workspaces sends the workspace requests of the management API
// (docs/remote/endpoints/storage.md#workspaces). Get one from
// [Client.Workspaces]; its zero value is not usable.
//
// Its [Client]'s base must be an org base, which it does not check. A slug
// is sent as one path segment and refused, with an error wrapping
// [ErrInvalidArgument] and nothing sent, when it would address another
// path; see the package documentation.
type Workspaces struct {
	c *Client
}

// Workspaces returns the workspace requests sent through c.
func (c *Client) Workspaces() Workspaces {
	return Workspaces{c: c}
}

// List returns every workspace the authenticated identity can see:
// GET /workspaces.
func (w Workspaces) List(ctx context.Context) ([]WorkspaceInfo, error) {
	return call[[]WorkspaceInfo](ctx, w.c, request{
		op:     "list_workspaces",
		method: http.MethodGet,
		path:   "/workspaces",
	})
}

// Create creates a workspace and returns it: POST /workspaces. A
// req.Visibility that is not exactly "public" or "private" is refused,
// with an error wrapping [ErrInvalidArgument], before anything is sent.
//
// A workspace that already exists is the server's to report. Whether it
// answers 409, which matches [remote.ErrConflict], is not documented
// (docs/remote/endpoints/storage.md#create_workspace), so a caller that
// wants get-or-create may call [Workspaces.Get] first.
func (w Workspaces) Create(ctx context.Context, req CreateWorkspaceRequest) (WorkspaceInfo, error) {
	if err := req.Visibility.check(); err != nil {
		return WorkspaceInfo{}, err
	}
	return call[WorkspaceInfo](ctx, w.c, request{
		op:     "create_workspace",
		method: http.MethodPost,
		path:   "/workspaces",
		body:   req,
	})
}

// Get returns the workspace slug names: GET /workspaces/{slug}. A
// workspace that does not exist is the server's 404, which matches
// [remote.ErrNotFound].
func (w Workspaces) Get(ctx context.Context, slug string) (WorkspaceInfo, error) {
	path, err := workspacePath(slug)
	if err != nil {
		return WorkspaceInfo{}, err
	}
	return call[WorkspaceInfo](ctx, w.c, request{
		op:     "get_workspace",
		method: http.MethodGet,
		path:   path,
	})
}

// Update changes the fields req sets on the workspace slug names and
// returns the workspace: PUT /workspaces/{slug}. A req.Visibility that is
// set but not exactly "public" or "private" is refused, with an error
// wrapping [ErrInvalidArgument], before anything is sent.
func (w Workspaces) Update(ctx context.Context, slug string, req UpdateWorkspaceRequest) (WorkspaceInfo, error) {
	path, err := workspacePath(slug)
	if err != nil {
		return WorkspaceInfo{}, err
	}
	if req.Visibility != nil {
		if err := req.Visibility.check(); err != nil {
			return WorkspaceInfo{}, err
		}
	}
	return call[WorkspaceInfo](ctx, w.c, request{
		op:     "update_workspace",
		method: http.MethodPut,
		path:   path,
		body:   req,
	})
}

// Delete deletes the workspace slug names, and with it every project in
// it: DELETE /workspaces/{slug}. Any 2xx response is success, whatever its
// body.
func (w Workspaces) Delete(ctx context.Context, slug string) error {
	path, err := workspacePath(slug)
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, w.c, request{
		op:     "delete_workspace",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}

// workspacePath returns the escaped path /workspaces/{slug}.
func workspacePath(slug string) (string, error) {
	ws, err := segment(slug)
	if err != nil {
		return "", err
	}
	return "/workspaces/" + ws, nil
}

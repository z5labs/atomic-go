package storage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// TeamVisibility is who can see a team
// (docs/remote/endpoints/storage.md#teamvisibility).
type TeamVisibility string

const (
	// TeamVisibilityVisible is a team every member of its org can see.
	TeamVisibilityVisible TeamVisibility = "visible"

	// TeamVisibilitySecret is a team only its members and the org's admins
	// can see.
	TeamVisibilitySecret TeamVisibility = "secret"
)

// check returns an error wrapping [ErrInvalidArgument] when v is not
// exactly "visible" or "secret", the two spellings serde accepts.
func (v TeamVisibility) check() error {
	if v != TeamVisibilityVisible && v != TeamVisibilitySecret {
		return fmt.Errorf("%w: team visibility %q is not %q or %q", ErrInvalidArgument, string(v), TeamVisibilityVisible, TeamVisibilitySecret)
	}
	return nil
}

// UnmarshalText reads the wire spelling, which must be exactly "visible"
// or "secret", as serde reads it.
func (v *TeamVisibility) UnmarshalText(b []byte) error {
	got := TeamVisibility(b)
	if err := got.check(); err != nil {
		return err
	}
	*v = got
	return nil
}

// TeamInfo is a team as the server reports it
// (docs/remote/endpoints/storage.md#teaminfo).
//
// It is read from camelCase members, and orgId, createdAt and updatedAt may
// also be spelled org_id, created_at and updated_at. Every member but
// description is required, and visibility must be exactly "visible" or
// "secret"; a member written twice, or under both spellings, is refused.
//
// The visibility check is the Rust client's: its TeamVisibility is a serde
// enum, so a team with any other visibility fails to decode there too, and
// with it the whole of a [Teams.List] reply. A role, by contrast, is kept
// as the server sent it; see [TeamMemberInfo].
type TeamInfo struct {
	// ID is the team's ID, which a grant names as its subject.
	ID uuid.UUID `json:"id"`

	// OrgID is the ID of the org the team belongs to.
	OrgID uuid.UUID `json:"orgId"`

	// Slug is the team's name in paths, such as "backend-eng": the
	// argument of the [Teams] and [TeamMembers] methods that take one.
	Slug string `json:"slug"`

	// Name is the display name.
	Name string `json:"name"`

	// Description is the description, or nil when the server sent none or
	// null.
	Description *string `json:"description"`

	// Visibility is who can see the team.
	Visibility TeamVisibility `json:"visibility"`

	// CreatedAt is when the team was created.
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the team was last changed.
	UpdatedAt time.Time `json:"updatedAt"`
}

// teamAlias returns the member a snake_case spelling TeamInfo also accepts
// stands for.
func teamAlias(name string) (string, bool) {
	switch name {
	case "org_id":
		return "orgId", true
	case "created_at":
		return "createdAt", true
	case "updated_at":
		return "updatedAt", true
	}
	return "", false
}

// UnmarshalJSON decodes a team with the aliases and required members
// [TeamInfo] describes.
func (t *TeamInfo) UnmarshalJSON(b []byte) error {
	type plain TeamInfo
	return unmarshalAliased(b, (*plain)(t), teamAlias, "id", "orgId", "slug", "name", "visibility", "createdAt", "updatedAt")
}

// CreateTeamRequest is the body of [Teams.Create]
// (docs/remote/endpoints/storage.md#createteamrequest). The server derives
// the new team's slug from Name and returns it in the [TeamInfo].
type CreateTeamRequest struct {
	// Name is the team's name.
	Name string `json:"name"`

	// Description is the description; an empty one is not sent.
	Description string `json:"description,omitzero"`

	// Visibility is who can see the team. An empty one is not sent, and
	// the server applies its own default; any other must be
	// [TeamVisibilityVisible] or [TeamVisibilitySecret].
	Visibility TeamVisibility `json:"visibility,omitzero"`
}

// UpdateTeamRequest is the body of [Teams.Update]
// (docs/remote/endpoints/storage.md#updateteamrequest). Only the fields
// that are not nil are sent, so a request with none set is {}, and the
// server leaves the fields it is not sent unchanged. A Description
// pointing at "" is sent as "".
type UpdateTeamRequest struct {
	// Name is the new name.
	Name *string `json:"name,omitzero"`

	// Description is the new description.
	Description *string `json:"description,omitzero"`

	// Visibility is the new visibility, which must be
	// [TeamVisibilityVisible] or [TeamVisibilitySecret].
	Visibility *TeamVisibility `json:"visibility,omitzero"`
}

// Teams sends the team requests of the management API
// (docs/remote/endpoints/storage.md#teams). Get one from [Client.Teams];
// its zero value is not usable.
//
// A team route carries no org: the org is the one in its [Client]'s base
// URL, an org base as serverurl.Server.OrgBase returns it, so the same
// slug names a different team on https://delta.example than on
// https://atomic.example. Which org that is is not checked. The Rust
// client takes an org slug it never sends; these methods take none. A team
// slug is sent as one path segment and refused, with an error wrapping
// [ErrInvalidArgument] and nothing sent, when it would address another
// path.
//
// A failure is the server's status: a 403 matches [remote.ErrForbidden], a
// 404 [remote.ErrNotFound] and a 409 [remote.ErrConflict], with the
// server's code and message on the [*remote.Error].
type Teams struct {
	c *Client
}

// Teams returns the team requests sent through c.
func (c *Client) Teams() Teams {
	return Teams{c: c}
}

// List returns every team of the base URL's org the caller can see:
// GET /teams. The Rust client documents that secret teams are returned only
// to their members and the org's admins.
func (t Teams) List(ctx context.Context) ([]TeamInfo, error) {
	return call[[]TeamInfo](ctx, t.c, request{
		op:     "list_teams",
		method: http.MethodGet,
		path:   "/teams",
	})
}

// Create creates a team in the base URL's org and returns it: POST /teams.
// A req.Visibility that is set but not exactly "visible" or "secret" is
// refused, with an error wrapping [ErrInvalidArgument], before anything is
// sent.
func (t Teams) Create(ctx context.Context, req CreateTeamRequest) (TeamInfo, error) {
	if req.Visibility != "" {
		if err := req.Visibility.check(); err != nil {
			return TeamInfo{}, err
		}
	}
	return call[TeamInfo](ctx, t.c, request{
		op:     "create_team",
		method: http.MethodPost,
		path:   "/teams",
		body:   req,
	})
}

// Get returns the team slug names in the base URL's org:
// GET /teams/{slug}. A team that does not exist is the server's 404, which
// matches [remote.ErrNotFound].
func (t Teams) Get(ctx context.Context, slug string) (TeamInfo, error) {
	path, err := teamPath(slug, "")
	if err != nil {
		return TeamInfo{}, err
	}
	return call[TeamInfo](ctx, t.c, request{
		op:     "get_team",
		method: http.MethodGet,
		path:   path,
	})
}

// Update changes the fields req sets on the team slug names and returns
// the team: PUT /teams/{slug}. A req.Visibility that is set but not exactly
// "visible" or "secret" is refused, with an error wrapping
// [ErrInvalidArgument], before anything is sent.
func (t Teams) Update(ctx context.Context, slug string, req UpdateTeamRequest) (TeamInfo, error) {
	path, err := teamPath(slug, "")
	if err != nil {
		return TeamInfo{}, err
	}
	if req.Visibility != nil {
		if err := req.Visibility.check(); err != nil {
			return TeamInfo{}, err
		}
	}
	return call[TeamInfo](ctx, t.c, request{
		op:     "update_team",
		method: http.MethodPut,
		path:   path,
		body:   req,
	})
}

// Delete deletes the team slug names: DELETE /teams/{slug}. The Rust client
// documents that the team's memberships and team-scoped grants go with it.
// Any 2xx response is success, whatever its body.
func (t Teams) Delete(ctx context.Context, slug string) error {
	path, err := teamPath(slug, "")
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, t.c, request{
		op:     "delete_team",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}

// teamPath returns the escaped path /teams/{slug} followed by suffix.
func teamPath(slug, suffix string) (string, error) {
	s, err := segment(slug)
	if err != nil {
		return "", err
	}
	return "/teams/" + s + suffix, nil
}

package storage

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// TeamRole is a member's role in a team
// (docs/remote/endpoints/storage.md#teamrole). The constants are listed
// strongest first.
type TeamRole string

const (
	// TeamRoleMaintainer manages the team and its members.
	TeamRoleMaintainer TeamRole = "maintainer"

	// TeamRoleContributor is a member who contributes to the team's work.
	TeamRoleContributor TeamRole = "contributor"

	// TeamRoleCollaborator is a member with less access than a
	// contributor.
	TeamRoleCollaborator TeamRole = "collaborator"

	// TeamRoleConsumer is the weakest role.
	TeamRoleConsumer TeamRole = "consumer"
)

// TeamMemberInfo is a membership of a team, as the team member requests
// report it (docs/remote/endpoints/storage.md#teammemberinfo).
//
// It is read from camelCase members, and teamId, identityId, addedAt and
// addedBy may also be spelled team_id, identity_id, added_at and added_by.
// Every member is required; a member written twice, or under both
// spellings, is refused.
type TeamMemberInfo struct {
	// TeamID is the team's ID.
	TeamID uuid.UUID `json:"teamId"`

	// IdentityID is the member's identity: the ID [TeamMembers] methods
	// that act on one member take.
	IdentityID uuid.UUID `json:"identityId"`

	// Role is the member's role in the team. It is the string the server
	// sent and is not checked against the [TeamRole] constants.
	Role TeamRole `json:"role"`

	// AddedAt is when the member was added to the team.
	AddedAt time.Time `json:"addedAt"`

	// AddedBy is the identity that added the member.
	AddedBy uuid.UUID `json:"addedBy"`
}

// teamMemberAlias returns the member a snake_case spelling TeamMemberInfo
// also accepts stands for.
func teamMemberAlias(name string) (string, bool) {
	switch name {
	case "team_id":
		return "teamId", true
	case "identity_id":
		return "identityId", true
	case "added_at":
		return "addedAt", true
	case "added_by":
		return "addedBy", true
	}
	return "", false
}

// UnmarshalJSON decodes a team membership with the aliases and required
// members [TeamMemberInfo] describes.
func (m *TeamMemberInfo) UnmarshalJSON(b []byte) error {
	type plain TeamMemberInfo
	return unmarshalAliased(b, (*plain)(m), teamMemberAlias, "teamId", "identityId", "role", "addedAt", "addedBy")
}

// AddTeamMemberRequest is the body of [TeamMembers.Add]
// (docs/remote/endpoints/storage.md#addteammemberrequest). Unlike most
// bodies it is written in snake_case.
type AddTeamMemberRequest struct {
	// IdentityID is the identity to add, written in its hyphenated form.
	IdentityID uuid.UUID `json:"identity_id"`

	// Role is the role to add it with.
	Role TeamRole `json:"role"`
}

// UpdateTeamMemberRoleRequest is the body of [TeamMembers.UpdateRole]
// (docs/remote/endpoints/storage.md#updateteammemberrolerequest).
type UpdateTeamMemberRoleRequest struct {
	// Role is the member's new role.
	Role TeamRole `json:"role"`
}

// TeamMembers sends the team member requests of the management API
// (docs/remote/endpoints/storage.md#teams). Get one from
// [Client.TeamMembers]; its zero value is not usable.
//
// As for [Teams], the org is the one in its [Client]'s base URL, and every
// method takes the team's slug but no org. The slug is sent as one path
// segment and refused, with an error wrapping [ErrInvalidArgument] and
// nothing sent, when it would address another path. An identity ID is sent
// in its hyphenated form.
//
// A failure is the server's status: a 403 matches [remote.ErrForbidden], a
// 404 [remote.ErrNotFound] and a 409 [remote.ErrConflict], with the
// server's code and message on the [*remote.Error]. The Rust client reports
// every 404 from these requests as TeamNotFound, although it documents
// MemberNotFound for a non-member
// (docs/remote/endpoints/storage.md#open-questions); here a 404 is only the
// server's, and its message says which was missing.
type TeamMembers struct {
	c *Client
}

// TeamMembers returns the team member requests sent through c.
func (c *Client) TeamMembers() TeamMembers {
	return TeamMembers{c: c}
}

// List returns every member of the team slug names:
// GET /teams/{slug}/members.
func (m TeamMembers) List(ctx context.Context, slug string) ([]TeamMemberInfo, error) {
	path, err := teamPath(slug, "/members")
	if err != nil {
		return nil, err
	}
	return call[[]TeamMemberInfo](ctx, m.c, request{
		op:     "list_team_members",
		method: http.MethodGet,
		path:   path,
	})
}

// Add adds req.IdentityID to the team slug names with req.Role and returns
// the new membership: POST /teams/{slug}/members. The Rust CLI resolves the
// identity first, as [Identities.ResolveByEmail] and
// [Identities.ResolveByName] do.
func (m TeamMembers) Add(ctx context.Context, slug string, req AddTeamMemberRequest) (TeamMemberInfo, error) {
	path, err := teamPath(slug, "/members")
	if err != nil {
		return TeamMemberInfo{}, err
	}
	return call[TeamMemberInfo](ctx, m.c, request{
		op:     "add_team_member",
		method: http.MethodPost,
		path:   path,
		body:   req,
	})
}

// UpdateRole changes the role of identityID in the team slug names to
// req.Role and returns the membership:
// PUT /teams/{slug}/members/{identityID}.
func (m TeamMembers) UpdateRole(ctx context.Context, slug string, identityID uuid.UUID, req UpdateTeamMemberRoleRequest) (TeamMemberInfo, error) {
	path, err := teamMemberPath(slug, identityID)
	if err != nil {
		return TeamMemberInfo{}, err
	}
	return call[TeamMemberInfo](ctx, m.c, request{
		op:     "update_team_member_role",
		method: http.MethodPut,
		path:   path,
		body:   req,
	})
}

// Remove removes identityID from the team slug names:
// DELETE /teams/{slug}/members/{identityID}. Any 2xx response is success,
// whatever its body.
func (m TeamMembers) Remove(ctx context.Context, slug string, identityID uuid.UUID) error {
	path, err := teamMemberPath(slug, identityID)
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, m.c, request{
		op:     "remove_team_member",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}

// teamMemberPath returns the escaped path
// /teams/{slug}/members/{identityID}.
func teamMemberPath(slug string, identityID uuid.UUID) (string, error) {
	return teamPath(slug, "/members/"+identityID.String())
}

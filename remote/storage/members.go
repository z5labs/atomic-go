package storage

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// OrgMemberInfo is a membership of an org, as the member requests report
// it (docs/remote/endpoints/storage.md#orgmemberinfo).
//
// It is read from camelCase members, and orgId, identityId, joinedAt,
// invitedBy and publicKey may also be spelled org_id, identity_id,
// joined_at, invited_by and public_key; the Rust client's tests say the
// server sends snake_case. orgId, identityId, role and joinedAt are
// required. The others may be absent or null, as older servers omit name,
// publicKey, status and email. A member written twice, or under both
// spellings, is refused.
type OrgMemberInfo struct {
	// OrgID is the org's ID.
	OrgID uuid.UUID `json:"orgId"`

	// IdentityID is the member's identity: the ID [OrgMembers] methods
	// that act on one member take.
	IdentityID uuid.UUID `json:"identityId"`

	// Role is the member's role in the org. It is the string the server
	// sent and is not checked against the [OrgRole] constants.
	Role OrgRole `json:"role"`

	// JoinedAt is when the member joined the org.
	JoinedAt time.Time `json:"joinedAt"`

	// InvitedBy is the identity that invited the member, or nil when the
	// server sent none or null.
	InvitedBy *uuid.UUID `json:"invitedBy"`

	// Name is the member's display name, or nil when the server sent none
	// or null.
	Name *string `json:"name"`

	// PublicKey is the member's base32 Ed25519 public key as the server
	// sent it, or nil when it sent none or null. It is not checked; parse
	// it with identity.ParsePublicKey.
	PublicKey *string `json:"publicKey"`

	// Status is the member's lifecycle status, such as "active",
	// "suspended" or "deleted", or nil when the server sent none or null.
	Status *string `json:"status"`

	// Email is the member's preferred email, or nil when the server sent
	// none or null.
	Email *string `json:"email"`
}

// orgMemberAlias returns the member a snake_case spelling OrgMemberInfo
// also accepts stands for.
func orgMemberAlias(name string) (string, bool) {
	switch name {
	case "org_id":
		return "orgId", true
	case "identity_id":
		return "identityId", true
	case "joined_at":
		return "joinedAt", true
	case "invited_by":
		return "invitedBy", true
	case "public_key":
		return "publicKey", true
	}
	return "", false
}

// UnmarshalJSON decodes a membership with the aliases and required members
// [OrgMemberInfo] describes.
func (m *OrgMemberInfo) UnmarshalJSON(b []byte) error {
	type plain OrgMemberInfo
	return unmarshalAliased(b, (*plain)(m), orgMemberAlias, "orgId", "identityId", "role", "joinedAt")
}

// AddMemberRequest is the body of [OrgMembers.Add]
// (docs/remote/endpoints/storage.md#addmemberrequest). Unlike most bodies
// it is written in snake_case.
type AddMemberRequest struct {
	// IdentityID is the identity to add, written in its hyphenated form.
	IdentityID uuid.UUID `json:"identity_id"`

	// Role is the role to add it with.
	Role OrgRole `json:"role"`
}

// UpdateMemberRoleRequest is the body of [OrgMembers.UpdateRole]
// (docs/remote/endpoints/storage.md#updatememberrolerequest).
type UpdateMemberRoleRequest struct {
	// Role is the member's new role.
	Role OrgRole `json:"role"`
}

// OrgMembers sends the org member requests of the management API
// (docs/remote/endpoints/storage.md#organizations). Get one from
// [Client.OrgMembers]; its zero value is not usable.
//
// Every request acts on one org and goes to an org base, as
// serverurl.Server.OrgBase returns it, which is what the Rust CLI sends
// them to; this is not checked. The slug is sent as one path segment and
// refused, with an error wrapping [ErrInvalidArgument] and nothing sent,
// when it would address another path. An identity ID is sent in its
// hyphenated form.
//
// A failure is the server's status: a 403 matches [remote.ErrForbidden], a
// 404 [remote.ErrNotFound] and a 409 [remote.ErrConflict].
type OrgMembers struct {
	c *Client
}

// OrgMembers returns the org member requests sent through c.
func (c *Client) OrgMembers() OrgMembers {
	return OrgMembers{c: c}
}

// List returns every member of the org slug names:
// GET /orgs/{slug}/members.
func (m OrgMembers) List(ctx context.Context, slug string) ([]OrgMemberInfo, error) {
	path, err := orgPath(slug, "/members")
	if err != nil {
		return nil, err
	}
	return call[[]OrgMemberInfo](ctx, m.c, request{
		op:     "list_members",
		method: http.MethodGet,
		path:   path,
	})
}

// Add adds req.IdentityID to the org slug names with req.Role and returns
// the new membership: POST /orgs/{slug}/members. The Rust CLI resolves the
// identity first, as [Identities.ResolveByEmail] and
// [Identities.ResolveByName] do.
func (m OrgMembers) Add(ctx context.Context, slug string, req AddMemberRequest) (OrgMemberInfo, error) {
	path, err := orgPath(slug, "/members")
	if err != nil {
		return OrgMemberInfo{}, err
	}
	return call[OrgMemberInfo](ctx, m.c, request{
		op:     "add_member",
		method: http.MethodPost,
		path:   path,
		body:   req,
	})
}

// Get returns the membership of identityID in the org slug names:
// GET /orgs/{slug}/members/{identityID}. One that does not exist is the
// server's 404, which matches [remote.ErrNotFound]. Nothing in the Rust
// client calls this request.
func (m OrgMembers) Get(ctx context.Context, slug string, identityID uuid.UUID) (OrgMemberInfo, error) {
	path, err := memberPath(slug, identityID)
	if err != nil {
		return OrgMemberInfo{}, err
	}
	return call[OrgMemberInfo](ctx, m.c, request{
		op:     "get_member",
		method: http.MethodGet,
		path:   path,
	})
}

// UpdateRole changes the role of identityID in the org slug names to
// req.Role and returns the membership: PUT /orgs/{slug}/members/{identityID}.
func (m OrgMembers) UpdateRole(ctx context.Context, slug string, identityID uuid.UUID, req UpdateMemberRoleRequest) (OrgMemberInfo, error) {
	path, err := memberPath(slug, identityID)
	if err != nil {
		return OrgMemberInfo{}, err
	}
	return call[OrgMemberInfo](ctx, m.c, request{
		op:     "update_member_role",
		method: http.MethodPut,
		path:   path,
		body:   req,
	})
}

// Remove removes identityID from the org slug names:
// DELETE /orgs/{slug}/members/{identityID}. Any 2xx response is success,
// whatever its body.
//
// Removing the org's last owner is refused by the server with a 409, which
// matches [remote.ErrConflict]. The Rust client documents that case as
// LastOwner but reports every 409 as AlreadyExists
// (docs/remote/endpoints/storage.md#open-questions); here a 409 from Remove
// is always the server's conflict, and its code and message are on the
// [*remote.Error].
func (m OrgMembers) Remove(ctx context.Context, slug string, identityID uuid.UUID) error {
	path, err := memberPath(slug, identityID)
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, m.c, request{
		op:     "remove_member",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}

// memberPath returns the escaped path /orgs/{slug}/members/{identityID}.
func memberPath(slug string, identityID uuid.UUID) (string, error) {
	return orgPath(slug, "/members/"+identityID.String())
}

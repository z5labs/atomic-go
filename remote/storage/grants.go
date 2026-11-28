package storage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// GrantRelation is the access a grant gives its subject
// (docs/remote/endpoints/storage.md#grantrelation). The constants are
// listed weakest first.
type GrantRelation string

const (
	// RelationRead lets the subject read.
	RelationRead GrantRelation = "read"

	// RelationWrite lets the subject read and write.
	RelationWrite GrantRelation = "write"

	// RelationAdmin lets the subject administer.
	RelationAdmin GrantRelation = "admin"

	// RelationOwner makes the subject an owner. The Rust client documents
	// it for org grants and its CLI refuses it for workspace grants, but
	// its workspace grant functions send it; [WorkspaceGrants.Add] sends it
	// too.
	RelationOwner GrantRelation = "owner"
)

// check returns an error wrapping [ErrInvalidArgument] when r is not
// exactly one of the four relations, the only spellings the Rust client's
// GrantRelation enum can send.
func (r GrantRelation) check() error {
	switch r {
	case RelationRead, RelationWrite, RelationAdmin, RelationOwner:
		return nil
	}
	return fmt.Errorf("%w: grant relation %q is not %q, %q, %q or %q", ErrInvalidArgument, string(r), RelationRead, RelationWrite, RelationAdmin, RelationOwner)
}

// GrantSubjectType is the kind of subject a grant is held by
// (docs/remote/endpoints/storage.md#grantsubjecttype).
type GrantSubjectType string

const (
	// SubjectUser is a grant held by one identity.
	SubjectUser GrantSubjectType = "user"

	// SubjectTeam is a grant held by a team.
	SubjectTeam GrantSubjectType = "team"

	// SubjectEveryone is a public grant, held by everyone. The server
	// reports it, with no subject ID, but the requests that add and revoke
	// grants take only [SubjectUser] or [SubjectTeam], so they refuse it
	// before anything is sent.
	SubjectEveryone GrantSubjectType = "everyone"
)

// checkMutable returns an error wrapping [ErrInvalidArgument] when t is not
// exactly "user" or "team", the subjects a grant can be added for or
// revoked from. [SubjectEveryone] is refused, so that a request can never
// make a workspace or org public, and so is every other spelling.
func (t GrantSubjectType) checkMutable() error {
	switch t {
	case SubjectUser, SubjectTeam:
		return nil
	case SubjectEveryone:
		return fmt.Errorf("%w: grant subject %q cannot be added or revoked; only %q or %q can", ErrInvalidArgument, string(t), SubjectUser, SubjectTeam)
	}
	return fmt.Errorf("%w: grant subject type %q is not %q or %q", ErrInvalidArgument, string(t), SubjectUser, SubjectTeam)
}

// GrantInfo is a grant on an org, as the org grant requests report it
// (docs/remote/endpoints/storage.md#grantinfo).
//
// Unlike most responses it is read from snake_case members only, with no
// other spellings, as the Rust client reads it. id, subject_type, relation
// and granted_at are required; subject_id and granted_by may be absent or
// null. A member written twice is refused.
type GrantInfo struct {
	// ID is the grant's ID.
	ID uuid.UUID `json:"id"`

	// SubjectType is the kind of subject that holds the grant. It is the
	// string the server sent and is not checked against the
	// [GrantSubjectType] constants.
	SubjectType GrantSubjectType `json:"subject_type"`

	// SubjectID is the identity or team that holds the grant, or nil when
	// the server sent none or null, as it does for a [SubjectEveryone]
	// grant.
	SubjectID *uuid.UUID `json:"subject_id"`

	// Relation is the access the grant gives. It is the string the server
	// sent and is not checked against the [GrantRelation] constants.
	Relation GrantRelation `json:"relation"`

	// GrantedBy is the identity that made the grant, or nil when the
	// server sent none or null.
	GrantedBy *uuid.UUID `json:"granted_by"`

	// GrantedAt is when the grant was made.
	GrantedAt time.Time `json:"granted_at"`
}

// UnmarshalJSON decodes an org grant with the required members [GrantInfo]
// describes.
func (g *GrantInfo) UnmarshalJSON(b []byte) error {
	type plain GrantInfo
	return unmarshalAliased(b, (*plain)(g), nil, "id", "subject_type", "relation", "granted_at")
}

// WorkspaceGrantInfo is a grant on a workspace, as the workspace grant
// requests report it (docs/remote/endpoints/storage.md#workspacegrantinfo).
//
// Like [GrantInfo] it is read from snake_case members only. id,
// subject_type, relation, object_type, object_id and granted_at are
// required; subject_id, subject_relation and granted_by may be absent or
// null. A member written twice is refused.
type WorkspaceGrantInfo struct {
	// ID is the ID of the access tuple.
	ID uuid.UUID `json:"id"`

	// SubjectType is the kind of subject that holds the grant. It is the
	// string the server sent and is not checked against the
	// [GrantSubjectType] constants.
	SubjectType GrantSubjectType `json:"subject_type"`

	// SubjectID is the identity or team that holds the grant, or nil when
	// the server sent none or null, as it does for a [SubjectEveryone]
	// grant on a public workspace.
	SubjectID *uuid.UUID `json:"subject_id"`

	// SubjectRelation is the subject's relation within its team, or nil
	// when the server sent none or null, which the Rust client documents
	// as the usual case.
	SubjectRelation *string `json:"subject_relation"`

	// Relation is the access the grant gives. It is the string the server
	// sent and is not checked against the [GrantRelation] constants.
	Relation GrantRelation `json:"relation"`

	// ObjectType is the kind of object granted on, which the Rust client
	// documents as always "workspace". It is not checked.
	ObjectType string `json:"object_type"`

	// ObjectID is the workspace's ID.
	ObjectID uuid.UUID `json:"object_id"`

	// GrantedAt is when the grant was made.
	GrantedAt time.Time `json:"granted_at"`

	// GrantedBy is the identity that made the grant, or nil when the
	// server sent none or null.
	GrantedBy *uuid.UUID `json:"granted_by"`
}

// UnmarshalJSON decodes a workspace grant with the required members
// [WorkspaceGrantInfo] describes.
func (g *WorkspaceGrantInfo) UnmarshalJSON(b []byte) error {
	type plain WorkspaceGrantInfo
	return unmarshalAliased(b, (*plain)(g), nil, "id", "subject_type", "relation", "object_type", "object_id", "granted_at")
}

// AddGrantRequest is the body of [OrgGrants.Add] and [WorkspaceGrants.Add]
// (docs/remote/endpoints/storage.md#addgrantrequest). Like the grant
// responses it is written in snake_case.
type AddGrantRequest struct {
	// SubjectType is the kind of subject to grant to: [SubjectUser] or
	// [SubjectTeam]. Any other value, [SubjectEveryone] included, is
	// refused before anything is sent.
	SubjectType GrantSubjectType `json:"subject_type"`

	// SubjectID is the identity's or team's ID, written in its hyphenated
	// form.
	SubjectID uuid.UUID `json:"subject_id"`

	// Relation is the access to grant: one of the [GrantRelation]
	// constants. Any other value is refused before anything is sent.
	Relation GrantRelation `json:"relation"`
}

// check refuses, with an error wrapping [ErrInvalidArgument], a request
// whose subject type or relation the Rust client could not send.
func (r AddGrantRequest) check() error {
	if err := r.SubjectType.checkMutable(); err != nil {
		return err
	}
	return r.Relation.check()
}

// OrgGrants sends the org grant requests of the management API
// (docs/remote/endpoints/storage.md#grants). Get one from
// [Client.OrgGrants]; its zero value is not usable.
//
// Nothing in the Rust client calls them, so which base they go to is
// assumed from their paths: an org base, as serverurl.Server.OrgBase
// returns it. This is not checked. The org slug is sent as one path segment
// and refused, with an error wrapping [ErrInvalidArgument] and nothing sent,
// when it would address another path. A subject ID is sent in its
// hyphenated form.
//
// A failure is the server's status: a 403 matches [remote.ErrForbidden], a
// 404 [remote.ErrNotFound] and a 409 [remote.ErrConflict].
type OrgGrants struct {
	c *Client
}

// OrgGrants returns the org grant requests sent through c.
func (c *Client) OrgGrants() OrgGrants {
	return OrgGrants{c: c}
}

// List returns every grant on the org slug names: GET /orgs/{slug}/grants.
func (g OrgGrants) List(ctx context.Context, slug string) ([]GrantInfo, error) {
	path, err := orgPath(slug, "/grants")
	if err != nil {
		return nil, err
	}
	return call[[]GrantInfo](ctx, g.c, request{
		op:     "list_org_grants",
		method: http.MethodGet,
		path:   path,
	})
}

// Add grants req.Relation on the org slug names to the subject req names
// and returns the new grant: POST /orgs/{slug}/grants. A subject type
// other than [SubjectUser] or [SubjectTeam], or a relation other than the
// [GrantRelation] constants, is refused with an error wrapping
// [ErrInvalidArgument] and nothing sent.
func (g OrgGrants) Add(ctx context.Context, slug string, req AddGrantRequest) (GrantInfo, error) {
	if err := req.check(); err != nil {
		return GrantInfo{}, err
	}
	path, err := orgPath(slug, "/grants")
	if err != nil {
		return GrantInfo{}, err
	}
	return call[GrantInfo](ctx, g.c, request{
		op:     "add_org_grant",
		method: http.MethodPost,
		path:   path,
		body:   req,
	})
}

// Revoke removes every relation the subject subjectType and subjectID name
// holds on the org slug names:
// DELETE /orgs/{slug}/grants/{subjectType}/{subjectID}. A subject type
// other than [SubjectUser] or [SubjectTeam] is refused with an error
// wrapping [ErrInvalidArgument] and nothing sent. Any 2xx response is
// success, whatever its body.
func (g OrgGrants) Revoke(ctx context.Context, slug string, subjectType GrantSubjectType, subjectID uuid.UUID) error {
	suffix, err := grantSubjectPath(subjectType, subjectID)
	if err != nil {
		return err
	}
	path, err := orgPath(slug, "/grants"+suffix)
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, g.c, request{
		op:     "revoke_org_grant",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}

// WorkspaceGrants sends the workspace grant requests of the management API
// (docs/remote/endpoints/storage.md#grants). Get one from
// [Client.WorkspaceGrants]; its zero value is not usable.
//
// Its [Client]'s base must be an org base, which it does not check. The
// workspace slug is sent as one path segment and refused, with an error
// wrapping [ErrInvalidArgument] and nothing sent, when it would address
// another path. A subject ID is sent in its hyphenated form.
//
// A failure is the server's status: a 403 matches [remote.ErrForbidden], a
// 404 [remote.ErrNotFound] and a 409 [remote.ErrConflict].
type WorkspaceGrants struct {
	c *Client
}

// WorkspaceGrants returns the workspace grant requests sent through c.
func (c *Client) WorkspaceGrants() WorkspaceGrants {
	return WorkspaceGrants{c: c}
}

// List returns every grant on the workspace slug names:
// GET /workspaces/{slug}/grants. A public workspace's list holds a
// [SubjectEveryone] grant with no subject ID.
func (g WorkspaceGrants) List(ctx context.Context, slug string) ([]WorkspaceGrantInfo, error) {
	path, err := workspaceGrantsPath(slug, "")
	if err != nil {
		return nil, err
	}
	return call[[]WorkspaceGrantInfo](ctx, g.c, request{
		op:     "list_workspace_grants",
		method: http.MethodGet,
		path:   path,
	})
}

// Add grants req.Relation on the workspace slug names to the subject req
// names and returns the new grant: POST /workspaces/{slug}/grants. A
// subject type other than [SubjectUser] or [SubjectTeam], or a relation
// other than the [GrantRelation] constants, is refused with an error
// wrapping [ErrInvalidArgument] and nothing sent. [RelationOwner] is sent,
// as the Rust client's add_workspace_grant sends it, although its CLI
// refuses it.
func (g WorkspaceGrants) Add(ctx context.Context, slug string, req AddGrantRequest) (WorkspaceGrantInfo, error) {
	if err := req.check(); err != nil {
		return WorkspaceGrantInfo{}, err
	}
	path, err := workspaceGrantsPath(slug, "")
	if err != nil {
		return WorkspaceGrantInfo{}, err
	}
	return call[WorkspaceGrantInfo](ctx, g.c, request{
		op:     "add_workspace_grant",
		method: http.MethodPost,
		path:   path,
		body:   req,
	})
}

// Revoke removes every relation the subject subjectType and subjectID name
// holds on the workspace slug names:
// DELETE /workspaces/{slug}/grants/{subjectType}/{subjectID}. A subject
// type other than [SubjectUser] or [SubjectTeam] is refused with an error
// wrapping [ErrInvalidArgument] and nothing sent. Any 2xx response is
// success, whatever its body.
func (g WorkspaceGrants) Revoke(ctx context.Context, slug string, subjectType GrantSubjectType, subjectID uuid.UUID) error {
	suffix, err := grantSubjectPath(subjectType, subjectID)
	if err != nil {
		return err
	}
	path, err := workspaceGrantsPath(slug, suffix)
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, g.c, request{
		op:     "revoke_workspace_grant",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}

// workspaceGrantsPath returns the escaped path /workspaces/{slug}/grants
// followed by suffix.
func workspaceGrantsPath(slug, suffix string) (string, error) {
	ws, err := workspacePath(slug)
	if err != nil {
		return "", err
	}
	return ws + "/grants" + suffix, nil
}

// grantSubjectPath returns the path suffix /{subjectType}/{subjectID} that
// follows /grants in a revoke, after refusing a subject type that cannot be revoked. The
// subject type is "user" or "team" by then, so it needs no escaping.
func grantSubjectPath(subjectType GrantSubjectType, subjectID uuid.UUID) (string, error) {
	if err := subjectType.checkMutable(); err != nil {
		return "", err
	}
	return "/" + string(subjectType) + "/" + subjectID.String(), nil
}

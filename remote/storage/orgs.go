package storage

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// OrgRole is a member's role in an org
// (docs/remote/endpoints/storage.md#orgrole).
type OrgRole string

const (
	// OrgRoleOwner has full control over the org.
	OrgRoleOwner OrgRole = "owner"

	// OrgRoleAdmin can manage the org's members and teams but cannot
	// delete it.
	OrgRoleAdmin OrgRole = "admin"

	// OrgRoleMember is a member with the default permissions.
	OrgRoleMember OrgRole = "member"
)

// OrgInfo is an org as the server reports it
// (docs/remote/endpoints/storage.md#orginfo).
//
// It is read from camelCase members, and createdAt and updatedAt may also
// be spelled created_at and updated_at. Every member but email and plan is
// required; a member written twice, or under both spellings, is refused.
//
// The Rust client requires plan, yet documents that the server redacts it
// for a caller who is not a member, so a reply without it would fail there
// (docs/remote/endpoints/storage.md#open-questions). Here a plan that is
// absent or null is read as "".
type OrgInfo struct {
	// ID is the org's ID.
	ID uuid.UUID `json:"id"`

	// Slug is the org's name in paths, such as "acme": the argument of the
	// [Orgs] methods that take one, and the org serverurl.Server.OrgBase
	// turns into the host of every org-scoped request.
	Slug string `json:"slug"`

	// Name is the display name.
	Name string `json:"name"`

	// Email is the contact email, or nil when the server sent none or
	// null.
	Email *string `json:"email"`

	// Kind is the org's kind, such as "personal" or "team".
	Kind string `json:"kind"`

	// Plan is the billing plan, such as "free", "team" or "enterprise", or
	// "" when the server sent none or null.
	Plan string `json:"plan"`

	// CreatedAt is when the org was created.
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the org was last changed.
	UpdatedAt time.Time `json:"updatedAt"`
}

// orgRequired is the members [OrgInfo] requires.
var orgRequired = []string{"id", "slug", "name", "kind", "createdAt", "updatedAt"}

// orgAlias returns the member a snake_case spelling OrgInfo also accepts
// stands for.
func orgAlias(name string) (string, bool) {
	switch name {
	case "created_at":
		return "createdAt", true
	case "updated_at":
		return "updatedAt", true
	}
	return "", false
}

// orgPlain is an [OrgInfo] without its UnmarshalJSON method, so that it can
// be decoded by [unmarshalAliased] and JSON-embedded in [myOrgWire].
type orgPlain OrgInfo

// UnmarshalJSON decodes an org with the aliases and required members
// [OrgInfo] describes.
func (o *OrgInfo) UnmarshalJSON(b []byte) error {
	return unmarshalAliased(b, (*orgPlain)(o), orgAlias, orgRequired...)
}

// MyOrgInfo is an org the caller belongs to, with the caller's membership,
// as [Orgs.ListMine] reports it
// (docs/remote/endpoints/storage.md#myorginfo).
//
// On the wire it is one flat object: the members of an [OrgInfo] beside
// role, joinedAt and invitedBy. Its JSON methods read and write it that way,
// so Org is not a member of its own. It is read from camelCase members, and
// createdAt, updatedAt, joinedAt and invitedBy may also be spelled
// created_at, updated_at, joined_at and invited_by. Every member but email,
// plan and invitedBy is required; a member written twice, or under both
// spellings, is refused.
type MyOrgInfo struct {
	// Org is the org.
	Org OrgInfo

	// Role is the caller's role in the org. It is the string the server
	// sent and is not checked against the [OrgRole] constants, as the Rust
	// client keeps it a plain string.
	Role OrgRole

	// JoinedAt is when the caller joined the org.
	JoinedAt time.Time

	// InvitedBy is the identity that invited the caller, or nil when the
	// server sent none or null.
	InvitedBy *uuid.UUID
}

// myOrgWire is a [MyOrgInfo] as the server writes it, with the org's
// members JSON-embedded beside the membership's.
type myOrgWire struct {
	Org       orgPlain   `json:",embed"`
	Role      OrgRole    `json:"role"`
	JoinedAt  time.Time  `json:"joinedAt"`
	InvitedBy *uuid.UUID `json:"invitedBy"`
}

// myOrgAlias returns the member a snake_case spelling MyOrgInfo also
// accepts stands for.
func myOrgAlias(name string) (string, bool) {
	switch name {
	case "joined_at":
		return "joinedAt", true
	case "invited_by":
		return "invitedBy", true
	}
	return orgAlias(name)
}

// UnmarshalJSON decodes one flat object with the aliases and required
// members [MyOrgInfo] describes.
func (m *MyOrgInfo) UnmarshalJSON(b []byte) error {
	var w myOrgWire
	required := append(append([]string{}, orgRequired...), "role", "joinedAt")
	if err := unmarshalAliased(b, &w, myOrgAlias, required...); err != nil {
		return err
	}
	*m = MyOrgInfo{
		Org:       OrgInfo(w.Org),
		Role:      w.Role,
		JoinedAt:  w.JoinedAt,
		InvitedBy: w.InvitedBy,
	}
	return nil
}

// MarshalJSON writes m as one flat object in camelCase, the form
// [MyOrgInfo.UnmarshalJSON] reads.
func (m MyOrgInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(myOrgWire{
		Org:       orgPlain(m.Org),
		Role:      m.Role,
		JoinedAt:  m.JoinedAt,
		InvitedBy: m.InvitedBy,
	})
}

// CreateOrgRequest is the body of [Orgs.Create]
// (docs/remote/endpoints/storage.md#createorgrequest).
type CreateOrgRequest struct {
	// Slug is the new org's slug. It is required. The Rust client always
	// derives it from the name, as [Slugify] does.
	Slug string `json:"slug"`

	// Name is the display name.
	Name string `json:"name"`

	// Email is the contact email; an empty one is not sent.
	Email string `json:"email,omitzero"`
}

// UpdateOrgRequest is the body of [Orgs.Update]
// (docs/remote/endpoints/storage.md#updateorgrequest). Only the fields
// that are not nil are sent, so a request with none set is {}, and the
// server leaves the fields it is not sent unchanged.
type UpdateOrgRequest struct {
	// Name is the new display name.
	Name *string `json:"name,omitzero"`

	// Email is the new contact email.
	Email *string `json:"email,omitzero"`
}

// Slugify returns the slug the Rust client derives from an org's name
// (docs/remote/endpoints/storage.md#create_org): name lowercased, with its
// ASCII letters and digits kept and every run of other characters turned
// into one '-', and with no '-' at either end. "Acme Corp" is "acme-corp",
// and a name with no ASCII letter or digit, such as "!!!", is "".
//
// Lowercasing is Unicode's, as Rust's char::to_lowercase is, so the Kelvin
// sign 'K' (U+212A) becomes the letter 'k', and 'İ' (U+0130) becomes "i"
// followed by a combining dot, which is a separator.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	emit := func(r rune) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
			return
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	for _, r := range name {
		if r == 'İ' {
			// The one character whose lowercase is two: Go's
			// unicode.ToLower maps it to 'i' alone.
			emit('i')
			emit('̇')
			continue
		}
		emit(unicode.ToLower(r))
	}
	return strings.TrimRight(b.String(), "-")
}

// Orgs sends the organization requests of the management API
// (docs/remote/endpoints/storage.md#organizations). Get one from
// [Client.Orgs]; its zero value is not usable.
//
// Two requests span orgs and go to the apex, the configured server URL as
// serverurl.Server.Apex returns it: [Orgs.Create] and [Orgs.ListMine]. The
// others act on one org and go to an org base, as
// serverurl.Server.OrgBase returns it, which is what the Rust CLI sends
// them to. Neither is checked. A slug is sent as one path segment and
// refused, with an error wrapping [ErrInvalidArgument] and nothing sent,
// when it would address another path; see the package documentation.
//
// A failure is the server's status, as the Rust client's HttpError is: a
// 403 matches [remote.ErrForbidden], a 404 [remote.ErrNotFound] and a 409
// [remote.ErrConflict]. atomic-teams turns these into its own errors with a
// context string (docs/remote/endpoints/storage.md#error-mapping); that is
// the caller's to do here.
type Orgs struct {
	c *Client
}

// Orgs returns the organization requests sent through c.
func (c *Client) Orgs() Orgs {
	return Orgs{c: c}
}

// Create creates an org and returns it: POST /orgs. Its [Client]'s base
// must be the apex, as the Rust client's is. A slug that is taken is the
// server's 409, which matches [remote.ErrConflict].
//
// req.Slug is required, so an empty one is refused, with an error wrapping
// [ErrInvalidArgument], before anything is sent. The Rust client sets it to
// the slug of the name, which is [Slugify](req.Name), and would send an
// empty slug for a name such as "!!!".
func (o Orgs) Create(ctx context.Context, req CreateOrgRequest) (OrgInfo, error) {
	if req.Slug == "" {
		return OrgInfo{}, fmt.Errorf("%w: the org slug is empty", ErrInvalidArgument)
	}
	return call[OrgInfo](ctx, o.c, request{
		op:     "create_org",
		method: http.MethodPost,
		path:   "/orgs",
		body:   req,
	})
}

// ListMine returns every org the caller belongs to, with the caller's role
// in each: GET /orgs. Its [Client]'s base must be the apex, as the Rust
// client's is.
func (o Orgs) ListMine(ctx context.Context) ([]MyOrgInfo, error) {
	return call[[]MyOrgInfo](ctx, o.c, request{
		op:     "list_my_orgs",
		method: http.MethodGet,
		path:   "/orgs",
	})
}

// Get returns the org slug names: GET /orgs/{slug}. Its [Client]'s base is
// an org base, as the Rust client's is, and slug need not be that base's
// org. An org that does not exist is the server's 404, which matches
// [remote.ErrNotFound].
func (o Orgs) Get(ctx context.Context, slug string) (OrgInfo, error) {
	path, err := orgPath(slug, "")
	if err != nil {
		return OrgInfo{}, err
	}
	return call[OrgInfo](ctx, o.c, request{
		op:     "get_org",
		method: http.MethodGet,
		path:   path,
	})
}

// Update changes the fields req sets on the org slug names and returns the
// org: PUT /orgs/{slug}. Its [Client]'s base is an org base, as the Rust
// client's is.
func (o Orgs) Update(ctx context.Context, slug string, req UpdateOrgRequest) (OrgInfo, error) {
	path, err := orgPath(slug, "")
	if err != nil {
		return OrgInfo{}, err
	}
	return call[OrgInfo](ctx, o.c, request{
		op:     "update_org",
		method: http.MethodPut,
		path:   path,
		body:   req,
	})
}

// Delete deletes the org slug names: DELETE /orgs/{slug}. Its [Client]'s
// base is an org base, as the Rust client's is. The Rust client documents
// that the caller must be an owner and that anyone else is refused with a
// 403, which matches [remote.ErrForbidden]. Any 2xx response is success,
// whatever its body.
func (o Orgs) Delete(ctx context.Context, slug string) error {
	path, err := orgPath(slug, "")
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, o.c, request{
		op:     "delete_org",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}

// Upgrade asks the server to upgrade the plan of the org slug names and
// returns the org: POST /orgs/{slug}/upgrade. Its [Client]'s base is an org
// base, as the Rust client's is. The request has no body but still carries
// the Content-Type every request does, as the Rust client's post_empty
// sends it, and which plan the org moves to is the server's to decide.
func (o Orgs) Upgrade(ctx context.Context, slug string) (OrgInfo, error) {
	path, err := orgPath(slug, "/upgrade")
	if err != nil {
		return OrgInfo{}, err
	}
	return call[OrgInfo](ctx, o.c, request{
		op:     "upgrade_org",
		method: http.MethodPost,
		path:   path,
	})
}

// orgPath returns the escaped path /orgs/{slug} followed by suffix.
func orgPath(slug, suffix string) (string, error) {
	s, err := segment(slug)
	if err != nil {
		return "", err
	}
	return "/orgs/" + s + suffix, nil
}

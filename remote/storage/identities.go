package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/z5labs/atomic-go/identity"
)

// ErrKeyNotBound is the cause of the [remote.KindProtocol] for a
// [Identities.ByDID] reply whose public key is missing or is not the
// requested DID's key, so a server cannot bind a stranger's key to a DID.
var ErrKeyNotBound = errors.New("storage: the returned public key is not bound to the DID")

// IdentityInfo is a registered identity as the resolve and lookup requests
// report it (docs/remote/endpoints/storage.md#identityinfo).
//
// It is read from camelCase members, and publicKey may also be spelled
// public_key. Every member but publicKey is required; a member written
// twice, or under both spellings, is refused.
type IdentityInfo struct {
	// ID is the server's ID for the identity. It is not the
	// [identity.ID] the client computes from the key.
	ID uuid.UUID `json:"id"`

	// Name is the display name.
	Name string `json:"name"`

	// Status is the identity's lifecycle status, such as "active".
	Status string `json:"status"`

	// CreatedAt is when the identity was registered.
	CreatedAt time.Time `json:"createdAt"`

	// PublicKey is the identity's base32 Ed25519 public key as the server
	// sent it, or nil when it sent none or null, as older servers do. It is
	// not checked, except by [Identities.ByDID]; parse it with
	// [identity.ParsePublicKey].
	PublicKey *string `json:"publicKey"`
}

// identityAlias returns the member a snake_case spelling IdentityInfo also
// accepts stands for.
func identityAlias(name string) (string, bool) {
	if name == "public_key" {
		return "publicKey", true
	}
	return "", false
}

// UnmarshalJSON decodes an identity with the alias and required members
// [IdentityInfo] describes.
func (i *IdentityInfo) UnmarshalJSON(b []byte) error {
	type plain IdentityInfo
	return unmarshalAliased(b, (*plain)(i), identityAlias, "id", "name", "status", "createdAt")
}

// Identities sends the identity lookups of the management API
// (docs/remote/endpoints/storage.md#identities). Get one from
// [Client.Identities]; its zero value is not usable.
//
// Whether the server accepts a token on the lookups it documents as
// unauthenticated is not known, so a caller may send them with no
// credentials.
type Identities struct {
	c *Client
}

// Identities returns the identity lookups sent through c.
func (c *Client) Identities() Identities {
	return Identities{c: c}
}

// ResolveByEmail returns the identity registered with email:
// GET /identities/resolve?email={email}, with email percent-encoded as the
// Rust client encodes it, so "alice@example.com" is sent as
// "alice%40example.com". Its [Client]'s base must be an org base, which it
// does not check. An identity that does not exist is the server's 404,
// which matches [remote.ErrNotFound].
func (ids Identities) ResolveByEmail(ctx context.Context, email string) (IdentityInfo, error) {
	return call[IdentityInfo](ctx, ids.c, request{
		op:     "resolve_identity_by_email",
		method: http.MethodGet,
		path:   "/identities/resolve",
		query:  "email=" + component(email),
	})
}

// ResolveByName returns the identity whose display name is name:
// GET /identities/resolve?name={name}, with name percent-encoded as the
// Rust client encodes it, so "a b" is sent as "a%20b". An identity that does
// not exist is the server's 404, which matches [remote.ErrNotFound].
func (ids Identities) ResolveByName(ctx context.Context, name string) (IdentityInfo, error) {
	return call[IdentityInfo](ctx, ids.c, request{
		op:     "resolve_identity_by_name",
		method: http.MethodGet,
		path:   "/identities/resolve",
		query:  "name=" + component(name),
	})
}

// ByPublicKey returns the identity registered for key:
// GET /identities/by-key/{key}. key is sent as one path segment, as the
// Rust client sends it, so a DID's colons are not escaped; it is refused,
// with an error wrapping [ErrInvalidArgument] and nothing sent, when it
// would address another path. Its form is not checked: atomic identity
// lookup-key sends a 52-character base32 public key, and signer discovery
// sends a did:atomic DID. Which forms the server accepts is not known.
//
// The returned key is not checked either. Signer discovery, which sends
// this request to a remote's origin after a pull, accepts a returned key
// only when the DID it asked about Matches it ([identity.DID.Matches]); the
// signers to look up, caching what is found, and the 10-second budget each
// lookup has are the caller's, the last through ctx
// (docs/remote/flows.md#request-3-signer-discovery). [Identities.ByDID]
// makes the check itself.
func (ids Identities) ByPublicKey(ctx context.Context, key string) (IdentityInfo, error) {
	seg, err := segment(key)
	if err != nil {
		return IdentityInfo{}, err
	}
	return call[IdentityInfo](ctx, ids.c, request{
		op:     "resolve_identity_by_public_key",
		method: http.MethodGet,
		path:   "/identities/by-key/" + seg,
	})
}

// ByDID returns the identity did names: GET /identities/by-did/{did}, with
// the DID percent-encoded as the Rust client encodes it, so its colons are
// sent as %3A. The zero DID is refused, with an error wrapping
// [ErrInvalidArgument], before anything is sent.
//
// It succeeds only when the reply's public key parses and did Matches it
// ([identity.DID.Matches]), as either its did:atomic or its did:key, so a
// server cannot bind a stranger's key to the DID. So on success the
// returned PublicKey is never nil, and [identity.ParsePublicKey] of it
// always succeeds with a key did Matches. A missing key or one that
// is not did's is a [remote.KindProtocol] wrapping [ErrKeyNotBound], and a
// key that does not parse is a [remote.KindProtocol] wrapping the error
// from [identity.ParsePublicKey], which wraps [identity.ErrInvalidKey].
func (ids Identities) ByDID(ctx context.Context, did identity.DID) (IdentityInfo, error) {
	if did.IsZero() {
		return IdentityInfo{}, fmt.Errorf("%w: the DID is the zero DID", ErrInvalidArgument)
	}
	return callChecked(ctx, ids.c, request{
		op:     "resolve_identity_by_did",
		method: http.MethodGet,
		path:   "/identities/by-did/" + component(did.String()),
	}, func(info IdentityInfo) error {
		return checkBound(did, info)
	})
}

// checkBound reports whether info carries the key did names.
func checkBound(did identity.DID, info IdentityInfo) error {
	if info.PublicKey == nil {
		return fmt.Errorf("%w: the identity for %s has no public key", ErrKeyNotBound, did)
	}
	k, err := identity.ParsePublicKey(*info.PublicKey)
	if err != nil {
		return fmt.Errorf("storage: the public key returned for %s: %w", did, err)
	}
	if !did.Matches(k) {
		return fmt.Errorf("%w: the public key %s returned for %s is not its key", ErrKeyNotBound, k, did)
	}
	return nil
}

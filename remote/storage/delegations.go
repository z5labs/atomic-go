package storage

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/z5labs/atomic-go/delegation"
)

// DelegationInfo is a delegation certificate as the server holds it, as the
// delegation requests report it
// (docs/remote/endpoints/storage.md#delegationinfo).
//
// It is read from camelCase members only. Every member but expiresAt and
// certificate is required; a member written twice is refused.
type DelegationInfo struct {
	// ID is the delegation's URN, urn:atomic:delegation:<base32>, as the
	// server sent it. It is not checked; parse it with
	// [delegation.ParseID].
	ID string `json:"id"`

	// DelegatorIdentityID is the ID of the human who issued the
	// delegation.
	DelegatorIdentityID uuid.UUID `json:"delegatorIdentityId"`

	// DelegateIdentityID is the ID of the agent it was issued to.
	DelegateIdentityID uuid.UUID `json:"delegateIdentityId"`

	// Status is the delegation's status. The server's documentation lists
	// "active", "expired", "revoked" and "superseded".
	Status string `json:"status"`

	// IssuedAt is when the delegation was issued.
	IssuedAt time.Time `json:"issuedAt"`

	// ExpiresAt is when the delegation expires, or nil when the server sent
	// none or null.
	ExpiresAt *time.Time `json:"expiresAt"`

	// Certificate is the signed certificate the server sent, so it can be
	// read and verified again with [delegation.ParseCertificate], or nil
	// when the server sent none or null. It is not checked. Its members and
	// their values are the server's, written as compact JSON, which does
	// not change what a certificate's signature covers: that is computed
	// over the document's canonical form. It is a [jsontext.Value], the
	// json/v2 counterpart of json.RawMessage, as the rest of this package
	// uses json/v2.
	Certificate jsontext.Value `json:"certificate,omitzero"`
}

// UnmarshalJSON decodes a delegation with the required members
// [DelegationInfo] describes.
func (d *DelegationInfo) UnmarshalJSON(b []byte) error {
	type plain DelegationInfo
	var out plain
	if err := unmarshalAliased(b, &out, nil, "id", "delegatorIdentityId", "delegateIdentityId", "status", "issuedAt"); err != nil {
		return err
	}
	if out.Certificate.Kind() == 'n' {
		out.Certificate = nil
	}
	*d = DelegationInfo(out)
	return nil
}

// DelegationStatusInfo is the public status of a delegation, as
// [Delegations.Status] reports it
// (docs/remote/endpoints/storage.md#delegationstatusinfo).
//
// It is read from camelCase members only. Both members are required; a
// member written twice is refused.
type DelegationStatusInfo struct {
	// ID is the delegation's ID as the server sent it.
	ID string `json:"id"`

	// Revoked reports whether the delegation is on the server's deny-list.
	// The server's documentation says an unknown ID answers the same as a
	// live one, so false does not mean the server knows the delegation.
	Revoked bool `json:"revoked"`
}

// UnmarshalJSON decodes a delegation status with the required members
// [DelegationStatusInfo] describes.
func (s *DelegationStatusInfo) UnmarshalJSON(b []byte) error {
	type plain DelegationStatusInfo
	return unmarshalAliased(b, (*plain)(s), nil, "id", "revoked")
}

// PushDelegationRequest is the body of [Delegations.Push]
// (docs/remote/endpoints/storage.md#pushdelegationrequest).
type PushDelegationRequest struct {
	// Certificate is the signed certificate to publish, from
	// [delegation.Issue] or a parse of one, sent as the object the
	// delegator signed. It must not be the zero Certificate.
	Certificate delegation.Certificate `json:"certificate"`
}

// RevokeDelegationRequest is the body of [Delegations.Revoke]
// (docs/remote/endpoints/storage.md#revokedelegationrequest).
type RevokeDelegationRequest struct {
	// Revocation is the delegator's signed revocation, from
	// [delegation.Revoke] or a parse of one, sent as the object the
	// delegator signed. It must not be the zero Revocation.
	Revocation delegation.Revocation `json:"revocation"`
}

// Delegations sends the delegation requests of the management API
// (docs/remote/endpoints/storage.md#delegations). Get one from
// [Client.Delegations]; its zero value is not usable.
//
// Every request goes to the apex, as serverurl.Server.Apex returns it,
// which is not checked. A delegation ID is sent as its URN, percent-encoded
// as the Rust client encodes it, so its colons travel as %3A.
//
// A failure is the server's status: a 404 matches [remote.ErrNotFound].
type Delegations struct {
	c *Client
}

// Delegations returns the delegation requests sent through c.
func (c *Client) Delegations() Delegations {
	return Delegations{c: c}
}

// Push publishes a new or renewed certificate and returns it as the server
// holds it: POST /delegations. A req.Certificate that is the zero
// Certificate is refused, with an error wrapping [ErrInvalidArgument],
// before anything is sent. Its signature is not verified here; the server
// checks it.
func (d Delegations) Push(ctx context.Context, req PushDelegationRequest) (DelegationInfo, error) {
	if err := checkCertificate("the pushed certificate", req.Certificate); err != nil {
		return DelegationInfo{}, err
	}
	return call[DelegationInfo](ctx, d.c, request{
		op:     "push_delegation",
		method: http.MethodPost,
		path:   "/delegations",
		body:   req,
	})
}

// List returns the delegations the authenticated identity issued:
// GET /delegations.
func (d Delegations) List(ctx context.Context) ([]DelegationInfo, error) {
	return call[[]DelegationInfo](ctx, d.c, request{
		op:     "list_delegations",
		method: http.MethodGet,
		path:   "/delegations",
	})
}

// Revoke revokes the delegation id names and returns it as the server now
// holds it: POST /delegations/{id}/revoke. The ID of a [DelegationInfo]
// the server listed is its unchecked text; [delegation.ParseID] reads it,
// as does the certificate's own [delegation.Certificate.ID].
//
// It refuses, with an error wrapping [ErrInvalidArgument] and nothing sent,
// the zero id, which names no delegation, a req.Revocation that is the zero
// Revocation, and one that revokes a delegation other than id. Its
// signature is not verified here; the server checks it.
func (d Delegations) Revoke(ctx context.Context, id delegation.ID, req RevokeDelegationRequest) (DelegationInfo, error) {
	path, err := delegationPath(id, "/revoke")
	if err != nil {
		return DelegationInfo{}, err
	}
	if _, err := req.Revocation.MarshalJSON(); err != nil {
		return DelegationInfo{}, fmt.Errorf("%w: the revocation: %w", ErrInvalidArgument, err)
	}
	if revoked := req.Revocation.DelegationRevocation().Delegation; revoked != id {
		return DelegationInfo{}, fmt.Errorf("%w: the revocation revokes %s, not %s", ErrInvalidArgument, revoked, id)
	}
	return call[DelegationInfo](ctx, d.c, request{
		op:     "revoke_delegation",
		method: http.MethodPost,
		path:   path,
		body:   req,
	})
}

// SetDelegatorEpoch sets the authenticated identity's delegator epoch to
// now, which the Rust client documents invalidates every grant it has
// issued, to every agent, and returns the new epoch:
// POST /delegations/epoch. The request has no body but still carries the
// Content-Type every request does.
func (d Delegations) SetDelegatorEpoch(ctx context.Context) (EpochInfo, error) {
	return call[EpochInfo](ctx, d.c, request{
		op:     "set_delegator_epoch",
		method: http.MethodPost,
		path:   "/delegations/epoch",
	})
}

// Status returns the public status of the delegation id names:
// GET /delegations/{id}/status. The Rust client documents that the server
// answers it without authentication, so a caller may send it with no
// credentials. The zero id is refused, with an error wrapping
// [ErrInvalidArgument], before anything is sent.
func (d Delegations) Status(ctx context.Context, id delegation.ID) (DelegationStatusInfo, error) {
	path, err := delegationPath(id, "/status")
	if err != nil {
		return DelegationStatusInfo{}, err
	}
	return call[DelegationStatusInfo](ctx, d.c, request{
		op:     "delegation_status",
		method: http.MethodGet,
		path:   path,
	})
}

// delegationPath returns /delegations/{id} followed by rest, with id's URN
// percent-encoded, or an error wrapping [ErrInvalidArgument] for the zero
// id.
func delegationPath(id delegation.ID, rest string) (string, error) {
	if id == (delegation.ID{}) {
		return "", fmt.Errorf("%w: the delegation ID is the zero ID", ErrInvalidArgument)
	}
	return "/delegations/" + component(id.String()) + rest, nil
}

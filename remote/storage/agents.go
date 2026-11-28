package storage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
)

// AgentIdentityInfo is an agent identity enrolled under a human, as the
// agent requests report it
// (docs/remote/endpoints/storage.md#agentidentityinfo).
//
// It is read from camelCase members only, as the Rust client reads it.
// Every member but softwareAgent, delegationsValidFrom and
// publishedDelegations is required; a member written twice is refused.
type AgentIdentityInfo struct {
	// ID is the agent's identity ID: the ID [Agents.Get], [Agents.Retire],
	// [Agents.SetEpoch] and [Agents.ClearEpoch] take.
	ID uuid.UUID `json:"id"`

	// Name is the display name.
	Name string `json:"name"`

	// DID is the did:atomic of the agent's key, as the server sent it. It
	// is not checked; parse it with [identity.ParseDID].
	DID string `json:"did"`

	// ParentIdentityID is the ID of the human the agent acts for.
	ParentIdentityID uuid.UUID `json:"parentIdentityId"`

	// SoftwareAgent is a software label such as
	// "urn:atomic:agent:claude-code", or nil when the server sent none or
	// null.
	SoftwareAgent *string `json:"softwareAgent"`

	// Status is the agent's lifecycle status, such as "active".
	Status string `json:"status"`

	// CreatedAt is when the agent was enrolled.
	CreatedAt time.Time `json:"createdAt"`

	// DelegationsValidFrom is the agent's epoch: grants issued before it
	// are rejected. It is nil when the server sent none or null.
	DelegationsValidFrom *time.Time `json:"delegationsValidFrom"`

	// PublishedDelegations is the grants a client chose to publish, newest
	// first, which the server's documentation calls advisory and usually
	// incomplete. It is nil when the server sent none or null, which the
	// Rust client reads as empty.
	PublishedDelegations []DelegationInfo `json:"publishedDelegations"`
}

// UnmarshalJSON decodes an agent identity with the required members
// [AgentIdentityInfo] describes.
func (a *AgentIdentityInfo) UnmarshalJSON(b []byte) error {
	type plain AgentIdentityInfo
	return unmarshalAliased(b, (*plain)(a), nil, "id", "name", "did", "parentIdentityId", "status", "createdAt")
}

// EpochInfo is the result of setting an epoch, as [Agents.SetEpoch] and
// [Delegations.SetDelegatorEpoch] report it
// (docs/remote/endpoints/storage.md#epochinfo).
//
// It is read from camelCase members only. identityId is required; a member
// written twice is refused.
type EpochInfo struct {
	// IdentityID is the ID of the agent or delegator whose epoch changed.
	IdentityID uuid.UUID `json:"identityId"`

	// DelegationsValidFrom is the new epoch, or nil when the server sent
	// none or null, which means the epoch was cleared.
	DelegationsValidFrom *time.Time `json:"delegationsValidFrom"`
}

// UnmarshalJSON decodes an epoch with the required member [EpochInfo]
// describes.
func (e *EpochInfo) UnmarshalJSON(b []byte) error {
	type plain EpochInfo
	return unmarshalAliased(b, (*plain)(e), nil, "identityId")
}

// EnrollAgentRequest is the body of [Agents.Enroll]
// (docs/remote/endpoints/storage.md#enrollagentrequest).
//
// Its key and certificate are typed, so what is sent is always a base32 key
// and the signed certificate as a JSON object. [Agents.Enroll] refuses a
// request whose certificate is the zero Certificate, or whose PublicKey is
// not the key the certificate delegates to.
type EnrollAgentRequest struct {
	// Name is the agent identity's name.
	Name string `json:"name"`

	// Email is the agent's email address. It is always sent, as null when
	// it is nil, as the Rust client sends it.
	Email *string `json:"email"`

	// PublicKey is the agent's public key, sent as base32. It must be the
	// key Certificate delegates to.
	PublicKey identity.PublicKey `json:"publicKey"`

	// Certificate is the signed certificate delegating to PublicKey, from
	// [delegation.Issue] or a parse of one, sent as the object the
	// delegator signed. The caller must be its delegator.
	Certificate delegation.Certificate `json:"certificate"`
}

// Agents sends the agent identity requests of the management API
// (docs/remote/endpoints/storage.md#agents). Get one from [Client.Agents];
// its zero value is not usable.
//
// Every request goes to the apex, as serverurl.Server.Apex returns it,
// which is not checked. An agent ID is sent in its hyphenated form.
//
// A failure is the server's status: a 404 matches [remote.ErrNotFound],
// which the Rust CLI reads, on [Agents.Enroll], as a server that does not
// support agent identities yet.
type Agents struct {
	c *Client
}

// Agents returns the agent identity requests sent through c.
func (c *Client) Agents() Agents {
	return Agents{c: c}
}

// Enroll enrolls the agent req describes under the authenticated identity,
// which must be the delegator req.Certificate names, and returns it:
// POST /identities/agents.
//
// It refuses, with an error wrapping [ErrInvalidArgument] and nothing sent,
// a req.Certificate that is the zero Certificate, and a req.PublicKey that
// is not the certificate's delegateKey, which the server would refuse. It
// does not verify the certificate's signature; the server checks it against
// the delegator's registered key.
func (a Agents) Enroll(ctx context.Context, req EnrollAgentRequest) (AgentIdentityInfo, error) {
	if err := checkCertificate("the enrollment's certificate", req.Certificate); err != nil {
		return AgentIdentityInfo{}, err
	}
	if delegateKey := req.Certificate.AgentDelegation().DelegateKey; !delegateKey.Matches(req.PublicKey) {
		return AgentIdentityInfo{}, fmt.Errorf("%w: the enrollment's public key %s is not the certificate's delegate key %s", ErrInvalidArgument, req.PublicKey, delegateKey)
	}
	return call[AgentIdentityInfo](ctx, a.c, request{
		op:     "enroll_agent",
		method: http.MethodPost,
		path:   "/identities/agents",
		body:   req,
	})
}

// List returns the agents enrolled under the authenticated identity:
// GET /identities/agents.
func (a Agents) List(ctx context.Context) ([]AgentIdentityInfo, error) {
	return call[[]AgentIdentityInfo](ctx, a.c, request{
		op:     "list_agents",
		method: http.MethodGet,
		path:   "/identities/agents",
	})
}

// Get returns the agent id names: GET /identities/agents/{id}.
func (a Agents) Get(ctx context.Context, id uuid.UUID) (AgentIdentityInfo, error) {
	return call[AgentIdentityInfo](ctx, a.c, request{
		op:     "get_agent",
		method: http.MethodGet,
		path:   agentPath(id, ""),
	})
}

// Retire retires the agent id names: DELETE /identities/agents/{id}. The
// Rust client documents that the server revokes the agent's delegations
// and marks its key unusable, keeping the identity so past changes stay
// attributable. Any 2xx response is success, whatever its body.
func (a Agents) Retire(ctx context.Context, id uuid.UUID) error {
	_, err := call[struct{}](ctx, a.c, request{
		op:     "retire_agent",
		method: http.MethodDelete,
		path:   agentPath(id, ""),
	})
	return err
}

// SetEpoch sets the epoch of the agent id names to now, so that every grant
// issued to it before now stops being valid, including grants the server
// has never seen, and returns the new epoch:
// POST /identities/agents/{id}/epoch. The request has no body but still
// carries the Content-Type every request does, as the Rust client's
// post_empty sends it.
func (a Agents) SetEpoch(ctx context.Context, id uuid.UUID) (EpochInfo, error) {
	return call[EpochInfo](ctx, a.c, request{
		op:     "set_agent_epoch",
		method: http.MethodPost,
		path:   agentPath(id, "/epoch"),
	})
}

// ClearEpoch clears the epoch of the agent id names, which the Rust client
// documents brings its unexpired grants back:
// DELETE /identities/agents/{id}/epoch. Any 2xx response is success,
// whatever its body.
func (a Agents) ClearEpoch(ctx context.Context, id uuid.UUID) error {
	_, err := call[struct{}](ctx, a.c, request{
		op:     "clear_agent_epoch",
		method: http.MethodDelete,
		path:   agentPath(id, "/epoch"),
	})
	return err
}

// agentPath returns /identities/agents/{id} followed by rest.
func agentPath(id uuid.UUID, rest string) string {
	return "/identities/agents/" + component(id.String()) + rest
}

// checkCertificate refuses the zero Certificate, which holds no certificate
// and cannot be sent. what names c in the error.
func checkCertificate(what string, c delegation.Certificate) error {
	if _, err := c.MarshalJSON(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidArgument, what, err)
	}
	return nil
}

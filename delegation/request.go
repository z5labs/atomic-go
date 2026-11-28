package delegation

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/z5labs/atomic-go/canonical"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/internal/chrono"
)

// TypeRequest is the @type of a delegation request.
const TypeRequest = "AgentDelegationRequest"

// memberRequested is the request's time member
// (docs/remote/auth.md#delegationrequest).
const memberRequested = "requested"

// AgentDelegationRequest is the typed view of a delegation request's
// members (docs/remote/auth.md#delegationrequest). It is read off a
// [Request] by [Request.AgentDelegationRequest], and says only what the
// document says: nothing in it is trusted until [Request.Verify] has
// succeeded.
type AgentDelegationRequest struct {
	// Context is the JSON-LD @context, [canonical.Context] in every request
	// Atomic writes, or "" when the document has none or it is not a
	// string. Atomic neither requires nor checks it.
	Context string

	// Type is the @type, [TypeRequest] in a request that verifies.
	Type string

	// AttributedTo names the document's author, or is "" when the document
	// has none. Nothing verifies it; trust Delegate, not this.
	AttributedTo string

	// ContentHash is "blake3:" followed by 64 lowercase hex digits.
	ContentHash string

	// Delegate is the agent's did:atomic. [Request.Verify] checks that it
	// names the key DelegateKey carries.
	Delegate identity.DID

	// DelegateKey is the agent's did:key, the key the request is verified
	// against.
	DelegateKey identity.DID

	// DelegateName is the name the agent asks to be known by. It is a
	// label, and nothing verifies it.
	DelegateName string

	// Proof is the agent's signature.
	Proof canonical.Proof

	// Requested is when the request was made, in UTC.
	Requested time.Time

	// SoftwareAgent is the URN naming the kind of software that holds the
	// key, beginning [SoftwareAgentPrefix], or "" when the document has
	// none. It is descriptive only.
	SoftwareAgent string
}

// Request is an agent's self-signed request to be delegated to: an
// admitted [canonical.Document] and the [AgentDelegationRequest] read from
// it.
//
// An agent whose key is generated somewhere its human never sees, such as a
// CI runner, signs a request with that key and hands it over. The signature
// proves the agent holds the key the request names, and nothing more: a
// Request grants nothing until the human verifies it ([Request.Verify]),
// turns it into a [Grant] ([Request.Grant]) and signs that ([Issue]).
//
// Like a [Certificate], a Request keeps the document exactly as it was
// admitted, and constructing one checks the document's shape, not what it
// claims. The zero Request holds the empty document and does not verify.
type Request struct {
	doc   canonical.Document
	canon []byte
	view  AgentDelegationRequest
}

// wireRequest is the unsigned request [NewRequest] builds, before
// [canonical.Attest] adds attributedTo, contentHash and proof.
type wireRequest struct {
	Context       string       `json:"@context"`
	Type          string       `json:"@type"`
	Delegate      identity.DID `json:"delegate"`
	DelegateKey   identity.DID `json:"delegateKey"`
	DelegateName  string       `json:"delegateName"`
	Requested     string       `json:"requested"`
	SoftwareAgent string       `json:"softwareAgent,omitempty"`
}

// NewRequest signs a delegation request as agent, as Atomic's mint_request
// does (docs/remote/auth.md#delegation-requests). Its @context is
// [canonical.Context], its @type [TypeRequest], its delegate and
// delegateKey agent's did:atomic and did:key, and its delegateName name.
// requested is written in UTC and truncated to whole seconds, and
// softwareAgent is left out when it is "". The document is then signed with
// [canonical.Attest], which adds attributedTo, contentHash and the proof.
//
// NewRequest verifies what it signed before returning it, so a
// [identity.Signer] whose signatures are not its key's is an error wrapping
// [ErrVerifyRequest]. A nil agent, a zero requested and a softwareAgent
// without [SoftwareAgentPrefix] are errors wrapping [ErrInvalidRequest].
// An error from agent is returned wrapped.
func NewRequest(ctx context.Context, agent identity.Signer, name string, requested time.Time, softwareAgent string) (Request, error) {
	if agent == nil {
		return Request{}, fmt.Errorf("%w: new request: nil agent signer", ErrInvalidRequest)
	}
	if requested.IsZero() {
		return Request{}, fmt.Errorf("%w: new request: requested is the zero time", ErrInvalidRequest)
	}
	if softwareAgent != "" && !strings.HasPrefix(softwareAgent, SoftwareAgentPrefix) {
		return Request{}, fmt.Errorf("%w: new request: softwareAgent %q does not begin %q",
			ErrInvalidRequest, softwareAgent, SoftwareAgentPrefix)
	}
	key := agent.PublicKey()
	w := wireRequest{
		Context:       canonical.Context,
		Type:          TypeRequest,
		Delegate:      key.AtomicDID(),
		DelegateKey:   key.KeyDID(),
		DelegateName:  name,
		Requested:     chrono.FormatSeconds(requested.UTC().Truncate(time.Second)),
		SoftwareAgent: softwareAgent,
	}
	doc, err := canonical.FromValue(w)
	if err != nil {
		return Request{}, fmt.Errorf("%w: new request: %w", ErrInvalidRequest, err)
	}
	signed, err := canonical.Attest(ctx, doc, agent)
	if err != nil {
		return Request{}, fmt.Errorf("delegation: new request: %w", err)
	}
	r, err := RequestFromDocument(signed)
	if err != nil {
		return Request{}, fmt.Errorf("delegation: new request: %w", err)
	}
	if err := r.Verify(); err != nil {
		return Request{}, fmt.Errorf("delegation: new request: %w", err)
	}
	return r, nil
}

// ParseRequest admits b as a JSON document ([canonical.Admit]) and reads it
// as a request ([RequestFromDocument]). Every failure wraps
// [ErrInvalidRequest], and [canonical.ErrNotAdmitted] when admission
// refused b.
//
// Parsing is not trusting: verify the result before relying on it.
func ParseRequest(b []byte) (Request, error) {
	doc, err := canonical.Admit(b)
	if err != nil {
		return Request{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return RequestFromDocument(doc)
}

// RequestFromDocument reads d as a delegation request.
//
// Every member Required by docs/remote/auth.md#delegationrequest must be
// present and a string, except proof, which must decode as a
// [canonical.Proof]. delegate and delegateKey must parse
// ([identity.ParseDID]), and requested must be an RFC 3339 time.
// attributedTo and softwareAgent may be absent, null or a string. @context
// is not required, and is read only when it is a string. Other members are
// kept and ignored.
//
// Values are not checked: a request whose @type is not [TypeRequest], whose
// delegate and delegateKey disagree or whose signature is wrong is
// constructed, and refused by [Request.Verify]. Every failure wraps
// [ErrInvalidRequest].
func RequestFromDocument(d canonical.Document) (Request, error) {
	view, err := parseAgentDelegationRequest(d)
	if err != nil {
		return Request{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	canon, err := d.Canonical()
	if err != nil {
		return Request{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return Request{doc: d, canon: canon, view: view}, nil
}

// parseAgentDelegationRequest reads the members of d.
func parseAgentDelegationRequest(d canonical.Document) (AgentDelegationRequest, error) {
	var r AgentDelegationRequest
	var err error
	r.Context, _ = d.Text(memberContext)

	if r.Type, err = requiredText(d, memberType); err != nil {
		return AgentDelegationRequest{}, err
	}
	if r.AttributedTo, err = optionalText(d, memberAttributedTo); err != nil {
		return AgentDelegationRequest{}, err
	}
	if r.ContentHash, err = requiredText(d, memberContentHash); err != nil {
		return AgentDelegationRequest{}, err
	}
	for _, m := range []struct {
		name string
		dst  *identity.DID
	}{
		{memberDelegate, &r.Delegate},
		{memberDelegateKey, &r.DelegateKey},
	} {
		s, err := requiredText(d, m.name)
		if err != nil {
			return AgentDelegationRequest{}, err
		}
		if *m.dst, err = identity.ParseDID(s); err != nil {
			return AgentDelegationRequest{}, fmt.Errorf("member %q: %w", m.name, err)
		}
	}
	if r.DelegateName, err = requiredText(d, memberDelegateName); err != nil {
		return AgentDelegationRequest{}, err
	}
	if r.Proof, err = canonical.ProofOf(d); err != nil {
		return AgentDelegationRequest{}, fmt.Errorf("member %q: %w", memberProof, err)
	}
	requested, err := requiredText(d, memberRequested)
	if err != nil {
		return AgentDelegationRequest{}, err
	}
	if r.Requested, err = chrono.Parse(requested); err != nil {
		return AgentDelegationRequest{}, fmt.Errorf("member %q: %w", memberRequested, err)
	}
	if r.SoftwareAgent, err = optionalText(d, memberSoftwareAgent); err != nil {
		return AgentDelegationRequest{}, err
	}
	return r, nil
}

// AgentDelegationRequest returns the members read from the request. They
// are untrusted until the request has been verified.
func (r Request) AgentDelegationRequest() AgentDelegationRequest {
	return r.view
}

// Document returns the document the request was read from, with every
// member it was admitted with.
func (r Request) Document() canonical.Document {
	return r.doc
}

// MarshalJSON returns the request's canonical bytes, proof included. It
// implements [json.Marshaler] and fails only for the zero Request, which
// holds no request.
func (r Request) MarshalJSON() ([]byte, error) {
	if r.canon == nil {
		return nil, fmt.Errorf("%w: the zero Request holds no request", ErrInvalidRequest)
	}
	return slices.Clone(r.canon), nil
}

// UnmarshalJSON reads b with [ParseRequest] and replaces r with the result.
// It implements [json.Unmarshaler]. JSON null is refused, like any other
// value that is not a request. r is unchanged on failure.
func (r *Request) UnmarshalJSON(b []byte) error {
	v, err := ParseRequest(b)
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// Verify checks the request's self-signature against the key its own
// delegateKey carries, as Atomic's verify_request does:
//
//  1. @type is [TypeRequest].
//  2. delegateKey is a did:key.
//  3. The document verifies against that key ([canonical.VerifyAttested]):
//     its contentHash recomputes ([canonical.ErrContentHash]), and its
//     proof is Atomic's, signed by the key and naming it
//     ([canonical.ErrProof]).
//  4. delegate is that key's did:atomic. Atomic reads it as a did:atomic,
//     so the did:key form of the same key is refused.
//
// The first failing step's error is returned, wrapping [ErrVerifyRequest],
// and the canonical error named above where there is one.
//
// A request that verifies proves that whoever produced it holds the private
// half of the key it names. It does not say who that is, and it carries no
// authority: only the certificate a human issues from it does.
func (r Request) Verify() error {
	t, _ := r.doc.Text(memberType)
	if t != TypeRequest {
		return fmt.Errorf("%w: @type is %q, want %q", ErrVerifyRequest, t, TypeRequest)
	}
	key, ok := r.view.DelegateKey.PublicKey()
	if !ok {
		return fmt.Errorf("%w: delegateKey %q is not a did:key", ErrVerifyRequest, r.view.DelegateKey)
	}
	if err := canonical.VerifyAttested(r.doc, key); err != nil {
		return fmt.Errorf("%w: %w", ErrVerifyRequest, err)
	}
	if r.view.Delegate != key.AtomicDID() {
		return fmt.Errorf("%w: delegate %s is not the did:atomic of delegateKey %s", ErrVerifyRequest, r.view.Delegate, r.view.DelegateKey)
	}
	return nil
}

// Grant returns the [Grant] that turns the request into a certificate when
// a human signs it with [Issue]: the agent's key, name and softwareAgent
// from the request, and delegatorName, issued, expires and scope as given.
//
// Grant reads the request as it stands and checks nothing. Call
// [Request.Verify] first: countersigning a request that does not verify
// would delegate to a key nobody has shown they hold. A request whose
// delegateKey is not a did:key gives a Grant with the zero DelegateKey,
// which [Issue] refuses.
func (r Request) Grant(delegatorName string, issued, expires time.Time, scope Scope) Grant {
	key, _ := r.view.DelegateKey.PublicKey()
	return Grant{
		DelegatorName: delegatorName,
		DelegateKey:   key,
		DelegateName:  r.view.DelegateName,
		Issued:        issued,
		Expires:       expires,
		Scope:         scope,
		SoftwareAgent: r.view.SoftwareAgent,
	}
}

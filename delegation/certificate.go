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

const (
	// TypeAgentDelegation is the @type of a delegation certificate.
	TypeAgentDelegation = "AgentDelegation"

	// SoftwareAgentPrefix begins a certificate's softwareAgent, the URN
	// naming the kind of software that holds the delegate's key, as in
	// urn:atomic:agent:claude-code.
	SoftwareAgentPrefix = "urn:atomic:agent:"
)

// Member names of a certificate (docs/remote/auth.md#agentdelegation).
const (
	memberContext       = "@context"
	memberID            = "@id"
	memberType          = "@type"
	memberAttributedTo  = "attributedTo"
	memberContentHash   = "contentHash"
	memberDelegate      = "delegate"
	memberDelegateKey   = "delegateKey"
	memberDelegateName  = "delegateName"
	memberDelegator     = "delegator"
	memberDelegatorKey  = "delegatorKey"
	memberDelegatorName = "delegatorName"
	memberExpires       = "expires"
	memberIssued        = "issued"
	memberProof         = "proof"
	memberScope         = "scope"
	memberSoftwareAgent = "softwareAgent"
)

// AgentDelegation is the typed view of a delegation certificate's members
// (docs/remote/auth.md#agentdelegation). It is read off a [Certificate] by
// [Certificate.AgentDelegation], and says only what the document says:
// nothing in it is trusted until [Certificate.Verify] has succeeded.
type AgentDelegation struct {
	// Context is the JSON-LD @context, [canonical.Context] in every
	// certificate Atomic writes, or "" when the document has none or it is
	// not a string. Atomic neither requires nor checks it.
	Context string

	// ID is the certificate's @id. [Certificate.Verify] checks that it is
	// the URN of [NewID] of the delegator, the delegate and Issued.
	ID ID

	// Type is the @type, [TypeAgentDelegation] in a certificate that
	// verifies.
	Type string

	// AttributedTo is the signer's did:atomic, filled in at signing, or ""
	// when the document has none.
	AttributedTo string

	// ContentHash is "blake3:" followed by 64 lowercase hex digits.
	ContentHash string

	// Delegate is the agent's did:atomic.
	Delegate identity.DID

	// DelegateKey is the agent's did:key, the source of its public key.
	DelegateKey identity.DID

	// DelegateName is the agent's name when the certificate was issued. It
	// is a label, and nothing verifies it.
	DelegateName string

	// Delegator is the human's did:atomic.
	Delegator identity.DID

	// DelegatorKey is the human's did:key.
	DelegatorKey identity.DID

	// DelegatorName is the human's name when the certificate was issued. It
	// is a label, and nothing verifies it.
	DelegatorName string

	// Expires is when the certificate stops being valid, in UTC, or the
	// zero time when the document has no expires or it is null, in which
	// case the certificate never expires.
	Expires time.Time

	// Issued is when the certificate was issued, in UTC.
	Issued time.Time

	// Proof is the delegator's signature.
	Proof canonical.Proof

	// Scope is what the agent may do.
	Scope Scope

	// SoftwareAgent is the URN naming the kind of software that holds the
	// key, beginning [SoftwareAgentPrefix], or "" when the document has
	// none. It is descriptive only.
	SoftwareAgent string
}

// Certificate is a delegation certificate: an admitted [canonical.Document]
// and the [AgentDelegation] read from it.
//
// A Certificate keeps the document exactly as it was admitted, including
// members this package does not model, so [Certificate.HeaderValue] and
// [Certificate.MarshalJSON] write what the delegator signed, and a
// certificate from a newer Atomic still verifies.
//
// Constructing one checks the document's shape: that every Required member
// is present and of the right type, and that each DID, time, id, proof and
// scope parses. It checks nothing the document claims. A Certificate is
// untrusted until [Certificate.Verify] or [Certificate.VerifySelfContained]
// has succeeded on it.
//
// The zero Certificate holds the empty document and verifies against
// nothing.
type Certificate struct {
	doc   canonical.Document
	canon []byte
	view  AgentDelegation
}

// Grant is what [Issue] signs: who the agent is, what it may do and for how
// long.
type Grant struct {
	// DelegatorName is the human's name, a label written into the
	// certificate.
	DelegatorName string

	// DelegateKey is the agent's public key. It must not be the zero key.
	DelegateKey identity.PublicKey

	// DelegateName is the agent's name, a label written into the
	// certificate.
	DelegateName string

	// Issued is the issue time. It is written in UTC and truncated to whole
	// seconds, and it must not be the zero time.
	Issued time.Time

	// Expires is when the certificate stops being valid, truncated to
	// whole seconds, or the zero time for a certificate that never expires,
	// which is written without an expires member. When set it must be
	// after Issued.
	Expires time.Time

	// Scope is what the agent may do. Every permission must be one of the
	// eight [Permission] constants.
	Scope Scope

	// SoftwareAgent names the kind of software that holds the agent's key,
	// such as "urn:atomic:agent:claude-code", or is "" to leave it out.
	// When set it must begin [SoftwareAgentPrefix].
	SoftwareAgent string
}

// wireCertificate is the unsigned certificate [Issue] builds, before
// [canonical.Attest] adds attributedTo, contentHash and proof. Member order
// does not matter: the document is canonicalized.
type wireCertificate struct {
	Context       string       `json:"@context"`
	ID            ID           `json:"@id"`
	Type          string       `json:"@type"`
	Delegate      identity.DID `json:"delegate"`
	DelegateKey   identity.DID `json:"delegateKey"`
	DelegateName  string       `json:"delegateName"`
	Delegator     identity.DID `json:"delegator"`
	DelegatorKey  identity.DID `json:"delegatorKey"`
	DelegatorName string       `json:"delegatorName"`
	Expires       string       `json:"expires,omitempty"`
	Issued        string       `json:"issued"`
	Scope         Scope        `json:"scope"`
	SoftwareAgent string       `json:"softwareAgent,omitempty"`
}

// Issue signs g as a certificate from delegator, as Atomic's mint does
// (docs/remote/auth.md#signing-a-delegation-document). The certificate's
// @context is [canonical.Context], its @type [TypeAgentDelegation], and its
// @id the URN of [NewID] of the two keys' identity ids and the issue time.
// Its times are written in UTC with whole seconds, expires is left out when
// g.Expires is zero, and softwareAgent when g.SoftwareAgent is "". The
// document is then signed with [canonical.Attest], which adds attributedTo,
// contentHash and the proof.
//
// Issue verifies what it signed against delegator's public key before
// returning it, so a [identity.Signer] whose signatures are not its key's is
// an error wrapping [ErrVerify].
//
// A nil delegator, a zero g.DelegateKey, a zero g.Issued, a g.Expires that
// is set but not after g.Issued, a g.SoftwareAgent without
// [SoftwareAgentPrefix] and a scope holding an unknown permission are
// errors wrapping [ErrInvalid]. An error from delegator is returned wrapped.
func Issue(ctx context.Context, delegator identity.Signer, g Grant) (Certificate, error) {
	if delegator == nil {
		return Certificate{}, fmt.Errorf("%w: issue: nil delegator signer", ErrInvalid)
	}
	if g.DelegateKey == (identity.PublicKey{}) {
		return Certificate{}, fmt.Errorf("%w: issue: delegate key is the zero key", ErrInvalid)
	}
	if g.Issued.IsZero() {
		return Certificate{}, fmt.Errorf("%w: issue: issued is the zero time", ErrInvalid)
	}
	issued := g.Issued.UTC().Truncate(time.Second)
	var expires time.Time
	if !g.Expires.IsZero() {
		expires = g.Expires.UTC().Truncate(time.Second)
		if !expires.After(issued) {
			return Certificate{}, fmt.Errorf("%w: issue: expires %s is not after issued %s",
				ErrInvalid, chrono.FormatSeconds(expires), chrono.FormatSeconds(issued))
		}
	}
	if g.SoftwareAgent != "" && !strings.HasPrefix(g.SoftwareAgent, SoftwareAgentPrefix) {
		return Certificate{}, fmt.Errorf("%w: issue: softwareAgent %q does not begin %q",
			ErrInvalid, g.SoftwareAgent, SoftwareAgentPrefix)
	}

	key := delegator.PublicKey()
	w := wireCertificate{
		Context:       canonical.Context,
		ID:            NewID(key.ID(), g.DelegateKey.ID(), issued),
		Type:          TypeAgentDelegation,
		Delegate:      g.DelegateKey.AtomicDID(),
		DelegateKey:   g.DelegateKey.KeyDID(),
		DelegateName:  g.DelegateName,
		Delegator:     key.AtomicDID(),
		DelegatorKey:  key.KeyDID(),
		DelegatorName: g.DelegatorName,
		Issued:        chrono.FormatSeconds(issued),
		Scope:         g.Scope,
		SoftwareAgent: g.SoftwareAgent,
	}
	if !expires.IsZero() {
		w.Expires = chrono.FormatSeconds(expires)
	}
	doc, err := canonical.FromValue(w)
	if err != nil {
		return Certificate{}, fmt.Errorf("%w: issue: %w", ErrInvalid, err)
	}
	signed, err := canonical.Attest(ctx, doc, delegator)
	if err != nil {
		return Certificate{}, fmt.Errorf("delegation: issue: %w", err)
	}
	c, err := CertificateFromDocument(signed)
	if err != nil {
		return Certificate{}, fmt.Errorf("delegation: issue: %w", err)
	}
	if err := c.Verify(key); err != nil {
		return Certificate{}, fmt.Errorf("delegation: issue: %w", err)
	}
	return c, nil
}

// ParseCertificate admits b as a JSON document ([canonical.Admit]) and reads
// it as a certificate ([CertificateFromDocument]). Every failure wraps
// [ErrInvalid], and [canonical.ErrNotAdmitted] when admission refused b.
//
// Parsing is not trusting: verify the result before relying on it.
func ParseCertificate(b []byte) (Certificate, error) {
	doc, err := canonical.Admit(b)
	if err != nil {
		return Certificate{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return CertificateFromDocument(doc)
}

// CertificateFromDocument reads d as a certificate, failing as Atomic's
// parse does on a Required member that is missing, which is step 6 of
// docs/remote/auth.md#verifying-a-certificate.
//
// Every member Required by docs/remote/auth.md#agentdelegation must be
// present and a string, except proof and scope, which must decode as a
// [canonical.Proof] and a [Scope]. Each DID must parse ([identity.ParseDID]),
// @id must be a delegation id ([ParseID]), and issued an RFC 3339 time.
// expires may be absent or null, which is a certificate that never expires,
// or an RFC 3339 time. attributedTo and softwareAgent may be absent, null or
// a string. @context is not required, and is read only when it is a string.
// Other members are kept and ignored.
//
// Values are not checked: a certificate whose @type is not
// [TypeAgentDelegation], whose parties disagree or whose signature is
// wrong is constructed, and refused by [Certificate.Verify]. Every failure
// wraps [ErrInvalid].
func CertificateFromDocument(d canonical.Document) (Certificate, error) {
	view, err := parseAgentDelegation(d)
	if err != nil {
		return Certificate{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	canon, err := d.Canonical()
	if err != nil {
		return Certificate{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return Certificate{doc: d, canon: canon, view: view}, nil
}

// parseAgentDelegation reads the members of d.
func parseAgentDelegation(d canonical.Document) (AgentDelegation, error) {
	var a AgentDelegation
	var err error
	a.Context, _ = d.Text(memberContext)

	idText, err := requiredText(d, memberID)
	if err != nil {
		return AgentDelegation{}, err
	}
	if a.ID, err = ParseID(idText); err != nil {
		return AgentDelegation{}, fmt.Errorf("member %q: %w", memberID, err)
	}
	if a.Type, err = requiredText(d, memberType); err != nil {
		return AgentDelegation{}, err
	}
	if a.AttributedTo, err = optionalText(d, memberAttributedTo); err != nil {
		return AgentDelegation{}, err
	}
	if a.ContentHash, err = requiredText(d, memberContentHash); err != nil {
		return AgentDelegation{}, err
	}
	for _, m := range []struct {
		name string
		dst  *identity.DID
	}{
		{memberDelegate, &a.Delegate},
		{memberDelegateKey, &a.DelegateKey},
		{memberDelegator, &a.Delegator},
		{memberDelegatorKey, &a.DelegatorKey},
	} {
		s, err := requiredText(d, m.name)
		if err != nil {
			return AgentDelegation{}, err
		}
		if *m.dst, err = identity.ParseDID(s); err != nil {
			return AgentDelegation{}, fmt.Errorf("member %q: %w", m.name, err)
		}
	}
	if a.DelegateName, err = requiredText(d, memberDelegateName); err != nil {
		return AgentDelegation{}, err
	}
	if a.DelegatorName, err = requiredText(d, memberDelegatorName); err != nil {
		return AgentDelegation{}, err
	}
	expires, err := optionalText(d, memberExpires)
	if err != nil {
		return AgentDelegation{}, err
	}
	if isText(d, memberExpires) {
		if a.Expires, err = chrono.Parse(expires); err != nil {
			return AgentDelegation{}, fmt.Errorf("member %q: %w", memberExpires, err)
		}
	}
	issued, err := requiredText(d, memberIssued)
	if err != nil {
		return AgentDelegation{}, err
	}
	if a.Issued, err = chrono.Parse(issued); err != nil {
		return AgentDelegation{}, fmt.Errorf("member %q: %w", memberIssued, err)
	}
	if a.Proof, err = canonical.ProofOf(d); err != nil {
		return AgentDelegation{}, fmt.Errorf("member %q: %w", memberProof, err)
	}
	if !d.Has(memberScope) {
		return AgentDelegation{}, fmt.Errorf("member %q is missing", memberScope)
	}
	if err := d.Decode(memberScope, &a.Scope); err != nil {
		return AgentDelegation{}, err
	}
	if a.SoftwareAgent, err = optionalText(d, memberSoftwareAgent); err != nil {
		return AgentDelegation{}, err
	}
	return a, nil
}

// isText reports whether d's member name is a string.
func isText(d canonical.Document, name string) bool {
	_, ok := d.Text(name)
	return ok
}

// requiredText returns the string member name, which must be present.
func requiredText(d canonical.Document, name string) (string, error) {
	if s, ok := d.Text(name); ok {
		return s, nil
	}
	if d.Has(name) {
		return "", fmt.Errorf("member %q is not a string", name)
	}
	return "", fmt.Errorf("member %q is missing", name)
}

// optionalText returns the string member name, or "" when it is absent or
// null.
func optionalText(d canonical.Document, name string) (string, error) {
	if s, ok := d.Text(name); ok {
		return s, nil
	}
	v, ok := d.Member(name)
	if !ok || v.Kind() == 'n' {
		return "", nil
	}
	return "", fmt.Errorf("member %q is neither a string nor null", name)
}

// AgentDelegation returns the members read from the certificate. They are
// untrusted until the certificate has been verified. The scope's lists are
// copies, so changing them does not change the certificate.
func (c Certificate) AgentDelegation() AgentDelegation {
	a := c.view
	a.Scope.Permissions = slices.Clone(a.Scope.Permissions)
	a.Scope.Servers = slices.Clone(a.Scope.Servers)
	a.Scope.Workspaces = slices.Clone(a.Scope.Workspaces)
	a.Scope.Projects = slices.Clone(a.Scope.Projects)
	a.Scope.Views = slices.Clone(a.Scope.Views)
	if a.Scope.MaxChanges != nil {
		n := *a.Scope.MaxChanges
		a.Scope.MaxChanges = &n
	}
	return a
}

// ID returns the certificate's @id, as the document states it.
func (c Certificate) ID() ID {
	return c.view.ID
}

// Document returns the document the certificate was read from, with every
// member it was admitted with.
func (c Certificate) Document() canonical.Document {
	return c.doc
}

// HeaderValue returns the certificate as the Atomic-Delegation header
// carries it: the unpadded base64url of its canonical bytes, proof included
// (docs/remote/auth.md#header-encoding). The same certificate always
// encodes the same way. It returns "" for the zero Certificate.
func (c Certificate) HeaderValue() string {
	if c.canon == nil {
		return ""
	}
	return encodeHeader(c.canon)
}

// MarshalJSON returns the certificate's canonical bytes. It implements
// [json.Marshaler] and fails only for the zero Certificate, which holds no
// certificate.
func (c Certificate) MarshalJSON() ([]byte, error) {
	if c.canon == nil {
		return nil, fmt.Errorf("%w: the zero Certificate holds no certificate", ErrInvalid)
	}
	return slices.Clone(c.canon), nil
}

// UnmarshalJSON reads b with [ParseCertificate] and replaces c with the
// result. It implements [json.Unmarshaler]. JSON null is refused, like any
// other value that is not a certificate. c is unchanged on failure.
func (c *Certificate) UnmarshalJSON(b []byte) error {
	v, err := ParseCertificate(b)
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// Verify checks the certificate against delegatorKey, the key the server
// holds for the delegator, in the nine steps of
// docs/remote/auth.md#verifying-a-certificate:
//
//  1. @type is [TypeAgentDelegation].
//  2. contentHash recomputes ([canonical.ErrContentHash]).
//  3. The proof's @type, cryptosuite and proofPurpose are Atomic's
//     ([canonical.ErrProof], as are 4 and 5).
//  4. The signature verifies against delegatorKey over the canonical bytes
//     of the document without its proof.
//  5. The proof's verificationMethod names delegatorKey, in either DID
//     form.
//  6. Every Required member is present. Constructing the Certificate
//     already checked this, so a Certificate never fails here.
//  7. delegator is delegatorKey's did:atomic, and delegatorKey is its
//     did:key.
//  8. delegate is the did:atomic of the key delegateKey carries, and
//     delegateKey is a did:key.
//  9. @id is exactly the URN of [NewID] of the two keys' identity ids and
//     issued.
//
// The first failing step's error is returned, wrapping [ErrVerify], and the
// canonical error named above where there is one. Verify checks neither
// expiry ([Certificate.Expired]) nor revocation.
func (c Certificate) Verify(delegatorKey identity.PublicKey) error {
	t, _ := c.doc.Text(memberType)
	if t != TypeAgentDelegation {
		return fmt.Errorf("%w: @type is %q, want %q", ErrVerify, t, TypeAgentDelegation)
	}
	if err := canonical.VerifyAttested(c.doc, delegatorKey); err != nil {
		return fmt.Errorf("%w: %w", ErrVerify, err)
	}
	a := c.view
	if a.Delegator != delegatorKey.AtomicDID() {
		return fmt.Errorf("%w: delegator %s is not the did:atomic of %s", ErrVerify, a.Delegator, delegatorKey)
	}
	if a.DelegatorKey != delegatorKey.KeyDID() {
		return fmt.Errorf("%w: delegatorKey %s is not the did:key of %s", ErrVerify, a.DelegatorKey, delegatorKey)
	}
	agent, ok := a.DelegateKey.PublicKey()
	if !ok {
		return fmt.Errorf("%w: delegateKey %s is not a did:key", ErrVerify, a.DelegateKey)
	}
	if a.Delegate != agent.AtomicDID() {
		return fmt.Errorf("%w: delegate %s is not the did:atomic of delegateKey %s", ErrVerify, a.Delegate, a.DelegateKey)
	}
	want := NewID(delegatorKey.ID(), agent.ID(), a.Issued).URN()
	if got, _ := c.doc.Text(memberID); got != want {
		return fmt.Errorf("%w: @id %q does not recompute: want %q", ErrVerify, got, want)
	}
	return nil
}

// VerifySelfContained verifies the certificate as [Certificate.Verify] does,
// against the key its own delegatorKey carries. That proves the document is
// intact and was signed by the key it names; it does not prove the key
// belongs to the person you think. Atomic's client uses it for the
// certificates it holds. A delegatorKey that is not a did:key is an error
// wrapping [ErrVerify].
func (c Certificate) VerifySelfContained() error {
	key, ok := c.view.DelegatorKey.PublicKey()
	if !ok {
		return fmt.Errorf("%w: delegatorKey %q is not a did:key", ErrVerify, c.view.DelegatorKey)
	}
	return c.Verify(key)
}

// Expired reports whether the certificate has expired at now: whether it has
// an expiry and that expiry is before now. A certificate is still valid at
// exactly its expiry, and one without an expiry never expires
// (docs/remote/auth.md#expiry).
func (c Certificate) Expired(now time.Time) bool {
	return !c.view.Expires.IsZero() && c.view.Expires.Before(now)
}

// Allows reports whether the certificate allows p on r at now: it has not
// [Certificate.Expired], and its scope allows p on r ([Scope.Allows]).
//
// Allows reads the certificate as it stands and checks neither its
// signature, nor revocation, nor the delegator's own grants. Verify the
// certificate first.
func (c Certificate) Allows(p Permission, r ResourceRef, now time.Time) bool {
	return !c.Expired(now) && c.view.Scope.Allows(p, r)
}

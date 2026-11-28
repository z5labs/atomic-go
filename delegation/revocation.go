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

// TypeRevocation is the @type of a delegation revocation.
const TypeRevocation = "DelegationRevocation"

// Member names of a revocation (docs/remote/auth.md#delegationrevocation)
// that a certificate does not have.
const (
	memberDelegation = "delegation"
	memberReason     = "reason"
	memberRevokedAt  = "revokedAt"
)

// DelegationRevocation is the typed view of a revocation's members
// (docs/remote/auth.md#delegationrevocation). It is read off a [Revocation]
// by [Revocation.DelegationRevocation], and says only what the document
// says: nothing in it is trusted until [Revocation.Verify] has succeeded.
type DelegationRevocation struct {
	// Context is the JSON-LD @context, [canonical.Context] in every
	// revocation Atomic writes, or "" when the document has none or it is
	// not a string. Atomic neither requires nor checks it.
	Context string

	// Type is the @type, [TypeRevocation] in a revocation that verifies.
	Type string

	// AttributedTo names the document's author, or is "" when the document
	// has none. Nothing verifies it; trust Delegator, not this.
	AttributedTo string

	// ContentHash is "blake3:" followed by 64 lowercase hex digits.
	ContentHash string

	// Delegation is the id of the certificate being revoked.
	Delegation ID

	// Delegator is the revoking human's did:atomic. [Revocation.Verify]
	// checks that it names the verifying key.
	Delegator identity.DID

	// Proof is the delegator's signature.
	Proof canonical.Proof

	// Reason is why the human revoked the certificate, or "" when the
	// document gives no reason.
	Reason string

	// RevokedAt is when the revocation was issued, in UTC.
	RevokedAt time.Time
}

// Revocation is a delegator's signed withdrawal of a delegation
// certificate: an admitted [canonical.Document] and the
// [DelegationRevocation] read from it.
//
// Like a [Certificate], a Revocation keeps the document exactly as it was
// admitted, including members this package does not model, so
// [Revocation.MarshalJSON] writes what the delegator signed. Constructing
// one checks the document's shape, not what it claims: a Revocation is
// untrusted until [Revocation.Verify] has succeeded on it.
//
// The zero Revocation holds the empty document and verifies against
// nothing.
type Revocation struct {
	doc   canonical.Document
	canon []byte
	view  DelegationRevocation
}

// wireRevocation is the unsigned revocation [Revoke] builds, before
// [canonical.Attest] adds attributedTo, contentHash and proof.
type wireRevocation struct {
	Context    string       `json:"@context"`
	Type       string       `json:"@type"`
	Delegation ID           `json:"delegation"`
	Delegator  identity.DID `json:"delegator"`
	Reason     string       `json:"reason,omitempty"`
	RevokedAt  string       `json:"revokedAt"`
}

// Revoke signs a revocation of the certificate named id, as Atomic's
// mint_revocation does (docs/remote/auth.md#revocation-and-epochs). Its
// @context is [canonical.Context], its @type [TypeRevocation], its
// delegation id's URN and its delegator signer's did:atomic. revokedAt is
// written in UTC and truncated to whole seconds, and reason is left out
// when it is "". The document is then signed with [canonical.Attest], which
// adds attributedTo, contentHash and the proof.
//
// Only the delegator of a certificate can revoke it, so signer must hold
// the delegator's key; Revoke cannot check that, since it is given only the
// certificate's id, and a revocation signed by anyone else fails
// [Revocation.Verify] against the delegator's key.
//
// Revoke verifies what it signed against signer's public key before
// returning it, so a [identity.Signer] whose signatures are not its key's
// is an error wrapping [ErrVerifyRevocation]. A nil signer and a zero
// revokedAt are errors wrapping [ErrInvalidRevocation]. An error from
// signer is returned wrapped.
func Revoke(ctx context.Context, signer identity.Signer, id ID, revokedAt time.Time, reason string) (Revocation, error) {
	if signer == nil {
		return Revocation{}, fmt.Errorf("%w: revoke: nil signer", ErrInvalidRevocation)
	}
	if revokedAt.IsZero() {
		return Revocation{}, fmt.Errorf("%w: revoke: revokedAt is the zero time", ErrInvalidRevocation)
	}
	key := signer.PublicKey()
	w := wireRevocation{
		Context:    canonical.Context,
		Type:       TypeRevocation,
		Delegation: id,
		Delegator:  key.AtomicDID(),
		Reason:     reason,
		RevokedAt:  chrono.FormatSeconds(revokedAt.UTC().Truncate(time.Second)),
	}
	doc, err := canonical.FromValue(w)
	if err != nil {
		return Revocation{}, fmt.Errorf("%w: revoke: %w", ErrInvalidRevocation, err)
	}
	signed, err := canonical.Attest(ctx, doc, signer)
	if err != nil {
		return Revocation{}, fmt.Errorf("delegation: revoke: %w", err)
	}
	r, err := RevocationFromDocument(signed)
	if err != nil {
		return Revocation{}, fmt.Errorf("delegation: revoke: %w", err)
	}
	if err := r.Verify(key); err != nil {
		return Revocation{}, fmt.Errorf("delegation: revoke: %w", err)
	}
	return r, nil
}

// ParseRevocation admits b as a JSON document ([canonical.Admit]) and reads
// it as a revocation ([RevocationFromDocument]). Every failure wraps
// [ErrInvalidRevocation], and [canonical.ErrNotAdmitted] when admission
// refused b.
//
// Parsing is not trusting: verify the result before relying on it.
func ParseRevocation(b []byte) (Revocation, error) {
	doc, err := canonical.Admit(b)
	if err != nil {
		return Revocation{}, fmt.Errorf("%w: %w", ErrInvalidRevocation, err)
	}
	return RevocationFromDocument(doc)
}

// RevocationFromDocument reads d as a revocation.
//
// Every member Required by docs/remote/auth.md#delegationrevocation must be
// present and a string, except proof, which must decode as a
// [canonical.Proof]. delegation must be a delegation id's URN ([URNPrefix]
// and its base32), delegator must parse ([identity.ParseDID]) and revokedAt
// must be an RFC 3339 time. attributedTo and reason may be absent, null or a
// string. @context is not required, and is read only when it is a string.
// Other members are kept and ignored.
//
// Values are not checked: a revocation whose @type is not [TypeRevocation]
// or whose signature is wrong is constructed, and refused by
// [Revocation.Verify]. Every failure wraps [ErrInvalidRevocation].
func RevocationFromDocument(d canonical.Document) (Revocation, error) {
	view, err := parseDelegationRevocation(d)
	if err != nil {
		return Revocation{}, fmt.Errorf("%w: %w", ErrInvalidRevocation, err)
	}
	canon, err := d.Canonical()
	if err != nil {
		return Revocation{}, fmt.Errorf("%w: %w", ErrInvalidRevocation, err)
	}
	return Revocation{doc: d, canon: canon, view: view}, nil
}

// parseDelegationRevocation reads the members of d.
func parseDelegationRevocation(d canonical.Document) (DelegationRevocation, error) {
	var r DelegationRevocation
	var err error
	r.Context, _ = d.Text(memberContext)

	if r.Type, err = requiredText(d, memberType); err != nil {
		return DelegationRevocation{}, err
	}
	if r.AttributedTo, err = optionalText(d, memberAttributedTo); err != nil {
		return DelegationRevocation{}, err
	}
	if r.ContentHash, err = requiredText(d, memberContentHash); err != nil {
		return DelegationRevocation{}, err
	}
	urn, err := requiredText(d, memberDelegation)
	if err != nil {
		return DelegationRevocation{}, err
	}
	if !strings.HasPrefix(urn, URNPrefix) {
		return DelegationRevocation{}, fmt.Errorf("member %q: %w: does not begin %q", memberDelegation, ErrInvalidID, URNPrefix)
	}
	if r.Delegation, err = ParseID(urn); err != nil {
		return DelegationRevocation{}, fmt.Errorf("member %q: %w", memberDelegation, err)
	}
	delegator, err := requiredText(d, memberDelegator)
	if err != nil {
		return DelegationRevocation{}, err
	}
	if r.Delegator, err = identity.ParseDID(delegator); err != nil {
		return DelegationRevocation{}, fmt.Errorf("member %q: %w", memberDelegator, err)
	}
	if r.Proof, err = canonical.ProofOf(d); err != nil {
		return DelegationRevocation{}, fmt.Errorf("member %q: %w", memberProof, err)
	}
	if r.Reason, err = optionalText(d, memberReason); err != nil {
		return DelegationRevocation{}, err
	}
	revokedAt, err := requiredText(d, memberRevokedAt)
	if err != nil {
		return DelegationRevocation{}, err
	}
	if r.RevokedAt, err = chrono.Parse(revokedAt); err != nil {
		return DelegationRevocation{}, fmt.Errorf("member %q: %w", memberRevokedAt, err)
	}
	return r, nil
}

// DelegationRevocation returns the members read from the revocation. They
// are untrusted until the revocation has been verified.
func (r Revocation) DelegationRevocation() DelegationRevocation {
	return r.view
}

// Document returns the document the revocation was read from, with every
// member it was admitted with.
func (r Revocation) Document() canonical.Document {
	return r.doc
}

// MarshalJSON returns the revocation's canonical bytes, proof included,
// which is the body Atomic's revoke call sends. It implements
// [json.Marshaler] and fails only for the zero Revocation, which holds no
// revocation.
func (r Revocation) MarshalJSON() ([]byte, error) {
	if r.canon == nil {
		return nil, fmt.Errorf("%w: the zero Revocation holds no revocation", ErrInvalidRevocation)
	}
	return slices.Clone(r.canon), nil
}

// UnmarshalJSON reads b with [ParseRevocation] and replaces r with the
// result. It implements [json.Unmarshaler]. JSON null is refused, like any
// other value that is not a revocation. r is unchanged on failure.
func (r *Revocation) UnmarshalJSON(b []byte) error {
	v, err := ParseRevocation(b)
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// Verify checks the revocation against delegatorKey, the key of the human
// who issued the certificate being revoked, as Atomic's verify_revocation
// does:
//
//  1. @type is [TypeRevocation].
//  2. The document verifies against delegatorKey ([canonical.VerifyAttested]):
//     its contentHash recomputes ([canonical.ErrContentHash]), and its proof
//     is Atomic's, signed by delegatorKey and naming it
//     ([canonical.ErrProof]).
//  3. delegator is delegatorKey's did:atomic. Atomic reads it as a
//     did:atomic, so the did:key form of the same key is refused.
//
// Only the delegator can revoke, so a revocation signed by anyone else,
// the delegate included, fails step 2. The first failing step's error is
// returned, wrapping [ErrVerifyRevocation], and the canonical error named
// above where there is one.
//
// Verify does not check that the certificate Delegation names was issued by
// delegatorKey: the revocation carries only its id. Look the certificate up
// and check that [Certificate.Verify] succeeds against the same key.
func (r Revocation) Verify(delegatorKey identity.PublicKey) error {
	t, _ := r.doc.Text(memberType)
	if t != TypeRevocation {
		return fmt.Errorf("%w: @type is %q, want %q", ErrVerifyRevocation, t, TypeRevocation)
	}
	if err := canonical.VerifyAttested(r.doc, delegatorKey); err != nil {
		return fmt.Errorf("%w: %w", ErrVerifyRevocation, err)
	}
	if r.view.Delegator != delegatorKey.AtomicDID() {
		return fmt.Errorf("%w: delegator %s is not the did:atomic of %s", ErrVerifyRevocation, r.view.Delegator, delegatorKey)
	}
	return nil
}

package projectidentity

import (
	"bytes"
	"context"
	"fmt"

	"github.com/z5labs/atomic-go/canonical"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
)

// Member names a project identity document is read and signed by.
const (
	memberProjectID = "atomic:projectId"
	memberIssuer    = "atomic:issuer"
	memberID        = "@id"
	memberProof     = "proof"
)

// Document is a project identity document: the exact bytes it was parsed
// from, and the [canonical.Document] admitted from them.
//
// The bytes are never rewritten. [Document.Bytes] and [Document.MarshalJSON]
// return them as they came in, white space and member order included, so
// a document read from one remote can be offered to another, or bound back,
// without this package changing what the server receives. Everything else
// is read from the admitted document: [Document.ProjectID],
// [Document.Issuer], [Document.Hash] and [Document.VerifyProof].
//
// Parsing checks that the bytes are one JSON object and nothing else; a
// Document is untrusted until [Document.VerifyProof] has succeeded on it.
//
// The zero Document holds no document: [Document.Bytes] returns nil, and
// [Document.MarshalJSON], [Document.ProjectID], [Document.Hash] and [Sign]
// refuse it with an error wrapping [ErrInvalid], so a missing document is
// never sent, hashed or signed as {}.
type Document struct {
	raw []byte
	doc canonical.Document
}

// Parse reads b as a project identity document. b must be exactly one JSON
// object, admitted by [canonical.Admit]; white space around it is allowed
// and kept. Anything else is an error wrapping [ErrInvalid] and
// [canonical.ErrNotAdmitted]: an array, a string, null, two values, a
// duplicate member name, a number that is not an integer within
// ±[canonical.MaxInteger], and the rest of what admission refuses.
//
// The members are not checked: a document with no atomic:projectId, or
// with an @type other than [DocumentType], parses, and fails only the
// methods that read what it lacks. b is copied.
func Parse(b []byte) (Document, error) {
	doc, err := canonical.Admit(b)
	if err != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return Document{raw: bytes.Clone(b), doc: doc}, nil
}

// Bytes returns a copy of the bytes d was parsed from, or nil for the zero
// Document. A Document returned by [Sign] holds the RFC 8785 canonical
// (JCS) form of the signed document, which is how Atomic stores one.
func (d Document) Bytes() []byte {
	return bytes.Clone(d.raw)
}

// MarshalJSON returns the bytes d was parsed from, unchanged, as
// [Document.Bytes]. The zero Document is an error wrapping [ErrInvalid].
//
// An enclosing marshal may still rewrite them: encoding/json (v1) compacts
// a MarshalJSON result, dropping its white space. Send [Document.Bytes]
// where the exact bytes matter.
func (d Document) MarshalJSON() ([]byte, error) {
	if err := d.check(); err != nil {
		return nil, err
	}
	return d.Bytes(), nil
}

// UnmarshalJSON parses b, as [Parse], and replaces d with the result. d is
// unchanged when parsing fails. JSON null is refused like any other value
// that is not an object; use a *Document field where null is allowed.
func (d *Document) UnmarshalJSON(b []byte) error {
	v, err := Parse(b)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// ProjectID returns the document's atomic:projectId, parsed with
// [ParseProjectID]. A missing member, one that is not a string, and one
// that is not a project id are errors wrapping [ErrInvalid].
func (d Document) ProjectID() (ProjectID, error) {
	if err := d.check(); err != nil {
		return ProjectID{}, err
	}
	s, ok := d.doc.Text(memberProjectID)
	if !ok {
		return ProjectID{}, fmt.Errorf("%w: document has no string %s", ErrInvalid, memberProjectID)
	}
	return ParseProjectID(s)
}

// Issuer returns the DID that the document's atomic:issuer names: the @id
// of the object {"@id": "<did>"}, which is how Atomic writes it. Either DID
// form is accepted.
//
// It returns false and no error when the document has no atomic:issuer or
// it is null, as an unsigned document has none. An atomic:issuer that is
// not an object, an object with no string @id, and an @id that is not a DID
// are errors wrapping [ErrInvalid], the last [identity.ErrInvalidDID] too.
//
// The issuer is a claim the document makes, not a check: nothing here
// compares it with the key [Document.VerifyProof] is given.
func (d Document) Issuer() (identity.DID, bool, error) {
	v, ok := d.doc.Member(memberIssuer)
	if !ok || v.Kind() == 'n' {
		return identity.DID{}, false, nil
	}
	if v.Kind() != '{' {
		return identity.DID{}, false, fmt.Errorf("%w: %s is not an object", ErrInvalid, memberIssuer)
	}
	obj, err := canonical.Admit(v)
	if err != nil {
		return identity.DID{}, false, fmt.Errorf("%w: %s: %w", ErrInvalid, memberIssuer, err)
	}
	s, ok := obj.Text(memberID)
	if !ok {
		return identity.DID{}, false, fmt.Errorf("%w: %s has no string %s", ErrInvalid, memberIssuer, memberID)
	}
	did, err := identity.ParseDID(s)
	if err != nil {
		return identity.DID{}, false, fmt.Errorf("%w: %s: %w", ErrInvalid, memberIssuer, err)
	}
	return did, true, nil
}

// Hash returns the document's project identity hash: the BLAKE3 of
// [HashDomain], a zero byte, and the RFC 8785 canonical (JCS) bytes of the
// document without its proof member (docs/remote/auth.md#project-identity).
// A proof does not change the hash; every other member does.
//
// It is computed from the admitted members, not from [Document.Bytes], so
// two spellings of one document hash alike. The zero Document is an error
// wrapping [ErrInvalid].
func (d Document) Hash() (ident.Hash, error) {
	if err := d.check(); err != nil {
		return ident.Hash{}, err
	}
	b, err := d.doc.Without(memberProof).Canonical()
	if err != nil {
		return ident.Hash{}, fmt.Errorf("projectidentity: hash: %w", err)
	}
	h := ident.NewHasher()
	h.Write([]byte(HashDomain))
	h.Write([]byte{0})
	h.Write(b)
	return h.Sum(), nil
}

// VerifyProof verifies the document's proof against key, as
// [canonical.VerifyProof] does: the proof is present and well formed, its
// @type, cryptosuite and proofPurpose are Atomic's, its signature verifies
// over the canonical bytes of the document without its proof, and its
// verificationMethod names key. Every failure wraps [canonical.ErrProof].
//
// It checks the signature and nothing the document says: not its @type,
// and not that atomic:issuer names key (see [Document.Issuer]).
func (d Document) VerifyProof(key identity.PublicKey) error {
	if err := canonical.VerifyProof(d.doc, key); err != nil {
		return fmt.Errorf("projectidentity: verify proof: %w", err)
	}
	return nil
}

// issuer is the atomic:issuer member [Sign] writes.
type issuer struct {
	ID string `json:"@id"`
}

// Sign signs d as Atomic signs a project identity when a repository is
// created with a default identity (docs/remote/auth.md#project-identity):
//
//  1. Set atomic:issuer to the object {"@id": "<signer's did:atomic>"},
//     replacing any issuer d already has.
//  2. Add a bare proof with [canonical.Sign], which replaces any proof d
//     already has and adds no contentHash or attributedTo.
//
// The returned Document's [Document.Bytes] are the RFC 8785 canonical (JCS)
// bytes of the signed document, proof included, which is how Atomic stores
// one. d is unchanged.
//
// Sign verifies what it signed against signer's public key before
// returning it, so a [identity.Signer] whose signatures are not its key's
// is an error wrapping [canonical.ErrProof]. A nil signer and the zero
// Document are errors wrapping [ErrInvalid]. An error from signer is
// returned wrapped.
func Sign(ctx context.Context, d Document, signer identity.Signer) (Document, error) {
	if signer == nil {
		return Document{}, fmt.Errorf("%w: sign: nil signer", ErrInvalid)
	}
	if err := d.check(); err != nil {
		return Document{}, fmt.Errorf("projectidentity: sign: %w", err)
	}
	key := signer.PublicKey()
	doc, err := d.doc.With(memberIssuer, issuer{ID: key.AtomicDID().String()})
	if err != nil {
		return Document{}, fmt.Errorf("projectidentity: sign: %w", err)
	}
	signed, err := canonical.Sign(ctx, doc, signer)
	if err != nil {
		return Document{}, fmt.Errorf("projectidentity: sign: %w", err)
	}
	b, err := signed.Canonical()
	if err != nil {
		return Document{}, fmt.Errorf("projectidentity: sign: %w", err)
	}
	out := Document{raw: b, doc: signed}
	if err := out.VerifyProof(key); err != nil {
		return Document{}, fmt.Errorf("projectidentity: sign: %w", err)
	}
	return out, nil
}

// check refuses the zero Document.
func (d Document) check() error {
	if d.raw == nil {
		return fmt.Errorf("%w: zero Document holds no document", ErrInvalid)
	}
	return nil
}

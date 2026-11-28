package canonical

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"lukechampine.com/blake3"

	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/internal/textenc"
)

const (
	// Context is the JSON-LD @context of Atomic's own documents, such as
	// delegation certificates (docs/remote/auth.md#agentdelegation).
	Context = "https://atomic.dev/ns/ctx.jsonld"

	// ProofType is the @type of every proof Atomic writes and accepts.
	ProofType = "DataIntegrityProof"

	// Cryptosuite names the suite of every proof Atomic writes and
	// accepts. It is Atomic's procedure, not the W3C suite of the same
	// name: the signature covers the JCS bytes of the document directly
	// (docs/remote/auth.md#signing-a-delegation-document).
	Cryptosuite = "eddsa-jcs-2022"

	// ProofPurpose is the proofPurpose of every proof Atomic writes and
	// accepts.
	ProofPurpose = "assertionMethod"
)

// Member names that attestation reads and writes.
const (
	memberProof        = "proof"
	memberContentHash  = "contentHash"
	memberAttributedTo = "attributedTo"
)

// contentHashPrefix opens every contentHash: the hash's algorithm.
const contentHashPrefix = "blake3:"

// multibaseBase58 is the multibase prefix of base58btc, the encoding of a
// proofValue.
const multibaseBase58 = 'z'

var (
	// ErrContentHash is wrapped by the error [VerifyAttested] returns for
	// a document whose contentHash is missing, is not a string, or does
	// not recompute.
	ErrContentHash = errors.New("canonical: content hash mismatch")

	// ErrProof is wrapped by every error for a proof that is missing,
	// malformed, carries constants other than [ProofType], [Cryptosuite]
	// and [ProofPurpose], holds a signature that does not verify, or names
	// a verification method that is not the verifying key's.
	ErrProof = errors.New("canonical: invalid proof")
)

// Proof is a Data Integrity proof (docs/remote/auth.md#proof). Every member
// is required, and decoding one refuses unknown members, as Atomic's own
// Proof does.
//
// A Proof is a value read off a document. Decoding checks its shape and
// nothing else: [VerifyAttested] and [VerifyProof] are what check its
// constants, its signature and its key.
type Proof struct {
	// Type is the proof's @type, [ProofType] in every proof Atomic writes.
	Type string `json:"@type"`

	// Cryptosuite is [Cryptosuite] in every proof Atomic writes.
	Cryptosuite string `json:"cryptosuite"`

	// ProofPurpose is [ProofPurpose] in every proof Atomic writes.
	ProofPurpose string `json:"proofPurpose"`

	// ProofValue is "z" followed by the base58btc of the 64-byte Ed25519
	// signature. [Proof.Signature] decodes it.
	ProofValue string `json:"proofValue"`

	// VerificationMethod is the signer's DID followed by "#key-1".
	// [Proof.Signer] reads the DID.
	VerificationMethod string `json:"verificationMethod"`
}

// UnmarshalJSON decodes a proof object. It refuses, with an error wrapping
// [ErrProof], a value that is not an object, an object with a member Proof
// does not have, and one missing a member, or holding one that is null or
// not a string. p is unchanged on failure.
func (p *Proof) UnmarshalJSON(b []byte) error {
	var raw struct {
		Type               *string `json:"@type"`
		Cryptosuite        *string `json:"cryptosuite"`
		ProofPurpose       *string `json:"proofPurpose"`
		ProofValue         *string `json:"proofValue"`
		VerificationMethod *string `json:"verificationMethod"`
	}
	if err := json.Unmarshal(b, &raw, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("%w: %w", ErrProof, err)
	}
	for _, m := range []struct {
		name string
		v    *string
	}{
		{"@type", raw.Type},
		{"cryptosuite", raw.Cryptosuite},
		{"proofPurpose", raw.ProofPurpose},
		{"proofValue", raw.ProofValue},
		{"verificationMethod", raw.VerificationMethod},
	} {
		if m.v == nil {
			return fmt.Errorf("%w: member %q is missing", ErrProof, m.name)
		}
	}
	*p = Proof{
		Type:               *raw.Type,
		Cryptosuite:        *raw.Cryptosuite,
		ProofPurpose:       *raw.ProofPurpose,
		ProofValue:         *raw.ProofValue,
		VerificationMethod: *raw.VerificationMethod,
	}
	return nil
}

// ProofOf reads d's proof member. A document with no proof, or whose proof
// [Proof.UnmarshalJSON] refuses, is an error wrapping [ErrProof].
func ProofOf(d Document) (Proof, error) {
	if !d.Has(memberProof) {
		return Proof{}, fmt.Errorf("%w: document has no proof", ErrProof)
	}
	var p Proof
	if err := d.Decode(memberProof, &p); err != nil {
		if errors.Is(err, ErrProof) {
			return Proof{}, err
		}
		return Proof{}, fmt.Errorf("%w: %w", ErrProof, err)
	}
	return p, nil
}

// Signature decodes the proof's proofValue into the 64 signature bytes. A
// value without the "z" multibase prefix, that is not base58btc, or that
// does not decode to 64 bytes is an error wrapping [ErrProof].
func (p Proof) Signature() ([]byte, error) {
	body, ok := strings.CutPrefix(p.ProofValue, string(multibaseBase58))
	if !ok {
		return nil, fmt.Errorf("%w: proofValue does not start with the multibase prefix %q", ErrProof, multibaseBase58)
	}
	sig, err := textenc.DecodeBase58(body)
	if err != nil {
		return nil, fmt.Errorf("%w: proofValue: %w", ErrProof, err)
	}
	if len(sig) != identity.SignatureSize {
		return nil, fmt.Errorf("%w: proofValue holds %d bytes, want %d", ErrProof, len(sig), identity.SignatureSize)
	}
	return sig, nil
}

// Signer parses the DID the proof's verificationMethod names: the part
// before the first "#", as Atomic reads it. Either DID form is accepted. A
// DID that does not parse is an error wrapping [ErrProof] and
// [identity.ErrInvalidDID].
func (p Proof) Signer() (identity.DID, error) {
	s, _, _ := strings.Cut(p.VerificationMethod, "#")
	did, err := identity.ParseDID(s)
	if err != nil {
		return identity.DID{}, fmt.Errorf("%w: verificationMethod: %w", ErrProof, err)
	}
	return did, nil
}

// ContentHash returns d's contentHash: "blake3:" followed by the
// lowercase hex BLAKE3 of the canonical bytes of d without its proof and
// contentHash members.
func ContentHash(d Document) (string, error) {
	b, err := d.Without(memberProof, memberContentHash).Canonical()
	if err != nil {
		return "", err
	}
	sum := blake3.Sum256(b)
	return contentHashPrefix + hex.EncodeToString(sum[:]), nil
}

// Attest signs d as Atomic signs a delegation certificate, a delegation
// request and a revocation, in the order of
// docs/remote/auth.md#signing-a-delegation-document:
//
//  1. Drop d's proof.
//  2. Set attributedTo to the signer's did:atomic when it is absent, or is
//     not a non-empty string. A non-empty attributedTo is kept, even one
//     naming somebody else.
//  3. Set contentHash to [ContentHash] of the result.
//  4. Take the canonical bytes of the document without proof, which
//     includes contentHash, as the signing input.
//  5. Sign them with s.
//  6. Add the proof: [ProofType], [Cryptosuite], [ProofPurpose], the
//     signer's did:atomic followed by "#key-1", and the signature.
//
// d is unchanged. An error from s is returned wrapped.
func Attest(ctx context.Context, d Document, s identity.Signer) (Document, error) {
	if s == nil {
		return Document{}, errors.New("canonical: attest: nil signer")
	}
	d = d.Without(memberProof)
	if author, ok := d.Text(memberAttributedTo); !ok || author == "" {
		var err error
		d, err = d.With(memberAttributedTo, s.PublicKey().AtomicDID().String())
		if err != nil {
			return Document{}, err
		}
	}
	hash, err := ContentHash(d)
	if err != nil {
		return Document{}, err
	}
	d, err = d.With(memberContentHash, hash)
	if err != nil {
		return Document{}, err
	}
	return sign(ctx, d, s)
}

// VerifyAttested verifies a document [Attest] signed, against key, in the
// order of docs/remote/auth.md#verifying-a-certificate:
//
//  1. contentHash is present, is a string and recomputes, or the error
//     wraps [ErrContentHash].
//  2. The proof decodes, as [ProofOf].
//  3. Its @type, cryptosuite and proofPurpose are exactly [ProofType],
//     [Cryptosuite] and [ProofPurpose]. The signature does not cover them,
//     so they are checked on their own.
//  4. Its signature verifies over the canonical bytes of the document
//     without proof.
//  5. Its verificationMethod names key, in either DID form.
//
// Failures from step 2 on wrap [ErrProof]. VerifyAttested checks the
// integrity of any attested document and nothing about what it says: a
// certificate's own checks, such as its @type and its parties, belong to
// the caller.
func VerifyAttested(d Document, key identity.PublicKey) error {
	want, ok := d.Text(memberContentHash)
	if !ok {
		return fmt.Errorf("%w: document has no string contentHash", ErrContentHash)
	}
	got, err := ContentHash(d)
	if err != nil {
		return err
	}
	if want != got {
		return fmt.Errorf("%w: document carries %s, recomputes to %s", ErrContentHash, want, got)
	}
	return VerifyProof(d, key)
}

// Sign adds a bare proof to d, as Atomic signs a project identity
// (docs/remote/auth.md#project-identity): the signature covers the
// canonical bytes of d without its proof, and no contentHash or
// attributedTo is added. Any proof d already has is replaced. d is
// unchanged. An error from s is returned wrapped.
func Sign(ctx context.Context, d Document, s identity.Signer) (Document, error) {
	if s == nil {
		return Document{}, errors.New("canonical: sign: nil signer")
	}
	return sign(ctx, d.Without(memberProof), s)
}

// VerifyProof verifies d's proof against key: steps 2 to 5 of
// [VerifyAttested], with no contentHash read. It is how a bare proof from
// [Sign] is checked. Every failure wraps [ErrProof].
func VerifyProof(d Document, key identity.PublicKey) error {
	p, err := ProofOf(d)
	if err != nil {
		return err
	}
	if p.Type != ProofType || p.Cryptosuite != Cryptosuite || p.ProofPurpose != ProofPurpose {
		return fmt.Errorf("%w: unexpected proof metadata: @type %q, cryptosuite %q, proofPurpose %q",
			ErrProof, p.Type, p.Cryptosuite, p.ProofPurpose)
	}
	sig, err := p.Signature()
	if err != nil {
		return err
	}
	msg, err := d.Without(memberProof).Canonical()
	if err != nil {
		return err
	}
	if !key.Verify(msg, sig) {
		return fmt.Errorf("%w: signature does not verify against %s", ErrProof, key)
	}
	did, err := p.Signer()
	if err != nil {
		return err
	}
	if !did.Matches(key) {
		return fmt.Errorf("%w: verificationMethod %q does not name %s", ErrProof, p.VerificationMethod, key)
	}
	return nil
}

// sign signs the canonical bytes of d, which has no proof, with s and adds
// the proof.
func sign(ctx context.Context, d Document, s identity.Signer) (Document, error) {
	msg, err := d.Canonical()
	if err != nil {
		return Document{}, err
	}
	sig, err := s.Sign(ctx, msg)
	if err != nil {
		return Document{}, fmt.Errorf("canonical: sign: %w", err)
	}
	if len(sig) != identity.SignatureSize {
		return Document{}, fmt.Errorf("canonical: sign: signature is %d bytes, want %d", len(sig), identity.SignatureSize)
	}
	key := s.PublicKey()
	return d.With(memberProof, Proof{
		Type:               ProofType,
		Cryptosuite:        Cryptosuite,
		ProofPurpose:       ProofPurpose,
		ProofValue:         string(multibaseBase58) + textenc.EncodeBase58(sig),
		VerificationMethod: key.AtomicDID().VerificationMethod(),
	})
}

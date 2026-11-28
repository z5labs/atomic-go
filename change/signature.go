package change

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/internal/postcard"
)

const (
	// SignatureDomain is the domain separator a change signature signs
	// under. The signed message is SignatureDomain, one 00 byte and the
	// 32-byte change hash; see [SignedMessage] and
	// docs/remote/change-format/container.md#signing.
	SignatureDomain = "atomic.change.signature.v1"

	// MethodEd25519 is the only signature method Atomic writes, and the
	// only one [VerifySignature] accepts.
	MethodEd25519 = "ed25519"
)

// ErrInvalidSignature reports a change signature that does not verify: its
// method is not [MethodEd25519], it signs a hash other than the change's,
// or its value is not a valid signature by the key it is checked against.
var ErrInvalidSignature = errors.New("invalid change signature")

// Signature is a change's signature, the SIGNATURE section's postcard
// ChangeSignature (docs/remote/change-format/sections.md#changesignature).
// The section is outside the change hash, so signing a change, or signing
// it again, never changes its identity.
type Signature struct {
	// SignerDID names the signer, conventionally by its did:atomic DID,
	// a fingerprint of its key. It is a discovery hint and nothing more:
	// it cannot verify the signature, nothing here parses it, and
	// [VerifySignature] never consults it. Resolve the signer's key some
	// other way, and use [identity.DID.Matches] if you want to check the
	// hint against it.
	SignerDID string

	// Value is the Ed25519 signature of [SignedMessage] of SignedHash.
	Value [identity.SignatureSize]byte

	// SignedHash is the change hash that was signed. A signature verifies
	// only for the change whose hash this is.
	SignedHash ident.Hash

	// Timestamp is when the signature was made, in Unix epoch seconds.
	Timestamp int64

	// Method is the signature scheme. [MethodEd25519] is the only value
	// Atomic writes.
	Method string
}

// SignedMessage returns the message a signature of the change hash h
// signs: [SignatureDomain], one 00 byte, then the 32 bytes of h.
func SignedMessage(h ident.Hash) []byte {
	b := make([]byte, 0, len(SignatureDomain)+1+len(h))
	b = append(b, SignatureDomain...)
	b = append(b, 0)
	return append(b, h[:]...)
}

// DecodeSignature decodes a SIGNATURE payload, a postcard ChangeSignature.
// Like postcard::from_bytes, which Atomic reads it with, it ignores bytes
// left over after the value.
//
// The signature and the signed hash are written as length-prefixed byte
// sequences, and a length other than 64 for the signature or 32 for the
// hash is refused, as Atomic refuses it. Every failure is a
// *[changefile.FormatError] of kind [changefile.ErrPayload] in the
// SIGNATURE section.
//
// DecodeSignature does not verify the signature; see [VerifySignature].
func DecodeSignature(b []byte) (Signature, error) {
	s, err := postcard.Decode(b, decodeSignature)
	if err != nil {
		return Signature{}, sectionError(changefile.SectionSignature, changefile.ErrPayload, "", err)
	}
	return s, nil
}

func decodeSignature(d *postcard.Decoder) Signature {
	var s Signature
	s.SignerDID = d.Str()
	decodeSized(d, "signature", s.Value[:])
	decodeSized(d, "signed_hash", s.SignedHash[:])
	s.Timestamp = d.I64()
	s.Method = d.Str()
	return s
}

// decodeSized decodes a fixed-size array that is written as a byte
// sequence, serde_bytes_array_64 and serde_bytes_array_32, into dst, and
// fails d unless the sequence is exactly len(dst) bytes.
func decodeSized(d *postcard.Decoder, field string, dst []byte) {
	b := d.ByteSeq()
	if d.Err() != nil {
		return
	}
	if len(b) != len(dst) {
		d.Fail(fmt.Errorf("%w: %s is %d bytes, want %d", postcard.ErrBadLength, field, len(b), len(dst)))
		return
	}
	copy(dst, b)
}

// AppendPayload appends the SIGNATURE payload of s, a postcard
// ChangeSignature, to dst. It is the encoding Atomic writes, and the one
// [DecodeSignature] reads back.
//
// It fails, and returns dst unchanged, with a *[changefile.FormatError]
// of kind [changefile.ErrPayload] when SignerDID or Method is not valid
// UTF-8, which a reader refuses. It does not check that the signature
// verifies, or that Method is [MethodEd25519].
func (s Signature) AppendPayload(dst []byte) ([]byte, error) {
	if !utf8.ValidString(s.SignerDID) {
		return dst, sectionError(changefile.SectionSignature, changefile.ErrPayload, "signer_did is not valid UTF-8", nil)
	}
	if !utf8.ValidString(s.Method) {
		return dst, sectionError(changefile.SectionSignature, changefile.ErrPayload, "method is not valid UTF-8", nil)
	}
	e := postcard.NewEncoder(dst)
	e.Str(s.SignerDID)
	e.ByteSeq(s.Value[:])
	e.ByteSeq(s.SignedHash[:])
	e.I64(s.Timestamp)
	e.Str(s.Method)
	return e.Bytes(), nil
}

// VerifySignature reports whether s is a valid signature of the change
// whose hash is change, by key, as Atomic's verify_change_signature
// decides: s's method must be [MethodEd25519], its signed hash must be
// change, and its value must be key's Ed25519 signature of
// [SignedMessage] of change. Any of these failing is an error wrapping
// [ErrInvalidSignature].
//
// key is the trust root, and the caller resolves it. s.SignerDID is not
// consulted.
func VerifySignature(s Signature, change ident.Hash, key identity.PublicKey) error {
	if s.Method != MethodEd25519 {
		return fmt.Errorf("%w: method %q, want %q", ErrInvalidSignature, s.Method, MethodEd25519)
	}
	if s.SignedHash != change {
		return fmt.Errorf("%w: signs %s, not the change %s", ErrInvalidSignature, s.SignedHash, change)
	}
	if !key.Verify(SignedMessage(change), s.Value[:]) {
		return fmt.Errorf("%w: does not verify against %s", ErrInvalidSignature, key)
	}
	return nil
}

// NewSignature signs the change whose hash is change with s, at the time
// at, which is kept in whole Unix seconds. The signature's SignerDID is
// the did:atomic DID of s's key and its method is [MethodEd25519].
//
// NewSignature returns ctx.Err() when ctx is done before it signs. A nil
// s is an error. An error from s is returned wrapped, and so is a
// signature that is not [identity.SignatureSize] bytes or does not verify
// against s's key.
func NewSignature(ctx context.Context, change ident.Hash, s identity.Signer, at time.Time) (Signature, error) {
	if err := ctx.Err(); err != nil {
		return Signature{}, err
	}
	if s == nil {
		return Signature{}, errors.New("change: sign: nil Signer")
	}
	key := s.PublicKey()
	msg := SignedMessage(change)
	sig, err := s.Sign(ctx, msg)
	if err != nil {
		return Signature{}, fmt.Errorf("change: sign: %w", err)
	}
	if len(sig) != identity.SignatureSize {
		return Signature{}, fmt.Errorf("change: sign: signature is %d bytes, want %d", len(sig), identity.SignatureSize)
	}
	if !key.Verify(msg, sig) {
		return Signature{}, fmt.Errorf("change: sign: signature does not verify against %s", key)
	}
	return Signature{
		SignerDID:  key.AtomicDID().String(),
		Value:      [identity.SignatureSize]byte(sig),
		SignedHash: change,
		Timestamp:  at.Unix(),
		Method:     MethodEd25519,
	}, nil
}

// Sign signs the change file f with s, at the time at, and returns the
// file's bytes with the signature in its SIGNATURE section, compressed
// with c. Any signature f already has is replaced. The SIGNATURE section
// is outside the change hash, so the signed file keeps f's hash; Sign
// works through [changefile.ReplaceUnhashed], which sets the header's
// [changefile.FlagSignature] and copies every other section byte for
// byte.
//
// Sign fails as [NewSignature] does, and as [changefile.ReplaceUnhashed]
// does for f and c.
func Sign(ctx context.Context, f *changefile.File, s identity.Signer, at time.Time, c changefile.Compressor) ([]byte, error) {
	if f == nil {
		return nil, changefile.ErrNilFile
	}
	if c == nil {
		return nil, changefile.ErrNilCompressor
	}
	sig, err := NewSignature(ctx, f.Hash, s, at)
	if err != nil {
		return nil, err
	}
	payload, err := sig.AppendPayload(nil)
	if err != nil {
		return nil, err
	}
	return changefile.ReplaceUnhashed(f, changefile.SectionSignature, payload, c)
}

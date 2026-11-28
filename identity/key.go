package identity

import (
	"bytes"
	"crypto/ed25519"
	"fmt"

	"github.com/z5labs/atomic-go/ident"
)

// base32Len is the length of the strict base32 text of a 32-byte value.
const base32Len = ident.Base32Len

// PublicKey is an Ed25519 public key. Its text form is [ident.Base32Len]
// characters of strict, upper-case, unpadded base32, the form Atomic uses
// for a JWT's kid and sub and for a registration's public_key.
//
// A PublicKey is not checked to be a valid curve point; [PublicKey.Verify]
// simply fails for one that is not.
type PublicKey [PublicKeySize]byte

// PublicKeyFromBytes returns the key held in b, which must be exactly
// [PublicKeySize] bytes, such as an [ed25519.PublicKey]. Any other length is
// an error wrapping [ErrInvalidKey].
func PublicKeyFromBytes(b []byte) (PublicKey, error) {
	if len(b) != PublicKeySize {
		return PublicKey{}, fmt.Errorf("%w: %d bytes, want %d", ErrInvalidKey, len(b), PublicKeySize)
	}
	return PublicKey(b), nil
}

// ParsePublicKey parses the strict base32 text of a public key with the
// rules of [ident.DecodeBase32Strict]: exactly [ident.Base32Len] upper-case
// characters, no padding or whitespace, and zero unused bits in the last
// character. A failure wraps [ErrInvalidKey] and the *[ident.ParseError]
// that describes it.
func ParsePublicKey(s string) (PublicKey, error) {
	b, err := ident.DecodeBase32Strict(s, PublicKeySize)
	if err != nil {
		return PublicKey{}, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	return PublicKey(b), nil
}

// String returns the canonical base32 text of k.
func (k PublicKey) String() string {
	return ident.EncodeBase32(k[:])
}

// AppendText appends the canonical base32 text of k to b. It implements
// [encoding.TextAppender] and never returns an error.
func (k PublicKey) AppendText(b []byte) ([]byte, error) {
	return ident.AppendBase32(b, k[:]), nil
}

// MarshalText returns the canonical base32 text of k. It implements
// [encoding.TextMarshaler] and never returns an error.
func (k PublicKey) MarshalText() ([]byte, error) {
	return k.AppendText(make([]byte, 0, base32Len))
}

// UnmarshalText parses text into k with the rules of [ParsePublicKey]. It
// implements [encoding.TextUnmarshaler]. On failure k is unchanged.
func (k *PublicKey) UnmarshalText(text []byte) error {
	v, err := ParsePublicKey(string(text))
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// Ed25519 returns k as a new [ed25519.PublicKey], which the caller may
// modify without changing k.
func (k PublicKey) Ed25519() ed25519.PublicKey {
	return ed25519.PublicKey(bytes.Clone(k[:]))
}

// Verify reports whether sig is a valid Ed25519 signature of msg by k, as
// [ed25519.Verify] decides. A sig that is not [SignatureSize] bytes never
// verifies.
func (k PublicKey) Verify(msg, sig []byte) bool {
	return ed25519.Verify(k[:], msg, sig)
}

// ID returns k's identity id, the BLAKE3 hash of its 32 bytes.
func (k PublicKey) ID() ID {
	return ID(ident.Sum(k[:]))
}

// AtomicDID returns k's did:atomic DID, which names k by its [ID] and so
// cannot be turned back into k.
func (k PublicKey) AtomicDID() DID {
	return k.ID().DID()
}

// KeyDID returns k's did:key DID, which carries k itself. Its text always
// begins "did:key:z6Mk".
func (k PublicKey) KeyDID() DID {
	return DID{method: MethodKey, id: k.ID(), key: k}
}

// ID is an identity id: the 32-byte BLAKE3 hash of an Ed25519 public key.
// Its text form is [ident.Base32Len] characters of strict base32, as for a
// [PublicKey].
//
// This is the id the client computes. A server's own records name
// identities by UUID instead, which this type is not.
type ID [ident.Size]byte

// ParseID parses the strict base32 text of an identity id with the rules of
// [ParsePublicKey]. A failure wraps [ErrInvalidID] and the
// *[ident.ParseError] that describes it.
func ParseID(s string) (ID, error) {
	b, err := ident.DecodeBase32Strict(s, ident.Size)
	if err != nil {
		return ID{}, fmt.Errorf("%w: %w", ErrInvalidID, err)
	}
	return ID(b), nil
}

// String returns the canonical base32 text of id.
func (id ID) String() string {
	return ident.EncodeBase32(id[:])
}

// DID returns the did:atomic DID that names id.
func (id ID) DID() DID {
	return DID{method: MethodAtomic, id: id}
}

// AppendText appends the canonical base32 text of id to b. It implements
// [encoding.TextAppender] and never returns an error.
func (id ID) AppendText(b []byte) ([]byte, error) {
	return ident.AppendBase32(b, id[:]), nil
}

// MarshalText returns the canonical base32 text of id. It implements
// [encoding.TextMarshaler] and never returns an error.
func (id ID) MarshalText() ([]byte, error) {
	return id.AppendText(make([]byte, 0, base32Len))
}

// UnmarshalText parses text into id with the rules of [ParseID]. It
// implements [encoding.TextUnmarshaler]. On failure id is unchanged.
func (id *ID) UnmarshalText(text []byte) error {
	v, err := ParseID(string(text))
	if err != nil {
		return err
	}
	*id = v
	return nil
}

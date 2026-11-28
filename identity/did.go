package identity

import (
	"fmt"
	"strings"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/textenc"
)

// DIDMethod is the method of a [DID]: the part between "did:" and the
// second colon.
type DIDMethod string

const (
	// MethodAtomic is Atomic's own method. A did:atomic DID is an [ID]
	// with a prefix, so it names a key without carrying it.
	MethodAtomic DIDMethod = "atomic"

	// MethodKey is the W3C did:key method. A did:key DID carries the
	// Ed25519 key itself, in base58btc.
	MethodKey DIDMethod = "key"
)

const (
	atomicPrefix = "did:atomic:"
	keyPrefix    = "did:key:"

	// verificationSuffix follows a DID to name the key a proof is
	// verified with.
	verificationSuffix = "#key-1"

	// multibaseBase58 is the multibase prefix for base58btc.
	multibaseBase58 = 'z'

	// maxKeyDIDBase58 bounds the base58 text decoded for a did:key, so an
	// enormous input is refused before the quadratic decoder sees it. The
	// 34 bytes of a key and its multicodec prefix encode to 47 characters.
	maxKeyDIDBase58 = 64
)

// ed25519Multicodec is the unsigned-varint multicodec code of ed25519-pub,
// 0xed, which prefixes the key bytes in a did:key.
var ed25519Multicodec = [2]byte{0xed, 0x01}

// DID names an Ed25519 identity in one of two forms
// (docs/remote/auth.md#did-forms):
//
//   - did:atomic:<base32 of the identity's ID>, a fingerprint of the key;
//   - did:key:z<base58btc of 0xED 0x01 and the 32 key bytes>, which carries
//     the key.
//
// Get one from [ParseDID], [ParseVerificationMethod], [PublicKey.AtomicDID],
// [PublicKey.KeyDID] or [ID.DID]. The zero DID is invalid: its String is ""
// and it matches no key.
//
// DIDs are comparable. Two DIDs are equal when they have the same method
// and name the same identity, so a did:atomic never equals the did:key of
// the same key; use [DID.Matches] to check a DID against a key.
type DID struct {
	method DIDMethod
	id     ID
	key    PublicKey // set only when method is MethodKey
}

// ParseDID parses a did:atomic or a did:key DID. Both prefixes are case
// sensitive.
//
// A did:atomic must be followed by the strict base32 text of an [ID], as
// [ParseID] reads it. A did:key must be followed by 'z' and the base58btc of
// the ed25519-pub multicodec bytes 0xED 0x01 and a 32-byte key.
//
// Every failure wraps [ErrInvalidDID]. A did:key whose multicodec prefix is
// not ed25519-pub also wraps [ErrNotEd25519].
func ParseDID(s string) (DID, error) {
	switch {
	case strings.HasPrefix(s, atomicPrefix):
		b, err := ident.DecodeBase32Strict(s[len(atomicPrefix):], ident.Size)
		if err != nil {
			return DID{}, fmt.Errorf("%w %q: %w", ErrInvalidDID, s, err)
		}
		return ID(b).DID(), nil
	case strings.HasPrefix(s, keyPrefix):
		k, err := parseKeyDID(s[len(keyPrefix):])
		if err != nil {
			return DID{}, fmt.Errorf("%w %q: %w", ErrInvalidDID, s, err)
		}
		return k.KeyDID(), nil
	}
	return DID{}, fmt.Errorf("%w %q: not did:atomic or did:key", ErrInvalidDID, s)
}

// parseKeyDID decodes the method-specific part of a did:key.
func parseKeyDID(s string) (PublicKey, error) {
	if s == "" || s[0] != multibaseBase58 {
		return PublicKey{}, fmt.Errorf("multibase prefix is not %q", multibaseBase58)
	}
	s = s[1:]
	if len(s) > maxKeyDIDBase58 {
		return PublicKey{}, fmt.Errorf("%d base58 characters is too long", len(s))
	}
	b, err := textenc.DecodeBase58(s)
	if err != nil {
		return PublicKey{}, err
	}
	if len(b) < len(ed25519Multicodec) || [2]byte(b) != ed25519Multicodec {
		return PublicKey{}, fmt.Errorf("multicodec prefix is not ed25519-pub: %w", ErrNotEd25519)
	}
	b = b[len(ed25519Multicodec):]
	if len(b) != PublicKeySize {
		return PublicKey{}, fmt.Errorf("key is %d bytes, want %d", len(b), PublicKeySize)
	}
	return PublicKey(b), nil
}

// ParseVerificationMethod parses a proof's verification method, a DID
// followed by "#key-1", and returns the DID. Anything else wraps
// [ErrInvalidDID].
func ParseVerificationMethod(s string) (DID, error) {
	did, ok := strings.CutSuffix(s, verificationSuffix)
	if !ok {
		return DID{}, fmt.Errorf("%w: verification method %q does not end %q", ErrInvalidDID, s, verificationSuffix)
	}
	return ParseDID(did)
}

// IsZero reports whether d is the zero DID, which is invalid.
func (d DID) IsZero() bool {
	return d == DID{}
}

// Method returns d's method: [MethodAtomic], [MethodKey], or "" for the
// zero DID.
func (d DID) Method() DIDMethod {
	return d.method
}

// ID returns the identity id d names. For a did:key it is the BLAKE3 of the
// key d carries. The zero DID returns the zero ID.
func (d DID) ID() ID {
	return d.id
}

// PublicKey returns the key a did:key carries. It reports false for a
// did:atomic, which carries none, and for the zero DID.
func (d DID) PublicKey() (PublicKey, bool) {
	if d.method != MethodKey {
		return PublicKey{}, false
	}
	return d.key, true
}

// Matches reports whether d is either DID form of k: k's did:atomic, or k's
// did:key. The zero DID matches no key.
func (d DID) Matches(k PublicKey) bool {
	switch d.method {
	case MethodAtomic:
		return d.id == k.ID()
	case MethodKey:
		return d.key == k
	}
	return false
}

// String returns d's text, or "" for the zero DID.
func (d DID) String() string {
	b, _ := d.AppendText(nil)
	return string(b)
}

// VerificationMethod returns d followed by "#key-1", the form a proof names
// its key by. It returns "" for the zero DID.
func (d DID) VerificationMethod() string {
	if d.IsZero() {
		return ""
	}
	return d.String() + verificationSuffix
}

// AppendText appends d's text to b. It implements [encoding.TextAppender].
// The zero DID is an error wrapping [ErrInvalidDID], and b is returned
// unchanged.
func (d DID) AppendText(b []byte) ([]byte, error) {
	switch d.method {
	case MethodAtomic:
		b = append(b, atomicPrefix...)
		return ident.AppendBase32(b, d.id[:]), nil
	case MethodKey:
		raw := make([]byte, 0, len(ed25519Multicodec)+PublicKeySize)
		raw = append(raw, ed25519Multicodec[:]...)
		raw = append(raw, d.key[:]...)
		b = append(b, keyPrefix...)
		b = append(b, multibaseBase58)
		return append(b, textenc.EncodeBase58(raw)...), nil
	}
	return b, fmt.Errorf("%w: the zero DID has no text", ErrInvalidDID)
}

// MarshalText returns d's text. It implements [encoding.TextMarshaler]. The
// zero DID is an error wrapping [ErrInvalidDID].
func (d DID) MarshalText() ([]byte, error) {
	return d.AppendText(nil)
}

// UnmarshalText parses text into d with the rules of [ParseDID]. It
// implements [encoding.TextUnmarshaler]. On failure d is unchanged.
func (d *DID) UnmarshalText(text []byte) error {
	v, err := ParseDID(string(text))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

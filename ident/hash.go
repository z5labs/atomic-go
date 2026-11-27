package ident

import (
	"bytes"
	"encoding/hex"

	"lukechampine.com/blake3"
)

// Hash is a 32-byte BLAKE3 digest. Atomic names changes, Merkle states,
// tags, attestations and provenance graphs by one.
//
// Its text form is [Base32Len] characters of base32; see [DecodeBase32].
// Compare hashes by value, not by their text, since the lenient decoder
// accepts more than one spelling of the same hash.
type Hash [Size]byte

// Sum returns the BLAKE3 hash of data.
func Sum(data []byte) Hash {
	return Hash(blake3.Sum256(data))
}

// Hasher computes a BLAKE3 [Hash] incrementally.
type Hasher struct {
	h *blake3.Hasher
}

// NewHasher returns a Hasher with nothing written to it.
func NewHasher() *Hasher {
	return &Hasher{h: blake3.New(Size, nil)}
}

// Write adds p to the data being hashed. It never returns an error.
func (h *Hasher) Write(p []byte) (int, error) {
	return h.h.Write(p)
}

// Sum returns the hash of everything written so far. It does not change
// the Hasher's state, so more can be written afterwards.
func (h *Hasher) Sum() Hash {
	var out Hash
	h.h.Sum(out[:0])
	return out
}

// ParseHash parses the base32 text of a hash, leniently, with the rules of
// [DecodeBase32]. A failure is a *[ParseError].
func ParseHash(s string) (Hash, error) {
	b, err := decodeBase32Lenient("hash", s)
	return Hash(b), err
}

// ParseHashHex parses 64 hex digits, of either case, as a hash.
//
// It is deliberately stricter than atomic-core's Merkle::from_hex, which
// parses each two-digit pair with Rust's u8::from_str_radix and so accepts a
// '+' at the start of a pair, as in "+f". Every hex writer in Atomic writes
// two plain lower-case digits per byte, so ParseHashHex refuses '+'
// anywhere. A failure is a *[ParseError].
func ParseHashHex(s string) (Hash, error) {
	b, err := decodeHex("hex hash", s)
	return Hash(b), err
}

func decodeHex(what, s string) ([Size]byte, error) {
	var out [Size]byte
	if len(s) != 2*Size {
		return out, parseError(what, s, min(len(s), 2*Size), ErrLength)
	}
	for i := 0; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return [Size]byte{}, parseError(what, s, i, ErrSyntax)
		}
	}
	if _, err := hex.Decode(out[:], []byte(s)); err != nil {
		// Unreachable: every byte was checked above.
		return [Size]byte{}, parseError(what, s, 0, ErrSyntax)
	}
	return out, nil
}

func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// IsZero reports whether h is all zero bytes, atomic-core's Merkle::ZERO.
func (h Hash) IsZero() bool {
	return h == Hash{}
}

// Compare returns -1, 0 or +1 as h sorts before, equal to or after o,
// comparing bytes.
func (h Hash) Compare(o Hash) int {
	return bytes.Compare(h[:], o[:])
}

// String returns the canonical base32 text of h: [Base32Len] upper-case
// characters.
func (h Hash) String() string {
	return EncodeBase32(h[:])
}

// Hex returns h as 64 lower-case hex digits.
func (h Hash) Hex() string {
	return hex.EncodeToString(h[:])
}

// AppendText appends the canonical base32 text of h to b. It implements
// [encoding.TextAppender] and never returns an error.
func (h Hash) AppendText(b []byte) ([]byte, error) {
	return AppendBase32(b, h[:]), nil
}

// MarshalText returns the canonical base32 text of h. It implements
// [encoding.TextMarshaler] and never returns an error.
func (h Hash) MarshalText() ([]byte, error) {
	return h.AppendText(nil)
}

// UnmarshalText parses base32 text into h with the rules of [ParseHash]. It
// implements [encoding.TextUnmarshaler]. On failure h is unchanged.
func (h *Hash) UnmarshalText(text []byte) error {
	v, err := ParseHash(string(text))
	if err != nil {
		return err
	}
	*h = v
	return nil
}

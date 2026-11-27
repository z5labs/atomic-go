package ident

import (
	"encoding/binary"
	"encoding/hex"
)

// SetID is an order-invariant identity for a set of hashes, which Atomic
// keeps beside a view's order-sensitive Merkle state. The empty set is the
// all-zero SetID.
//
// Its text forms are those of a [Hash]: base32, and lower-case hex.
type SetID [Size]byte

// SetIDOf returns the SetID of members. A member listed more than once is
// counted once. Which hashes are members, such as whether a view's tags
// are, is the caller's choice.
func SetIDOf(members []Hash) SetID {
	seen := make(map[Hash]struct{}, len(members))
	var s SetID
	for _, h := range members {
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		s = s.Add(h)
	}
	return s
}

// Add returns s with h added: the BLAKE3 hash of [SetIDDomain] || h, read
// as four little-endian uint64 lanes, is added lane by lane to s, wrapping
// mod 2^64 with no carry between lanes.
//
// Add does not know what s already holds, so adding a hash twice counts it
// twice. Use [SetIDOf] to build a SetID from a list that may repeat.
func (s SetID) Add(h Hash) SetID {
	var buf [len(SetIDDomain) + Size]byte
	copy(buf[:], SetIDDomain)
	copy(buf[len(SetIDDomain):], h[:])
	e := Sum(buf[:])

	var out SetID
	for i := 0; i < Size; i += 8 {
		lane := binary.LittleEndian.Uint64(s[i:]) + binary.LittleEndian.Uint64(e[i:])
		binary.LittleEndian.PutUint64(out[i:], lane)
	}
	return out
}

// IsZero reports whether s is all zero bytes, the SetID of the empty set.
func (s SetID) IsZero() bool {
	return s == SetID{}
}

// ParseSetID parses the base32 text of a SetID with the lenient rules of
// [DecodeBase32]. A failure is a *[ParseError].
func ParseSetID(s string) (SetID, error) {
	b, err := decodeBase32Lenient("set id", s)
	return SetID(b), err
}

// ParseSetIDHex parses 64 hex digits, of either case, as a SetID, with the
// rules of [ParseHashHex]. A failure is a *[ParseError].
func ParseSetIDHex(s string) (SetID, error) {
	b, err := decodeHex("hex set id", s)
	return SetID(b), err
}

// String returns the canonical base32 text of s.
func (s SetID) String() string {
	return EncodeBase32(s[:])
}

// Hex returns s as 64 lower-case hex digits.
func (s SetID) Hex() string {
	return hex.EncodeToString(s[:])
}

// AppendText appends the canonical base32 text of s to b. It implements
// [encoding.TextAppender] and never returns an error.
func (s SetID) AppendText(b []byte) ([]byte, error) {
	return AppendBase32(b, s[:]), nil
}

// MarshalText returns the canonical base32 text of s. It implements
// [encoding.TextMarshaler] and never returns an error.
func (s SetID) MarshalText() ([]byte, error) {
	return s.AppendText(nil)
}

// UnmarshalText parses base32 text into s with the rules of [ParseSetID].
// It implements [encoding.TextUnmarshaler]. On failure s is unchanged.
func (s *SetID) UnmarshalText(text []byte) error {
	v, err := ParseSetID(string(text))
	if err != nil {
		return err
	}
	*s = v
	return nil
}

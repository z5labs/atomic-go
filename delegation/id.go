package delegation

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
)

const (
	// URNPrefix begins the URN form of an [ID], which documents and tokens
	// carry.
	URNPrefix = "urn:atomic:delegation:"

	// IDDomain is the domain separator a delegation id's hash input begins
	// with (docs/remote/auth.md#delegation-ids).
	IDDomain = "atomic:delegation:v1"
)

// ID names a delegation certificate: a 32-byte BLAKE3 hash of who
// delegated to whom, and when (docs/remote/auth.md#delegation-ids).
//
// It has two text forms: [ident.Base32Len] characters of strict, upper-case,
// unpadded base32, used in file names and paths, and the URN, which is
// [URNPrefix] followed by the base32, used as a certificate's @id and a
// token's dlg. [ID.String] and the text methods use the URN.
type ID [ident.Size]byte

// NewID returns the id of a delegation from delegator to delegate issued at
// issued: the BLAKE3 hash of [IDDomain], the delegator's 32 bytes, the
// delegate's 32 bytes and issued as a little-endian signed 64-bit count of
// Unix seconds. Anything finer than a second in issued is ignored.
func NewID(delegator, delegate identity.ID, issued time.Time) ID {
	b := make([]byte, 0, len(IDDomain)+2*ident.Size+8)
	b = append(b, IDDomain...)
	b = append(b, delegator[:]...)
	b = append(b, delegate[:]...)
	b = binary.LittleEndian.AppendUint64(b, uint64(issued.Unix()))
	return ID(ident.Sum(b))
}

// ParseID parses a delegation id written as its base32, with or without
// [URNPrefix] before it. The base32 is decoded with the rules of
// [ident.DecodeBase32Strict], so lower case, padding and white space are
// refused, and so is a prefix written in any other case. A failure wraps
// [ErrInvalidID], and the *[ident.ParseError] that describes it when the
// base32 is at fault.
func ParseID(s string) (ID, error) {
	b, err := ident.DecodeBase32Strict(strings.TrimPrefix(s, URNPrefix), ident.Size)
	if err != nil {
		return ID{}, fmt.Errorf("%w: %w", ErrInvalidID, err)
	}
	return ID(b), nil
}

// Base32 returns the strict base32 text of id, without the URN prefix.
func (id ID) Base32() string {
	return ident.EncodeBase32(id[:])
}

// URN returns id's URN: [URNPrefix] followed by its base32.
func (id ID) URN() string {
	return URNPrefix + id.Base32()
}

// String returns id's URN.
func (id ID) String() string {
	return id.URN()
}

// AppendText appends id's URN to b. It implements [encoding.TextAppender]
// and never returns an error.
func (id ID) AppendText(b []byte) ([]byte, error) {
	b = append(b, URNPrefix...)
	return ident.AppendBase32(b, id[:]), nil
}

// MarshalText returns id's URN. It implements [encoding.TextMarshaler] and
// never returns an error.
func (id ID) MarshalText() ([]byte, error) {
	return id.AppendText(make([]byte, 0, len(URNPrefix)+ident.Base32Len))
}

// UnmarshalText parses text into id with the rules of [ParseID], so it
// accepts the base32 with or without the URN prefix. It implements
// [encoding.TextUnmarshaler]. On failure id is unchanged.
func (id *ID) UnmarshalText(text []byte) error {
	v, err := ParseID(string(text))
	if err != nil {
		return err
	}
	*id = v
	return nil
}

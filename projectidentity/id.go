package projectidentity

import (
	"fmt"
	"io"

	"github.com/z5labs/atomic-go/ident"
)

// ProjectID names a project lineage: 32 random bytes, chosen once when a
// project is created and shared by every clone of it
// (docs/remote/auth.md#project-identity).
//
// Its text form, the value of a document's atomic:projectId, is
// [ident.Base32Len] characters of strict, upper-case, unpadded base32.
type ProjectID [ident.Size]byte

// NewProjectID reads a new project id from rand, which is required: it is
// the only source of the id's randomness, and nothing falls back to the
// host's. Pass crypto/rand.Reader at the program's edge, and a fixed
// reader in tests.
//
// A nil rand is an error wrapping [ErrInvalid]. A rand that yields fewer
// than 32 bytes is an error wrapping the read's error, [io.ErrUnexpectedEOF]
// for a short read.
func NewProjectID(rand io.Reader) (ProjectID, error) {
	if rand == nil {
		return ProjectID{}, fmt.Errorf("%w: new project id: nil random source", ErrInvalid)
	}
	var id ProjectID
	if _, err := io.ReadFull(rand, id[:]); err != nil {
		return ProjectID{}, fmt.Errorf("projectidentity: new project id: %w", err)
	}
	return id, nil
}

// ParseProjectID parses a project id written as its base32, with the rules
// of [ident.DecodeBase32Strict]: only the upper-case alphabet, no padding
// or white space, exactly [ident.Base32Len] characters and zero unused low
// bits in the last one. A failure wraps [ErrInvalid] and the
// *[ident.ParseError] that describes it.
func ParseProjectID(s string) (ProjectID, error) {
	b, err := ident.DecodeBase32Strict(s, ident.Size)
	if err != nil {
		return ProjectID{}, fmt.Errorf("%w: project id: %w", ErrInvalid, err)
	}
	return ProjectID(b), nil
}

// IsZero reports whether id is the zero project id.
func (id ProjectID) IsZero() bool {
	return id == ProjectID{}
}

// String returns id's strict base32.
func (id ProjectID) String() string {
	return ident.EncodeBase32(id[:])
}

// AppendText appends id's base32 to b. It implements
// [encoding.TextAppender] and never returns an error.
func (id ProjectID) AppendText(b []byte) ([]byte, error) {
	return ident.AppendBase32(b, id[:]), nil
}

// MarshalText returns id's base32. It implements [encoding.TextMarshaler]
// and never returns an error.
func (id ProjectID) MarshalText() ([]byte, error) {
	return id.AppendText(make([]byte, 0, ident.Base32Len))
}

// UnmarshalText parses text into id with the rules of [ParseProjectID]. It
// implements [encoding.TextUnmarshaler]. On failure id is unchanged.
func (id *ProjectID) UnmarshalText(text []byte) error {
	v, err := ParseProjectID(string(text))
	if err != nil {
		return err
	}
	*id = v
	return nil
}

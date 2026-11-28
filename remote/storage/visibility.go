package storage

import (
	"fmt"
	"strings"
)

// Visibility is who can see a workspace or project
// (docs/remote/endpoints/storage.md#visibility).
type Visibility string

const (
	// VisibilityPublic is a workspace or project anyone can see.
	VisibilityPublic Visibility = "public"

	// VisibilityPrivate is a workspace or project only its members can
	// see.
	VisibilityPrivate Visibility = "private"
)

// ParseVisibility returns the Visibility s names, in any case, as the Rust
// client's FromStr reads a CLI flag: "Public", "PUBLIC" and "public" are
// all [VisibilityPublic]. Any other s is an error wrapping
// [ErrInvalidArgument].
func ParseVisibility(s string) (Visibility, error) {
	v := Visibility(strings.ToLower(s))
	if !v.valid() {
		return "", fmt.Errorf("%w: visibility %q is not public or private", ErrInvalidArgument, s)
	}
	return v, nil
}

// valid reports whether v is one of the two spellings the server accepts.
func (v Visibility) valid() bool {
	return v == VisibilityPublic || v == VisibilityPrivate
}

// check returns an error wrapping [ErrInvalidArgument] when v is not
// exactly "public" or "private". The wire spelling is matched exactly, as
// serde matches it, so "PUBLIC" is refused here; [ParseVisibility] is the
// way to accept any case.
func (v Visibility) check() error {
	if !v.valid() {
		return fmt.Errorf("%w: visibility %q is not %q or %q", ErrInvalidArgument, string(v), VisibilityPublic, VisibilityPrivate)
	}
	return nil
}

// UnmarshalText reads the wire spelling, which must be exactly "public" or
// "private", as serde reads it.
func (v *Visibility) UnmarshalText(b []byte) error {
	got := Visibility(b)
	if err := got.check(); err != nil {
		return err
	}
	*v = got
	return nil
}

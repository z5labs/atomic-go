package ident

import (
	"errors"
	"strconv"
	"strings"
)

// Position is a byte offset inside a change: the change's hash and an
// offset into its content.
//
// Its text form, written by [Position.MarshalText] and read by
// [ParsePosition], is the change's base32, a '.', and the offset in a
// variable-length base32. [Position.String] writes a different, display-only
// form: the change's base32, a ':', and the offset in decimal. The formats
// are documented in docs/remote/types.md#positions.
type Position struct {
	// Change is the hash of the change the offset is in.
	Change Hash
	// Offset is the byte offset inside the change.
	Offset uint64
}

// ParsePosition parses the text form of a position the way atomic-core's
// Position decoder does:
//
//   - the text is split at the first '.', and there must be one;
//   - the part before it is a change hash, read with the rules of
//     [ParseHash];
//   - the part after it is the offset's 5-bit groups, most significant
//     first, as base32 digits of either case. Leading 'A's are accepted, and
//     an empty offset is 0.
//
// A failure is a *[ParseError] whose Offset counts from the start of s. An
// offset above 2^64-1 wraps [ErrRange]; a missing '.' or a byte outside the
// alphabet wraps [ErrSyntax]; a change part of the wrong length wraps
// [ErrLength].
func ParsePosition(s string) (Position, error) {
	const what = "position"
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return Position{}, parseError(what, s, len(s), ErrSyntax)
	}
	change, err := decodeBase32Lenient(what, s[:dot])
	if err != nil {
		// The change part starts at offset 0, so the offset already counts
		// from the start of s; only the input needs widening.
		var pe *ParseError
		if errors.As(err, &pe) {
			pe.Input = s
		}
		return Position{}, err
	}
	var off uint64
	for i := dot + 1; i < len(s); i++ {
		v, ok := base32Value(s[i], true)
		if !ok {
			return Position{}, parseError(what, s, i, ErrSyntax)
		}
		if off > ^uint64(0)>>5 {
			return Position{}, parseError(what, s, i, ErrRange)
		}
		off = off<<5 | uint64(v)
	}
	return Position{Change: Hash(change), Offset: off}, nil
}

// AppendText appends the text form of p, the form [ParsePosition] reads, to
// b. The offset is written from its most significant non-zero 5-bit group
// down, so 0 is "A" and 100 is "DE". It implements [encoding.TextAppender]
// and never returns an error.
func (p Position) AppendText(b []byte) ([]byte, error) {
	b = AppendBase32(b, p.Change[:])
	b = append(b, '.')
	return appendOffset(b, p.Offset), nil
}

func appendOffset(b []byte, off uint64) []byte {
	shift := 0
	for shift+5 < 64 && off>>(shift+5) != 0 {
		shift += 5
	}
	for ; shift >= 0; shift -= 5 {
		b = append(b, base32Alphabet[(off>>shift)&0x1f])
	}
	return b
}

// MarshalText returns the text form of p; see [Position.AppendText]. It
// implements [encoding.TextMarshaler] and never returns an error.
func (p Position) MarshalText() ([]byte, error) {
	return p.AppendText(nil)
}

// UnmarshalText parses the text form of a position into p with the rules of
// [ParsePosition]. It implements [encoding.TextUnmarshaler]. On failure p is
// unchanged.
func (p *Position) UnmarshalText(text []byte) error {
	v, err := ParsePosition(string(text))
	if err != nil {
		return err
	}
	*p = v
	return nil
}

// String returns the display form of p: the change's base32, a ':', and the
// offset in decimal, as atomic-core's Display does. [ParsePosition] does not
// read this form; use [Position.MarshalText] for text that is read back.
func (p Position) String() string {
	b := AppendBase32(nil, p.Change[:])
	b = append(b, ':')
	return string(strconv.AppendUint(b, p.Offset, 10))
}

// Package postcard reads and writes the postcard 1.1.3 wire format that
// Atomic's change payloads, sync packs and tag records are encoded in.
//
// Postcard writes no type tags, field names or field counts, so a payload
// is decoded by calling the [Decoder] method for each field of the Rust
// type, in declaration order. A [Decoder] works on a byte slice already in
// memory; a [Reader] decodes the same encoding from an [io.Reader] one
// value at a time, so a large stream never has to be held whole. An
// [Encoder] writes the minimal encoding of each value, which is the one
// Atomic hashes.
//
// The rules followed here are the ones Atomic's Rust code gets from the
// postcard crate, including the ones a Go reader would not guess: an
// overlong varint is accepted up to the type's maximum length, strings must
// be valid UTF-8, a bool or option tag other than 0 or 1 is an error, and
// [Decode] ignores bytes left over after the value, as postcard::from_bytes
// does. Every failure is an *[Error] carrying the byte offset at which the
// value that failed begins.
//
// char, maps, u128 and i128 are not supported; no Atomic payload uses them.
//
// The format is documented in docs/remote/change-format/container.md.
package postcard

import (
	"errors"
	"fmt"
)

// Varint limits. A varint of a type takes at most this many bytes, and the
// last of those bytes may not exceed the type's last-byte limit, which
// would overflow the type.
const (
	maxVarintU16 = 3
	maxVarintU32 = 5
	maxVarintU64 = 10

	lastByteU16 = 0x03
	lastByteU32 = 0x0f
	lastByteU64 = 0x01
)

var (
	// ErrUnexpectedEnd reports input that ends before the value does,
	// including a length or count larger than the input that is left.
	ErrUnexpectedEnd = errors.New("unexpected end of input")

	// ErrBadVarint reports a varint that is too long for its type, or
	// whose last byte would overflow it.
	ErrBadVarint = errors.New("bad varint")

	// ErrBadBool reports a bool byte other than 0x00 or 0x01.
	ErrBadBool = errors.New("bad bool")

	// ErrBadOption reports an option tag other than 0x00 or 0x01.
	ErrBadOption = errors.New("bad option tag")

	// ErrBadUTF8 reports a string whose bytes are not valid UTF-8.
	ErrBadUTF8 = errors.New("invalid UTF-8")

	// ErrBadLength reports a length the type being decoded does not allow,
	// such as a length-prefixed signature that is not 64 bytes long. The
	// codec cannot know such a rule, so callers report it with
	// [Decoder.Fail].
	ErrBadLength = errors.New("bad length")

	// ErrUnknownVariant reports an enum variant index beyond the last
	// variant the type declares.
	ErrUnknownVariant = errors.New("unknown enum variant")

	// ErrTrailingBytes reports bytes left over after the value, from
	// [DecodeExact].
	ErrTrailingBytes = errors.New("trailing bytes after value")

	// ErrTooLong reports a length or count above a [Reader]'s limit.
	ErrTooLong = errors.New("length above limit")
)

// Error is a decoding failure. Err is one of the package's sentinel errors,
// an error passed to [Decoder.Fail], or the error of a [Reader]'s
// underlying reader, so callers can test it with [errors.Is].
type Error struct {
	// Offset is the byte offset in the input at which the value that
	// failed begins.
	Offset int64
	// Err is the reason.
	Err error
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("postcard: %v at offset %d", e.Err, e.Offset)
}

// Unwrap returns e.Err.
func (e *Error) Unwrap() error {
	return e.Err
}

func zigzag16(u uint16) int16 { return int16(u>>1) ^ -int16(u&1) }
func zigzag32(u uint32) int32 { return int32(u>>1) ^ -int32(u&1) }
func zigzag64(u uint64) int64 { return int64(u>>1) ^ -int64(u&1) }

func unzigzag16(n int16) uint16 { return uint16(n<<1) ^ uint16(n>>15) }
func unzigzag32(n int32) uint32 { return uint32(n<<1) ^ uint32(n>>31) }
func unzigzag64(n int64) uint64 { return uint64(n<<1) ^ uint64(n>>63) }

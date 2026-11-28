// Package changefile holds the fixed parts of Atomic's V3 change-file
// container: the 64-byte [FileHeader] and its [Flags], the [SectionType]s
// and the order they appear in, the [Section] and [ChunkInfo] views of a
// framed section, the [HashTable] that payloads name other changes through,
// and the one [FormatError] every change-file failure is reported as.
//
// A change file is the header, the hash dedup table, the sections and a
// 32-byte trailer, back to back. The trailer is the BLAKE3 change hash of
// the table and the hashed sections, so it is the change's identity.
//
// The package does no compression of its own and imports no zstd code.
// Code that reads or writes sections takes a [Decompressor] or a
// [Compressor], the narrowest calls it makes; package zstdframe provides
// both.
//
// The format is documented in docs/remote/change-format/container.md.
package changefile

import (
	"errors"
	"fmt"

	"github.com/z5labs/atomic-go/ident"
)

const (
	// Magic is the four bytes every V3 change file starts with.
	Magic = "ATOM"

	// HeaderSize is the size of the [FileHeader] at the start of a file.
	HeaderSize = 64

	// TrailerSize is the size of the trailer at the end of a file: the
	// change hash.
	TrailerSize = ident.Size

	// MaxHashTableEntries is the most entries a [HashTable] may hold,
	// 65534, so the highest index that names a slot is 0xFFFD and
	// [ThisChange] never does.
	MaxHashTableEntries = 1<<16 - 2
)

// Decompressor decompresses one section payload: it appends the
// decompressed bytes of src, every zstd frame in it concatenated, to dst and
// returns the extended slice, leaving dst's existing bytes as they are.
//
// A change file is untrusted input, and a few bytes of payload can claim
// gigabytes of output, so an implementation must bound what one call
// produces and fail past its bound. The bound is the implementation's,
// set when it is built: a *zstdframe.Decoder is one, with the limit given
// to zstdframe.NewDecoder.
type Decompressor interface {
	Decompress(dst, src []byte) ([]byte, error)
}

// Compressor compresses one section payload, appending the compressed bytes
// of src to dst. A *zstdframe.Encoder and zstdframe.Stored are both one.
//
// Compressing a byte slice in memory cannot fail, so Compress returns no
// error; see [ErrCompress].
type Compressor interface {
	Compress(dst, src []byte) []byte
}

// The kinds of [FormatError]. Test for one with [errors.Is].
var (
	// ErrInvalidMagic reports a file that does not start with [Magic].
	ErrInvalidMagic = errors.New("invalid magic bytes")

	// ErrLegacyFormat reports a file in the pre-V3 bincode format, which
	// starts with the little-endian u32 1 or 2 in place of [Magic]. Atomic
	// cannot read such a file either; it must be re-recorded.
	ErrLegacyFormat = errors.New("legacy pre-V3 change format")

	// ErrUnsupportedVersion reports a header version other than 1, 2 or 3.
	ErrUnsupportedVersion = errors.New("unsupported format version")

	// ErrInvalidHeader reports a header whose fields contradict each other,
	// or a hash table with a repeated entry.
	ErrInvalidHeader = errors.New("invalid header")

	// ErrInvalidSectionType reports a section type byte that is not one of
	// the nine [SectionType]s.
	ErrInvalidSectionType = errors.New("invalid section type")

	// ErrUnexpectedSection reports a section out of order, repeated, or not
	// allowed in the file's version.
	ErrUnexpectedSection = errors.New("unexpected section")

	// ErrTruncated reports a file that ends inside a structure.
	ErrTruncated = errors.New("truncated change file")

	// ErrDecompress reports a section payload that would not decompress.
	ErrDecompress = errors.New("decompression error")

	// ErrCompress reports a section payload that would not compress. It
	// mirrors Atomic's FormatError::Compress. A [Compressor] cannot fail, so
	// nothing compressing through one reports it; it is there for code that
	// compresses some other way.
	ErrCompress = errors.New("compression error")

	// ErrHashMismatch reports a trailer that is not the change hash of the
	// file. The error is, or wraps, a *[HashMismatchError].
	ErrHashMismatch = errors.New("hash mismatch")

	// ErrHashTableFull reports a hash table of more than
	// [MaxHashTableEntries] entries.
	ErrHashTableFull = errors.New("hash dedup table is full")

	// ErrHashIndexOutOfBounds reports a [HashIndex] that names no slot of
	// its table, [ThisChange] included.
	ErrHashIndexOutOfBounds = errors.New("hash index out of bounds")

	// ErrHashNotFound reports a hash a writer refers to that was never added
	// to its [TableBuilder].
	ErrHashNotFound = errors.New("hash not found in dedup table")

	// ErrInvalidChangeMetadata reports change metadata that breaks a rule,
	// such as a dependency index that names the zero hash.
	ErrInvalidChangeMetadata = errors.New("invalid change metadata")

	// ErrInvalidGraphOp reports a GRAPH operation that breaks a rule.
	ErrInvalidGraphOp = errors.New("invalid graph operation")

	// ErrPayload reports a section payload that does not decode as its
	// type.
	ErrPayload = errors.New("invalid section payload")

	// ErrValidation reports a decoded change that fails Atomic's
	// validate_v2 rules.
	ErrValidation = errors.New("change validation failed")

	// ErrTrailingBytes reports bytes after the trailer.
	ErrTrailingBytes = errors.New("trailing bytes after change file")

	// ErrChunkMismatch reports a CONTENT frame whose chunk index, hash or
	// length does not match its chunk.
	ErrChunkMismatch = errors.New("content chunk mismatch")
)

// FormatError is a change-file failure. Every error this package and the
// change-file reader and writer report is a *FormatError, or a
// *[HashMismatchError] for a trailer that does not match.
//
// [errors.Is] matches a FormatError against its Kind, and reaches Err
// through Unwrap.
type FormatError struct {
	// Kind is the sentinel that classifies the failure, such as
	// [ErrInvalidHeader].
	Kind error

	// Section is the type of the section the failure is in, or 0, which
	// is no section type, when it is in none.
	Section SectionType

	// Offset is the byte offset in the file where the failure was found,
	// or -1 when it has none, as for a hash table lookup. 0 is a real
	// offset, the magic, so a FormatError built by hand for a failure with
	// no offset must set -1.
	Offset int64

	// Detail says what was wrong, or is "".
	Detail string

	// Err is the underlying cause, such as a decompressor's error, or nil.
	Err error
}

// Error implements the error interface.
func (e *FormatError) Error() string {
	kind := "format error"
	if e.Kind != nil {
		kind = e.Kind.Error()
	}
	s := "changefile: " + kind
	if e.Section != 0 {
		s += " in " + e.Section.String() + " section"
	}
	if e.Offset >= 0 {
		s += fmt.Sprintf(" at offset %d", e.Offset)
	}
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

// Is reports whether target is e's Kind.
func (e *FormatError) Is(target error) bool {
	return e.Kind != nil && target == e.Kind
}

// Unwrap returns e.Err.
func (e *FormatError) Unwrap() error {
	return e.Err
}

// HashMismatchError reports a trailer that is not the change hash the
// file's bytes give. It matches [ErrHashMismatch] with [errors.Is].
type HashMismatchError struct {
	// Trailer is the hash the file's trailer holds.
	Trailer ident.Hash

	// Computed is the change hash of the file's table and hashed sections.
	Computed ident.Hash
}

// Error implements the error interface. Unlike Atomic's message, which
// prints the first 8 bytes of each, it prints both hashes in full.
func (e *HashMismatchError) Error() string {
	return "changefile: hash mismatch: trailer " + e.Trailer.String() + ", computed " + e.Computed.String()
}

// Is reports whether target is [ErrHashMismatch].
func (e *HashMismatchError) Is(target error) bool {
	return target == ErrHashMismatch
}

func formatError(kind error, section SectionType, offset int64, detail string) *FormatError {
	return &FormatError{Kind: kind, Section: section, Offset: offset, Detail: detail}
}

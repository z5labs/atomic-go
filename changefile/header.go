package changefile

import (
	"encoding/binary"
	"fmt"
)

// Version is the schema number in a [FileHeader]. It decides how the HEADER
// payload and the section manifest are read.
type Version uint32

const (
	// Version1 is the legacy, read-only schema of every change recorded
	// before atomic 0.20. Its reader is lenient: section order, duplicates
	// and counts are not checked.
	Version1 Version = 1

	// Version2 is the current schema, written for every change without a
	// SPAN section.
	Version2 Version = 2

	// Version3 is the current schema for a change with one SPAN section.
	Version3 Version = 3
)

// Supported reports whether v is 1, 2 or 3, the versions a reader accepts.
func (v Version) Supported() bool {
	return v >= Version1 && v <= Version3
}

// Flags is the bitfield in header bytes 8 to 11. Bits 4 to 31 are
// reserved: a parsed header keeps them as they were, they add no sections,
// and [FileHeader.AppendBinary] writes them back.
type Flags uint32

const (
	// FlagProvenance says one PROVENANCE section is present.
	FlagProvenance Flags = 1 << 0

	// FlagSemantic says SemanticSections is greater than 0. It adds no
	// section by itself.
	FlagSemantic Flags = 1 << 1

	// FlagUnhashed says one UNHASHED section is present.
	FlagUnhashed Flags = 1 << 2

	// FlagSignature says one SIGNATURE section is present.
	FlagSignature Flags = 1 << 3

	knownFlags = FlagProvenance | FlagSemantic | FlagUnhashed | FlagSignature
)

// Has reports whether every bit of flag is set in f.
func (f Flags) Has(flag Flags) bool {
	return f&flag == flag
}

// Unknown returns the bits of f that are none of the four flags.
func (f Flags) Unknown() Flags {
	return f &^ knownFlags
}

// Offsets of the header's fields.
const (
	offVersion           = 4
	offFlags             = 8
	offHashTableEntries  = 12
	offGraphSections     = 16
	offSemanticSections  = 20
	offContentChunks     = 24
	offTotalUncompressed = 28
	offReserved          = 36
)

// FileHeader is the fixed 64-byte header at the start of a change file,
// after the four [Magic] bytes. Every integer is little-endian. The header
// is not part of the change hash.
type FileHeader struct {
	// Version is the schema number.
	Version Version

	// Flags says which optional sections are present.
	Flags Flags

	// HashTableEntries is the number of 32-byte [HashTable] entries that
	// follow the header. Atomic's writer always writes at least 1, for the
	// zero hash in slot 0.
	HashTableEntries uint32

	// GraphSections is the number of GRAPH sections.
	GraphSections uint32

	// SemanticSections is the number of SEMANTIC sections. It is greater
	// than 0 exactly when [FlagSemantic] is set.
	SemanticSections uint32

	// ContentChunks is the number of CONTENT sections.
	ContentChunks uint32

	// TotalUncompressed is described as the sum of the uncompressed section
	// sizes. No reader uses it, and Atomic's writer leaves it 0.
	TotalUncompressed uint64

	// Reserved is kept as it was read. Writers write zeros, and readers
	// ignore it.
	Reserved [28]byte
}

// ParseFileHeader parses the header in the first [HeaderSize] bytes of b,
// as Atomic's FileHeader::from_bytes does. Bytes after the header are
// ignored. It fails with a *[FormatError] of kind:
//
//   - [ErrTruncated], when b is shorter than the magic or the header;
//   - [ErrInvalidMagic], when b does not start with [Magic];
//   - [ErrUnsupportedVersion], when the version is 0 or above 3.
//
// It does not check that the fields agree with each other; see
// [FileHeader.Validate]. Nor does it tell a pre-V3 file from any other
// without the magic: that is the reader's check, reported as
// [ErrLegacyFormat].
func ParseFileHeader(b []byte) (FileHeader, error) {
	if len(b) < len(Magic) {
		return FileHeader{}, formatError(ErrTruncated, 0, int64(len(b)), fmt.Sprintf("file header needs %d bytes, got %d", HeaderSize, len(b)))
	}
	if string(b[:len(Magic)]) != Magic {
		return FileHeader{}, formatError(ErrInvalidMagic, 0, 0, fmt.Sprintf("expected % x, got % x", Magic, b[:len(Magic)]))
	}
	if len(b) < HeaderSize {
		return FileHeader{}, formatError(ErrTruncated, 0, int64(len(b)), fmt.Sprintf("file header needs %d bytes, got %d", HeaderSize, len(b)))
	}
	le := binary.LittleEndian
	h := FileHeader{
		Version:           Version(le.Uint32(b[offVersion:])),
		Flags:             Flags(le.Uint32(b[offFlags:])),
		HashTableEntries:  le.Uint32(b[offHashTableEntries:]),
		GraphSections:     le.Uint32(b[offGraphSections:]),
		SemanticSections:  le.Uint32(b[offSemanticSections:]),
		ContentChunks:     le.Uint32(b[offContentChunks:]),
		TotalUncompressed: le.Uint64(b[offTotalUncompressed:]),
	}
	copy(h.Reserved[:], b[offReserved:HeaderSize])
	if !h.Version.Supported() {
		return FileHeader{}, unsupportedVersion(h.Version)
	}
	return h, nil
}

func unsupportedVersion(v Version) *FormatError {
	return formatError(ErrUnsupportedVersion, 0, offVersion, fmt.Sprintf("expected 1, 2 or 3, got %d", v))
}

// Validate checks the header as Atomic's FileHeader::validate does, and
// that its version is supported. It fails with a *[FormatError] of kind
// [ErrUnsupportedVersion] for a version other than 1, 2 or 3, and of kind
// [ErrInvalidHeader] when:
//
//   - HashTableEntries is above [MaxHashTableEntries];
//   - [FlagSemantic] is set and SemanticSections is 0;
//   - SemanticSections is above 0 and [FlagSemantic] is not set.
//
// No other flag is compared with a count here: whether the sections in a
// file match the header is the reader's check.
func (h FileHeader) Validate() error {
	if !h.Version.Supported() {
		return unsupportedVersion(h.Version)
	}
	if h.HashTableEntries > MaxHashTableEntries {
		return formatError(ErrInvalidHeader, 0, offHashTableEntries, fmt.Sprintf("hash_table_entries %d exceeds the maximum of %d", h.HashTableEntries, MaxHashTableEntries))
	}
	semantic := h.Flags.Has(FlagSemantic)
	if semantic && h.SemanticSections == 0 {
		return formatError(ErrInvalidHeader, 0, offFlags, "HAS_SEMANTIC is set but semantic_section_count is 0")
	}
	if !semantic && h.SemanticSections > 0 {
		return formatError(ErrInvalidHeader, 0, offSemanticSections, fmt.Sprintf("semantic_section_count is %d but HAS_SEMANTIC is not set", h.SemanticSections))
	}
	return nil
}

// SectionCount returns the number of sections the header says follow it,
// as Atomic's FileHeader::section_count does: HEADER and DEPS, one each for
// [FlagProvenance], [FlagUnhashed] and [FlagSignature], the GRAPH, SEMANTIC
// and CONTENT counts, and one SPAN section in version 3. [FlagSemantic] and
// unknown flag bits add none.
func (h FileHeader) SectionCount() uint64 {
	n := uint64(2)
	for _, f := range []Flags{FlagProvenance, FlagUnhashed, FlagSignature} {
		if h.Flags.Has(f) {
			n++
		}
	}
	n += uint64(h.GraphSections) + uint64(h.SemanticSections) + uint64(h.ContentChunks)
	if h.Version == Version3 {
		n++
	}
	return n
}

// AppendBinary appends the header's [HeaderSize] bytes, [Magic] first, to
// b. Every field is written as it is, unknown flag bits and Reserved
// included, so a parsed header re-encodes to the bytes it was parsed from.
// It does not validate the header, and never returns an error; it
// implements [encoding.BinaryAppender].
func (h FileHeader) AppendBinary(b []byte) ([]byte, error) {
	le := binary.LittleEndian
	b = append(b, Magic...)
	b = le.AppendUint32(b, uint32(h.Version))
	b = le.AppendUint32(b, uint32(h.Flags))
	b = le.AppendUint32(b, h.HashTableEntries)
	b = le.AppendUint32(b, h.GraphSections)
	b = le.AppendUint32(b, h.SemanticSections)
	b = le.AppendUint32(b, h.ContentChunks)
	b = le.AppendUint64(b, h.TotalUncompressed)
	b = append(b, h.Reserved[:]...)
	return b, nil
}

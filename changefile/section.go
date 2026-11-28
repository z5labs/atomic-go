package changefile

import (
	"fmt"

	"github.com/z5labs/atomic-go/ident"
)

// SectionType is the byte that opens every section's frame. Sections appear
// in the order of [SectionType.Order], not of their bytes: CONTENT (0x20)
// comes after SEMANTIC (0x30).
type SectionType uint8

const (
	// SectionHeader holds the change's metadata. Exactly one per file.
	SectionHeader SectionType = 0x01

	// SectionDeps holds the change's dependencies as hash indices. Exactly
	// one per file, possibly an empty list.
	SectionDeps SectionType = 0x02

	// SectionProvenance holds the change's provenance list. One when
	// [FlagProvenance] is set.
	SectionProvenance SectionType = 0x03

	// SectionSignature holds the change's signature. One when
	// [FlagSignature] is set. It is not hashed.
	SectionSignature SectionType = 0x04

	// SectionGraph holds graph operations. As many as
	// [FileHeader.GraphSections].
	SectionGraph SectionType = 0x10

	// SectionSpan holds the span patch. Exactly one in version 3, none
	// otherwise.
	SectionSpan SectionType = 0x11

	// SectionContent holds one chunk of the content blob, behind a 45-byte
	// frame. As many as [FileHeader.ContentChunks].
	SectionContent SectionType = 0x20

	// SectionSemantic holds file operations. As many as
	// [FileHeader.SemanticSections].
	SectionSemantic SectionType = 0x30

	// SectionUnhashed holds JSON metadata. One when [FlagUnhashed] is set.
	// It is not hashed.
	SectionUnhashed SectionType = 0xF0
)

const (
	// FrameSize is the size of the frame before every section but CONTENT:
	// the type byte and a little-endian u32 compressed length.
	FrameSize = 5

	// ChunkFrameSize is the size of the frame before a CONTENT section:
	// the type byte, then the little-endian u32 chunk index, the 32-byte
	// chunk hash and the little-endian u32 uncompressed and compressed
	// lengths.
	ChunkFrameSize = 45
)

// Known reports whether t is one of the nine section types. A reader
// cannot skip a section of any other type, because the type decides how
// long its frame is.
func (t SectionType) Known() bool {
	return t.Order() >= 0
}

// Hashed reports whether a section of type t is part of the change hash.
// Every known type is but SIGNATURE and UNHASHED; an unknown type is not.
func (t SectionType) Hashed() bool {
	return t.Known() && t != SectionSignature && t != SectionUnhashed
}

// Order returns t's place in a file's section order, from HEADER 0 to
// UNHASHED 8, or -1 for an unknown type.
func (t SectionType) Order() int {
	switch t {
	case SectionHeader:
		return 0
	case SectionDeps:
		return 1
	case SectionProvenance:
		return 2
	case SectionSignature:
		return 3
	case SectionGraph:
		return 4
	case SectionSpan:
		return 5
	case SectionSemantic:
		return 6
	case SectionContent:
		return 7
	case SectionUnhashed:
		return 8
	default:
		return -1
	}
}

// FrameSize returns the size of the frame before a section of type t:
// [ChunkFrameSize] for CONTENT, otherwise [FrameSize]. For a type that is
// not [SectionType.Known] the result means nothing: no frame length is
// defined for it, and a section of that type cannot be skipped.
func (t SectionType) FrameSize() int {
	if t == SectionContent {
		return ChunkFrameSize
	}
	return FrameSize
}

// String returns t's name, such as "HEADER", or "SectionType(0xNN)" for an
// unknown type.
func (t SectionType) String() string {
	switch t {
	case SectionHeader:
		return "HEADER"
	case SectionDeps:
		return "DEPS"
	case SectionProvenance:
		return "PROVENANCE"
	case SectionSignature:
		return "SIGNATURE"
	case SectionGraph:
		return "GRAPH"
	case SectionSpan:
		return "SPAN"
	case SectionSemantic:
		return "SEMANTIC"
	case SectionContent:
		return "CONTENT"
	case SectionUnhashed:
		return "UNHASHED"
	default:
		return fmt.Sprintf("SectionType(0x%02x)", uint8(t))
	}
}

// ChunkInfo is what a CONTENT section's frame says about its chunk.
// Atomic's reader checks none of it.
type ChunkInfo struct {
	// Index is the chunk's number, counting from 0.
	Index uint32

	// Hash is the BLAKE3 hash of the uncompressed chunk.
	Hash ident.Hash

	// UncompressedLen is the uncompressed chunk's length.
	UncompressedLen uint32
}

// Section is one framed section of a change file.
type Section struct {
	// Type is the section's type.
	Type SectionType

	// Offset is the byte offset in the file of the frame's first byte.
	Offset int64

	// Frame is the section's frame as it is in the file: [FrameSize]
	// bytes, or [ChunkFrameSize] for CONTENT.
	Frame []byte

	// Compressed is the section's payload as it is in the file, the
	// compressed length the frame gives.
	Compressed []byte

	// Payload is Compressed decompressed, or nil when it has not been.
	Payload []byte

	// Chunk is the chunk a CONTENT frame describes. It is the zero value
	// for every other type.
	Chunk ChunkInfo
}

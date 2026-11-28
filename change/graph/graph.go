// Package graph reads and writes the GRAPH section of an Atomic change: the
// graph operations (hunks) that add, delete, move and edit files, each in
// the compact form that names other changes by their index in the file's
// hash dedup table. A GRAPH payload is one [Payload].
//
// [Decode] reads a payload and [AppendPayload] writes one. Every field is
// kept as written, the raw [EdgeFlags] bytes included, so a payload that
// decodes is written back byte for byte, provided its varints are minimal,
// as every writer's are. Hash indices are kept as indices. [Resolve] turns
// one into the change it names in the file's hash table, [Check] runs the
// checks Atomic makes once an op's indices are resolved, and [Indices] and
// [Remap] list and renumber an op's indices for writing it against a new
// table.
//
// The payload is documented in
// docs/remote/change-format/graph.md#graph-section.
package graph

import (
	"fmt"

	"github.com/z5labs/atomic-go/changefile"
)

// Payload is a change's GRAPH payload: Atomic's GraphSectionPayload
// (docs/remote/change-format/graph.md#graphsectionpayload). The fields are
// in wire order.
type Payload struct {
	// Path is the file the doc comment says the section covers. Atomic
	// writes "" and its reader ignores it; each op carries its own path.
	Path string

	// Ops is the change's hunks, in order. A payload with none holds nil.
	Ops []Op

	// ContentStart is ignored by Atomic's reader. Atomic writes 0.
	ContentStart uint64

	// ContentEnd is ignored by Atomic's reader. Atomic writes the length
	// of the change's whole content blob.
	ContentEnd uint64
}

// OpVariant is the variant index of a CompactGraphOp
// (docs/remote/change-format/graph.md#compactgraphop).
type OpVariant uint32

// The CompactGraphOp variants a default build of Atomic declares, in
// declaration order. Index 17, PrototypeContext, exists only in an
// experimental build and is not named here.
const (
	OpFileAdd OpVariant = iota
	OpDirAdd
	OpDirDel
	OpDirUndel
	OpFileDel
	OpFileUndel
	OpFileMove
	OpEdit
	OpReplacement
	OpSolveNameConflict
	OpUnsolveNameConflict
	OpSolveOrderConflict
	OpUnsolveOrderConflict
	OpResurrectZombies
	OpAddRoot
	OpDelRoot
	OpSetAttr
)

// String returns the variant's name as Atomic spells it, or OpVariant(n)
// for a value with no name.
func (v OpVariant) String() string {
	switch v {
	case OpFileAdd:
		return "FileAdd"
	case OpDirAdd:
		return "DirAdd"
	case OpDirDel:
		return "DirDel"
	case OpDirUndel:
		return "DirUndel"
	case OpFileDel:
		return "FileDel"
	case OpFileUndel:
		return "FileUndel"
	case OpFileMove:
		return "FileMove"
	case OpEdit:
		return "Edit"
	case OpReplacement:
		return "Replacement"
	case OpSolveNameConflict:
		return "SolveNameConflict"
	case OpUnsolveNameConflict:
		return "UnsolveNameConflict"
	case OpSolveOrderConflict:
		return "SolveOrderConflict"
	case OpUnsolveOrderConflict:
		return "UnsolveOrderConflict"
	case OpResurrectZombies:
		return "ResurrectZombies"
	case OpAddRoot:
		return "AddRoot"
	case OpDelRoot:
		return "DelRoot"
	case OpSetAttr:
		return "SetAttr"
	default:
		return fmt.Sprintf("OpVariant(%d)", uint32(v))
	}
}

// Op is one graph operation, a CompactGraphOp. The types in this package
// are the only ones that implement it.
type Op interface {
	// Variant returns the op's variant index.
	Variant() OpVariant

	// graphOp seals the interface.
	graphOp()
}

// FileAdd adds a file: variant 0.
type FileAdd struct {
	// AddName is the name vertex, holding the file name, under the parent
	// directory.
	AddName Insertion

	// AddInode is the empty inode vertex after the name.
	AddInode Insertion

	// Contents is the file's first content vertex, or nil for an empty
	// file.
	Contents *Insertion

	// Path is the file's path.
	Path string

	// Encoding is the content's text encoding, or nil.
	Encoding *Encoding
}

// DirAdd adds a directory: variant 1.
type DirAdd struct {
	// AddName is the name vertex under the parent directory.
	AddName Insertion

	// AddInode is the empty inode vertex after the name.
	AddInode Insertion

	// Path is the directory's path.
	Path string
}

// DirDel deletes a directory: variant 2.
type DirDel struct {
	// Del is the edges deleted.
	Del EdgeUpdate

	// Path is the directory's path.
	Path string
}

// DirUndel undeletes a directory: variant 3.
type DirUndel struct {
	// Undel is the edges restored.
	Undel EdgeUpdate

	// Path is the directory's path.
	Path string
}

// FileDel deletes a file: variant 4.
type FileDel struct {
	// Del is the name and inode edges deleted.
	Del EdgeUpdate

	// Contents is the content edges deleted, or nil.
	Contents *EdgeUpdate

	// Path is the file's path.
	Path string

	// Encoding is the content's text encoding, or nil.
	Encoding *Encoding
}

// FileUndel undeletes a file: variant 5.
type FileUndel struct {
	// Undel is the name and inode edges restored.
	Undel EdgeUpdate

	// Contents is the content edges restored, or nil.
	Contents *EdgeUpdate

	// Path is the file's path.
	Path string

	// Encoding is the content's text encoding, or nil.
	Encoding *Encoding
}

// FileMove moves or renames a file: variant 6.
type FileMove struct {
	// Del is the old name edges deleted.
	Del EdgeUpdate

	// Add is the new name vertex.
	Add Insertion

	// Path is the new path.
	Path string
}

// Edit inserts content into a file or changes the flags of its edges:
// variant 7.
type Edit struct {
	// Change is the edit itself, an [Insertion] or an [EdgeUpdate].
	Change Atom

	// Local is where the edit is, for display.
	Local Local

	// Encoding is the content's text encoding, or nil.
	Encoding *Encoding
}

// Replacement replaces content: it deletes some edges and inserts new
// content in their place: variant 8.
type Replacement struct {
	// Change is the edges deleted.
	Change EdgeUpdate

	// Replacement is the content inserted.
	Replacement Insertion

	// Local is where the replacement is, for display.
	Local Local

	// Encoding is the content's text encoding, or nil.
	Encoding *Encoding
}

// SolveNameConflict resolves a name conflict by deleting the losing name
// edges: variant 9. Every edge goes from Folder|Block to
// Folder|Block|Deleted ([Decode] and [AppendPayload] check this).
type SolveNameConflict struct {
	// Name is the losing name edges deleted. A version 1 file may hold
	// none, as a legacy marker.
	Name EdgeUpdate

	// Path is the surviving path. It must not be empty.
	Path string
}

// UnsolveNameConflict undoes a [SolveNameConflict] by restoring the name
// edges it deleted: variant 10. Every edge goes from Folder|Block|Deleted
// to Folder|Block.
type UnsolveNameConflict struct {
	// Name is the name edges restored. A version 1 file may hold none, as
	// a legacy marker.
	Name EdgeUpdate

	// Path is the surviving path. It must not be empty.
	Path string
}

// SolveOrderConflict resolves an order conflict between content vertices:
// variant 11.
type SolveOrderConflict struct {
	// Change is the edge changes that order the content.
	Change EdgeUpdate

	// Local is where the conflict is, for display.
	Local Local
}

// UnsolveOrderConflict undoes a [SolveOrderConflict]: variant 12.
type UnsolveOrderConflict struct {
	// Change is the edge changes undone.
	Change EdgeUpdate

	// Local is where the conflict is, for display.
	Local Local
}

// ResurrectZombies restores deleted content that other changes still
// depend on: variant 13.
type ResurrectZombies struct {
	// Change is the edges restored.
	Change EdgeUpdate

	// Local is where the content is, for display.
	Local Local

	// Encoding is the content's text encoding, or nil.
	Encoding *Encoding
}

// AddRoot adds a root vertex: variant 14. It has no path.
type AddRoot struct {
	// Name is the root's name vertex.
	Name Insertion

	// Inode is the root's inode vertex.
	Inode Insertion
}

// DelRoot deletes a root vertex: variant 15. It has no path.
type DelRoot struct {
	// Name is the name edges deleted.
	Name EdgeUpdate

	// Inode is the inode edges deleted.
	Inode EdgeUpdate
}

// SetAttr sets an attribute of a file: variant 16.
type SetAttr struct {
	// Inode is the file's inode vertex.
	Inode Position

	// Path is the file's path. It must not be empty.
	Path string

	// Value is the attribute set: a [Mode] of at most 0o777, or one of the
	// three [InodeKind] values.
	Value InodeAttr
}

// Variant returns [OpFileAdd].
func (FileAdd) Variant() OpVariant { return OpFileAdd }

// Variant returns [OpDirAdd].
func (DirAdd) Variant() OpVariant { return OpDirAdd }

// Variant returns [OpDirDel].
func (DirDel) Variant() OpVariant { return OpDirDel }

// Variant returns [OpDirUndel].
func (DirUndel) Variant() OpVariant { return OpDirUndel }

// Variant returns [OpFileDel].
func (FileDel) Variant() OpVariant { return OpFileDel }

// Variant returns [OpFileUndel].
func (FileUndel) Variant() OpVariant { return OpFileUndel }

// Variant returns [OpFileMove].
func (FileMove) Variant() OpVariant { return OpFileMove }

// Variant returns [OpEdit].
func (Edit) Variant() OpVariant { return OpEdit }

// Variant returns [OpReplacement].
func (Replacement) Variant() OpVariant { return OpReplacement }

// Variant returns [OpSolveNameConflict].
func (SolveNameConflict) Variant() OpVariant { return OpSolveNameConflict }

// Variant returns [OpUnsolveNameConflict].
func (UnsolveNameConflict) Variant() OpVariant { return OpUnsolveNameConflict }

// Variant returns [OpSolveOrderConflict].
func (SolveOrderConflict) Variant() OpVariant { return OpSolveOrderConflict }

// Variant returns [OpUnsolveOrderConflict].
func (UnsolveOrderConflict) Variant() OpVariant { return OpUnsolveOrderConflict }

// Variant returns [OpResurrectZombies].
func (ResurrectZombies) Variant() OpVariant { return OpResurrectZombies }

// Variant returns [OpAddRoot].
func (AddRoot) Variant() OpVariant { return OpAddRoot }

// Variant returns [OpDelRoot].
func (DelRoot) Variant() OpVariant { return OpDelRoot }

// Variant returns [OpSetAttr].
func (SetAttr) Variant() OpVariant { return OpSetAttr }

func (FileAdd) graphOp()              {}
func (DirAdd) graphOp()               {}
func (DirDel) graphOp()               {}
func (DirUndel) graphOp()             {}
func (FileDel) graphOp()              {}
func (FileUndel) graphOp()            {}
func (FileMove) graphOp()             {}
func (Edit) graphOp()                 {}
func (Replacement) graphOp()          {}
func (SolveNameConflict) graphOp()    {}
func (UnsolveNameConflict) graphOp()  {}
func (SolveOrderConflict) graphOp()   {}
func (UnsolveOrderConflict) graphOp() {}
func (ResurrectZombies) graphOp()     {}
func (AddRoot) graphOp()              {}
func (DelRoot) graphOp()              {}
func (SetAttr) graphOp()              {}

// Position is a byte offset in some change's content: a CompactPosition
// (docs/remote/change-format/graph.md#compactposition).
type Position struct {
	// Change is the change the offset is in, as a hash index.
	// [changefile.ThisChange] is the change the file holds.
	Change changefile.HashIndex

	// Pos is the byte offset in that change's content blob.
	Pos uint32
}

// Node is a vertex, a byte range in some change's content: a
// CompactGraphNode (docs/remote/change-format/graph.md#compactgraphnode).
type Node struct {
	// Change is the change whose content holds the vertex, as a hash
	// index.
	Change changefile.HashIndex

	// Start and End are the range's inclusive start and exclusive end.
	Start, End uint32
}

// Atom is new content or a set of edge changes: a CompactAtom
// (docs/remote/change-format/graph.md#compactatom). [Insertion] is
// variant 0 and [EdgeUpdate] variant 1; no other type implements it.
type Atom interface {
	// atom seals the interface.
	atom()
}

// Insertion is a new vertex, a byte range of this change's content joined
// to the graph after its predecessors and before its successors: a
// CompactInsertion (docs/remote/change-format/graph.md#compactinsertion).
type Insertion struct {
	// Predecessors are the positions before the new content. An insertion
	// with none holds nil.
	Predecessors []Position

	// Successors are the positions after it. An insertion with none holds
	// nil.
	Successors []Position

	// Flag is the flags of the new edges, kept raw.
	Flag EdgeFlags

	// Start and End are the vertex's inclusive start and exclusive end in
	// this change's content blob. Start == End is an empty vertex.
	Start, End uint32

	// Inode is the file the new vertex belongs to.
	Inode Position
}

// EdgeUpdate is a list of edge changes in one file: a CompactEdgeUpdate
// (docs/remote/change-format/graph.md#compactedgeupdate).
type EdgeUpdate struct {
	// Edges is the edge changes. An update with none holds nil.
	Edges []NewEdge

	// Inode is the file the edges belong to.
	Inode Position
}

func (Insertion) atom()  {}
func (EdgeUpdate) atom() {}

// NewEdge is one edge whose flags change from Previous to Flag: a
// CompactNewEdge (docs/remote/change-format/graph.md#compactnewedge).
type NewEdge struct {
	// Previous is the flags the edge has now, kept raw.
	Previous EdgeFlags

	// Flag is the flags it has after the change, kept raw.
	Flag EdgeFlags

	// From is the edge's source position.
	From Position

	// To is the edge's destination vertex.
	To Node

	// IntroducedBy is the change that introduced the edge, as a hash
	// index.
	IntroducedBy changefile.HashIndex
}

// EdgeFlags is the flags byte of an edge
// (docs/remote/change-format/graph.md#edgeflags). Decoding keeps it raw,
// bits Atomic does not define included, so that it is written back as it
// was; [EdgeFlags.Known] drops the undefined bits, as Atomic's reader does.
type EdgeFlags uint8

// The defined edge flag bits.
const (
	// Block connects sequential content within a file.
	Block EdgeFlags = 0x01

	// Pseudo is computed during apply and not explicitly stored in
	// changes.
	Pseudo EdgeFlags = 0x04

	// Folder is file-system hierarchy.
	Folder EdgeFlags = 0x10

	// Parent is the reverse direction of another edge.
	Parent EdgeFlags = 0x20

	// Deleted marks content that is removed but stays in the graph.
	Deleted EdgeFlags = 0x80
)

// knownFlags is every defined bit.
const knownFlags = Block | Pseudo | Folder | Parent | Deleted

// Known returns f with every bit Atomic does not define cleared, as
// Atomic's from_bits_truncate reads it.
func (f EdgeFlags) Known() EdgeFlags {
	return f & knownFlags
}

// Local is where an edit is, for display: a path and a line
// (docs/remote/change-format/graph.md#local). It is part of the hashed
// GRAPH payload.
type Local struct {
	// Path is the file path relative to the repository root.
	Path string

	// Line is the 1-based line after which content is inserted, or the
	// first line deleted.
	Line uint64
}

// Encoding is the text encoding of a file's content
// (docs/remote/change-format/graph.md#encoding). The values are the
// variant indices.
type Encoding uint8

const (
	// Utf8 is UTF-8.
	Utf8 Encoding = iota

	// Utf16Le is UTF-16, little-endian.
	Utf16Le

	// Utf16Be is UTF-16, big-endian.
	Utf16Be

	// Latin1 is ISO-8859-1.
	Latin1

	// Binary is not text.
	Binary
)

// encodingVariants is the number of variants Encoding declares.
const encodingVariants = 5

// String returns the encoding's name as Atomic spells it, or Encoding(n)
// for a value with no name.
func (e Encoding) String() string {
	switch e {
	case Utf8:
		return "Utf8"
	case Utf16Le:
		return "Utf16Le"
	case Utf16Be:
		return "Utf16Be"
	case Latin1:
		return "Latin1"
	case Binary:
		return "Binary"
	default:
		return fmt.Sprintf("Encoding(%d)", uint8(e))
	}
}

// InodeKind is the kind of a non-directory inode
// (docs/remote/change-format/graph.md#inodekind). The values are the
// variant indices. As an [InodeAttr] it is the Kind variant.
type InodeKind uint8

const (
	// Regular is a regular file.
	Regular InodeKind = iota

	// Symlink is a symbolic link; the target is the file content.
	Symlink

	// Gitlink is a Git submodule link; the object ID is the file content.
	Gitlink
)

// String returns the kind's name as Atomic spells it, or InodeKind(n) for
// a value with no name.
func (k InodeKind) String() string {
	switch k {
	case Regular:
		return "Regular"
	case Symlink:
		return "Symlink"
	case Gitlink:
		return "Gitlink"
	default:
		return fmt.Sprintf("InodeKind(%d)", uint8(k))
	}
}

// inodeKindVariants is the number of variants InodeKind declares.
const inodeKindVariants = 3

// Mode is Unix permission bits. As an [InodeAttr] it is the Mode variant.
// Atomic accepts 0o000 to [MaxMode].
type Mode uint16

// MaxMode is the largest [Mode] Atomic accepts.
const MaxMode Mode = 0o777

// InodeAttr is a file attribute value, set by a SetAttr op
// (docs/remote/change-format/graph.md#inodeattr). [Mode] is variant 0 and
// [InodeKind] variant 1; no other type implements it.
type InodeAttr interface {
	// inodeAttr seals the interface.
	inodeAttr()
}

func (Mode) inodeAttr()      {}
func (InodeKind) inodeAttr() {}

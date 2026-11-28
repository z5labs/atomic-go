// Package semantic reads and writes the SEMANTIC section of an Atomic
// change: the per-file trunk, branch and leaf operations that describe what
// a change did to each file's structure, line by line and token by token.
// Review tools read them to show a change without replaying its graph.
//
// A SEMANTIC payload is a list of [FileOps], one per file the change
// touches. [Decode] reads a payload and [AppendPayload] writes one. Unlike
// the GRAPH section, the SEMANTIC section names nodes by [NodeID], not by
// hash index; 0 is the change itself.
//
// The sections module of Atomic also defines a SemanticSectionPayload, a
// path, one FileOps and a content range, but no writer writes it and the
// reader never reads it, so this package does not have one. A payload in
// that shape decodes as a list of FileOps, the path's length read as the
// count, and [AppendPayload] never writes one.
//
// The payload is documented in
// docs/remote/change-format/graph.md#semantic-section.
package semantic

import (
	"fmt"

	"github.com/z5labs/atomic-go/change/graph"
	"github.com/z5labs/atomic-go/change/span"
)

// NodeID names a node in a semantic identity: Atomic's NodeId
// (docs/remote/change-format/graph.md#node-ids-in-semantic-identities).
// [ThisChange], 0, names the change that holds the operation; any other
// value names an existing node, in the recording repository's numbering.
type NodeID uint64

// ThisChange is the NodeID of the change that holds the operation: the
// placeholder Atomic gives every ID a change creates until it is applied.
const ThisChange NodeID = 0

// IsThisChange reports whether id is [ThisChange].
func (id NodeID) IsThisChange() bool {
	return id == ThisChange
}

// TrunkID is a file's identity: the node that created it and the file's
// index within that node
// (docs/remote/change-format/graph.md#trunkid).
type TrunkID struct {
	// Change is the node that created the file.
	Change NodeID

	// FileIdx is the file's index within that node.
	FileIdx uint32
}

// BranchID is a line's identity
// (docs/remote/change-format/graph.md#branchid).
type BranchID struct {
	// Change is the node that created the line.
	Change NodeID

	// BranchIdx is the line's index within that node.
	BranchIdx uint32
}

// LeafID is a token's identity
// (docs/remote/change-format/graph.md#leafid).
type LeafID struct {
	// Change is the node that created the token.
	Change NodeID

	// LeafIdx is the token's index within that node.
	LeafIdx uint32
}

// FileOps is the operations a change makes to one file
// (docs/remote/change-format/graph.md#fileops). The fields are in wire
// order.
type FileOps struct {
	// Trunk is the file's identity. A [ProjectedSpanEdit] has {0, 0}.
	Trunk TrunkID

	// Path is the file's path.
	Path string

	// TrunkOp is the file-level operation: a create, delete, move,
	// undelete, attribute change or projected span edit. It is nil, None
	// on the wire, when only the file's content changes.
	TrunkOp TrunkOp

	// LineOps is the line operations in the file. A file with none holds
	// nil.
	LineOps []LineOps
}

// TrunkOp is a file-level operation
// (docs/remote/change-format/graph.md#trunkop). The variants, in index
// order, are [TrunkCreate], [TrunkDelete], [TrunkMove], [TrunkUndelete],
// [TrunkSetMode], [TrunkSetKind] and [ProjectedSpanEdit]; no other type
// implements it.
type TrunkOp interface {
	// trunkOp seals the interface.
	trunkOp()
}

// TrunkCreate creates a file. It is TrunkOp variant 0.
type TrunkCreate struct {
	// Path is the new file's path.
	Path string

	// Encoding is the content's text encoding, or nil for binary or
	// unknown content.
	Encoding *graph.Encoding
}

// TrunkDelete deletes a file. It is TrunkOp variant 1.
type TrunkDelete struct {
	// Trunk is the file deleted.
	Trunk TrunkID
}

// TrunkMove moves a file. It is TrunkOp variant 2.
type TrunkMove struct {
	// Trunk is the file moved.
	Trunk TrunkID

	// NewPath is the file's new path.
	NewPath string
}

// TrunkUndelete brings back a deleted file. It is TrunkOp variant 3.
type TrunkUndelete struct {
	// Trunk is the file brought back.
	Trunk TrunkID
}

// TrunkSetMode sets a file's Unix permission bits. It is TrunkOp variant
// 4.
type TrunkSetMode struct {
	// Trunk is the file changed.
	Trunk TrunkID

	// Mode is the new permission bits, a u16 on the wire. It is kept as
	// written: nothing here, and nothing the documentation shows in
	// Atomic, checks it against [graph.MaxMode].
	Mode graph.Mode
}

// TrunkSetKind sets a file's inode kind. It is TrunkOp variant 5.
type TrunkSetKind struct {
	// Trunk is the file changed.
	Trunk TrunkID

	// Kind is the new kind.
	Kind graph.InodeKind
}

// ProjectedSpanEdit is an edit to a file whose history is held as spans,
// in the SPAN section, rather than as a trunk. It is TrunkOp variant 6.
// Atomic gives its [FileOps] the Trunk {0, 0}.
type ProjectedSpanEdit struct {
	// File is the file edited.
	File span.ExternalFile

	// Baseline is the semantic snapshot the edit is based on, which must
	// be in the edit's dependency closure.
	Baseline span.ProjectionID
}

func (TrunkCreate) trunkOp()       {}
func (TrunkDelete) trunkOp()       {}
func (TrunkMove) trunkOp()         {}
func (TrunkUndelete) trunkOp()     {}
func (TrunkSetMode) trunkOp()      {}
func (TrunkSetKind) trunkOp()      {}
func (ProjectedSpanEdit) trunkOp() {}

// LineOps is an operation on one line
// (docs/remote/change-format/graph.md#lineops). The fields are in wire
// order, and every one is written, the optional ones as None.
type LineOps struct {
	// Branch is the line's identity.
	Branch BranchID

	// Op is what happens to the line. It is required: a nil Op cannot be
	// written.
	Op BranchOp

	// OldLine is the line's number in the old file, or nil for a pure
	// insertion.
	OldLine *uint64

	// NewLine is the line's number in the new file, or nil for a pure
	// deletion.
	NewLine *uint64

	// ContentRange is the range of this change's content blob that holds
	// the line's graph vertex, or nil. Atomic sets it for inserts.
	ContentRange *Range
}

// Range is a range of offsets into this change's content blob: an
// inclusive start and an exclusive end.
type Range struct {
	Start, End uint64
}

// BranchOp is a line-level operation
// (docs/remote/change-format/graph.md#branchop). The variants, in index
// order, are [BranchInsert], [BranchDelete], [BranchModify],
// [BranchRestore] and [BranchReparent]; no other type implements it.
type BranchOp interface {
	// branchOp seals the interface.
	branchOp()
}

// BranchInsert inserts a new line. It is BranchOp variant 0.
type BranchInsert struct {
	// After is the line the new one follows, or nil for the start of the
	// file.
	After *BranchID

	// Content is the new line's tokens. A line with none holds nil.
	Content []LeafOp
}

// BranchDelete deletes a line. It is BranchOp variant 1.
type BranchDelete struct {
	// Branch is the line deleted.
	Branch BranchID

	// Content is the line's old tokens, kept for display. A line with
	// none holds nil.
	Content []LeafOp
}

// BranchModify replaces a line's content, keeping its identity. It is
// BranchOp variant 2.
type BranchModify struct {
	// Branch is the line changed.
	Branch BranchID

	// OldContent and NewContent are the line's tokens before and after.
	// Either holds nil when it has none.
	OldContent, NewContent []LeafOp
}

// BranchRestore brings back a deleted line. It is BranchOp variant 3.
type BranchRestore struct {
	// Branch is the line brought back.
	Branch BranchID
}

// BranchReparent moves a line. It is BranchOp variant 4.
type BranchReparent struct {
	// Branch is the line moved.
	Branch BranchID

	// NewAfter is the line it now follows, or nil for the start of the
	// file.
	NewAfter *BranchID
}

func (BranchInsert) branchOp()   {}
func (BranchDelete) branchOp()   {}
func (BranchModify) branchOp()   {}
func (BranchRestore) branchOp()  {}
func (BranchReparent) branchOp() {}

// LeafOp is a token-level operation within a line
// (docs/remote/change-format/graph.md#leafop). The variants, in index
// order, are [LeafInsert], [LeafDelete], [LeafReplace] and [LeafRestore];
// no other type implements it.
type LeafOp interface {
	// leafOp seals the interface.
	leafOp()
}

// LeafInsert inserts a new token. It is LeafOp variant 0.
type LeafInsert struct {
	// After is the token the new one follows, or nil for the start of the
	// line.
	After *LeafID

	// Kind is the token's category.
	Kind TokenKind

	// Content is the token's bytes. An empty token holds nil.
	Content []byte
}

// LeafDelete deletes a token. It is LeafOp variant 1.
type LeafDelete struct {
	// Leaf is the token deleted.
	Leaf LeafID
}

// LeafReplace replaces a token's bytes, keeping its identity. It is LeafOp
// variant 2.
type LeafReplace struct {
	// Leaf is the token changed.
	Leaf LeafID

	// NewContent is the token's new bytes. An empty token holds nil.
	NewContent []byte
}

// LeafRestore brings back a deleted token. It is LeafOp variant 3.
type LeafRestore struct {
	// Leaf is the token brought back.
	Leaf LeafID
}

func (LeafInsert) leafOp()  {}
func (LeafDelete) leafOp()  {}
func (LeafReplace) leafOp() {}
func (LeafRestore) leafOp() {}

// TokenKind is the category of a token
// (docs/remote/change-format/graph.md#tokenkind). The values are the
// variant indices.
type TokenKind uint8

const (
	// Word is an identifier or keyword.
	Word TokenKind = iota

	// Whitespace is spaces and tabs, not newlines.
	Whitespace

	// Operator is an operator such as +, == or ->.
	Operator

	// Punctuation is structural punctuation such as ( or ;.
	Punctuation

	// String is a string literal with its delimiters.
	String

	// Number is a numeric literal.
	Number

	// Comment is a // comment.
	Comment

	// Newline is \n or \r\n.
	Newline

	// Other is anything else.
	Other
)

// tokenKindVariants is the number of variants TokenKind declares.
const tokenKindVariants = 9

// String returns the kind's name as Atomic spells it, or TokenKind(n) for
// a value with no name.
func (k TokenKind) String() string {
	switch k {
	case Word:
		return "Word"
	case Whitespace:
		return "Whitespace"
	case Operator:
		return "Operator"
	case Punctuation:
		return "Punctuation"
	case String:
		return "String"
	case Number:
		return "Number"
	case Comment:
		return "Comment"
	case Newline:
		return "Newline"
	case Other:
		return "Other"
	default:
		return fmt.Sprintf("TokenKind(%d)", uint8(k))
	}
}

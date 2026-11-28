package semantic

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/change/graph"
	"github.com/z5labs/atomic-go/change/span"
	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/internal/postcard"
)

// Variant counts, in declaration order. Encoding and InodeKind are the
// graph package's; their last declared values bound them.
const (
	trunkOpVariants   = 7
	branchOpVariants  = 5
	leafOpVariants    = 4
	encodingVariants  = uint32(graph.Binary) + 1
	inodeKindVariants = uint32(graph.Gitlink) + 1
)

// TrunkOp variant indices.
const (
	variantTrunkCreate uint32 = iota
	variantTrunkDelete
	variantTrunkMove
	variantTrunkUndelete
	variantTrunkSetMode
	variantTrunkSetKind
	variantProjectedSpanEdit
)

// BranchOp variant indices.
const (
	variantBranchInsert uint32 = iota
	variantBranchDelete
	variantBranchModify
	variantBranchRestore
	variantBranchReparent
)

// LeafOp variant indices.
const (
	variantLeafInsert uint32 = iota
	variantLeafDelete
	variantLeafReplace
	variantLeafRestore
)

// Decode decodes the SEMANTIC payload b, a postcard Vec<FileOps>. Like
// postcard::from_bytes, which Atomic reads the section with, it ignores
// bytes left over after the list
// (docs/remote/change-format/graph.md#reading). So a payload in the shape
// of a SemanticSectionPayload with an empty path decodes as an empty list,
// as it does in Atomic. Every field is kept as written. An empty list, and
// every empty list or byte string inside it, decodes to nil, and nothing in
// the result aliases b.
//
// Every failure is a *[changefile.FormatError] of kind
// [changefile.ErrPayload] in [changefile.SectionSemantic], with no offset,
// wrapping the postcard failure, which carries the byte offset in b at
// which the failed value begins: the payload ends early, holds a path that
// is not UTF-8 or an option tag other than 0 or 1, or names a TrunkOp,
// BranchOp, LeafOp, [TokenKind], [graph.Encoding] or [graph.InodeKind]
// variant beyond the ones declared.
func Decode(b []byte) ([]FileOps, error) {
	ops, err := postcard.Decode(b, decodeList)
	if err != nil {
		return nil, formatError("Vec<FileOps>", err)
	}
	return ops, nil
}

func decodeList(d *postcard.Decoder) []FileOps {
	return nilIfEmpty(postcard.Seq(d, decodeFileOps))
}

func decodeFileOps(d *postcard.Decoder) FileOps {
	return FileOps{
		Trunk:   decodeTrunkID(d),
		Path:    d.Str(),
		TrunkOp: decodeOptionalTrunkOp(d),
		LineOps: nilIfEmpty(postcard.Seq(d, decodeLineOps)),
	}
}

func decodeTrunkID(d *postcard.Decoder) TrunkID {
	return TrunkID{Change: NodeID(d.U64()), FileIdx: d.U32()}
}

func decodeBranchID(d *postcard.Decoder) BranchID {
	return BranchID{Change: NodeID(d.U64()), BranchIdx: d.U32()}
}

func decodeLeafID(d *postcard.Decoder) LeafID {
	return LeafID{Change: NodeID(d.U64()), LeafIdx: d.U32()}
}

// decodeOptionalTrunkOp decodes an Option<TrunkOp>. It returns nil for
// None, and on failure.
func decodeOptionalTrunkOp(d *postcard.Decoder) TrunkOp {
	if !d.OptionTag() {
		return nil
	}
	return decodeTrunkOp(d)
}

// decodeTrunkOp decodes one TrunkOp. It returns nil when the variant index
// fails to decode; the Decoder then holds the failure.
func decodeTrunkOp(d *postcard.Decoder) TrunkOp {
	v := d.Variant(trunkOpVariants)
	if d.Err() != nil {
		return nil
	}
	switch v {
	case variantTrunkCreate:
		return TrunkCreate{
			Path:     d.Str(),
			Encoding: postcard.Option(d, decodeEncoding),
		}
	case variantTrunkDelete:
		return TrunkDelete{Trunk: decodeTrunkID(d)}
	case variantTrunkMove:
		return TrunkMove{Trunk: decodeTrunkID(d), NewPath: d.Str()}
	case variantTrunkUndelete:
		return TrunkUndelete{Trunk: decodeTrunkID(d)}
	case variantTrunkSetMode:
		return TrunkSetMode{Trunk: decodeTrunkID(d), Mode: graph.Mode(d.U16())}
	case variantTrunkSetKind:
		return TrunkSetKind{Trunk: decodeTrunkID(d), Kind: graph.InodeKind(d.Variant(inodeKindVariants))}
	default:
		return ProjectedSpanEdit{
			File: span.ExternalFile{
				Owner:   d.Array32(),
				Ordinal: d.U32(),
			},
			Baseline: span.ProjectionID(d.Array32()),
		}
	}
}

func decodeEncoding(d *postcard.Decoder) graph.Encoding {
	return graph.Encoding(d.Variant(encodingVariants))
}

func decodeLineOps(d *postcard.Decoder) LineOps {
	return LineOps{
		Branch:       decodeBranchID(d),
		Op:           decodeBranchOp(d),
		OldLine:      postcard.Option(d, (*postcard.Decoder).U64),
		NewLine:      postcard.Option(d, (*postcard.Decoder).U64),
		ContentRange: postcard.Option(d, decodeRange),
	}
}

func decodeRange(d *postcard.Decoder) Range {
	return Range{Start: d.U64(), End: d.U64()}
}

// decodeBranchOp decodes one BranchOp. It returns nil when the variant
// index fails to decode.
func decodeBranchOp(d *postcard.Decoder) BranchOp {
	v := d.Variant(branchOpVariants)
	if d.Err() != nil {
		return nil
	}
	switch v {
	case variantBranchInsert:
		return BranchInsert{
			After:   postcard.Option(d, decodeBranchID),
			Content: decodeLeafOps(d),
		}
	case variantBranchDelete:
		return BranchDelete{
			Branch:  decodeBranchID(d),
			Content: decodeLeafOps(d),
		}
	case variantBranchModify:
		return BranchModify{
			Branch:     decodeBranchID(d),
			OldContent: decodeLeafOps(d),
			NewContent: decodeLeafOps(d),
		}
	case variantBranchRestore:
		return BranchRestore{Branch: decodeBranchID(d)}
	default:
		return BranchReparent{
			Branch:   decodeBranchID(d),
			NewAfter: postcard.Option(d, decodeBranchID),
		}
	}
}

func decodeLeafOps(d *postcard.Decoder) []LeafOp {
	return nilIfEmpty(postcard.Seq(d, decodeLeafOp))
}

// decodeLeafOp decodes one LeafOp. It returns nil when the variant index
// fails to decode.
func decodeLeafOp(d *postcard.Decoder) LeafOp {
	v := d.Variant(leafOpVariants)
	if d.Err() != nil {
		return nil
	}
	switch v {
	case variantLeafInsert:
		return LeafInsert{
			After:   postcard.Option(d, decodeLeafID),
			Kind:    TokenKind(d.Variant(tokenKindVariants)),
			Content: cloneBytes(d.ByteSeq()),
		}
	case variantLeafDelete:
		return LeafDelete{Leaf: decodeLeafID(d)}
	case variantLeafReplace:
		return LeafReplace{
			Leaf:       decodeLeafID(d),
			NewContent: cloneBytes(d.ByteSeq()),
		}
	default:
		return LeafRestore{Leaf: decodeLeafID(d)}
	}
}

// AppendPayload appends the SEMANTIC payload of ops to dst: a postcard
// Vec<FileOps>, every field in wire order and every varint minimal. A list
// that [Decode] returned is written back to the bytes it came from,
// provided they were minimal and nothing trailed them. It never writes a
// SemanticSectionPayload.
//
// It checks ops first, and on failure returns dst unchanged and a
// *[changefile.FormatError] of kind [changefile.ErrPayload] in
// [changefile.SectionSemantic], with no offset, naming the FileOps, line
// and token at fault: when a path is not valid UTF-8; when a TrunkOp is
// not one of this package's TrunkOp values (a pointer to one is not);
// when a LineOps has a nil Op, or one that is not one of this package's
// BranchOp values; when a LeafOp is nil or not one of this package's
// LeafOp values; and when a [graph.Encoding], [graph.InodeKind] or
// [TokenKind] is beyond the ones Atomic declares. Node IDs are not
// checked.
func AppendPayload(dst []byte, ops []FileOps) ([]byte, error) {
	for i, f := range ops {
		if reason := fileOpsError(f); reason != "" {
			return dst, formatError(fmt.Sprintf("file ops %d: %s", i, reason), nil)
		}
	}
	e := postcard.NewEncoder(dst)
	postcard.PutSeq(e, ops, putFileOps)
	return e.Bytes(), nil
}

// fileOpsError returns why f cannot be written, or "" when it can.
func fileOpsError(f FileOps) string {
	if reason := pathError(f.Path); reason != "" {
		return reason
	}
	if reason := trunkOpError(f.TrunkOp); reason != "" {
		return reason
	}
	for i, l := range f.LineOps {
		if reason := branchOpError(l.Op); reason != "" {
			return fmt.Sprintf("line op %d: %s", i, reason)
		}
	}
	return ""
}

func trunkOpError(op TrunkOp) string {
	switch op := op.(type) {
	case nil, TrunkDelete, TrunkUndelete, TrunkSetMode, ProjectedSpanEdit:
		return ""
	case TrunkCreate:
		if reason := pathError(op.Path); reason != "" {
			return "trunk op: " + reason
		}
		if op.Encoding != nil && uint32(*op.Encoding) >= encodingVariants {
			return fmt.Sprintf("trunk op: encoding %s is not one Atomic declares", *op.Encoding)
		}
		return ""
	case TrunkMove:
		if reason := pathError(op.NewPath); reason != "" {
			return "trunk op: new " + reason
		}
		return ""
	case TrunkSetKind:
		if uint32(op.Kind) >= inodeKindVariants {
			return fmt.Sprintf("trunk op: inode kind %s is not one Atomic declares", op.Kind)
		}
		return ""
	default:
		return fmt.Sprintf("trunk op is a %T, not one of the semantic package's TrunkOp values", op)
	}
}

func branchOpError(op BranchOp) string {
	switch op := op.(type) {
	case BranchInsert:
		return leafOpsError("content", op.Content)
	case BranchDelete:
		return leafOpsError("content", op.Content)
	case BranchModify:
		if reason := leafOpsError("old content", op.OldContent); reason != "" {
			return reason
		}
		return leafOpsError("new content", op.NewContent)
	case BranchRestore, BranchReparent:
		return ""
	case nil:
		return "branch op is nil"
	default:
		return fmt.Sprintf("branch op is a %T, not one of the semantic package's BranchOp values", op)
	}
}

// leafOpsError returns why one of ops, the tokens in field, cannot be
// written, or "" when all of them can.
func leafOpsError(field string, ops []LeafOp) string {
	for i, op := range ops {
		var reason string
		switch op := op.(type) {
		case LeafInsert:
			if op.Kind >= tokenKindVariants {
				reason = fmt.Sprintf("token kind %s is not one Atomic declares", op.Kind)
			}
		case LeafDelete, LeafReplace, LeafRestore:
		case nil:
			reason = "leaf op is nil"
		default:
			reason = fmt.Sprintf("leaf op is a %T, not one of the semantic package's LeafOp values", op)
		}
		if reason != "" {
			return fmt.Sprintf("%s leaf op %d: %s", field, i, reason)
		}
	}
	return ""
}

func pathError(path string) string {
	if utf8.ValidString(path) {
		return ""
	}
	return fmt.Sprintf("path %q is not valid UTF-8", path)
}

// putFileOps writes f, which AppendPayload has checked.
func putFileOps(e *postcard.Encoder, f FileOps) {
	putTrunkID(e, f.Trunk)
	e.Str(f.Path)
	e.OptionTag(f.TrunkOp != nil)
	if f.TrunkOp != nil {
		putTrunkOp(e, f.TrunkOp)
	}
	postcard.PutSeq(e, f.LineOps, putLineOps)
}

func putTrunkID(e *postcard.Encoder, id TrunkID) {
	e.U64(uint64(id.Change))
	e.U32(id.FileIdx)
}

func putBranchID(e *postcard.Encoder, id BranchID) {
	e.U64(uint64(id.Change))
	e.U32(id.BranchIdx)
}

func putLeafID(e *postcard.Encoder, id LeafID) {
	e.U64(uint64(id.Change))
	e.U32(id.LeafIdx)
}

func putTrunkOp(e *postcard.Encoder, op TrunkOp) {
	switch op := op.(type) {
	case TrunkCreate:
		e.Variant(variantTrunkCreate)
		e.Str(op.Path)
		postcard.PutOption(e, op.Encoding, putEncoding)
	case TrunkDelete:
		e.Variant(variantTrunkDelete)
		putTrunkID(e, op.Trunk)
	case TrunkMove:
		e.Variant(variantTrunkMove)
		putTrunkID(e, op.Trunk)
		e.Str(op.NewPath)
	case TrunkUndelete:
		e.Variant(variantTrunkUndelete)
		putTrunkID(e, op.Trunk)
	case TrunkSetMode:
		e.Variant(variantTrunkSetMode)
		putTrunkID(e, op.Trunk)
		e.U16(uint16(op.Mode))
	case TrunkSetKind:
		e.Variant(variantTrunkSetKind)
		putTrunkID(e, op.Trunk)
		e.Variant(uint32(op.Kind))
	case ProjectedSpanEdit:
		e.Variant(variantProjectedSpanEdit)
		e.Array32(op.File.Owner)
		e.U32(op.File.Ordinal)
		e.Array32(op.Baseline)
	}
}

func putEncoding(e *postcard.Encoder, enc graph.Encoding) {
	e.Variant(uint32(enc))
}

func putLineOps(e *postcard.Encoder, l LineOps) {
	putBranchID(e, l.Branch)
	putBranchOp(e, l.Op)
	postcard.PutOption(e, l.OldLine, (*postcard.Encoder).U64)
	postcard.PutOption(e, l.NewLine, (*postcard.Encoder).U64)
	postcard.PutOption(e, l.ContentRange, putRange)
}

func putRange(e *postcard.Encoder, r Range) {
	e.U64(r.Start)
	e.U64(r.End)
}

func putBranchOp(e *postcard.Encoder, op BranchOp) {
	switch op := op.(type) {
	case BranchInsert:
		e.Variant(variantBranchInsert)
		postcard.PutOption(e, op.After, putBranchID)
		postcard.PutSeq(e, op.Content, putLeafOp)
	case BranchDelete:
		e.Variant(variantBranchDelete)
		putBranchID(e, op.Branch)
		postcard.PutSeq(e, op.Content, putLeafOp)
	case BranchModify:
		e.Variant(variantBranchModify)
		putBranchID(e, op.Branch)
		postcard.PutSeq(e, op.OldContent, putLeafOp)
		postcard.PutSeq(e, op.NewContent, putLeafOp)
	case BranchRestore:
		e.Variant(variantBranchRestore)
		putBranchID(e, op.Branch)
	case BranchReparent:
		e.Variant(variantBranchReparent)
		putBranchID(e, op.Branch)
		postcard.PutOption(e, op.NewAfter, putBranchID)
	}
}

func putLeafOp(e *postcard.Encoder, op LeafOp) {
	switch op := op.(type) {
	case LeafInsert:
		e.Variant(variantLeafInsert)
		postcard.PutOption(e, op.After, putLeafID)
		e.Variant(uint32(op.Kind))
		e.ByteSeq(op.Content)
	case LeafDelete:
		e.Variant(variantLeafDelete)
		putLeafID(e, op.Leaf)
	case LeafReplace:
		e.Variant(variantLeafReplace)
		putLeafID(e, op.Leaf)
		e.ByteSeq(op.NewContent)
	case LeafRestore:
		e.Variant(variantLeafRestore)
		putLeafID(e, op.Leaf)
	}
}

func formatError(detail string, err error) *changefile.FormatError {
	return &changefile.FormatError{Kind: changefile.ErrPayload, Section: changefile.SectionSemantic, Offset: -1, Detail: detail, Err: err}
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// cloneBytes copies b, returning nil when it is empty.
func cloneBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return bytes.Clone(b)
}

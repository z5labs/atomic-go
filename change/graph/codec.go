package graph

import (
	"fmt"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/internal/postcard"
)

// decodedVariants is the number of CompactGraphOp variants [Decode]
// accepts: FileAdd to Replacement. A higher index is an unknown variant.
const decodedVariants = uint32(OpReplacement) + 1

// atomVariants is the number of variants CompactAtom declares.
const atomVariants = 2

// Decode decodes the GRAPH payload b of a change file of schema version v.
// Like postcard::from_bytes, which Atomic reads the section with, it
// ignores bytes left over after the payload
// (docs/remote/change-format/graph.md#reading). Every field is kept as
// written, [EdgeFlags] bits Atomic does not define included. Empty op,
// position and edge lists decode to nil, and nothing in the result aliases
// b.
//
// Every failure is a *[changefile.FormatError] in
// [changefile.SectionGraph], with no offset. A version that is not
// [changefile.Version.Supported] is of kind
// [changefile.ErrUnsupportedVersion]. A payload that does not decode is of
// kind [changefile.ErrPayload] and wraps the postcard failure, which
// carries the byte offset in b at which the failed value begins: the
// payload ends early, holds a string that is not UTF-8 or an option tag
// other than 0 or 1, or names an op, atom or encoding variant beyond the
// ones declared. Ops are decoded up to variant 8, Replacement; a higher
// index is refused as an unknown variant ([postcard.ErrUnknownVariant]).
func Decode(b []byte, v changefile.Version) (Payload, error) {
	if !v.Supported() {
		return Payload{}, formatError(changefile.ErrUnsupportedVersion, fmt.Sprintf("schema version %d", uint32(v)), nil)
	}
	p, err := postcard.Decode(b, decodePayload)
	if err != nil {
		return Payload{}, formatError(changefile.ErrPayload, "GraphSectionPayload", err)
	}
	return p, nil
}

func decodePayload(d *postcard.Decoder) Payload {
	return Payload{
		Path:         d.Str(),
		Ops:          nilIfEmpty(postcard.Seq(d, decodeOp)),
		ContentStart: d.U64(),
		ContentEnd:   d.U64(),
	}
}

// decodeOp decodes one CompactGraphOp. It returns nil when the variant
// index fails to decode; the Decoder then holds the failure.
func decodeOp(d *postcard.Decoder) Op {
	v := d.Variant(decodedVariants)
	if d.Err() != nil {
		return nil
	}
	switch OpVariant(v) {
	case OpFileAdd:
		return FileAdd{
			AddName:  decodeInsertion(d),
			AddInode: decodeInsertion(d),
			Contents: postcard.Option(d, decodeInsertion),
			Path:     d.Str(),
			Encoding: postcard.Option(d, decodeEncoding),
		}
	case OpDirAdd:
		return DirAdd{
			AddName:  decodeInsertion(d),
			AddInode: decodeInsertion(d),
			Path:     d.Str(),
		}
	case OpDirDel:
		return DirDel{
			Del:  decodeEdgeUpdate(d),
			Path: d.Str(),
		}
	case OpDirUndel:
		return DirUndel{
			Undel: decodeEdgeUpdate(d),
			Path:  d.Str(),
		}
	case OpFileDel:
		return FileDel{
			Del:      decodeEdgeUpdate(d),
			Contents: postcard.Option(d, decodeEdgeUpdate),
			Path:     d.Str(),
			Encoding: postcard.Option(d, decodeEncoding),
		}
	case OpFileUndel:
		return FileUndel{
			Undel:    decodeEdgeUpdate(d),
			Contents: postcard.Option(d, decodeEdgeUpdate),
			Path:     d.Str(),
			Encoding: postcard.Option(d, decodeEncoding),
		}
	case OpFileMove:
		return FileMove{
			Del:  decodeEdgeUpdate(d),
			Add:  decodeInsertion(d),
			Path: d.Str(),
		}
	case OpEdit:
		return Edit{
			Change:   decodeAtom(d),
			Local:    decodeLocal(d),
			Encoding: postcard.Option(d, decodeEncoding),
		}
	case OpReplacement:
		return Replacement{
			Change:      decodeEdgeUpdate(d),
			Replacement: decodeInsertion(d),
			Local:       decodeLocal(d),
			Encoding:    postcard.Option(d, decodeEncoding),
		}
	default:
		// Unreachable while decodedVariants and the cases above agree; a
		// variant admitted without a case fails rather than being misread.
		d.Fail(postcard.ErrUnknownVariant)
		return nil
	}
}

func decodePosition(d *postcard.Decoder) Position {
	return Position{
		Change: changefile.HashIndex(d.U16()),
		Pos:    d.U32(),
	}
}

func decodeNode(d *postcard.Decoder) Node {
	return Node{
		Change: changefile.HashIndex(d.U16()),
		Start:  d.U32(),
		End:    d.U32(),
	}
}

func decodeInsertion(d *postcard.Decoder) Insertion {
	return Insertion{
		Predecessors: nilIfEmpty(postcard.Seq(d, decodePosition)),
		Successors:   nilIfEmpty(postcard.Seq(d, decodePosition)),
		Flag:         EdgeFlags(d.U8()),
		Start:        d.U32(),
		End:          d.U32(),
		Inode:        decodePosition(d),
	}
}

func decodeEdgeUpdate(d *postcard.Decoder) EdgeUpdate {
	return EdgeUpdate{
		Edges: nilIfEmpty(postcard.Seq(d, decodeNewEdge)),
		Inode: decodePosition(d),
	}
}

func decodeNewEdge(d *postcard.Decoder) NewEdge {
	return NewEdge{
		Previous:     EdgeFlags(d.U8()),
		Flag:         EdgeFlags(d.U8()),
		From:         decodePosition(d),
		To:           decodeNode(d),
		IntroducedBy: changefile.HashIndex(d.U16()),
	}
}

// decodeAtom decodes one CompactAtom. It returns nil when the variant
// index fails to decode.
func decodeAtom(d *postcard.Decoder) Atom {
	v := d.Variant(atomVariants)
	if d.Err() != nil {
		return nil
	}
	if v == 0 {
		return decodeInsertion(d)
	}
	return decodeEdgeUpdate(d)
}

func decodeLocal(d *postcard.Decoder) Local {
	return Local{
		Path: d.Str(),
		Line: d.U64(),
	}
}

func decodeEncoding(d *postcard.Decoder) Encoding {
	return Encoding(d.Variant(encodingVariants))
}

// AppendPayload appends the GRAPH payload of p to dst: p's fields in wire
// order, every varint minimal and every [EdgeFlags] byte as held. A
// [Payload] that [Decode] returned is written back to the bytes it came
// from, provided they were minimal.
//
// It checks p first, and on failure returns dst unchanged and a
// *[changefile.FormatError] in [changefile.SectionGraph], with no offset.
// A Path that is not valid UTF-8 is of kind [changefile.ErrPayload]. An op
// is of kind [changefile.ErrInvalidGraphOp], naming its index, when it is
// nil or not one of this package's op values (a pointer to one is not),
// when one of its paths is
// not valid UTF-8, when an [Edit]'s Change is nil or not an [Insertion] or
// an [EdgeUpdate] value, or when an Encoding is not one of the five
// declared. Hash indices and ranges are not checked.
func AppendPayload(dst []byte, p Payload) ([]byte, error) {
	if !utf8.ValidString(p.Path) {
		return dst, formatError(changefile.ErrPayload, fmt.Sprintf("path %q is not valid UTF-8", p.Path), nil)
	}
	for i, op := range p.Ops {
		if reason := opError(op); reason != "" {
			return dst, formatError(changefile.ErrInvalidGraphOp, fmt.Sprintf("op %d: %s", i, reason), nil)
		}
	}

	e := postcard.NewEncoder(dst)
	e.Str(p.Path)
	postcard.PutSeq(e, p.Ops, putOp)
	e.U64(p.ContentStart)
	e.U64(p.ContentEnd)
	return e.Bytes(), nil
}

// opError returns why op cannot be written, or "" when it can.
func opError(op Op) string {
	switch op := op.(type) {
	case FileAdd:
		return firstOf(pathError(op.Path), encodingError(op.Encoding))
	case DirAdd:
		return pathError(op.Path)
	case DirDel:
		return pathError(op.Path)
	case DirUndel:
		return pathError(op.Path)
	case FileDel:
		return firstOf(pathError(op.Path), encodingError(op.Encoding))
	case FileUndel:
		return firstOf(pathError(op.Path), encodingError(op.Encoding))
	case FileMove:
		return pathError(op.Path)
	case Edit:
		return firstOf(atomError(op.Change), pathError(op.Local.Path), encodingError(op.Encoding))
	case Replacement:
		return firstOf(pathError(op.Local.Path), encodingError(op.Encoding))
	case nil:
		return "op is nil"
	default:
		return fmt.Sprintf("op is a %T, not one of the graph package's op values", op)
	}
}

func atomError(a Atom) string {
	switch a.(type) {
	case Insertion, EdgeUpdate:
		return ""
	case nil:
		return "edit change is nil"
	default:
		return fmt.Sprintf("edit change is a %T, not a graph.Insertion or graph.EdgeUpdate", a)
	}
}

func pathError(path string) string {
	if utf8.ValidString(path) {
		return ""
	}
	return fmt.Sprintf("path %q is not valid UTF-8", path)
}

func encodingError(enc *Encoding) string {
	if enc == nil || *enc < encodingVariants {
		return ""
	}
	return fmt.Sprintf("encoding %s is not one Atomic declares", *enc)
}

func firstOf(reasons ...string) string {
	for _, r := range reasons {
		if r != "" {
			return r
		}
	}
	return ""
}

// putOp writes op, which AppendPayload has checked.
func putOp(e *postcard.Encoder, op Op) {
	e.Variant(uint32(op.Variant()))
	switch op := op.(type) {
	case FileAdd:
		putInsertion(e, op.AddName)
		putInsertion(e, op.AddInode)
		postcard.PutOption(e, op.Contents, putInsertion)
		e.Str(op.Path)
		postcard.PutOption(e, op.Encoding, putEncoding)
	case DirAdd:
		putInsertion(e, op.AddName)
		putInsertion(e, op.AddInode)
		e.Str(op.Path)
	case DirDel:
		putEdgeUpdate(e, op.Del)
		e.Str(op.Path)
	case DirUndel:
		putEdgeUpdate(e, op.Undel)
		e.Str(op.Path)
	case FileDel:
		putEdgeUpdate(e, op.Del)
		postcard.PutOption(e, op.Contents, putEdgeUpdate)
		e.Str(op.Path)
		postcard.PutOption(e, op.Encoding, putEncoding)
	case FileUndel:
		putEdgeUpdate(e, op.Undel)
		postcard.PutOption(e, op.Contents, putEdgeUpdate)
		e.Str(op.Path)
		postcard.PutOption(e, op.Encoding, putEncoding)
	case FileMove:
		putEdgeUpdate(e, op.Del)
		putInsertion(e, op.Add)
		e.Str(op.Path)
	case Edit:
		putAtom(e, op.Change)
		putLocal(e, op.Local)
		postcard.PutOption(e, op.Encoding, putEncoding)
	case Replacement:
		putEdgeUpdate(e, op.Change)
		putInsertion(e, op.Replacement)
		putLocal(e, op.Local)
		postcard.PutOption(e, op.Encoding, putEncoding)
	}
}

func putPosition(e *postcard.Encoder, p Position) {
	e.U16(uint16(p.Change))
	e.U32(p.Pos)
}

func putNode(e *postcard.Encoder, n Node) {
	e.U16(uint16(n.Change))
	e.U32(n.Start)
	e.U32(n.End)
}

func putInsertion(e *postcard.Encoder, ins Insertion) {
	postcard.PutSeq(e, ins.Predecessors, putPosition)
	postcard.PutSeq(e, ins.Successors, putPosition)
	e.U8(uint8(ins.Flag))
	e.U32(ins.Start)
	e.U32(ins.End)
	putPosition(e, ins.Inode)
}

func putEdgeUpdate(e *postcard.Encoder, u EdgeUpdate) {
	postcard.PutSeq(e, u.Edges, putNewEdge)
	putPosition(e, u.Inode)
}

func putNewEdge(e *postcard.Encoder, ne NewEdge) {
	e.U8(uint8(ne.Previous))
	e.U8(uint8(ne.Flag))
	putPosition(e, ne.From)
	putNode(e, ne.To)
	e.U16(uint16(ne.IntroducedBy))
}

// putAtom writes a, which AppendPayload has checked is an Insertion or an
// EdgeUpdate.
func putAtom(e *postcard.Encoder, a Atom) {
	switch a := a.(type) {
	case Insertion:
		e.Variant(0)
		putInsertion(e, a)
	case EdgeUpdate:
		e.Variant(1)
		putEdgeUpdate(e, a)
	}
}

func putLocal(e *postcard.Encoder, l Local) {
	e.Str(l.Path)
	e.U64(l.Line)
}

func putEncoding(e *postcard.Encoder, enc Encoding) {
	e.Variant(uint32(enc))
}

// formatError returns a *changefile.FormatError of kind in the GRAPH
// section, with no file offset: the codec does not know where the payload
// sits in the file.
func formatError(kind error, detail string, err error) *changefile.FormatError {
	return &changefile.FormatError{Kind: kind, Section: changefile.SectionGraph, Offset: -1, Detail: detail, Err: err}
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

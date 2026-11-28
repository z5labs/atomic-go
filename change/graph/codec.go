package graph

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/internal/postcard"
)

// decodedVariants is the number of CompactGraphOp variants [Decode]
// accepts: FileAdd to SetAttr, the ones a default build of Atomic declares.
// A higher index, PrototypeContext (17) included, is an unknown variant.
const decodedVariants = uint32(OpSetAttr) + 1

// inodeAttrVariants is the number of variants InodeAttr declares.
const inodeAttrVariants = 2

// The flags a name-conflict edge moves between: Folder|Block while the
// name is alive, and Folder|Block|Deleted once it has lost.
const (
	nameAlive   = Folder | Block
	nameDeleted = Folder | Block | Deleted
)

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
// ones declared. Ops are decoded up to variant 16, SetAttr; variant 17,
// PrototypeContext, exists only in an experimental build of Atomic and is
// refused, like any higher index, as an unknown variant
// ([postcard.ErrUnknownVariant]). So is an [InodeKind] beyond the three
// declared.
//
// Once the whole payload has decoded, each op is checked in order as
// Atomic's reader checks it before expansion
// (docs/remote/change-format/graph.md#compactgraphop), and the first that
// fails is of kind [changefile.ErrInvalidGraphOp], naming its index: a
// [SetAttr] with an empty Path or a [Mode] above [MaxMode]; a
// [SolveNameConflict] or [UnsolveNameConflict] with an empty Path or no
// edges, or with an edge that does not go exactly from Folder|Block to
// Folder|Block|Deleted (the reverse for Unsolve), that targets an empty
// vertex (To.Start >= To.End), or that repeats an earlier edge of the op.
// In a [changefile.Version1] payload, a name-conflict op with no edges and
// a non-empty Path is a legacy marker and is accepted
// (docs/remote/change-format/graph.md#legacy-name-conflict-markers).
// The checks Atomic runs after expansion, on what the hash indices refer
// to, are not run here.
func Decode(b []byte, v changefile.Version) (Payload, error) {
	if !v.Supported() {
		return Payload{}, formatError(changefile.ErrUnsupportedVersion, fmt.Sprintf("schema version %d", uint32(v)), nil)
	}
	p, err := postcard.Decode(b, decodePayload)
	if err != nil {
		return Payload{}, formatError(changefile.ErrPayload, "GraphSectionPayload", err)
	}
	legacy := v == changefile.Version1
	for i, op := range p.Ops {
		if reason := ruleError(op, legacy); reason != "" {
			return Payload{}, formatError(changefile.ErrInvalidGraphOp, fmt.Sprintf("op %d: %s", i, reason), nil)
		}
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
	case OpSolveNameConflict:
		return SolveNameConflict{
			Name: decodeEdgeUpdate(d),
			Path: d.Str(),
		}
	case OpUnsolveNameConflict:
		return UnsolveNameConflict{
			Name: decodeEdgeUpdate(d),
			Path: d.Str(),
		}
	case OpSolveOrderConflict:
		return SolveOrderConflict{
			Change: decodeEdgeUpdate(d),
			Local:  decodeLocal(d),
		}
	case OpUnsolveOrderConflict:
		return UnsolveOrderConflict{
			Change: decodeEdgeUpdate(d),
			Local:  decodeLocal(d),
		}
	case OpResurrectZombies:
		return ResurrectZombies{
			Change:   decodeEdgeUpdate(d),
			Local:    decodeLocal(d),
			Encoding: postcard.Option(d, decodeEncoding),
		}
	case OpAddRoot:
		return AddRoot{
			Name:  decodeInsertion(d),
			Inode: decodeInsertion(d),
		}
	case OpDelRoot:
		return DelRoot{
			Name:  decodeEdgeUpdate(d),
			Inode: decodeEdgeUpdate(d),
		}
	case OpSetAttr:
		return SetAttr{
			Inode: decodePosition(d),
			Path:  d.Str(),
			Value: decodeInodeAttr(d),
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

// decodeInodeAttr decodes one InodeAttr. It returns nil when the variant
// index fails to decode.
func decodeInodeAttr(d *postcard.Decoder) InodeAttr {
	v := d.Variant(inodeAttrVariants)
	if d.Err() != nil {
		return nil
	}
	if v == 0 {
		return Mode(d.U16())
	}
	return InodeKind(d.Variant(inodeKindVariants))
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
// an [EdgeUpdate] value, when an Encoding is not one of the five declared,
// when a [SetAttr]'s Value is nil or not a [Mode] or an [InodeKind] value,
// or an InodeKind beyond the three declared, and when it fails one of the
// checks [Decode] runs. Those are applied as to a version 2 or 3 file:
// a name-conflict op with no edges, the legacy marker a version 1 file may
// hold, is refused. Hash indices, and ranges other than a name-conflict
// edge's To, are not checked.
func AppendPayload(dst []byte, p Payload) ([]byte, error) {
	if !utf8.ValidString(p.Path) {
		return dst, formatError(changefile.ErrPayload, fmt.Sprintf("path %q is not valid UTF-8", p.Path), nil)
	}
	for i, op := range p.Ops {
		if reason := firstOf(opError(op), ruleError(op, false)); reason != "" {
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
	case SolveNameConflict:
		return pathError(op.Path)
	case UnsolveNameConflict:
		return pathError(op.Path)
	case SolveOrderConflict:
		return pathError(op.Local.Path)
	case UnsolveOrderConflict:
		return pathError(op.Local.Path)
	case ResurrectZombies:
		return firstOf(pathError(op.Local.Path), encodingError(op.Encoding))
	case AddRoot, DelRoot:
		return ""
	case SetAttr:
		return firstOf(pathError(op.Path), attrError(op.Value))
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

// attrError returns why a, a SetAttr value, cannot be written, or "" when
// it can. A Mode above MaxMode is ruleError's to report, since Decode
// refuses it too.
func attrError(a InodeAttr) string {
	switch a := a.(type) {
	case Mode:
		return ""
	case InodeKind:
		if a < inodeKindVariants {
			return ""
		}
		return fmt.Sprintf("inode kind %s is not one Atomic declares", a)
	case nil:
		return "attribute value is nil"
	default:
		return fmt.Sprintf("attribute value is a %T, not a graph.Mode or graph.InodeKind", a)
	}
}

// ruleError returns which of the checks Atomic's reader runs before
// expansion op fails, or "" when it passes them
// (docs/remote/change-format/graph.md#compactgraphop). legacy accepts the
// edge-less name-conflict markers of a version 1 file. The checks are
// Atomic's, in Atomic's order.
func ruleError(op Op, legacy bool) string {
	switch op := op.(type) {
	case SetAttr:
		if op.Path == "" {
			return "SetAttr requires a non-empty path"
		}
		if mode, ok := op.Value.(Mode); ok && mode > MaxMode {
			return fmt.Sprintf("SetAttr mode %#o is above %#o", uint16(mode), uint16(MaxMode))
		}
		return ""
	case SolveNameConflict:
		return nameConflictError(op.Variant(), op.Name, op.Path, nameAlive, nameDeleted, legacy)
	case UnsolveNameConflict:
		return nameConflictError(op.Variant(), op.Name, op.Path, nameDeleted, nameAlive, legacy)
	default:
		return ""
	}
}

// nameConflictError checks a SolveNameConflict or UnsolveNameConflict
// whose edges must each go from previous to flag.
func nameConflictError(v OpVariant, name EdgeUpdate, path string, previous, flag EdgeFlags, legacy bool) string {
	if legacy && len(name.Edges) == 0 && path != "" {
		return ""
	}
	if path == "" {
		return fmt.Sprintf("%s requires a non-empty surviving path", v)
	}
	if len(name.Edges) == 0 {
		return fmt.Sprintf("%s requires at least one losing name claim", v)
	}
	for i, edge := range name.Edges {
		if edge.Previous != previous || edge.Flag != flag {
			return fmt.Sprintf("%s edge %d must transition exactly from 0x%02X to 0x%02X, got 0x%02X to 0x%02X",
				v, i, uint8(previous), uint8(flag), uint8(edge.Previous), uint8(edge.Flag))
		}
		if edge.To.Start >= edge.To.End {
			return fmt.Sprintf("%s edge %d must target a non-empty name vertex", v, i)
		}
		if slices.Contains(name.Edges[:i], edge) {
			return fmt.Sprintf("%s edge %d duplicates an earlier losing claim", v, i)
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
	case SolveNameConflict:
		putEdgeUpdate(e, op.Name)
		e.Str(op.Path)
	case UnsolveNameConflict:
		putEdgeUpdate(e, op.Name)
		e.Str(op.Path)
	case SolveOrderConflict:
		putEdgeUpdate(e, op.Change)
		putLocal(e, op.Local)
	case UnsolveOrderConflict:
		putEdgeUpdate(e, op.Change)
		putLocal(e, op.Local)
	case ResurrectZombies:
		putEdgeUpdate(e, op.Change)
		putLocal(e, op.Local)
		postcard.PutOption(e, op.Encoding, putEncoding)
	case AddRoot:
		putInsertion(e, op.Name)
		putInsertion(e, op.Inode)
	case DelRoot:
		putEdgeUpdate(e, op.Name)
		putEdgeUpdate(e, op.Inode)
	case SetAttr:
		putPosition(e, op.Inode)
		e.Str(op.Path)
		putInodeAttr(e, op.Value)
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

// putInodeAttr writes a, which AppendPayload has checked is a Mode or an
// InodeKind.
func putInodeAttr(e *postcard.Encoder, a InodeAttr) {
	switch a := a.(type) {
	case Mode:
		e.Variant(0)
		e.U16(uint16(a))
	case InodeKind:
		e.Variant(1)
		e.Variant(uint32(a))
	}
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

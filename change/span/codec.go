package span

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/internal/postcard"
)

// SpanOp variant indices, in declaration order.
const (
	variantRoot uint32 = iota
	variantEdit
	opVariants
)

// Decode decodes the SPAN payload b: [Magic], then the postcard encoding of
// a patch in the layout its version names. It accepts only the canonical
// encoding, as Atomic does, so a payload that decodes is one
// [AppendPayload] writes back byte for byte.
//
// A payload that does not begin with [Magic] fails with an error wrapping
// [ErrMagic], and one whose version byte is not [V1], [V2] or
// [LegacySource] with an error wrapping [ErrVersion]. Every other failure
// wraps [ErrNotCanonical]: a body that does not decode (it ends early, holds
// a path that is not UTF-8, or names an operation variant other than Root
// and Edit), bytes left over after the patch, and a payload that decodes but
// re-encodes differently, such as one holding an overlong varint.
//
// Decode does not check the patch's ranges or owners; see
// [Patch.ValidateContentRanges]. Empty prior metadata, operations and
// owners decode to nil, and nothing in the result aliases b.
func Decode(b []byte) (Patch, error) {
	if !bytes.HasPrefix(b, []byte(Magic)) {
		return Patch{}, ErrMagic
	}
	if len(b) > len(Magic) {
		if v := Version(b[len(Magic)]); !v.valid() {
			return Patch{}, fmt.Errorf("%w: %s", ErrVersion, v)
		}
	}
	p, err := postcard.DecodeExact(b, decodePatch)
	if err != nil {
		return Patch{}, fmt.Errorf("%w: %w", ErrNotCanonical, err)
	}
	// Every patch Decode returns has a valid version and, being decoded,
	// only Root and Edit values, so it always encodes.
	re, err := AppendPayload(nil, p)
	if err != nil {
		return Patch{}, fmt.Errorf("%w: %w", ErrNotCanonical, err)
	}
	if !bytes.Equal(re, b) {
		return Patch{}, fmt.Errorf("%w: payload re-encodes to different bytes", ErrNotCanonical)
	}
	return p, nil
}

// decodePatch decodes a whole payload, magic included, so that postcard
// offsets are offsets into the payload. The caller has checked the magic
// and the version.
func decodePatch(d *postcard.Decoder) Patch {
	d.Fixed(len(Magic))
	p := Patch{Version: Version(d.U8())}
	if meta := d.ByteSeq(); len(meta) > 0 {
		p.PriorMetadata = bytes.Clone(meta)
	}
	if ops := postcard.Seq(d, decodeOp); len(ops) > 0 {
		p.Ops = ops
	}
	if p.Version == V1 {
		return p
	}
	if owners := postcard.Seq(d, decodeOwner); len(owners) > 0 {
		p.SemanticOwners = owners
	}
	return p
}

func decodeOp(d *postcard.Decoder) Op {
	switch d.Variant(opVariants) {
	case variantRoot:
		return Root{
			Path:    d.Str(),
			Ordinal: d.U32(),
			Start:   d.U64(),
			End:     d.U64(),
		}
	default:
		return Edit{
			File: ExternalFile{
				Owner:   d.Array32(),
				Ordinal: d.U32(),
			},
			Target: ExternalSpan{
				Owner: d.Array32(),
				Start: d.U64(),
				End:   d.U64(),
			},
			Start:       d.U64(),
			End:         d.U64(),
			InsertStart: d.U64(),
			InsertEnd:   d.U64(),
		}
	}
}

func decodeOwner(d *postcard.Decoder) SemanticOwner {
	return SemanticOwner{
		AuthoredID: d.U64(),
		Owner:      d.Array32(),
	}
}

// AppendPayload appends the SPAN payload of p to dst: [Magic], then p in
// the layout its version names, every varint minimal. It is the canonical
// encoding, the only one [Decode] accepts.
//
// It fails, and returns dst unchanged, when p's version is not [V1], [V2]
// or [LegacySource] (wrapping [ErrVersion]); when p is a [V1] patch with
// semantic owners, which that layout cannot carry (wrapping
// [ErrInvalidOwners]); and when an operation is nil, is not a [Root] or an
// [Edit] value, or is a [Root] whose path is not valid UTF-8 (wrapping
// [ErrInvalidOp]). It does not check ranges or owners; see
// [Patch.ValidateContentRanges].
func AppendPayload(dst []byte, p Patch) ([]byte, error) {
	if !p.Version.valid() {
		return dst, fmt.Errorf("%w: %s", ErrVersion, p.Version)
	}
	if p.Version == V1 && len(p.SemanticOwners) > 0 {
		return dst, fmt.Errorf("%w: a V1 patch cannot carry %d semantic owners", ErrInvalidOwners, len(p.SemanticOwners))
	}
	for i, op := range p.Ops {
		switch op := op.(type) {
		case Root:
			if !utf8.ValidString(op.Path) {
				return dst, invalidOp(i, fmt.Sprintf("root path %q is not valid UTF-8", op.Path))
			}
		case Edit:
		default:
			return dst, invalidOp(i, opTypeError(op))
		}
	}

	e := postcard.NewEncoder(append(dst, Magic...))
	e.U8(uint8(p.Version))
	e.ByteSeq(p.PriorMetadata)
	postcard.PutSeq(e, p.Ops, putOp)
	if p.Version != V1 {
		postcard.PutSeq(e, p.SemanticOwners, putOwner)
	}
	return e.Bytes(), nil
}

// putOp writes op, which AppendPayload has checked is a Root or an Edit.
func putOp(e *postcard.Encoder, op Op) {
	switch op := op.(type) {
	case Root:
		e.Variant(variantRoot)
		e.Str(op.Path)
		e.U32(op.Ordinal)
		e.U64(op.Start)
		e.U64(op.End)
	case Edit:
		e.Variant(variantEdit)
		e.Array32(op.File.Owner)
		e.U32(op.File.Ordinal)
		e.Array32(op.Target.Owner)
		e.U64(op.Target.Start)
		e.U64(op.Target.End)
		e.U64(op.Start)
		e.U64(op.End)
		e.U64(op.InsertStart)
		e.U64(op.InsertEnd)
	}
}

func putOwner(e *postcard.Encoder, o SemanticOwner) {
	e.U64(o.AuthoredID)
	e.Array32(o.Owner)
}

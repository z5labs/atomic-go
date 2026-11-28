// Package span reads and writes the SPAN section of a version 3 Atomic
// change: the immutable span operations that address authored bytes by
// change hash and byte range, and the owners of the semantic IDs the
// change's SEMANTIC operations use. A SPAN payload is one [Patch].
//
// [Decode] reads a payload and [AppendPayload] writes one. Unlike every
// other change payload, SPAN is accepted only in its canonical form: Atomic
// refuses trailing bytes and any payload that does not re-encode to the
// same bytes, so [Decode] does too. [Patch.ValidateContentRanges] runs the
// range and owner checks Atomic runs when it reads or writes a change. The
// payload is documented in docs/remote/change-format/sections.md#span-section.
package span

import (
	"errors"
	"fmt"

	"github.com/z5labs/atomic-go/ident"
)

// Magic is the four ASCII bytes every SPAN payload begins with.
const Magic = "ATSP"

var (
	// ErrMagic is wrapped by the error [Decode] returns for a payload that
	// does not begin with [Magic].
	ErrMagic = errors.New("span: payload does not begin with ATSP")

	// ErrVersion is wrapped by the error [Decode] and [AppendPayload]
	// return for a version other than [V1], [V2] and [LegacySource].
	ErrVersion = errors.New("span: unsupported version")

	// ErrNotCanonical is wrapped by the error [Decode] returns for a
	// payload that is not the canonical encoding of a patch: one whose
	// body does not decode, that has bytes left over after the patch, or
	// that does not re-encode to the same bytes, such as one holding an
	// overlong varint. When the body does not decode or has bytes left
	// over, the error also wraps the postcard failure, which carries the
	// byte offset in the payload at which the failure begins.
	ErrNotCanonical = errors.New("span: payload is not canonical")

	// ErrInvalidOp is wrapped by the error [Patch.ValidateContentRanges]
	// returns for an operation whose ranges are invalid, and by the error
	// [AppendPayload] returns for an operation it cannot write. The error
	// reads "invalid span operation N: reason", as Atomic's does.
	ErrInvalidOp = errors.New("span: invalid span operation")

	// ErrInvalidOwners is wrapped by the error
	// [Patch.ValidateContentRanges] returns for an invalid semantic owner
	// list, and by the error [AppendPayload] returns for a [V1] patch that
	// has semantic owners, which that layout cannot carry.
	ErrInvalidOwners = errors.New("span: invalid semantic owners")
)

// Version is a SPAN payload version, the byte after [Magic]. It selects the
// layout of the rest of the payload.
type Version uint8

const (
	// V1 is the original layout, SpanPatchV1: the version, the prior
	// metadata and the operations, with no semantic owners. Atomic keeps
	// it so that the bytes and hashes of changes written in it are
	// preserved.
	V1 Version = 1

	// V2 is the layout SpanPatch::new writes: V1's fields followed by the
	// semantic owners.
	V2 Version = 2

	// LegacySource has the layout of [V2], and also allows verified legacy
	// graph ranges as authored sources.
	LegacySource Version = 3
)

// valid reports whether v is a version Atomic reads.
func (v Version) valid() bool {
	return v >= V1 && v <= LegacySource
}

// String returns the version's name, or Version(n) for a value with no
// name.
func (v Version) String() string {
	switch v {
	case V1:
		return "V1"
	case V2:
		return "V2"
	case LegacySource:
		return "LegacySource"
	default:
		return fmt.Sprintf("Version(%d)", uint8(v))
	}
}

// Patch is a change's SPAN payload: Atomic's SpanPatch, or SpanPatchV1 when
// the version is [V1]. The fields are in wire order.
type Patch struct {
	// Version selects the layout.
	Version Version

	// PriorMetadata is metadata carried by older span payloads. It is
	// separate from the HEADER's metadata. A patch with none holds nil.
	PriorMetadata []byte

	// Ops is the change's span operations, each a [Root] or an [Edit].
	Ops []Op

	// SemanticOwners maps the non-ROOT semantic IDs used by the change's
	// SEMANTIC operations to the changes that own them. A [V1] patch has
	// none.
	SemanticOwners []SemanticOwner
}

// Op is a span operation: a [Root] or an [Edit]. No other type
// implements it.
type Op interface {
	// spanOp seals the interface.
	spanOp()
}

// Root introduces a file's initial authored range: variant 0 of Atomic's
// SpanOp.
type Root struct {
	// Path is the file's path.
	Path string

	// Ordinal forms the file's portable identity; see [ExternalFile].
	Ordinal uint32

	// Start and End are the range's inclusive start and exclusive end,
	// as offsets into this change's contents.
	Start, End uint64
}

// Edit replaces part of an existing authored span with a range of this
// change's contents: variant 1 of Atomic's SpanOp.
type Edit struct {
	// File is the file the edit applies to.
	File ExternalFile

	// Target is the span being edited.
	Target ExternalSpan

	// Start and End are the inclusive start and exclusive end of the
	// replaced range, as offsets relative to Target's start.
	Start, End uint64

	// InsertStart and InsertEnd are the inclusive start and exclusive end
	// of the inserted range, as offsets into this change's contents.
	InsertStart, InsertEnd uint64
}

func (Root) spanOp() {}
func (Edit) spanOp() {}

// ExternalSpan is a range of another change's authored bytes.
type ExternalSpan struct {
	// Owner is the hash of the change that owns the bytes.
	Owner ident.Hash

	// Start and End are the range's inclusive start and exclusive end, as
	// offsets into the owner's contents.
	Start, End uint64
}

// ExternalFile is a portable file identity: the change that created the
// file and the file's ordinal within that change
// (docs/remote/change-format/graph.md#externalfile).
type ExternalFile struct {
	// Owner is the hash of the change that created the file.
	Owner ident.Hash

	// Ordinal is the [Root.Ordinal] of the operation that introduced the
	// file.
	Ordinal uint32
}

// ProjectionID is the content-addressed identity of a span projection,
// Atomic's SpanProjectionId. On the wire it is the hash's 32 raw bytes
// (docs/remote/change-format/graph.md#spanprojectionid).
type ProjectionID ident.Hash

// SemanticOwner names the change that introduced the items a semantic ID
// refers to.
type SemanticOwner struct {
	// AuthoredID is a non-ROOT numeric name used by the change's semantic
	// encoding. It is never 0.
	AuthoredID uint64

	// Owner is the change that introduced the named items.
	Owner ident.Hash
}

// ValidateContentRanges checks p's ranges against a change whose contents
// are contentLen bytes long, and its semantic owners, as Atomic's
// validate_content_ranges does when it reads or writes a change.
//
// An operation fails, with an error wrapping [ErrInvalidOp] that names its
// index, when it is a [Root] whose range is reversed or runs past the
// contents, or an [Edit] whose file or target owner is the zero hash, whose
// target is reversed, whose edit range is reversed or runs past the
// target's length, or whose inserted range is reversed or runs past the
// contents. A nil operation, or one that is not a [Root] or an [Edit]
// value, fails the same way. The semantic owners fail, with an error
// wrapping [ErrInvalidOwners], when an AuthoredID is 0, an owner is the
// zero hash, the AuthoredIDs are not strictly ascending, or one owner hash
// appears twice. The operations are checked first, in order, and the first
// failure is returned.
//
// The checks that need repository state, such as dependency closure and
// file ownership, are not run here; Atomic runs them when it applies the
// change.
func (p Patch) ValidateContentRanges(contentLen uint64) error {
	for i, op := range p.Ops {
		if reason := opRangeError(op, contentLen); reason != "" {
			return invalidOp(i, reason)
		}
	}
	return validateOwners(p.SemanticOwners)
}

// invalidOp returns the error for operation i, in Atomic's wording.
func invalidOp(i int, reason string) error {
	return fmt.Errorf("%w %d: %s", ErrInvalidOp, i, reason)
}

// opRangeError returns why op's ranges are invalid, or "" when they are
// valid.
func opRangeError(op Op, contentLen uint64) string {
	switch op := op.(type) {
	case Root:
		switch {
		case op.Start > op.End:
			return fmt.Sprintf("root range %d..%d is reversed", op.Start, op.End)
		case op.End > contentLen:
			return fmt.Sprintf("root range %d..%d runs past the %d content bytes", op.Start, op.End, contentLen)
		}
	case Edit:
		t := op.Target
		switch {
		case op.File.Owner.IsZero():
			return "edit file owner is the zero hash"
		case t.Owner.IsZero():
			return "edit target owner is the zero hash"
		case t.Start > t.End:
			return fmt.Sprintf("edit target %d..%d is reversed", t.Start, t.End)
		case op.Start > op.End:
			return fmt.Sprintf("edit range %d..%d is reversed", op.Start, op.End)
		case op.End > t.End-t.Start:
			return fmt.Sprintf("edit range %d..%d runs past the %d-byte target", op.Start, op.End, t.End-t.Start)
		case op.InsertStart > op.InsertEnd:
			return fmt.Sprintf("inserted range %d..%d is reversed", op.InsertStart, op.InsertEnd)
		case op.InsertEnd > contentLen:
			return fmt.Sprintf("inserted range %d..%d runs past the %d content bytes", op.InsertStart, op.InsertEnd, contentLen)
		}
	default:
		return opTypeError(op)
	}
	return ""
}

// opTypeError returns why op is not an operation a patch can hold.
func opTypeError(op Op) string {
	if op == nil {
		return "operation is nil"
	}
	return fmt.Sprintf("operation is a %T, not a span.Root or span.Edit", op)
}

// validateOwners checks a semantic owner list as Atomic's
// SemanticOwners::validate does.
func validateOwners(owners []SemanticOwner) error {
	seen := make(map[ident.Hash]int, len(owners))
	for i, o := range owners {
		switch {
		case o.AuthoredID == 0:
			return fmt.Errorf("%w: owner %d has authored_id 0", ErrInvalidOwners, i)
		case o.Owner.IsZero():
			return fmt.Errorf("%w: owner %d is the zero hash", ErrInvalidOwners, i)
		case i > 0 && o.AuthoredID <= owners[i-1].AuthoredID:
			return fmt.Errorf("%w: owner %d has authored_id %d, not above the previous %d",
				ErrInvalidOwners, i, o.AuthoredID, owners[i-1].AuthoredID)
		}
		if j, dup := seen[o.Owner]; dup {
			return fmt.Errorf("%w: owners %d and %d are both %s", ErrInvalidOwners, j, i, o.Owner)
		}
		seen[o.Owner] = i
	}
	return nil
}

// Package syncproto builds and reads the binary bodies push, pull and clone
// exchange on a project's /code endpoint: a [Pack] of objects and ref moves,
// and the [Wants] a pull or clone sends to ask for one.
//
// A body is the value in postcard, compressed as one zstd frame. The
// [Pack.AppendPostcard], [Pack.UnmarshalPostcard], [Wants.AppendPostcard]
// and [Wants.UnmarshalPostcard] methods handle the postcard layer alone; a
// [Codec] handles both layers, refuses a body that inflates past its limit,
// and can stream a large pull one object at a time with [Codec.StreamPack],
// so memory stays bounded by the largest object rather than the whole pack.
//
// The protocol is documented in docs/remote/endpoints/project.md, under
// Sync protocol and Types, and its decode failures in
// docs/remote/errors.md, under Decode failures and version mismatch.
package syncproto

import (
	"strconv"
)

const (
	// ProtocolHeader is the request header that marks a /code request as
	// the binary sync protocol.
	ProtocolHeader = "Atomic-Protocol"

	// ProtocolVersion is the value of [ProtocolHeader]. Its suffix is
	// bumped on any wire-breaking change.
	ProtocolVersion = "sync/1"

	// ContentType is the Content-Type of a sync request body.
	ContentType = "application/vnd.atomic.sync.v1"

	// MaxDecodedSize is the most bytes a body may inflate to by default,
	// 2 GiB, the limit Atomic's own decoder applies.
	MaxDecodedSize = 2 << 30
)

// Family is the family an object in transit belongs to. Its value is the
// family's postcard index, in Atomic's declaration order.
//
// A decoded Family can be one this package does not know, kept so that a
// newer server's objects are not lost on the way through; [Family.Known]
// tells them apart. Encoding refuses an unknown Family.
type Family uint32

// The families Atomic declares.
const (
	Change     Family = iota // a .change object
	Tag                      // a .tag object, a named signed state
	Provenance               // a .provenance object, a change's provenance graph sidecar
	Attest                   // an .attest object, an attestation sidecar
	View                     // a view snapshot
	Conflict                 // a .conflict object, a view's conflict state
)

// Known reports whether f is one of the families Atomic declares.
func (f Family) Known() bool {
	return f <= Conflict
}

// Segment returns the storage key segment of f, such as "changes" for
// [Change], or "" if f is not [Family.Known].
func (f Family) Segment() string {
	switch f {
	case Change:
		return "changes"
	case Tag:
		return "tags"
	case Provenance:
		return "provenance"
	case Attest:
		return "attest"
	case View:
		return "views"
	case Conflict:
		return "conflicts"
	}
	return ""
}

// String returns the variant name of f, such as "Change", or "Family(9)" if
// f is not [Family.Known].
func (f Family) String() string {
	switch f {
	case Change:
		return "Change"
	case Tag:
		return "Tag"
	case Provenance:
		return "Provenance"
	case Attest:
		return "Attest"
	case View:
		return "View"
	case Conflict:
		return "Conflict"
	}
	return "Family(" + strconv.FormatUint(uint64(f), 10) + ")"
}

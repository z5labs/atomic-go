// Package tags reads and writes the tag records a sync pack carries in its
// Tag objects, and computes the key each one is stored under.
//
// A [Record] travels as Atomic's TagRecord in postcard: ten fields, in
// declaration order, with no names or count. [Record.MarshalPostcard]
// writes all ten and [Record.UnmarshalPostcard] reads them back, keeping
// the timestamp text exactly as it was written. Atomic's key for a tag
// object is not a digest of those bytes but of the decoded fields:
// [Record.ContentHash] computes it and [Record.Key] is its base32 text. It
// leaves out the metadata, the author's identity and the timestamp's
// fraction of a second, so records differing only in those share a key.
//
// The record is documented in docs/remote/endpoints/project.md, under
// TagRecord, and its key in docs/remote/types.md, under Which identifier is
// which.
package tags

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/z5labs/atomic-go/change"
	"github.com/z5labs/atomic-go/ident"
)

var (
	// ErrMalformed reports bytes that do not decode as a tag record. The
	// error that wraps it also wraps the postcard failure, or describes
	// the field that was refused.
	ErrMalformed = errors.New("tags: malformed tag record")

	// ErrInvalidRecord reports a [Record] that [Record.MarshalPostcard]
	// cannot write as bytes a reader accepts: text that is not valid
	// UTF-8, a timestamp that cannot be written, a [Kind] Atomic does not
	// declare, or metadata that is not valid JSON.
	ErrInvalidRecord = errors.New("tags: invalid tag record")
)

// Kind is what a tag marks. Its value is the variant's postcard index,
// which is also the byte [Record.ContentHash] writes for it.
type Kind uint32

// The kinds Atomic declares.
const (
	Release    Kind = iota // a release
	ReviewGate             // a review gate
	Custom                 // anything else
)

// kindVariants is the number of variants TagKind declares.
const kindVariants = 3

// String returns the variant name of k, such as "Release", or "Kind(7)"
// for a value Atomic does not declare.
func (k Kind) String() string {
	switch k {
	case Release:
		return "Release"
	case ReviewGate:
		return "ReviewGate"
	case Custom:
		return "Custom"
	}
	return "Kind(" + strconv.FormatUint(uint64(k), 10) + ")"
}

// Record is a tag, Atomic's TagRecord: a named point in a view's history.
type Record struct {
	// Name is the tag's name, such as "v1.2.3".
	Name string

	// View is the name of the view the tag belongs to.
	View string

	// Sequence is the view's sequence number at the tagged point.
	Sequence ident.Sequence

	// State is the view's Merkle state at the tagged point.
	State ident.Hash

	// ChangeHash is the hash of the change at that sequence position.
	ChangeHash ident.Hash

	// Timestamp is when the tag was created.
	Timestamp time.Time

	// TimestampText is Timestamp as the record writes it, in chrono's
	// RFC 3339 form. One instant has more than one spelling, so a decoded
	// record keeps the text it was read from and an encoded one writes it
	// back unchanged. Empty means Timestamp is formatted the way chrono
	// formats it.
	//
	// When TimestampText is set it takes precedence, in
	// [Record.MarshalPostcard] and in [Record.ContentHash] alike, and
	// Timestamp is not consulted. Clear it after changing Timestamp, or
	// the old instant is what gets written and hashed.
	TimestampText string

	// Author is who created the tag, or nil when it is not recorded.
	Author *change.Author

	// Message is the tag's human-readable message, or nil when it has
	// none.
	Message *string

	// Kind is what the tag marks.
	Kind Kind

	// Metadata is the tag's extensible metadata as JSON text, such as Git
	// provenance or CI status, or nil when it has none. It is written and
	// kept verbatim. An empty Metadata is the same as nil.
	Metadata json.RawMessage
}

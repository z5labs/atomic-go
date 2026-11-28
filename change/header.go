package change

import "time"

// Header is the human-readable part of a change, Atomic's ChangeHeader. It
// is the whole HEADER payload of a schema version 1 change file and the
// first field of the envelope in versions 2 and 3
// (docs/remote/change-format/sections.md#changeheader).
type Header struct {
	// Message is the change's short summary. The format accepts an empty
	// message.
	Message string

	// Description is the change's longer body, or nil when it has none.
	Description *string

	// Timestamp is when the change was created.
	Timestamp time.Time

	// TimestampText is Timestamp as the HEADER payload writes it, in
	// chrono's RFC 3339 form. The text is part of the change hash, and
	// one instant has more than one spelling, so a decoded header keeps
	// the text it was read from and an encoded one writes it back
	// unchanged. Empty means Timestamp is formatted the way chrono
	// formats it.
	TimestampText string

	// Authors are the change's authors, in order. The list may be empty.
	Authors []Author
}

// Author is one author of a change: a display name and, optionally, an
// email address and a reference to the author's cryptographic identity
// (docs/remote/change-format/sections.md#author).
//
// Package remote/tags reuses this type for a tag record's author and
// encodes it with a codec of its own: the name as a string, then the email
// and then the identity, each as an option.
type Author struct {
	// Name is the author's display name. Nothing in the format refuses an
	// empty one.
	Name string

	// Email is the author's email address, or nil when it is not given.
	Email *string

	// Identity names the author's cryptographic identity, such as a
	// base32 public key, or is nil when it is not given. The format does
	// not interpret it.
	Identity *string
}

// Kind says what a change is part of: durable repository history, or a
// snapshot of one working copy's complete state
// (docs/remote/change-format/sections.md#wirechangekindv2). The zero Kind
// is durable history.
type Kind struct {
	// Snapshot is true for a working-copy snapshot and false for durable
	// history.
	Snapshot bool

	// WorkingCopy is the ULID of the working copy a snapshot captures. It
	// is set only when Snapshot is true, and is zero otherwise.
	WorkingCopy [16]byte
}

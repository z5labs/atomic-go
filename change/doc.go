// Package change holds the typed values of an Atomic change: for now, what
// its HEADER section carries. A [Header] is the human-readable part, its
// message, description, timestamp and [Author]s; a [Kind] says whether the
// change is durable history or a working-copy snapshot; and an [Origin]
// says whether Atomic authored it natively or synthesized it from a Git
// commit, which [Origin.Validate] checks the way Atomic does while it
// decodes a change.
//
// The package does no encoding yet and imports no change-file code. The
// HEADER payload these types are decoded from is documented in
// docs/remote/change-format/sections.md#header-section.
package change

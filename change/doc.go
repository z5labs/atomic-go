// Package change holds the typed values of an Atomic change: for now, what
// its HEADER section carries. A [Header] is the human-readable part, its
// message, description, timestamp and [Author]s; a [Kind] says whether the
// change is durable history or a working-copy snapshot; and an [Origin]
// says whether Atomic authored it natively or synthesized it from a Git
// commit, which [Origin.Validate] checks the way Atomic does while it
// decodes a change.
//
// The package also holds the unexported codecs for the HEADER and DEPS
// payloads, which resolve hash indices through a changefile.HashTable and
// report every failure as a *changefile.FormatError. The payloads are
// documented in docs/remote/change-format/sections.md#header-section and
// #deps-section.
package change

// Package change holds the typed values of an Atomic change. [Decode] reads
// a whole change file into a [Change], one typed field per section, with
// the checks and the error order of Atomic's Change::deserialize, and
// returns it as a [Verified] change, bound to the bytes and hash that give
// it its identity. [Change.Validate] applies Atomic's validate_v2 rules,
// which a decoded change has already passed; the reader is documented in
// docs/remote/change-format/container.md#reader-validation. [Encode] is
// the reverse: it writes a Change as a version 2 or 3 file in the layout
// of Atomic's Change::serialize, splitting its content blob with a
// [Chunker], and is documented in
// docs/remote/change-format/container.md#writer-rules.
//
// A [Header] is the human-readable part of the HEADER section, its message,
// description, timestamp and [Author]s; a [Kind] says whether the change is
// durable history or a working-copy snapshot; and an [Origin] says whether
// Atomic authored it natively or synthesized it from a Git commit, which
// [Origin.Validate] checks the way Atomic does while it decodes a change.
//
// The package also holds the unexported codecs for the HEADER and DEPS
// payloads, which resolve hash indices through a changefile.HashTable and
// report every failure as a *changefile.FormatError. The payloads are
// documented in docs/remote/change-format/sections.md#header-section and
// #deps-section.
//
// A [Signature] is the SIGNATURE section's value. [NewSignature] signs a
// change hash with any identity.Signer, [Sign] signs a change file without
// changing its hash, and [VerifySignature] checks a signature against a
// key the caller resolved: the signer DID a signature carries is only a
// hint, and nothing here trusts it. Signing is documented in
// docs/remote/change-format/container.md#signing.
package change

package change

import (
	"encoding/json"
	"fmt"

	"github.com/z5labs/atomic-go/change/graph"
	"github.com/z5labs/atomic-go/change/provenance"
	"github.com/z5labs/atomic-go/change/semantic"
	"github.com/z5labs/atomic-go/change/span"
	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
)

// Change is a whole decoded change: every section of a change file in
// typed form. [Decode] fills one from a change file; a Change is not tied
// to the bytes it came from, which [Verified] keeps alongside it.
//
// Every list that is empty is nil. The hash indices of the HEADER and DEPS
// sections are resolved through Table to hashes; those of the GRAPH
// operations are kept as indices, which Table resolves.
type Change struct {
	// Version is the change file's schema version. It decides which
	// [Change.Validate] rules apply.
	Version changefile.Version

	// Header is the human-readable part of the HEADER section.
	Header Header

	// Kind says whether the change is durable history or a working-copy
	// snapshot.
	Kind Kind

	// Supersedes is the snapshot this snapshot replaces, or nil.
	Supersedes *ident.Hash

	// Origin says whether Atomic authored the change or synthesized it
	// from a Git commit.
	Origin Origin

	// CausalFrontier is the roots whose closures the change knows, in
	// strictly ascending hash order. Only a GitResolution has any.
	CausalFrontier []ident.Hash

	// ExtraKnown is the changes known when the change was made but not
	// required by it, in strictly ascending hash order.
	ExtraKnown []ident.Hash

	// Metadata is the HEADER's opaque hashed application bytes, kept as
	// they are. They are never read as a span patch, even when they begin
	// with [span.Magic]; the SPAN section is in Span.
	Metadata []byte

	// Dependencies is the DEPS section: the changes that must be applied
	// before this one, in strictly ascending hash order in versions 2
	// and 3.
	Dependencies []ident.Hash

	// Provenance is the PROVENANCE section's records, or nil when the
	// change has none.
	Provenance []provenance.Record

	// ProvenanceLayout is the layout Provenance was decoded in. It is the
	// zero Layout, [provenance.LayoutCurrent], when there is no PROVENANCE
	// section.
	ProvenanceLayout provenance.Layout

	// Signature is the SIGNATURE section, decoded but not verified, or nil
	// when the change is not signed. See [VerifySignature].
	Signature *Signature

	// Table is the file's hash dedup table, which the GRAPH operations'
	// hash indices name slots of.
	Table changefile.HashTable

	// Graph is the operations of every GRAPH section, concatenated in file
	// order. Their hash indices are left as indices into Table.
	Graph []graph.Op

	// Span is the SPAN section, which only a version 3 change has, or nil.
	Span *span.Patch

	// Semantic is the file operations of every SEMANTIC section,
	// concatenated in file order.
	Semantic []semantic.FileOps

	// Contents is the change's content blob: every CONTENT chunk,
	// concatenated in file order.
	Contents []byte

	// Unhashed is the UNHASHED section's JSON text as it is, or nil when
	// the change has none. It is outside the change hash.
	Unhashed json.RawMessage
}

// ContentsHash returns the BLAKE3 hash of c.Contents, Atomic's
// contents_hash.
func (c Change) ContentsHash() ident.Hash {
	return ident.Sum(c.Contents)
}

// Validate checks c the way Atomic's Change::validate_v2 does when it
// reads or writes a change, and fails with a *[changefile.FormatError] of
// kind [changefile.ErrValidation].
//
// In every version it refuses a Version that is not
// [changefile.Version.Supported], a nil Span in version 3 and a Span in
// any other version. Atomic validates nothing else in a version 1 change,
// so neither does Validate.
//
// In versions 2 and 3 it applies every reader check of
// docs/remote/change-format/sections.md#header-section and #deps-section,
// in this order:
//
//   - the dependencies are non-zero and in strictly ascending hash order;
//   - extra_known is non-zero, strictly ascending, and repeats no
//     dependency;
//   - causal_frontier is non-zero and strictly ascending;
//   - supersedes is set only on a Snapshot, is not the zero hash, and
//     names no dependency;
//   - a Snapshot has a Native origin, so a Git origin has a Durable kind;
//   - the origin passes [Origin.Validate];
//   - causal_frontier is non-empty exactly when the origin is a
//     GitResolution;
//   - a GitResolution has at least two parents;
//   - no SHA-256 Git parent's 32 bytes equal a dependency hash.
//
// Then, in version 3, it runs [span.Patch.ValidateContentRanges] against
// the length of Contents.
//
// The contents_hash comparison validate_v2 also makes is always equal for
// a decoded change, since the hash is derived from Contents; see
// [Change.ContentsHash].
func (c Change) Validate() error {
	if !c.Version.Supported() {
		return invalid(0, fmt.Sprintf("schema version %d", uint32(c.Version)), nil)
	}
	switch {
	case c.Version == changefile.Version3 && c.Span == nil:
		return invalid(changefile.SectionSpan, "a version 3 change has no span", nil)
	case c.Version != changefile.Version3 && c.Span != nil:
		return invalid(changefile.SectionSpan, fmt.Sprintf("a version %d change has a span", uint32(c.Version)), nil)
	}
	if c.Version == changefile.Version1 {
		return nil
	}

	if err := strictlyAscending(changefile.SectionDeps, "dependencies", c.Dependencies); err != nil {
		return err
	}
	deps := make(map[ident.Hash]int, len(c.Dependencies))
	for i, h := range c.Dependencies {
		deps[h] = i
	}
	if err := strictlyAscending(changefile.SectionHeader, "extra_known", c.ExtraKnown); err != nil {
		return err
	}
	for i, h := range c.ExtraKnown {
		if d, ok := deps[h]; ok {
			return invalid(changefile.SectionHeader, fmt.Sprintf("extra_known %d is also dependency %d", i, d), nil)
		}
	}
	if err := strictlyAscending(changefile.SectionHeader, "causal_frontier", c.CausalFrontier); err != nil {
		return err
	}
	if c.Supersedes != nil {
		switch {
		case !c.Kind.Snapshot:
			return invalid(changefile.SectionHeader, "supersedes is set on a Durable change", nil)
		case c.Supersedes.IsZero():
			return invalid(changefile.SectionHeader, "supersedes is the zero hash", nil)
		}
		if d, ok := deps[*c.Supersedes]; ok {
			return invalid(changefile.SectionHeader, fmt.Sprintf("supersedes names dependency %d", d), nil)
		}
	}
	if c.Kind.Snapshot && c.Origin.Kind != Native {
		return invalid(changefile.SectionHeader, fmt.Sprintf("a Snapshot has a %s origin, not Native", c.Origin.Kind), nil)
	}
	if err := c.Origin.Validate(); err != nil {
		return invalid(changefile.SectionHeader, "origin", err)
	}
	if res := c.Origin.Kind == GitResolution; res != (len(c.CausalFrontier) > 0) {
		if res {
			return invalid(changefile.SectionHeader, "a GitResolution has an empty causal_frontier", nil)
		}
		return invalid(changefile.SectionHeader, fmt.Sprintf("a %s origin has a non-empty causal_frontier", c.Origin.Kind), nil)
	}
	if c.Origin.Kind == GitResolution && len(c.Origin.Parents) < 2 {
		return invalid(changefile.SectionHeader, fmt.Sprintf("a GitResolution has %d parents, want at least 2", len(c.Origin.Parents)), nil)
	}
	for i, p := range c.Origin.Parents {
		if !p.SHA256 || len(p.Bytes) != ident.Size {
			continue
		}
		if d, ok := deps[ident.Hash(p.Bytes)]; ok {
			return invalid(changefile.SectionHeader, fmt.Sprintf("Git parent %d is dependency %d", i, d), nil)
		}
	}

	if c.Version == changefile.Version3 {
		if err := c.Span.ValidateContentRanges(uint64(len(c.Contents))); err != nil {
			return invalid(changefile.SectionSpan, "", err)
		}
	}
	return nil
}

// strictlyAscending checks that hs holds no zero hash and is in strictly
// ascending hash order.
func strictlyAscending(section changefile.SectionType, what string, hs []ident.Hash) error {
	for i, h := range hs {
		switch {
		case h.IsZero():
			return invalid(section, fmt.Sprintf("%s entry %d is the zero hash", what, i), nil)
		case i > 0 && hs[i-1].Compare(h) >= 0:
			return invalid(section, fmt.Sprintf("%s is not in strictly ascending hash order at entry %d", what, i), nil)
		}
	}
	return nil
}

// invalid returns a validation failure in section.
func invalid(section changefile.SectionType, detail string, err error) *changefile.FormatError {
	return sectionError(section, changefile.ErrValidation, detail, err)
}

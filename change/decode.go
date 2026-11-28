package change

import (
	"encoding/json"
	"slices"

	"github.com/z5labs/atomic-go/change/graph"
	"github.com/z5labs/atomic-go/change/provenance"
	"github.com/z5labs/atomic-go/change/semantic"
	"github.com/z5labs/atomic-go/change/span"
	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
)

// DecodeOption changes how [Decode] reads a change file.
type DecodeOption func(*decodeOptions)

type decodeOptions struct {
	read            []changefile.ReadOption
	provenanceOrder []provenance.Layout
}

// WithReadOptions passes opts to the [changefile.Reader] Decode reads the
// file with, such as [changefile.AllowTrailingBytes] or
// [changefile.CheckChunks]. Given more than once, the options add up.
func WithReadOptions(opts ...changefile.ReadOption) DecodeOption {
	return func(o *decodeOptions) { o.read = append(o.read, opts...) }
}

// WithProvenanceOrder sets the layouts a PROVENANCE payload is tried in,
// as [provenance.Decode] takes them. Without it Decode uses
// [provenance.FullReaderOrder], as Atomic's Change::deserialize does;
// [provenance.SectionReaderOrder] reads the July 2026 layout instead
// (docs/remote/change-format/sections.md#july-2026-misread).
func WithProvenanceOrder(order []provenance.Layout) DecodeOption {
	return func(o *decodeOptions) { o.provenanceOrder = slices.Clone(order) }
}

// Verified is a change decoded from a change file whose hash has been
// verified, kept with the bytes that give it its identity. Re-encoding a
// Change need not reproduce those bytes, and so need not reproduce Hash;
// File.Bytes is what to store or send.
type Verified struct {
	// Hash is the change hash, the file's verified trailer.
	Hash ident.Hash

	// Change is the decoded change. It has passed [Change.Validate].
	Change Change

	// File is the change file the change was decoded from.
	File *changefile.File
}

// Decode reads the change file b, decompressing its payloads with dec,
// and decodes and validates the change it holds, with the checks and in
// the error order of Atomic's Change::deserialize
// (docs/remote/change-format/container.md#reader-validation).
//
// Each section's payload is decoded as soon as [changefile.Reader.Next]
// returns it, before the next section is read, so the first error reported
// is the first failure in file order: a DEPS payload that does not decode
// is reported before an unknown section type after it.
//
//   - HEADER and DEPS resolve their hash indices through the file's table.
//   - PROVENANCE is decoded in the order [WithProvenanceOrder] sets, and
//     Change.ProvenanceLayout records the layout that decoded.
//   - SIGNATURE is decoded with [DecodeSignature] and is not verified.
//   - Each GRAPH payload is decoded with [graph.Decode], and each of its
//     operations is checked with [graph.Check] against the table; the
//     operations of every GRAPH section are concatenated.
//   - SPAN is decoded with [span.Decode], which accepts only the canonical
//     encoding.
//   - The SEMANTIC sections' file operations, and the CONTENT chunks, are
//     concatenated in file order.
//   - UNHASHED is kept as JSON text, which must be valid JSON.
//
// In a version 1 file a repeated HEADER, DEPS, PROVENANCE, UNHASHED or
// SIGNATURE section replaces the earlier one, so the last wins; every one
// is still decoded, and the first that fails is reported
// (docs/remote/change-format/container.md#versioning).
//
// Once every section is decoded, [changefile.Reader.Finish] checks the
// trailer, the section manifest and any trailing bytes, and then
// [Change.Validate] runs.
//
// Every failure is a *[changefile.FormatError], or a
// *[changefile.HashMismatchError] for a trailer that does not match, but
// [changefile.ErrNilDecompressor] for a nil dec. A payload that does not
// decode is of kind [changefile.ErrPayload], wrapping the payload
// package's error where it has its own, such as [span.ErrNotCanonical] or
// [provenance.ErrMalformed]; the payload decoders' other kinds, such as
// [changefile.ErrHashIndexOutOfBounds], [changefile.ErrInvalidChangeMetadata]
// and [changefile.ErrInvalidGraphOp], are returned as they are.
func Decode(b []byte, dec changefile.Decompressor, opts ...DecodeOption) (*Verified, error) {
	o := decodeOptions{provenanceOrder: provenance.FullReaderOrder()}
	for _, opt := range opts {
		opt(&o)
	}
	r, err := changefile.NewReader(b, dec, o.read...)
	if err != nil {
		return nil, err
	}
	c := Change{Version: r.Header().Version, Table: r.Table()}
	for {
		s, ok, err := r.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if err := c.decodeSection(s, o); err != nil {
			return nil, err
		}
	}
	f, err := r.Finish()
	if err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Verified{Hash: f.Hash, Change: c, File: f}, nil
}

// decodeSection decodes the payload of s into c. A section the reader
// allows to repeat, which in version 2 and 3 files is only GRAPH, SEMANTIC
// and CONTENT, is added to what came before; any other replaces it.
func (c *Change) decodeSection(s changefile.Section, o decodeOptions) error {
	switch s.Type {
	case changefile.SectionHeader:
		env, err := decodeHeader(c.Version, s.Payload, c.Table)
		if err != nil {
			return err
		}
		c.Header = env.header
		c.Kind = env.kind
		c.Supersedes = env.supersedes
		c.Origin = env.origin
		c.CausalFrontier = env.causalFrontier
		c.ExtraKnown = env.extraKnown
		c.Metadata = env.metadata
	case changefile.SectionDeps:
		deps, err := decodeDeps(s.Payload, c.Table)
		if err != nil {
			return err
		}
		c.Dependencies = deps
	case changefile.SectionProvenance:
		records, layout, err := provenance.Decode(s.Payload, o.provenanceOrder)
		if err != nil {
			return sectionError(changefile.SectionProvenance, changefile.ErrPayload, "", err)
		}
		c.Provenance = nilIfEmpty(records)
		c.ProvenanceLayout = layout
	case changefile.SectionSignature:
		sig, err := DecodeSignature(s.Payload)
		if err != nil {
			return err
		}
		c.Signature = &sig
	case changefile.SectionGraph:
		p, err := graph.Decode(s.Payload, c.Version)
		if err != nil {
			return err
		}
		for _, op := range p.Ops {
			if err := graph.Check(op, c.Table); err != nil {
				return err
			}
		}
		c.Graph = append(c.Graph, p.Ops...)
	case changefile.SectionSpan:
		p, err := span.Decode(s.Payload)
		if err != nil {
			return sectionError(changefile.SectionSpan, changefile.ErrPayload, "", err)
		}
		c.Span = &p
	case changefile.SectionSemantic:
		ops, err := semantic.Decode(s.Payload)
		if err != nil {
			return err
		}
		c.Semantic = append(c.Semantic, ops...)
	case changefile.SectionContent:
		c.Contents = append(c.Contents, s.Payload...)
	case changefile.SectionUnhashed:
		if !json.Valid(s.Payload) {
			return sectionError(changefile.SectionUnhashed, changefile.ErrPayload, "not valid JSON", nil)
		}
		c.Unhashed = json.RawMessage(slices.Clone(s.Payload))
	}
	return nil
}

package change

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/z5labs/atomic-go/change/graph"
	"github.com/z5labs/atomic-go/change/provenance"
	"github.com/z5labs/atomic-go/change/semantic"
	"github.com/z5labs/atomic-go/change/span"
	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
)

var (
	// ErrNilChange reports a nil *[Change] given to [Encode]. It is a
	// mistake in the caller and is returned as it is, not as a
	// *changefile.FormatError.
	ErrNilChange = errors.New("change: nil Change")

	// ErrNilChunker reports a nil [Chunker] given to [Encode]. It is
	// returned as it is, not as a *changefile.FormatError.
	ErrNilChunker = errors.New("change: nil Chunker")
)

// Chunker splits a change's content blob into the chunks [Encode] writes
// as its CONTENT sections
// (docs/remote/change-format/container.md#content-chunking).
//
// Cut returns the offsets at which content is cut, in strictly ascending
// order and each strictly between 0 and len(content): the cuts [2] split
// "hello" into "he" and "llo", and no cuts leave it whole. Cut must not
// modify or keep content. Any split gives a valid file, but the split is
// hashed, so reproducing the change hash Atomic computes for content of
// more than 16384 bytes takes a FastCDC 2020 chunker with Atomic's
// parameters.
type Chunker interface {
	Cut(content []byte) []int
}

// WholeChunker is the [Chunker] that never cuts, so the content blob is
// one CONTENT chunk, or none when it is empty. It splits content of up to
// 16384 bytes as Atomic does.
type WholeChunker struct{}

// Cut returns no cuts.
func (WholeChunker) Cut([]byte) []int { return nil }

// Encode writes c as a change file in the layout of Atomic's
// Change::serialize, compressing each payload with comp and splitting the
// content blob with ch, and returns the file and its change hash. See
// docs/remote/change-format/container.md#writer-rules.
//
// The file is version 3 when c has a Span and version 2 otherwise, which
// is c.Version once c passes [Change.Validate]: Encode refuses a version 1
// change, as Atomic writes no version 1 file, and any change that fails
// Validate. It builds the hash dedup table as serialize does, adding to
// the zero hash in slot 0 the dependencies, then extra_known, then
// supersedes, then the causal frontier, and then every hash the graph
// operations name, in [graph.Indices] order, each at its first insertion.
// The graph operations' indices into c.Table are renumbered into that
// table with [graph.Remap], so c.Table need not be the table of the file
// written.
//
// It writes HEADER and DEPS; PROVENANCE when there are records; SIGNATURE
// when c.Signature is set; one GRAPH section holding every operation, with
// path "", content_start 0 and content_end len(c.Contents), when there are
// operations; SPAN in version 3; one SEMANTIC section when there are file
// operations; a CONTENT chunk for each piece ch cuts c.Contents into; and
// UNHASHED, as it is, when c.Unhashed is set. PROVENANCE is written in the
// current layout, whatever c.ProvenanceLayout says. Where comp writes the
// bytes libzstd would, and the payloads are those Atomic would write, the
// file is byte for byte the file Atomic writes; zstdframe.Stored does
// for payloads libzstd stores raw.
//
// Encode fails with [ErrNilChange], [changefile.ErrNilCompressor] or
// [ErrNilChunker] for a nil c, comp or ch. Every other failure is a
// *[changefile.FormatError]: of kind [changefile.ErrUnsupportedVersion]
// for a version 1 change; Validate's error for a change that fails it;
// of kind [changefile.ErrPayload] in [changefile.SectionContent] for
// content of 2^32 bytes or more, which a GRAPH position cannot address,
// or cuts that break the rules of [Chunker], and in
// [changefile.SectionUnhashed] for Unhashed that is not valid JSON; and
// otherwise the payload encoder's error, or the table's
// [changefile.ErrHashTableFull], as it is. A graph operation is checked
// with [graph.Check] against c.Table before it is renumbered.
func Encode(c *Change, comp changefile.Compressor, ch Chunker) ([]byte, ident.Hash, error) {
	switch {
	case c == nil:
		return nil, ident.Hash{}, ErrNilChange
	case comp == nil:
		return nil, ident.Hash{}, changefile.ErrNilCompressor
	case ch == nil:
		return nil, ident.Hash{}, ErrNilChunker
	}
	if c.Version == changefile.Version1 {
		return nil, ident.Hash{}, sectionError(0, changefile.ErrUnsupportedVersion, "Encode writes version 2 or 3, not 1", nil)
	}
	if err := c.Validate(); err != nil {
		return nil, ident.Hash{}, err
	}
	if err := checkContentLen(uint64(len(c.Contents))); err != nil {
		return nil, ident.Hash{}, err
	}
	chunks, err := cutContents(c.Contents, ch)
	if err != nil {
		return nil, ident.Hash{}, err
	}
	v := changefile.Version2
	if c.Span != nil {
		v = changefile.Version3
	}

	table, ops, err := buildTable(c)
	if err != nil {
		return nil, ident.Hash{}, err
	}
	sections, err := c.payloads(v, table, ops)
	if err != nil {
		return nil, ident.Hash{}, err
	}

	w, err := changefile.NewWriter(v, table, comp)
	if err != nil {
		return nil, ident.Hash{}, err
	}
	for _, s := range sections {
		if err := w.Add(s.typ, s.payload); err != nil {
			return nil, ident.Hash{}, err
		}
	}
	return finish(w, chunks, c.Unhashed)
}

// finish adds the CONTENT chunks and the UNHASHED section to w, which
// holds every section before them, and returns the file.
func finish(w *changefile.Writer, chunks [][]byte, unhashed json.RawMessage) ([]byte, ident.Hash, error) {
	for _, chunk := range chunks {
		if err := w.AddChunk(chunk); err != nil {
			return nil, ident.Hash{}, err
		}
	}
	if unhashed != nil {
		if err := w.Add(changefile.SectionUnhashed, unhashed); err != nil {
			return nil, ident.Hash{}, err
		}
	}
	return w.Finish()
}

// checkContentLen refuses a content blob of n bytes when n is 2^32 or
// more: a GRAPH position is a u32 offset into the blob, and Atomic's
// compactor truncates a larger one without a word.
func checkContentLen(n uint64) error {
	if n > math.MaxUint32 {
		return sectionError(changefile.SectionContent, changefile.ErrPayload, fmt.Sprintf("content of %d bytes is past the 2^32-byte reach of a graph position", n), nil)
	}
	return nil
}

// cutContents splits contents at the cuts ch returns, after checking them
// against the rules of [Chunker]. Empty contents are no chunks.
func cutContents(contents []byte, ch Chunker) ([][]byte, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	cuts := ch.Cut(contents)
	chunks := make([][]byte, 0, len(cuts)+1)
	prev := 0
	for i, cut := range cuts {
		if cut <= prev || cut >= len(contents) {
			return nil, sectionError(changefile.SectionContent, changefile.ErrPayload, fmt.Sprintf("cut %d is at %d, which is not between %d and %d", i, cut, prev, len(contents)), nil)
		}
		chunks = append(chunks, contents[prev:cut])
		prev = cut
	}
	return append(chunks, contents[prev:]), nil
}

// buildTable returns the hash dedup table of c as Change::serialize builds
// it, and c's graph operations renumbered into it.
func buildTable(c *Change) (changefile.HashTable, []graph.Op, error) {
	var b changefile.TableBuilder
	add := func(hs ...ident.Hash) error {
		for _, h := range hs {
			if _, err := b.Add(h); err != nil {
				return err
			}
		}
		return nil
	}
	if err := add(c.Dependencies...); err != nil {
		return changefile.HashTable{}, nil, err
	}
	if err := add(c.ExtraKnown...); err != nil {
		return changefile.HashTable{}, nil, err
	}
	if c.Supersedes != nil {
		if err := add(*c.Supersedes); err != nil {
			return changefile.HashTable{}, nil, err
		}
	}
	if err := add(c.CausalFrontier...); err != nil {
		return changefile.HashTable{}, nil, err
	}

	// renumber maps an index into c.Table to the index of the same hash
	// in the table being built.
	renumber := make(map[changefile.HashIndex]changefile.HashIndex)
	for _, op := range c.Graph {
		if err := graph.Check(op, c.Table); err != nil {
			return changefile.HashTable{}, nil, err
		}
		for old := range graph.Indices(op) {
			if _, ok := renumber[old]; ok {
				continue
			}
			h, err := graph.Resolve(c.Table, old)
			if err != nil {
				return changefile.HashTable{}, nil, err
			}
			i, err := b.Add(h.Hash)
			if err != nil {
				return changefile.HashTable{}, nil, err
			}
			renumber[old] = i
		}
	}
	var ops []graph.Op
	for n, op := range c.Graph {
		// Remap and Indices walk an op separately. An index Remap visits
		// that Indices did not yield would otherwise become 0, ROOT.
		var missing []changefile.HashIndex
		ops = append(ops, graph.Remap(op, func(i changefile.HashIndex) changefile.HashIndex {
			j, ok := renumber[i]
			if !ok {
				missing = append(missing, i)
			}
			return j
		}))
		if len(missing) > 0 {
			return changefile.HashTable{}, nil, sectionError(changefile.SectionGraph, changefile.ErrInvalidGraphOp, fmt.Sprintf("op %d: hash index %d was not among the indices it was found to hold", n, missing[0]), nil)
		}
	}
	return b.Table(), ops, nil
}

// payload is a section's type and decompressed payload.
type payload struct {
	typ     changefile.SectionType
	payload []byte
}

// payloads returns the decompressed payloads of every section of c before
// CONTENT, in the order a file holds them, with hashes named by their
// index in t and ops, renumbered into t, as the graph operations.
func (c *Change) payloads(v changefile.Version, t changefile.HashTable, ops []graph.Op) ([]payload, error) {
	var out []payload
	add := func(st changefile.SectionType, b []byte) { out = append(out, payload{typ: st, payload: b}) }

	header, err := appendHeader(nil, v, envelope{
		header:         c.Header,
		kind:           c.Kind,
		supersedes:     c.Supersedes,
		origin:         c.Origin,
		causalFrontier: c.CausalFrontier,
		extraKnown:     c.ExtraKnown,
		metadata:       c.Metadata,
	}, t)
	if err != nil {
		return nil, err
	}
	add(changefile.SectionHeader, header)

	deps := make([]changefile.HashIndex, 0, len(c.Dependencies))
	for _, h := range c.Dependencies {
		// Every dependency was added to t first.
		i, _ := t.IndexOf(h)
		deps = append(deps, i)
	}
	b, err := appendDeps(nil, deps)
	if err != nil {
		return nil, err
	}
	add(changefile.SectionDeps, b)

	if len(c.Provenance) > 0 {
		b, err := provenance.AppendPayload(nil, c.Provenance)
		if err != nil {
			return nil, sectionError(changefile.SectionProvenance, changefile.ErrPayload, "", err)
		}
		add(changefile.SectionProvenance, b)
	}
	if c.Signature != nil {
		b, err := c.Signature.AppendPayload(nil)
		if err != nil {
			return nil, err
		}
		add(changefile.SectionSignature, b)
	}
	if len(ops) > 0 {
		b, err := graph.AppendPayload(nil, graph.Payload{Ops: ops, ContentEnd: uint64(len(c.Contents))})
		if err != nil {
			return nil, err
		}
		add(changefile.SectionGraph, b)
	}
	if v == changefile.Version3 {
		b, err := span.AppendPayload(nil, *c.Span)
		if err != nil {
			return nil, sectionError(changefile.SectionSpan, changefile.ErrPayload, "", err)
		}
		add(changefile.SectionSpan, b)
	}
	if len(c.Semantic) > 0 {
		b, err := semantic.AppendPayload(nil, c.Semantic)
		if err != nil {
			return nil, err
		}
		add(changefile.SectionSemantic, b)
	}
	if c.Unhashed != nil && !json.Valid(c.Unhashed) {
		return nil, sectionError(changefile.SectionUnhashed, changefile.ErrPayload, "not valid JSON", nil)
	}
	return out, nil
}

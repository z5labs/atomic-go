package change

import (
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/change/graph"
	"github.com/z5labs/atomic-go/change/provenance"
	"github.com/z5labs/atomic-go/change/semantic"
	"github.com/z5labs/atomic-go/change/span"
	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/changefixture"
	"github.com/z5labs/atomic-go/zstdframe"
)

// unhex decodes the hex text s, which may hold spaces and newlines.
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	require.NoError(t, err)
	return b
}

// exampleGraphPayload is the 97-byte GRAPH payload of atomic 0.19.1's
// file_add fixture, transcribed from
// docs/remote/change-format/graph.md#example-graph-payload.
func exampleGraphPayload(t *testing.T) []byte {
	t.Helper()
	b := unhex(t, `
		00 03
		00
		01 00 00 00 11 00 05 00 00
		01 ff ff 03 05 00 11 05 05 ff ff 03 05
		01 01 ff ff 03 05 00 01 05 0b ff ff 03 05
		05 61 2e 74 78 74
		01 00
		07 00 01 ff ff 03 0b 00 01 0b 10 ff ff 03 05 05 61 2e 74 78 74 02 01 00
		07 00 01 ff ff 03 10 00 01 10 16 ff ff 03 05 05 61 2e 74 78 74 03 01 00
		00 16`)
	require.Len(t, b, 97)
	return b
}

// exampleSemanticPayload is the 83-byte SEMANTIC payload of the same
// fixture, transcribed from
// docs/remote/change-format/graph.md#example-semantic-payload.
func exampleSemanticPayload(t *testing.T) []byte {
	t.Helper()
	b := unhex(t, `
		01
		00 00
		05 61 2e 74 78 74
		01 00
		05 61 2e 74 78 74
		01 00
		03
		00 00 00 00 01 00 00 00 05 61 6c 70 68 61 00 01 01 01 05 0b
		00 01 00 01 00 00 01 00 00 00 04 62 65 74 61 00 01 02 01 0b 10
		00 02 00 01 00 01 01 00 00 00 05 67 61 6d 6d 61 00 01 03 01 10 16`)
	require.Len(t, b, 83)
	return b
}

// exampleContents is the content blob the example payloads index.
const exampleContents = "a.txtalpha\nbeta\ngamma\n"

// workedExampleChange is the change of changefixture.WorkedExample, built
// by hand from docs/remote/change-format/sections.md#header-example.
func workedExampleChange() *Change {
	return &Change{
		Version: changefile.Version2,
		Header: Header{
			Message:   "opaque v2 fixture",
			Timestamp: time.Unix(1700000000, 0).UTC(),
		},
		Metadata: []byte("ATSP\x01\x00\x00"),
		Contents: []byte(changefixture.WorkedExampleContent),
	}
}

func encode(t *testing.T, c *Change) ([]byte, ident.Hash) {
	t.Helper()
	b, h, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
	require.NoError(t, err)
	return b, h
}

func TestEncodeWorkedExample(t *testing.T) {
	want, err := ident.ParseHash(changefixture.WorkedExampleHash)
	require.NoError(t, err)

	t.Run("built by hand", func(t *testing.T) {
		b, h := encode(t, workedExampleChange())
		require.Equal(t, changefixture.WorkedExample(), b)
		require.Equal(t, want, h)
	})

	t.Run("decoded", func(t *testing.T) {
		v, err := Decode(changefixture.WorkedExample(), newDecoder(t))
		require.NoError(t, err)
		b, h := encode(t, &v.Change)
		require.Equal(t, changefixture.WorkedExample(), b)
		require.Equal(t, want, h)
	})
}

// richChange returns a version 3 change that has every section, with a
// Table that is the one Encode builds for it, so that decoding the file
// Encode writes gives the change back.
func richChange(t *testing.T) *Change {
	t.Helper()
	p, err := graph.Decode(exampleGraphPayload(t), changefile.Version2)
	require.NoError(t, err)
	fileOps, err := semantic.Decode(exampleSemanticPayload(t))
	require.NoError(t, err)

	deps := []ident.Hash{hashOf(1), hashOf(2)}
	extra := []ident.Hash{hashOf(3)}
	table, err := changefile.NewHashTable(slices.Concat([]ident.Hash{{}}, deps, extra))
	require.NoError(t, err)

	sig := exampleSignature(t)
	return &Change{
		Version: changefile.Version3,
		Header: Header{
			Message:       "add a.txt",
			Description:   ptr("a longer body"),
			Timestamp:     time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC),
			TimestampText: "2023-11-14T22:13:20Z",
			Authors:       []Author{{Name: "ada", Email: ptr("ada@example.com")}},
		},
		Origin: Origin{
			Kind:       GitSynthesized,
			Commit:     sha1Commit(7),
			Parents:    []GitObjectID{sha1Commit(8)},
			Derivation: DerivationFirstParent,
		},
		ExtraKnown:   extra,
		Metadata:     []byte("app"),
		Dependencies: deps,
		Provenance:   []provenance.Record{{Model: "m", Prompt: provenance.Prompt{Kind: provenance.PromptNone}}},
		Signature:    &sig,
		Table:        table,
		Graph:        p.Ops,
		Span:         &span.Patch{Version: span.V2, Ops: []span.Op{span.Root{Path: "a.txt", Start: 0, End: 5}}},
		Semantic:     fileOps,
		Contents:     []byte(exampleContents),
		Unhashed:     json.RawMessage(`{"git":{"a":1}}`),
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	c := richChange(t)
	b, h := encode(t, c)

	v, err := Decode(b, newDecoder(t))
	require.NoError(t, err)
	require.Equal(t, h, v.Hash)
	require.Equal(t, *c, v.Change)

	var types []changefile.SectionType
	for _, s := range v.File.Sections {
		types = append(types, s.Type)
	}
	require.Equal(t, []changefile.SectionType{
		changefile.SectionHeader, changefile.SectionDeps, changefile.SectionProvenance,
		changefile.SectionSignature, changefile.SectionGraph, changefile.SectionSpan,
		changefile.SectionSemantic, changefile.SectionContent, changefile.SectionUnhashed,
	}, types)
	require.Equal(t, changefile.Version3, v.File.Header.Version)

	// Encoding the decoded change writes the same file again.
	again, againHash := encode(t, &v.Change)
	require.Equal(t, b, again)
	require.Equal(t, h, againHash)
}

func TestEncodeVersion2WithoutSpan(t *testing.T) {
	c := richChange(t)
	c.Version = changefile.Version2
	c.Span = nil
	b, _ := encode(t, c)

	v, err := Decode(b, newDecoder(t))
	require.NoError(t, err)
	require.Equal(t, changefile.Version2, v.File.Header.Version)
	require.Equal(t, *c, v.Change)
}

func TestEncodeTranscribedPayloads(t *testing.T) {
	b, _ := encode(t, richChange(t))
	v, err := Decode(b, newDecoder(t))
	require.NoError(t, err)

	payloads := map[changefile.SectionType][]byte{}
	for _, s := range v.File.Sections {
		payloads[s.Type] = s.Payload
	}
	// One GRAPH section, with path "", content_start 0 and content_end
	// the length of the content blob, as atomic 0.19.1 wrote it.
	require.Equal(t, exampleGraphPayload(t), payloads[changefile.SectionGraph])
	require.Equal(t, exampleSemanticPayload(t), payloads[changefile.SectionSemantic])
	require.Equal(t, []byte(exampleContents), payloads[changefile.SectionContent])
}

// setAttr returns a SetAttr whose inode is in the change table index i
// names.
func setAttr(i changefile.HashIndex) graph.Op {
	return graph.SetAttr{Inode: graph.Position{Change: i}, Path: "a", Value: graph.Mode(0o644)}
}

func TestEncodeTableOrder(t *testing.T) {
	d1, d2, e, f, g1, g2 := hashOf(1), hashOf(2), hashOf(3), hashOf(4), hashOf(5), hashOf(6)

	t.Run("dependencies, extra_known, frontier, then graph hashes", func(t *testing.T) {
		// The change's own table holds the graph hashes in another
		// order, and a dependency the graph also names.
		own, err := changefile.NewHashTable([]ident.Hash{{}, g2, d1, g1})
		require.NoError(t, err)
		c := &Change{
			Version:        changefile.Version2,
			Origin:         Origin{Kind: GitResolution, Commit: sha1Commit(1), Parents: []GitObjectID{sha1Commit(2), sha1Commit(3)}},
			CausalFrontier: []ident.Hash{f},
			ExtraKnown:     []ident.Hash{e},
			Dependencies:   []ident.Hash{d1, d2},
			Table:          own,
			Graph:          []graph.Op{setAttr(3), setAttr(2), setAttr(1), setAttr(3), setAttr(changefile.ThisChange)},
		}
		b, _ := encode(t, c)
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)

		require.Equal(t, []ident.Hash{{}, d1, d2, e, f, g1, g2}, slices.Collect(tableHashes(v.Change.Table)))
		require.Equal(t, []graph.Op{setAttr(5), setAttr(1), setAttr(6), setAttr(5), setAttr(changefile.ThisChange)}, v.Change.Graph)
		require.Equal(t, c.Dependencies, v.Change.Dependencies)
		require.Equal(t, c.CausalFrontier, v.Change.CausalFrontier)

		// The input is not changed.
		require.Equal(t, []graph.Op{setAttr(3), setAttr(2), setAttr(1), setAttr(3), setAttr(changefile.ThisChange)}, c.Graph)
	})

	t.Run("supersedes comes after extra_known", func(t *testing.T) {
		s := hashOf(9)
		c := &Change{
			Version:      changefile.Version2,
			Kind:         Kind{Snapshot: true, WorkingCopy: [16]byte{1}},
			Supersedes:   &s,
			ExtraKnown:   []ident.Hash{e},
			Dependencies: []ident.Hash{d1},
		}
		b, _ := encode(t, c)
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, []ident.Hash{{}, d1, e, s}, slices.Collect(tableHashes(v.Change.Table)))
		require.Equal(t, &s, v.Change.Supersedes)
	})
}

func tableHashes(t changefile.HashTable) func(func(ident.Hash) bool) {
	return func(yield func(ident.Hash) bool) {
		for _, h := range t.All() {
			if !yield(h) {
				return
			}
		}
	}
}

// fixedCuts is a Chunker that returns the same cuts for any content.
type fixedCuts []int

func (f fixedCuts) Cut([]byte) []int { return f }

func TestEncodeChunks(t *testing.T) {
	hello := func() *Change {
		c := workedExampleChange()
		c.Contents = []byte("hello")
		return c
	}

	t.Run("he|llo stays valid", func(t *testing.T) {
		b, h, err := Encode(hello(), zstdframe.Stored{}, fixedCuts{2})
		require.NoError(t, err)
		v, err := Decode(b, newDecoder(t), WithReadOptions(changefile.CheckChunks()))
		require.NoError(t, err)
		require.Equal(t, h, v.Hash)
		require.Equal(t, []byte("hello"), v.Change.Contents)

		var chunks [][]byte
		for _, s := range v.File.Sections {
			if s.Type == changefile.SectionContent {
				chunks = append(chunks, s.Payload)
			}
		}
		require.Equal(t, [][]byte{[]byte("he"), []byte("llo")}, chunks)
		require.Equal(t, uint32(2), v.File.Header.ContentChunks)

		// The split is hashed.
		_, whole := encode(t, hello())
		require.NotEqual(t, whole, h)
	})

	t.Run("empty content is no chunks, whatever the cuts", func(t *testing.T) {
		c := hello()
		c.Contents = nil
		b, _, err := Encode(c, zstdframe.Stored{}, fixedCuts{7})
		require.NoError(t, err)
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, uint32(0), v.File.Header.ContentChunks)
	})

	for name, cuts := range map[string]fixedCuts{
		"a cut at 0":           {0},
		"a cut at the end":     {5},
		"a cut past the end":   {6},
		"a negative cut":       {-1},
		"a repeated cut":       {2, 2},
		"cuts out of order":    {3, 2},
		"a cut after the last": {2, 5},
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			_, _, err := Encode(hello(), zstdframe.Stored{}, cuts)
			requireFormatError(t, err, changefile.ErrPayload, changefile.SectionContent)
		})
	}
}

func TestCheckContentLen(t *testing.T) {
	require.NoError(t, checkContentLen(1<<32-1))
	requireFormatError(t, checkContentLen(1<<32), changefile.ErrPayload, changefile.SectionContent)
}

// recorder is a Compressor that records each payload it is given and
// stores it as zstdframe.Stored does.
type recorder struct{ payloads [][]byte }

func (r *recorder) Compress(dst, src []byte) []byte {
	r.payloads = append(r.payloads, slices.Clone(src))
	return zstdframe.Stored{}.Compress(dst, src)
}

func TestEncodeCompressesEachPayload(t *testing.T) {
	c := richChange(t)
	var r recorder
	b, h, err := Encode(c, &r, fixedCuts{5})
	require.NoError(t, err)

	v, err := Decode(b, newDecoder(t))
	require.NoError(t, err)
	require.Equal(t, h, v.Hash)

	// Each section's decompressed payload was compressed on its own, in
	// file order.
	var want [][]byte
	for _, s := range v.File.Sections {
		want = append(want, s.Payload)
	}
	require.Len(t, r.payloads, 10)
	require.Equal(t, want, r.payloads)
	require.Equal(t, []byte("a.txt"), r.payloads[7])
	require.Equal(t, []byte("alpha\nbeta\ngamma\n"), r.payloads[8])
}

func TestEncodeRefuses(t *testing.T) {
	t.Run("nil arguments", func(t *testing.T) {
		_, _, err := Encode(nil, zstdframe.Stored{}, WholeChunker{})
		require.ErrorIs(t, err, ErrNilChange)
		_, _, err = Encode(workedExampleChange(), nil, WholeChunker{})
		require.ErrorIs(t, err, changefile.ErrNilCompressor)
		_, _, err = Encode(workedExampleChange(), zstdframe.Stored{}, nil)
		require.ErrorIs(t, err, ErrNilChunker)
	})

	t.Run("a version 1 change", func(t *testing.T) {
		c := workedExampleChange()
		c.Version = changefile.Version1
		c.Metadata = nil
		require.NoError(t, c.Validate())
		_, _, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
		requireFormatError(t, err, changefile.ErrUnsupportedVersion, 0)
	})

	t.Run("an unsupported version", func(t *testing.T) {
		c := workedExampleChange()
		c.Version = 0
		_, _, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
		requireFormatError(t, err, changefile.ErrValidation, 0)
	})

	t.Run("unsorted dependencies", func(t *testing.T) {
		c := workedExampleChange()
		c.Dependencies = []ident.Hash{hashOf(2), hashOf(1)}
		_, _, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
		requireFormatError(t, err, changefile.ErrValidation, changefile.SectionDeps)
	})

	t.Run("a span in a version 2 change", func(t *testing.T) {
		c := richChange(t)
		c.Version = changefile.Version2
		_, _, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
		requireFormatError(t, err, changefile.ErrValidation, changefile.SectionSpan)
	})

	t.Run("a graph index outside the change's table", func(t *testing.T) {
		c := workedExampleChange()
		c.Table = emptyTable(t)
		c.Graph = []graph.Op{setAttr(4)}
		_, _, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
		requireFormatError(t, err, changefile.ErrHashIndexOutOfBounds, changefile.SectionGraph)
	})

	t.Run("an invalid graph op", func(t *testing.T) {
		c := workedExampleChange()
		c.Table = emptyTable(t)
		c.Graph = []graph.Op{setAttr(0)}
		_, _, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
		requireFormatError(t, err, changefile.ErrInvalidGraphOp, changefile.SectionGraph)
	})

	t.Run("unhashed text that is not JSON", func(t *testing.T) {
		c := workedExampleChange()
		c.Unhashed = json.RawMessage(`{"git":`)
		_, _, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
		requireFormatError(t, err, changefile.ErrPayload, changefile.SectionUnhashed)
	})

	t.Run("an invalid provenance record", func(t *testing.T) {
		c := workedExampleChange()
		c.Provenance = []provenance.Record{{Prompt: provenance.Prompt{Kind: provenance.PromptHashed}}}
		_, _, err := Encode(c, zstdframe.Stored{}, WholeChunker{})
		requireFormatError(t, err, changefile.ErrPayload, changefile.SectionProvenance)
		require.ErrorIs(t, err, provenance.ErrInvalidRecord)
	})
}

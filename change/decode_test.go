package change

import (
	"encoding/binary"
	"encoding/json"
	"testing"

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

func newDecoder(t *testing.T) *zstdframe.Decoder {
	t.Helper()
	dec, err := zstdframe.NewDecoder(zstdframe.AtomicWindow)
	require.NoError(t, err)
	t.Cleanup(dec.Close)
	return dec
}

// sha256Parent returns a SHA-256 Git object id holding the bytes of h.
func sha256Parent(h ident.Hash) GitObjectID {
	return GitObjectID{SHA256: true, Bytes: h[:]}
}

// sha1Commit returns a SHA-1 Git object id of 20 bytes b.
func sha1Commit(b byte) GitObjectID {
	id := make([]byte, SHA1Size)
	for i := range id {
		id[i] = b
	}
	return GitObjectID{Bytes: id}
}

// spec describes a change for [writeChange] to write with
// changefile.Writer and zstdframe.Stored.
type spec struct {
	version changefile.Version
	env     envelope
	// table is the file's table after its zero slot; deps are indices
	// into it.
	table []ident.Hash
	deps  []changefile.HashIndex
	// extra, after DEPS, are further sections, in order.
	extra []section
}

type section struct {
	t       changefile.SectionType
	payload []byte
}

// validEnv returns an envelope that passes every validation rule.
func validEnv() envelope {
	return envelope{header: headerM()}
}

func writeChange(t *testing.T, s spec) []byte {
	t.Helper()
	tab, err := changefile.NewHashTable(append([]ident.Hash{{}}, s.table...))
	require.NoError(t, err)
	header, err := appendHeader(nil, s.version, s.env, tab)
	require.NoError(t, err)
	deps, err := appendDeps(nil, s.deps)
	require.NoError(t, err)
	w, err := changefile.NewWriter(s.version, tab, zstdframe.Stored{})
	require.NoError(t, err)
	require.NoError(t, w.Add(changefile.SectionHeader, header))
	require.NoError(t, w.Add(changefile.SectionDeps, deps))
	for _, x := range s.extra {
		if x.t == changefile.SectionContent {
			require.NoError(t, w.AddChunk(x.payload))
			continue
		}
		require.NoError(t, w.Add(x.t, x.payload))
	}
	b, _, err := w.Finish()
	require.NoError(t, err)
	return b
}

// rawSection returns a 5-byte-framed section of type t holding payload as
// a stored zstd frame, or a 45-byte-framed chunk for CONTENT.
func rawSection(t changefile.SectionType, payload []byte) []byte {
	c := zstdframe.Stored{}.Compress(nil, payload)
	b := []byte{byte(t)}
	le := binary.LittleEndian
	if t == changefile.SectionContent {
		h := ident.Sum(payload)
		b = le.AppendUint32(b, 0)
		b = append(b, h[:]...)
		b = le.AppendUint32(b, uint32(len(payload)))
	}
	b = le.AppendUint32(b, uint32(len(c)))
	return append(b, c...)
}

// buildFile returns a file with header h, the table zero followed by
// table, and sections as they are, ending with the right trailer. Unlike
// writeChange it writes whatever it is given, so it can build files no
// Writer would.
func buildFile(t *testing.T, h changefile.FileHeader, table []ident.Hash, sections ...[]byte) []byte {
	t.Helper()
	entries := append([]ident.Hash{{}}, table...)
	h.HashTableEntries = uint32(len(entries))
	b, err := h.AppendBinary(nil)
	require.NoError(t, err)
	hasher := ident.NewHasher()
	for _, e := range entries {
		b = append(b, e[:]...)
		hasher.Write(e[:])
	}
	for _, s := range sections {
		b = append(b, s...)
		if changefile.SectionType(s[0]).Hashed() {
			hasher.Write(s)
		}
	}
	sum := hasher.Sum()
	return append(b, sum[:]...)
}

func headerV1Payload(t *testing.T, message string) []byte {
	t.Helper()
	h := headerM()
	h.Message = message
	b, err := appendHeaderV1(nil, h)
	require.NoError(t, err)
	return b
}

func TestDecodeWorkedExample(t *testing.T) {
	v, err := Decode(changefixture.WorkedExample(), newDecoder(t))
	require.NoError(t, err)

	want, err := ident.ParseHash(changefixture.WorkedExampleHash)
	require.NoError(t, err)
	require.Equal(t, want, v.Hash)
	require.Equal(t, want, v.File.Hash)
	require.Equal(t, changefixture.WorkedExample(), v.File.Bytes())

	c := v.Change
	require.Equal(t, changefile.Version2, c.Version)
	require.Equal(t, "opaque v2 fixture", c.Header.Message)
	require.Equal(t, int64(1700000000), c.Header.Timestamp.Unix())
	require.Equal(t, Kind{}, c.Kind)
	require.Equal(t, Origin{}, c.Origin)
	require.Nil(t, c.Supersedes)
	require.Nil(t, c.Dependencies)

	// The metadata begins with ATSP, and is still only metadata.
	require.Equal(t, []byte("ATSP\x01\x00\x00"), c.Metadata)
	require.Nil(t, c.Span)

	require.Equal(t, []byte(changefixture.WorkedExampleContent), c.Contents)
	require.Equal(t, ident.Sum([]byte(changefixture.WorkedExampleContent)), c.ContentsHash())
	require.Nil(t, c.Provenance)
	require.Nil(t, c.Signature)
	require.Nil(t, c.Graph)
	require.Nil(t, c.Semantic)
	require.Nil(t, c.Unhashed)
	require.NoError(t, c.Validate())
}

func TestDecodeNilDecompressor(t *testing.T) {
	_, err := Decode(changefixture.WorkedExample(), nil)
	require.ErrorIs(t, err, changefile.ErrNilDecompressor)
}

func TestDecodeValidation(t *testing.T) {
	h1, h2, h3 := hashOf(1), hashOf(2), hashOf(3)
	resolution := func(parents ...GitObjectID) Origin {
		return Origin{Kind: GitResolution, Commit: sha256Parent(hashOf(9)), Parents: parents}
	}

	testCases := []struct {
		name    string
		spec    spec
		section changefile.SectionType
		detail  string
	}{
		{
			name: "unsorted dependencies",
			spec: spec{
				version: changefile.Version2, env: validEnv(),
				table: []ident.Hash{h3, h1}, deps: []changefile.HashIndex{1, 2},
			},
			section: changefile.SectionDeps,
			detail:  "dependencies is not in strictly ascending hash order",
		},
		{
			name: "extra_known overlapping the dependencies",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.extraKnown = []ident.Hash{h1}
					return e
				}(),
				table: []ident.Hash{h1}, deps: []changefile.HashIndex{1},
			},
			section: changefile.SectionHeader,
			detail:  "extra_known 0 is also dependency 0",
		},
		{
			name: "supersedes on a Durable change",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.supersedes = &h2
					return e
				}(),
				table: []ident.Hash{h2},
			},
			section: changefile.SectionHeader,
			detail:  "supersedes is set on a Durable change",
		},
		{
			name: "supersedes naming a dependency",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.kind = Kind{Snapshot: true, WorkingCopy: [16]byte{1}}
					e.supersedes = &h1
					return e
				}(),
				table: []ident.Hash{h1}, deps: []changefile.HashIndex{1},
			},
			section: changefile.SectionHeader,
			detail:  "supersedes names dependency 0",
		},
		{
			name: "a Snapshot with a Git origin",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.kind = Kind{Snapshot: true}
					e.origin = Origin{Kind: GitSynthesized, Commit: sha1Commit(7), Derivation: DerivationRoot}
					return e
				}(),
			},
			section: changefile.SectionHeader,
			detail:  "a Snapshot has a GitSynthesized origin",
		},
		{
			name: "a Git origin with a Snapshot kind",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.kind = Kind{Snapshot: true}
					e.origin = resolution(sha256Parent(h2), sha256Parent(h3))
					e.causalFrontier = []ident.Hash{h1}
					return e
				}(),
				table: []ident.Hash{h1},
			},
			section: changefile.SectionHeader,
			detail:  "a Snapshot has a GitResolution origin",
		},
		{
			name: "a causal frontier on a Native origin",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.causalFrontier = []ident.Hash{h1}
					return e
				}(),
				table: []ident.Hash{h1},
			},
			section: changefile.SectionHeader,
			detail:  "a Native origin has a non-empty causal_frontier",
		},
		{
			name: "an empty causal frontier on a GitResolution",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.origin = resolution(sha256Parent(h2), sha256Parent(h3))
					return e
				}(),
			},
			section: changefile.SectionHeader,
			detail:  "a GitResolution has an empty causal_frontier",
		},
		{
			name: "a GitResolution with one parent",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.origin = resolution(sha256Parent(h2))
					e.causalFrontier = []ident.Hash{h1}
					return e
				}(),
				table: []ident.Hash{h1},
			},
			section: changefile.SectionHeader,
			detail:  "a GitResolution has 1 parents",
		},
		{
			name: "a SHA-256 parent equal to a dependency",
			spec: spec{
				version: changefile.Version2,
				env: func() envelope {
					e := validEnv()
					e.origin = resolution(sha256Parent(h2), sha256Parent(h1))
					e.causalFrontier = []ident.Hash{h3}
					return e
				}(),
				table: []ident.Hash{h1, h3}, deps: []changefile.HashIndex{1},
			},
			section: changefile.SectionHeader,
			detail:  "Git parent 1 is dependency 0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := writeChange(t, tc.spec)

			_, err := Decode(b, newDecoder(t))
			requireFormatError(t, err, changefile.ErrValidation, tc.section)
			require.ErrorContains(t, err, tc.detail)

			// The file itself is sound: only the change is refused.
			_, err = changefile.Parse(b, newDecoder(t))
			require.NoError(t, err)
		})
	}

	t.Run("a valid GitResolution passes", func(t *testing.T) {
		e := validEnv()
		e.origin = resolution(sha256Parent(h2), sha256Parent(h3))
		e.causalFrontier = []ident.Hash{h1}
		b := writeChange(t, spec{
			version: changefile.Version2, env: e,
			table: []ident.Hash{h1}, deps: []changefile.HashIndex{1},
		})
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, []ident.Hash{h1}, v.Change.Dependencies)
		require.Equal(t, []ident.Hash{h1}, v.Change.CausalFrontier)
		require.Equal(t, GitResolution, v.Change.Origin.Kind)
	})
}

func spanPayload(t *testing.T, p span.Patch) []byte {
	t.Helper()
	b, err := span.AppendPayload(nil, p)
	require.NoError(t, err)
	return b
}

func TestDecodeSpan(t *testing.T) {
	root := span.Patch{Version: span.V2, Ops: []span.Op{span.Root{Path: "a", Start: 0, End: 4}}}

	t.Run("a version 3 file with a SPAN decodes", func(t *testing.T) {
		b := writeChange(t, spec{
			version: changefile.Version3, env: validEnv(),
			extra: []section{
				{changefile.SectionSpan, spanPayload(t, root)},
				{changefile.SectionContent, []byte("abcd")},
			},
		})
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.NotNil(t, v.Change.Span)
		require.Equal(t, root, *v.Change.Span)
		require.Equal(t, []byte("abcd"), v.Change.Contents)
	})

	t.Run("a span range past the contents", func(t *testing.T) {
		b := writeChange(t, spec{
			version: changefile.Version3, env: validEnv(),
			extra: []section{
				{changefile.SectionSpan, spanPayload(t, root)},
				{changefile.SectionContent, []byte("abc")},
			},
		})
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrValidation, changefile.SectionSpan)
		require.ErrorIs(t, err, span.ErrInvalidOp)
	})

	header, err := appendHeaderV2(nil, validEnv(), emptyTable(t))
	require.NoError(t, err)
	deps := []byte{0}

	t.Run("a version 3 file without a SPAN", func(t *testing.T) {
		// A CONTENT chunk stands where the SPAN should be, so the
		// reader frames the right number of sections and reaches the
		// manifest check.
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version3}, nil,
			rawSection(changefile.SectionHeader, header),
			rawSection(changefile.SectionDeps, deps),
			rawSection(changefile.SectionContent, []byte("x")),
		)
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrInvalidHeader, changefile.SectionSpan)
	})

	t.Run("a version 2 file with a SPAN", func(t *testing.T) {
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version2, GraphSections: 1}, nil,
			rawSection(changefile.SectionHeader, header),
			rawSection(changefile.SectionDeps, deps),
			rawSection(changefile.SectionSpan, spanPayload(t, span.Patch{Version: span.V2})),
		)
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrUnexpectedSection, changefile.SectionSpan)
	})

	t.Run("a non-canonical SPAN", func(t *testing.T) {
		payload := append(spanPayload(t, span.Patch{Version: span.V2}), 0x00)
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version3}, nil,
			rawSection(changefile.SectionHeader, header),
			rawSection(changefile.SectionDeps, deps),
			rawSection(changefile.SectionSpan, payload),
		)
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrPayload, changefile.SectionSpan)
		require.ErrorIs(t, err, span.ErrNotCanonical)
	})
}

func TestDecodeSectionCountMismatch(t *testing.T) {
	header, err := appendHeaderV2(nil, validEnv(), emptyTable(t))
	require.NoError(t, err)

	// The header declares a GRAPH section, and a CONTENT chunk stands in
	// its place.
	b := buildFile(t, changefile.FileHeader{Version: changefile.Version2, GraphSections: 1}, nil,
		rawSection(changefile.SectionHeader, header),
		rawSection(changefile.SectionDeps, []byte{0}),
		rawSection(changefile.SectionContent, []byte("x")),
	)
	_, err = Decode(b, newDecoder(t))
	requireFormatError(t, err, changefile.ErrInvalidHeader, changefile.SectionGraph)
}

func TestDecodeVersion1(t *testing.T) {
	h1, h2 := hashOf(1), hashOf(2)

	t.Run("no HEADER", func(t *testing.T) {
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version1}, nil,
			rawSection(changefile.SectionDeps, []byte{0}),
			rawSection(changefile.SectionDeps, []byte{0}),
		)
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrInvalidHeader, changefile.SectionHeader)
	})

	t.Run("the last of each repeated section wins", func(t *testing.T) {
		// Two repeated sections make the file two longer than the two a
		// version 1 header frames, so the header declares two CONTENT
		// chunks, which version 1 does not check against what it holds.
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version1, ContentChunks: 2}, []ident.Hash{h1, h2},
			rawSection(changefile.SectionHeader, headerV1Payload(t, "first")),
			rawSection(changefile.SectionDeps, []byte{1, 1}),
			rawSection(changefile.SectionHeader, headerV1Payload(t, "second")),
			rawSection(changefile.SectionDeps, []byte{1, 2}),
		)
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, changefile.Version1, v.Change.Version)
		require.Equal(t, "second", v.Change.Header.Message)
		require.Equal(t, []ident.Hash{h2}, v.Change.Dependencies)
	})

	t.Run("the last PROVENANCE, SIGNATURE and UNHASHED win", func(t *testing.T) {
		first := exampleSignature(t)
		second := first
		second.Timestamp++
		firstPayload, err := first.AppendPayload(nil)
		require.NoError(t, err)
		secondPayload, err := second.AppendPayload(nil)
		require.NoError(t, err)

		// Eight sections, six more than a version 1 header frames.
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version1, ContentChunks: 6}, nil,
			rawSection(changefile.SectionHeader, headerV1Payload(t, "m")),
			rawSection(changefile.SectionDeps, []byte{0}),
			rawSection(changefile.SectionProvenance, july2026Provenance()),
			rawSection(changefile.SectionProvenance, []byte{0}),
			rawSection(changefile.SectionSignature, firstPayload),
			rawSection(changefile.SectionSignature, secondPayload),
			rawSection(changefile.SectionUnhashed, []byte(`{"a":1}`)),
			rawSection(changefile.SectionUnhashed, []byte(`{"b":2}`)),
		)
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Nil(t, v.Change.Provenance)
		require.Equal(t, provenance.LayoutCurrent, v.Change.ProvenanceLayout)
		require.NotNil(t, v.Change.Signature)
		require.Equal(t, second, *v.Change.Signature)
		require.Equal(t, json.RawMessage(`{"b":2}`), v.Change.Unhashed)
	})

	t.Run("SEMANTIC and CONTENT sections are concatenated", func(t *testing.T) {
		a := []semantic.FileOps{{Trunk: semantic.TrunkID{FileIdx: 1}, Path: "a"}}
		bOps := []semantic.FileOps{{Trunk: semantic.TrunkID{FileIdx: 2}, Path: "b"}}
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version1, ContentChunks: 4}, nil,
			rawSection(changefile.SectionHeader, headerV1Payload(t, "m")),
			rawSection(changefile.SectionDeps, []byte{0}),
			rawSection(changefile.SectionSemantic, semanticPayload(t, a)),
			rawSection(changefile.SectionSemantic, semanticPayload(t, bOps)),
			rawSection(changefile.SectionContent, []byte("ab")),
			rawSection(changefile.SectionContent, []byte("cd")),
		)
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, append(a, bOps...), v.Change.Semantic)
		require.Equal(t, []byte("abcd"), v.Change.Contents)
	})

	t.Run("a repeated section that fails is reported even if a later one would not", func(t *testing.T) {
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version1, ContentChunks: 1}, []ident.Hash{h1},
			rawSection(changefile.SectionHeader, headerV1Payload(t, "m")),
			rawSection(changefile.SectionDeps, []byte{1, 5}),
			rawSection(changefile.SectionDeps, []byte{1, 1}),
		)
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrHashIndexOutOfBounds, changefile.SectionDeps)
	})

	t.Run("GRAPH sections are concatenated", func(t *testing.T) {
		op1 := graph.SetAttr{Inode: graph.Position{Change: changefile.ThisChange}, Path: "a", Value: graph.Mode(0o644)}
		op2 := graph.SetAttr{Inode: graph.Position{Change: changefile.ThisChange}, Path: "b", Value: graph.Mode(0o600)}
		b := buildFile(t, changefile.FileHeader{Version: changefile.Version1, GraphSections: 2}, nil,
			rawSection(changefile.SectionHeader, headerV1Payload(t, "m")),
			rawSection(changefile.SectionDeps, []byte{0}),
			rawSection(changefile.SectionGraph, graphPayload(t, op1)),
			rawSection(changefile.SectionGraph, graphPayload(t, op2)),
		)
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, []graph.Op{op1, op2}, v.Change.Graph)
	})
}

func semanticPayload(t *testing.T, ops []semantic.FileOps) []byte {
	t.Helper()
	b, err := semantic.AppendPayload(nil, ops)
	require.NoError(t, err)
	return b
}

func graphPayload(t *testing.T, ops ...graph.Op) []byte {
	t.Helper()
	b, err := graph.AppendPayload(nil, graph.Payload{Ops: ops})
	require.NoError(t, err)
	return b
}

func TestDecodeGraph(t *testing.T) {
	ok := graph.SetAttr{Inode: graph.Position{Change: changefile.ThisChange}, Path: "a", Value: graph.Mode(0o644)}
	onRoot := graph.SetAttr{Inode: graph.Position{Change: 0}, Path: "a", Value: graph.Mode(0o644)}

	t.Run("sections are concatenated", func(t *testing.T) {
		b := writeChange(t, spec{
			version: changefile.Version2, env: validEnv(),
			extra: []section{
				{changefile.SectionGraph, graphPayload(t, ok)},
				{changefile.SectionGraph, graphPayload(t, ok, ok)},
			},
		})
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, []graph.Op{ok, ok, ok}, v.Change.Graph)
	})

	t.Run("each operation is checked against the table", func(t *testing.T) {
		b := writeChange(t, spec{
			version: changefile.Version2, env: validEnv(),
			extra: []section{{changefile.SectionGraph, graphPayload(t, onRoot)}},
		})
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrInvalidGraphOp, changefile.SectionGraph)
	})
}

func TestDecodeErrorOrder(t *testing.T) {
	header, err := appendHeaderV2(nil, validEnv(), emptyTable(t))
	require.NoError(t, err)

	// A DEPS payload that promises five indices and holds none, then a
	// section of the unknown type 0x00. Atomic decodes DEPS before it
	// reads the next frame, so the DEPS failure is the one reported.
	b := buildFile(t, changefile.FileHeader{Version: changefile.Version2, ContentChunks: 1}, nil,
		rawSection(changefile.SectionHeader, header),
		rawSection(changefile.SectionDeps, []byte{5}),
		[]byte{0x00, 0, 0, 0, 0},
	)
	_, err = Decode(b, newDecoder(t))
	requireFormatError(t, err, changefile.ErrPayload, changefile.SectionDeps)

	// The reader alone, which decodes nothing, reports the section type.
	_, err = changefile.Verify(b)
	require.ErrorIs(t, err, changefile.ErrInvalidSectionType)
}

// july2026Provenance returns the PROVENANCE payload of
// docs/remote/change-format/sections.md#provenance-example, Atomic's
// july_2026_legacy_provenance.hex, which Atomic's two readers read
// differently.
func july2026Provenance() []byte {
	var b []byte
	str := func(s string) {
		b = append(b, byte(len(s)))
		b = append(b, s...)
	}
	b = append(b, 0x01, 0x09) // 1 record; vendor Local
	str("moonshotai")         // model
	str("kimi-k3")            // model_version
	b = append(b, 0x00, 0x04) // tool Api; suggestion_type Review
	str("opencode")           // agent_name
	b = append(b, 0x00, 0x03, 0x00)
	b = append(b, make([]byte, 6)...) // tokens
	b = append(b, make([]byte, 8)...) // cost.usd
	b = append(b, 0x00, 0x00)         // cost.micro_usd; temperature
	b = append(b, 0x01, 0x86, 0xe3, 0xea, 0xa5, 0x0d, 0x00)
	b = append(b, 0x01)
	str("ses_08365450affeTbe2zvcG63oKEX")
	b = append(b, 0x02)
	str("turn_number")
	str("1")
	str("agent_name")
	str("opencode")
	return append(b, make([]byte, 7)...)
}

func TestDecodeProvenanceOrder(t *testing.T) {
	payload := july2026Provenance()
	require.Len(t, payload, 132)
	b := writeChange(t, spec{
		version: changefile.Version2, env: validEnv(),
		extra: []section{{changefile.SectionProvenance, payload}},
	})

	t.Run("the full reader's order by default", func(t *testing.T) {
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, provenance.LayoutBeforeXai, v.Change.ProvenanceLayout)
		require.Len(t, v.Change.Provenance, 1)
		require.Equal(t, "kimi-k3", v.Change.Provenance[0].Model)
	})

	t.Run("the section reader's order when asked", func(t *testing.T) {
		v, err := Decode(b, newDecoder(t), WithProvenanceOrder(provenance.SectionReaderOrder()))
		require.NoError(t, err)
		require.Equal(t, provenance.LayoutJuly2026, v.Change.ProvenanceLayout)
		require.Len(t, v.Change.Provenance, 1)
		require.Equal(t, "moonshotai", v.Change.Provenance[0].Model)
	})

	t.Run("an empty order decodes no PROVENANCE", func(t *testing.T) {
		_, err := Decode(b, newDecoder(t), WithProvenanceOrder(nil))
		requireFormatError(t, err, changefile.ErrPayload, changefile.SectionProvenance)
		require.ErrorIs(t, err, provenance.ErrUnsupportedLayout)
	})

	t.Run("a payload no layout decodes", func(t *testing.T) {
		b := writeChange(t, spec{
			version: changefile.Version2, env: validEnv(),
			extra: []section{{changefile.SectionProvenance, []byte{0x01}}},
		})
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrPayload, changefile.SectionProvenance)
		require.ErrorIs(t, err, provenance.ErrMalformed)
	})
}

func TestDecodeUnhashedAndSignature(t *testing.T) {
	sig := exampleSignature(t)
	sigPayload, err := sig.AppendPayload(nil)
	require.NoError(t, err)

	t.Run("both decode", func(t *testing.T) {
		b := writeChange(t, spec{
			version: changefile.Version2, env: validEnv(),
			extra: []section{
				{changefile.SectionSignature, sigPayload},
				{changefile.SectionUnhashed, []byte(`{"git":{"a":1}}`)},
			},
		})
		v, err := Decode(b, newDecoder(t))
		require.NoError(t, err)
		require.NotNil(t, v.Change.Signature)
		require.Equal(t, sig, *v.Change.Signature)
		require.Equal(t, json.RawMessage(`{"git":{"a":1}}`), v.Change.Unhashed)
	})

	t.Run("UNHASHED that is not JSON", func(t *testing.T) {
		b := writeChange(t, spec{
			version: changefile.Version2, env: validEnv(),
			extra: []section{{changefile.SectionUnhashed, []byte(`{"git":`)}},
		})
		_, err := Decode(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrPayload, changefile.SectionUnhashed)
	})
}

func TestDecodeReadOptions(t *testing.T) {
	b := append(changefixture.WorkedExample(), 0xEE)

	_, err := Decode(b, newDecoder(t))
	require.ErrorIs(t, err, changefile.ErrTrailingBytes)

	v, err := Decode(b, newDecoder(t), WithReadOptions(changefile.AllowTrailingBytes()))
	require.NoError(t, err)
	require.Equal(t, changefixture.WorkedExample(), v.File.Bytes())
}

func TestValidate(t *testing.T) {
	h1, h2 := hashOf(1), hashOf(2)

	testCases := []struct {
		name    string
		change  Change
		section changefile.SectionType
		detail  string
	}{
		{"an unsupported version", Change{Version: 4}, 0, "schema version 4"},
		{"a version 3 change without a span", Change{Version: changefile.Version3}, changefile.SectionSpan, "a version 3 change has no span"},
		{"a version 2 change with a span", Change{Version: changefile.Version2, Span: &span.Patch{Version: span.V2}}, changefile.SectionSpan, "a version 2 change has a span"},
		{"a version 1 change with a span", Change{Version: changefile.Version1, Span: &span.Patch{Version: span.V2}}, changefile.SectionSpan, "a version 1 change has a span"},
		{"a zero dependency", Change{Version: changefile.Version2, Dependencies: []ident.Hash{{}}}, changefile.SectionDeps, "dependencies entry 0 is the zero hash"},
		{"a repeated dependency", Change{Version: changefile.Version2, Dependencies: []ident.Hash{h1, h1}}, changefile.SectionDeps, "dependencies is not in strictly ascending"},
		{"unsorted extra_known", Change{Version: changefile.Version2, ExtraKnown: []ident.Hash{h2, h1}}, changefile.SectionHeader, "extra_known is not in strictly ascending"},
		{"unsorted causal_frontier", Change{Version: changefile.Version2, CausalFrontier: []ident.Hash{h2, h1}}, changefile.SectionHeader, "causal_frontier is not in strictly ascending"},
		{"a zero supersedes", Change{Version: changefile.Version2, Kind: Kind{Snapshot: true}, Supersedes: &ident.Hash{}}, changefile.SectionHeader, "supersedes is the zero hash"},
		{"an invalid origin", Change{Version: changefile.Version2, Origin: Origin{Kind: Native, Parents: []GitObjectID{sha1Commit(1)}}}, changefile.SectionHeader, "a Native origin has 1 parents"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.change.Validate()
			requireFormatError(t, err, changefile.ErrValidation, tc.section)
			require.ErrorContains(t, err, tc.detail)
		})
	}

	t.Run("a version 1 change is not checked further", func(t *testing.T) {
		c := Change{Version: changefile.Version1, Dependencies: []ident.Hash{h2, h1}}
		require.NoError(t, c.Validate())
	})
}

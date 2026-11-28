package changefile_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/changefixture"
	"github.com/z5labs/atomic-go/zstdframe"
)

// recordingCompressor compresses with [zstdframe.Stored] and records each
// payload it is given.
type recordingCompressor struct {
	calls [][]byte
}

func (r *recordingCompressor) Compress(dst, src []byte) []byte {
	r.calls = append(r.calls, append([]byte(nil), src...))
	return zstdframe.Stored{}.Compress(dst, src)
}

// unhashedJSON is the UNHASHED value of
// docs/remote/change-format/sections.md#unhashed-section, compact, as
// serde_json writes it.
const unhashedJSON = `{"editor":"vim","custom_field":42}`

func newWriter(t *testing.T, v changefile.Version, c changefile.Compressor) *changefile.Writer {
	t.Helper()
	var tb changefile.TableBuilder
	w, err := changefile.NewWriter(v, tb.Table(), c)
	require.NoError(t, err)
	return w
}

// writeWorkedExample writes the worked example's sections to w.
func writeWorkedExample(t *testing.T, w *changefile.Writer) {
	t.Helper()
	require.NoError(t, w.Add(changefile.SectionHeader, changefixture.WorkedExampleHeader()))
	require.NoError(t, w.Add(changefile.SectionDeps, []byte{0}))
	require.NoError(t, w.AddChunk([]byte(changefixture.WorkedExampleContent)))
}

func TestWriterWritesTheWorkedExample(t *testing.T) {
	w := newWriter(t, changefile.Version2, zstdframe.Stored{})
	writeWorkedExample(t, w)
	b, h, err := w.Finish()
	require.NoError(t, err)
	require.Equal(t, changefixture.WorkedExample(), b)
	require.Equal(t, workedExampleHash(t), h)

	f, err := changefile.Parse(b, newDecoder(t), changefile.CheckChunks())
	require.NoError(t, err)
	require.Equal(t, h, f.Hash)
}

func TestWriterFinishTwice(t *testing.T) {
	w := newWriter(t, changefile.Version2, zstdframe.Stored{})
	writeWorkedExample(t, w)
	b1, h1, err := w.Finish()
	require.NoError(t, err)
	b2, h2, err := w.Finish()
	require.NoError(t, err)
	require.Equal(t, b1, b2)
	require.Equal(t, h1, h2)

	require.ErrorIs(t, w.Add(changefile.SectionUnhashed, []byte(unhashedJSON)), changefile.ErrWriterFinished)
	require.ErrorIs(t, w.AddChunk([]byte("x")), changefile.ErrWriterFinished)
	require.ErrorIs(t, w.AddRaw(changefile.Section{}), changefile.ErrWriterFinished)
}

func TestWriterCompressesEachSectionAndChunk(t *testing.T) {
	rec := &recordingCompressor{}
	w := newWriter(t, changefile.Version2, rec)
	require.NoError(t, w.Add(changefile.SectionHeader, changefixture.WorkedExampleHeader()))
	require.NoError(t, w.Add(changefile.SectionDeps, []byte{0}))
	require.NoError(t, w.AddChunk([]byte("he")))
	require.NoError(t, w.AddChunk([]byte("llo")))
	require.NoError(t, w.Add(changefile.SectionUnhashed, []byte(unhashedJSON)))
	b, _, err := w.Finish()
	require.NoError(t, err)

	require.Equal(t, [][]byte{
		changefixture.WorkedExampleHeader(),
		{0},
		[]byte("he"),
		[]byte("llo"),
		[]byte(unhashedJSON),
	}, rec.calls)

	f, err := changefile.Parse(b, newDecoder(t), changefile.CheckChunks())
	require.NoError(t, err)
	require.Equal(t, "hello", string(f.Content()))
	require.Equal(t, uint32(2), f.Header.ContentChunks)
	require.Equal(t, changefile.FlagUnhashed, f.Header.Flags)
	require.Equal(t, uint32(1), f.Sections[3].Chunk.Index)
}

func TestWriterSetsCountsAndFlags(t *testing.T) {
	var tb changefile.TableBuilder
	dep := ident.Sum([]byte("dep"))
	i, err := tb.Add(dep)
	require.NoError(t, err)
	require.Equal(t, changefile.HashIndex(1), i)

	w, err := changefile.NewWriter(changefile.Version3, tb.Table(), zstdframe.Stored{})
	require.NoError(t, err)
	for _, s := range []struct {
		t       changefile.SectionType
		payload string
	}{
		{changefile.SectionHeader, "h"},
		{changefile.SectionDeps, "\x01\x01"},
		{changefile.SectionProvenance, "\x00"},
		{changefile.SectionSignature, "sig"},
		{changefile.SectionGraph, "g1"},
		{changefile.SectionGraph, "g2"},
		{changefile.SectionSpan, "s"},
		{changefile.SectionSemantic, "m1"},
		{changefile.SectionSemantic, "m2"},
		{changefile.SectionSemantic, "m3"},
	} {
		require.NoError(t, w.Add(s.t, []byte(s.payload)), s.t.String())
	}
	require.NoError(t, w.AddChunk([]byte("c")))
	require.NoError(t, w.Add(changefile.SectionUnhashed, []byte("{}")))
	b, h, err := w.Finish()
	require.NoError(t, err)

	f, err := changefile.Parse(b, newDecoder(t))
	require.NoError(t, err)
	require.Equal(t, h, f.Hash)
	require.Equal(t, changefile.FileHeader{
		Version:          changefile.Version3,
		Flags:            changefile.FlagProvenance | changefile.FlagSemantic | changefile.FlagUnhashed | changefile.FlagSignature,
		HashTableEntries: 2,
		GraphSections:    2,
		SemanticSections: 3,
		ContentChunks:    1,
	}, f.Header)
	require.Equal(t, tb.Table(), f.Table)
}

func TestWriterAddRaw(t *testing.T) {
	src, err := changefile.Parse(changefixture.WorkedExample(), newDecoder(t))
	require.NoError(t, err)

	rec := &recordingCompressor{}
	w := newWriter(t, changefile.Version2, rec)
	for _, s := range src.Sections {
		require.NoError(t, w.AddRaw(s))
	}
	b, h, err := w.Finish()
	require.NoError(t, err)
	require.Equal(t, changefixture.WorkedExample(), b)
	require.Equal(t, src.Hash, h)
	require.Empty(t, rec.calls)
}

func TestWriterAddRawRefusesABadFrame(t *testing.T) {
	src, err := changefile.Parse(changefixture.WorkedExample(), newDecoder(t))
	require.NoError(t, err)
	header := src.Sections[0]

	testCases := []struct {
		name string
		s    changefile.Section
	}{
		{"short frame", changefile.Section{Type: changefile.SectionHeader, Frame: header.Frame[:4], Compressed: header.Compressed}},
		{"frame of another type", changefile.Section{Type: changefile.SectionDeps, Frame: header.Frame, Compressed: header.Compressed}},
		{"length mismatch", changefile.Section{Type: changefile.SectionHeader, Frame: header.Frame, Compressed: header.Compressed[1:]}},
		{"content frame for a 5-byte type", changefile.Section{Type: changefile.SectionContent, Frame: header.Frame, Compressed: header.Compressed}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWriter(t, changefile.Version2, zstdframe.Stored{})
			err := w.AddRaw(tc.s)
			require.ErrorIs(t, err, changefile.ErrInvalidHeader)
			errAs(t, err)
		})
	}

	t.Run("unknown type", func(t *testing.T) {
		w := newWriter(t, changefile.Version2, zstdframe.Stored{})
		err := w.AddRaw(changefile.Section{Type: 0x05, Frame: []byte{5, 0, 0, 0, 0}})
		require.ErrorIs(t, err, changefile.ErrInvalidSectionType)
	})
}

func TestNewWriterRefusals(t *testing.T) {
	for _, v := range []changefile.Version{0, changefile.Version1, 4} {
		_, err := changefile.NewWriter(v, changefile.HashTable{}, zstdframe.Stored{})
		require.ErrorIs(t, err, changefile.ErrUnsupportedVersion, "version %d", v)
		errAs(t, err)
	}

	_, err := changefile.NewWriter(changefile.Version2, changefile.HashTable{}, nil)
	require.ErrorIs(t, err, changefile.ErrNilCompressor)
	var fe *changefile.FormatError
	require.False(t, errors.As(err, &fe))
}

func TestWriterAddRefusals(t *testing.T) {
	type step func(w *changefile.Writer) error
	add := func(t changefile.SectionType) step {
		return func(w *changefile.Writer) error { return w.Add(t, []byte("p")) }
	}
	chunk := func(w *changefile.Writer) error { return w.AddChunk([]byte("c")) }

	testCases := []struct {
		name    string
		version changefile.Version
		before  []step
		then    step
		kind    error
	}{
		{"CONTENT through Add", changefile.Version2, nil, add(changefile.SectionContent), changefile.ErrUnexpectedSection},
		{"unknown type", changefile.Version2, nil, add(0x05), changefile.ErrInvalidSectionType},
		{"DEPS before HEADER", changefile.Version2, []step{add(changefile.SectionDeps)}, add(changefile.SectionHeader), changefile.ErrUnexpectedSection},
		{"SIGNATURE after GRAPH", changefile.Version2, []step{add(changefile.SectionGraph)}, add(changefile.SectionSignature), changefile.ErrUnexpectedSection},
		{"SEMANTIC after CONTENT", changefile.Version2, []step{chunk}, add(changefile.SectionSemantic), changefile.ErrUnexpectedSection},
		{"CONTENT after UNHASHED", changefile.Version2, []step{add(changefile.SectionUnhashed)}, chunk, changefile.ErrUnexpectedSection},
		{"duplicate HEADER", changefile.Version2, []step{add(changefile.SectionHeader)}, add(changefile.SectionHeader), changefile.ErrUnexpectedSection},
		{"duplicate DEPS", changefile.Version2, []step{add(changefile.SectionDeps)}, add(changefile.SectionDeps), changefile.ErrUnexpectedSection},
		{"duplicate PROVENANCE", changefile.Version2, []step{add(changefile.SectionProvenance)}, add(changefile.SectionProvenance), changefile.ErrUnexpectedSection},
		{"duplicate SIGNATURE", changefile.Version2, []step{add(changefile.SectionSignature)}, add(changefile.SectionSignature), changefile.ErrUnexpectedSection},
		{"duplicate SPAN", changefile.Version3, []step{add(changefile.SectionSpan)}, add(changefile.SectionSpan), changefile.ErrUnexpectedSection},
		{"duplicate UNHASHED", changefile.Version2, []step{add(changefile.SectionUnhashed)}, add(changefile.SectionUnhashed), changefile.ErrUnexpectedSection},
		{"SPAN in version 2", changefile.Version2, nil, add(changefile.SectionSpan), changefile.ErrUnexpectedSection},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingCompressor{}
			w := newWriter(t, tc.version, rec)
			for _, s := range tc.before {
				require.NoError(t, s(w))
			}
			calls := len(rec.calls)
			err := tc.then(w)
			require.ErrorIs(t, err, tc.kind)
			errAs(t, err)
			require.Len(t, rec.calls, calls, "a refused section is not compressed")
		})
	}
}

func TestWriterRefusalLeavesTheWriterUnchanged(t *testing.T) {
	w := newWriter(t, changefile.Version2, zstdframe.Stored{})
	require.NoError(t, w.Add(changefile.SectionHeader, changefixture.WorkedExampleHeader()))
	require.Error(t, w.Add(changefile.SectionHeader, []byte("again")))
	require.Error(t, w.Add(changefile.SectionContent, []byte("x")))
	require.NoError(t, w.Add(changefile.SectionDeps, []byte{0}))
	require.NoError(t, w.AddChunk([]byte(changefixture.WorkedExampleContent)))
	b, _, err := w.Finish()
	require.NoError(t, err)
	require.Equal(t, changefixture.WorkedExample(), b)
}

func TestWriterFinishRequiresSections(t *testing.T) {
	testCases := []struct {
		name    string
		version changefile.Version
		adds    []changefile.SectionType
		missing changefile.SectionType
	}{
		{"no HEADER", changefile.Version2, []changefile.SectionType{changefile.SectionDeps}, changefile.SectionHeader},
		{"no DEPS", changefile.Version2, []changefile.SectionType{changefile.SectionHeader}, changefile.SectionDeps},
		{"no SPAN in version 3", changefile.Version3, []changefile.SectionType{changefile.SectionHeader, changefile.SectionDeps}, changefile.SectionSpan},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWriter(t, tc.version, zstdframe.Stored{})
			for _, s := range tc.adds {
				require.NoError(t, w.Add(s, []byte{0}))
			}
			_, _, err := w.Finish()
			require.ErrorIs(t, err, changefile.ErrInvalidHeader)
			require.Equal(t, tc.missing, errAs(t, err).Section)
		})
	}

	t.Run("can be finished once the section is added", func(t *testing.T) {
		w := newWriter(t, changefile.Version3, zstdframe.Stored{})
		require.NoError(t, w.Add(changefile.SectionHeader, []byte{0}))
		require.NoError(t, w.Add(changefile.SectionDeps, []byte{0}))
		_, _, err := w.Finish()
		require.ErrorIs(t, err, changefile.ErrInvalidHeader)
		require.NoError(t, w.Add(changefile.SectionSpan, []byte{0}))
		b, h, err := w.Finish()
		require.NoError(t, err)
		got, err := changefile.Verify(b)
		require.NoError(t, err)
		require.Equal(t, h, got)
	})
}

func TestReplaceUnhashedKeepsTheHash(t *testing.T) {
	dec := newDecoder(t)
	want := workedExampleHash(t)
	signature := []byte("not a real ChangeSignature")

	f, err := changefile.Parse(changefixture.WorkedExample(), dec)
	require.NoError(t, err)

	b, err := changefile.ReplaceUnhashed(f, changefile.SectionUnhashed, []byte(unhashedJSON), zstdframe.Stored{})
	require.NoError(t, err)
	f, err = changefile.Parse(b, dec)
	require.NoError(t, err)
	require.Equal(t, want, f.Hash)
	require.Equal(t, changefile.FlagUnhashed, f.Header.Flags)
	require.Equal(t, changefile.Flags(0x04), f.Header.Flags)
	require.Equal(t, changefile.SectionUnhashed, f.Sections[len(f.Sections)-1].Type)
	require.Equal(t, unhashedJSON, string(f.Sections[len(f.Sections)-1].Payload))

	// The Writer writes the same file from the same sections.
	w := newWriter(t, changefile.Version2, zstdframe.Stored{})
	writeWorkedExample(t, w)
	require.NoError(t, w.Add(changefile.SectionUnhashed, []byte(unhashedJSON)))
	written, h, err := w.Finish()
	require.NoError(t, err)
	require.Equal(t, written, b)
	require.Equal(t, want, h)

	b, err = changefile.ReplaceUnhashed(f, changefile.SectionSignature, signature, zstdframe.Stored{})
	require.NoError(t, err)
	f, err = changefile.Parse(b, dec)
	require.NoError(t, err)
	require.Equal(t, want, f.Hash)
	require.Equal(t, changefile.Flags(0x04|0x08), f.Header.Flags)
	require.Equal(t, []changefile.SectionType{
		changefile.SectionHeader,
		changefile.SectionDeps,
		changefile.SectionSignature,
		changefile.SectionContent,
		changefile.SectionUnhashed,
	}, types(f))
	require.Equal(t, signature, f.Sections[2].Payload)

	b, err = changefile.ReplaceUnhashed(f, changefile.SectionUnhashed, nil, nil)
	require.NoError(t, err)
	f, err = changefile.Parse(b, dec)
	require.NoError(t, err)
	require.Equal(t, want, f.Hash)
	require.Equal(t, changefile.FlagSignature, f.Header.Flags)

	b, err = changefile.ReplaceUnhashed(f, changefile.SectionSignature, nil, nil)
	require.NoError(t, err)
	require.Len(t, b, exLen)
	require.Equal(t, changefixture.WorkedExample(), b)
}

func TestReplaceUnhashedReplacesTheSection(t *testing.T) {
	dec := newDecoder(t)
	f, err := changefile.Parse(changefixture.WorkedExample(), dec)
	require.NoError(t, err)

	b, err := changefile.ReplaceUnhashed(f, changefile.SectionSignature, []byte("first"), zstdframe.Stored{})
	require.NoError(t, err)
	f, err = changefile.Parse(b, dec)
	require.NoError(t, err)

	b, err = changefile.ReplaceUnhashed(f, changefile.SectionSignature, []byte("second"), zstdframe.Stored{})
	require.NoError(t, err)
	f, err = changefile.Parse(b, dec)
	require.NoError(t, err)
	require.Equal(t, workedExampleHash(t), f.Hash)
	require.Len(t, f.Sections, 4)
	require.Equal(t, "second", string(f.Sections[2].Payload))

	t.Run("with an empty payload", func(t *testing.T) {
		b, err := changefile.ReplaceUnhashed(f, changefile.SectionSignature, []byte{}, zstdframe.Stored{})
		require.NoError(t, err)
		g, err := changefile.Parse(b, dec)
		require.NoError(t, err)
		require.Equal(t, changefile.FlagSignature, g.Header.Flags)
		require.Empty(t, g.Sections[2].Payload)
	})
}

func TestReplaceUnhashedVersion1(t *testing.T) {
	dec := newDecoder(t)
	v1 := changefixture.WorkedExample()
	binary.LittleEndian.PutUint32(v1[4:], 1) // the version is not hashed

	f, err := changefile.Parse(v1, dec)
	require.NoError(t, err)
	require.Equal(t, changefile.Version1, f.Header.Version)

	b, err := changefile.ReplaceUnhashed(f, changefile.SectionSignature, []byte("sig"), zstdframe.Stored{})
	require.NoError(t, err)
	g, err := changefile.Parse(b, dec)
	require.NoError(t, err)
	require.Equal(t, workedExampleHash(t), g.Hash)
	require.Equal(t, changefile.Version1, g.Header.Version)
	require.Equal(t, changefile.FlagSignature, g.Header.Flags)

	b, err = changefile.ReplaceUnhashed(g, changefile.SectionSignature, nil, nil)
	require.NoError(t, err)
	require.Equal(t, v1, b)
}

func TestReplaceUnhashedKeepsOtherHeaderFields(t *testing.T) {
	dec := newDecoder(t)
	b := changefixture.WorkedExample()
	binary.LittleEndian.PutUint32(b[8:], 1<<20) // an unknown flag bit
	b[40] = 0xAA                                // a reserved byte

	f, err := changefile.Parse(b, dec)
	require.NoError(t, err)
	out, err := changefile.ReplaceUnhashed(f, changefile.SectionUnhashed, []byte("{}"), zstdframe.Stored{})
	require.NoError(t, err)
	g, err := changefile.Parse(out, dec)
	require.NoError(t, err)
	require.Equal(t, changefile.Flags(1<<20)|changefile.FlagUnhashed, g.Header.Flags)
	require.Equal(t, byte(0xAA), g.Header.Reserved[4])
}

func TestReplaceUnhashedRefusals(t *testing.T) {
	f, err := changefile.Parse(changefixture.WorkedExample(), newDecoder(t))
	require.NoError(t, err)

	for _, typ := range []changefile.SectionType{
		changefile.SectionHeader,
		changefile.SectionDeps,
		changefile.SectionProvenance,
		changefile.SectionGraph,
		changefile.SectionSpan,
		changefile.SectionSemantic,
		changefile.SectionContent,
		0x05,
	} {
		_, err := changefile.ReplaceUnhashed(f, typ, []byte("x"), zstdframe.Stored{})
		require.ErrorIs(t, err, changefile.ErrUnexpectedSection, typ.String())
		errAs(t, err)
	}

	_, err = changefile.ReplaceUnhashed(nil, changefile.SectionUnhashed, nil, nil)
	require.ErrorIs(t, err, changefile.ErrNilFile)

	_, err = changefile.ReplaceUnhashed(f, changefile.SectionUnhashed, []byte("{}"), nil)
	require.ErrorIs(t, err, changefile.ErrNilCompressor)

	t.Run("a section count the flag does not match", func(t *testing.T) {
		// A version 1 file whose two UNHASHED sections stand in for the
		// SIGNATURE section its flags declare.
		h := changefile.FileHeader{Version: changefile.Version1, Flags: changefile.FlagUnhashed | changefile.FlagSignature, ContentChunks: 0}
		b := build(t, h,
			section(changefile.SectionHeader, []byte("h")),
			section(changefile.SectionDeps, []byte{0}),
			section(changefile.SectionUnhashed, []byte("{}")),
			section(changefile.SectionUnhashed, []byte("{}")),
		)
		g, err := changefile.Parse(b, newDecoder(t))
		require.NoError(t, err)
		_, err = changefile.ReplaceUnhashed(g, changefile.SectionUnhashed, nil, nil)
		require.ErrorIs(t, err, changefile.ErrInvalidHeader)
		_, err = changefile.ReplaceUnhashed(g, changefile.SectionSignature, []byte("sig"), zstdframe.Stored{})
		require.ErrorIs(t, err, changefile.ErrInvalidHeader)
	})
}

func TestWriterAddRawRefusesWhatAddDoes(t *testing.T) {
	dec := newDecoder(t)
	full, _, err := writeFull(t, []byte("signature"), []byte("{}")).Finish()
	require.NoError(t, err)
	f, err := changefile.Parse(full, dec)
	require.NoError(t, err)
	raw := func(t *testing.T, typ changefile.SectionType) changefile.Section {
		t.Helper()
		for _, s := range f.Sections {
			if s.Type == typ {
				return s
			}
		}
		require.FailNow(t, "no section of type "+typ.String())
		return changefile.Section{}
	}

	testCases := []struct {
		name    string
		version changefile.Version
		before  []changefile.SectionType
		then    changefile.SectionType
	}{
		{"out of order", changefile.Version3, []changefile.SectionType{changefile.SectionGraph}, changefile.SectionProvenance},
		{"duplicate HEADER", changefile.Version3, []changefile.SectionType{changefile.SectionHeader}, changefile.SectionHeader},
		{"duplicate DEPS", changefile.Version3, []changefile.SectionType{changefile.SectionDeps}, changefile.SectionDeps},
		{"duplicate SIGNATURE", changefile.Version3, []changefile.SectionType{changefile.SectionSignature}, changefile.SectionSignature},
		{"duplicate SPAN", changefile.Version3, []changefile.SectionType{changefile.SectionSpan}, changefile.SectionSpan},
		{"duplicate UNHASHED", changefile.Version3, []changefile.SectionType{changefile.SectionUnhashed}, changefile.SectionUnhashed},
		{"SPAN in version 2", changefile.Version2, nil, changefile.SectionSpan},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWriter(t, tc.version, zstdframe.Stored{})
			for _, typ := range tc.before {
				require.NoError(t, w.AddRaw(raw(t, typ)))
			}
			err := w.AddRaw(raw(t, tc.then))
			require.ErrorIs(t, err, changefile.ErrUnexpectedSection)
			errAs(t, err)
		})
	}
}

func TestWriterAddRawChunkIndex(t *testing.T) {
	dec := newDecoder(t)
	src, err := changefile.Parse(changefixture.WorkedExample(), dec)
	require.NoError(t, err)
	chunk0 := src.Sections[2]
	require.Equal(t, changefile.SectionContent, chunk0.Type)

	t.Run("AddChunk numbers after a raw chunk", func(t *testing.T) {
		w := newWriter(t, changefile.Version2, zstdframe.Stored{})
		require.NoError(t, w.AddRaw(src.Sections[0]))
		require.NoError(t, w.AddRaw(src.Sections[1]))
		require.NoError(t, w.AddRaw(chunk0))
		require.NoError(t, w.AddChunk([]byte("more\n")))
		b, _, err := w.Finish()
		require.NoError(t, err)
		f, err := changefile.Parse(b, dec, changefile.CheckChunks())
		require.NoError(t, err)
		require.Equal(t, changefixture.WorkedExampleContent+"more\n", string(f.Content()))
		require.Equal(t, uint32(1), f.Sections[3].Chunk.Index)
	})

	t.Run("a raw chunk out of its place", func(t *testing.T) {
		w := newWriter(t, changefile.Version2, zstdframe.Stored{})
		require.NoError(t, w.AddChunk([]byte("first")))
		err := w.AddRaw(chunk0)
		require.ErrorIs(t, err, changefile.ErrChunkMismatch)
		errAs(t, err)

		// The refusal left the Writer as it was.
		require.NoError(t, w.AddChunk([]byte("second")))
	})
}

// writeFull returns a version 3 Writer holding one section of every
// hashed type, two GRAPH and two SEMANTIC sections and two chunks, and
// signature and unhashed when they are not nil.
func writeFull(t *testing.T, signature, unhashed []byte) *changefile.Writer {
	t.Helper()
	var tb changefile.TableBuilder
	_, err := tb.Add(ident.Sum([]byte("dep")))
	require.NoError(t, err)
	w, err := changefile.NewWriter(changefile.Version3, tb.Table(), zstdframe.Stored{})
	require.NoError(t, err)

	add := func(typ changefile.SectionType, payload string) {
		require.NoError(t, w.Add(typ, []byte(payload)), typ.String())
	}
	add(changefile.SectionHeader, "header")
	add(changefile.SectionDeps, "\x01\x01")
	add(changefile.SectionProvenance, "\x00")
	if signature != nil {
		add(changefile.SectionSignature, string(signature))
	}
	add(changefile.SectionGraph, "graph 1")
	add(changefile.SectionGraph, "graph 2")
	add(changefile.SectionSpan, "span")
	add(changefile.SectionSemantic, "semantic 1")
	add(changefile.SectionSemantic, "semantic 2")
	require.NoError(t, w.AddChunk([]byte("chunk 1")))
	require.NoError(t, w.AddChunk([]byte("chunk 2")))
	if unhashed != nil {
		add(changefile.SectionUnhashed, string(unhashed))
	}
	return w
}

func TestReplaceUnhashedPlacesSectionsInOrder(t *testing.T) {
	dec := newDecoder(t)
	signature := []byte("signature")

	plain, hash, err := writeFull(t, nil, nil).Finish()
	require.NoError(t, err)
	signed, signedHash, err := writeFull(t, signature, nil).Finish()
	require.NoError(t, err)
	both, bothHash, err := writeFull(t, signature, []byte(unhashedJSON)).Finish()
	require.NoError(t, err)
	require.Equal(t, hash, signedHash)
	require.Equal(t, hash, bothHash)

	f, err := changefile.Parse(plain, dec)
	require.NoError(t, err)

	b, err := changefile.ReplaceUnhashed(f, changefile.SectionSignature, signature, zstdframe.Stored{})
	require.NoError(t, err)
	require.Equal(t, signed, b)
	f, err = changefile.Parse(b, dec)
	require.NoError(t, err)
	require.Equal(t, hash, f.Hash)
	require.Equal(t, []changefile.SectionType{
		changefile.SectionHeader,
		changefile.SectionDeps,
		changefile.SectionProvenance,
		changefile.SectionSignature,
		changefile.SectionGraph,
		changefile.SectionGraph,
		changefile.SectionSpan,
		changefile.SectionSemantic,
		changefile.SectionSemantic,
		changefile.SectionContent,
		changefile.SectionContent,
	}, types(f))

	b, err = changefile.ReplaceUnhashed(f, changefile.SectionUnhashed, []byte(unhashedJSON), zstdframe.Stored{})
	require.NoError(t, err)
	require.Equal(t, both, b)
	f, err = changefile.Parse(b, dec)
	require.NoError(t, err)
	require.Equal(t, hash, f.Hash)

	b, err = changefile.ReplaceUnhashed(f, changefile.SectionSignature, nil, nil)
	require.NoError(t, err)
	f, err = changefile.Parse(b, dec)
	require.NoError(t, err)
	b, err = changefile.ReplaceUnhashed(f, changefile.SectionUnhashed, nil, nil)
	require.NoError(t, err)
	require.Equal(t, plain, b)
}

package changefile_test

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/changefixture"
	"github.com/z5labs/atomic-go/zstdframe"
)

// Offsets in the worked example, from
// docs/remote/change-format/container.md#worked-example-a-version-2-file.
const (
	exHeaderSection  = 96
	exDepsSection    = 164
	exDepsZstd       = 169
	exContentSection = 179
	exChunkIndex     = 180
	exChunkHash      = 184
	exUncompressed   = 216
	exContentZstd    = 224
	exContentPayload = 233
	exTrailer        = 243
	exLen            = 275
)

func newDecoder(t *testing.T) *zstdframe.Decoder {
	t.Helper()
	d, err := zstdframe.NewDecoder(zstdframe.AtomicWindow)
	require.NoError(t, err)
	t.Cleanup(d.Close)
	return d
}

// countingDecompressor records how often it is called.
type countingDecompressor struct {
	dec   changefile.Decompressor
	calls int
}

func (c *countingDecompressor) Decompress(dst, src []byte) ([]byte, error) {
	c.calls++
	return c.dec.Decompress(dst, src)
}

// failingDecompressor fails every call with err.
type failingDecompressor struct{ err error }

func (f failingDecompressor) Decompress(dst, src []byte) ([]byte, error) {
	return dst, f.err
}

func workedExampleHash(t *testing.T) ident.Hash {
	t.Helper()
	h, err := ident.ParseHash(changefixture.WorkedExampleHash)
	require.NoError(t, err)
	return h
}

// retrail sets the trailer of b, a file with no unhashed sections and a
// one-entry table, to the hash of everything from the table on.
func retrail(b []byte) []byte {
	h := ident.Sum(b[changefile.HeaderSize : len(b)-changefile.TrailerSize])
	copy(b[len(b)-changefile.TrailerSize:], h[:])
	return b
}

// section returns a 5-byte-framed section of type t holding payload as a
// stored zstd frame.
func section(t changefile.SectionType, payload []byte) []byte {
	c := zstdframe.Stored{}.Compress(nil, payload)
	b := []byte{byte(t)}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(c)))
	return append(b, c...)
}

// rawSection returns a 5-byte-framed section of type t holding compressed
// as it is.
func rawSection(t changefile.SectionType, compressed []byte) []byte {
	b := []byte{byte(t)}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(compressed)))
	return append(b, compressed...)
}

// build returns a file with header h, a one-entry table holding the zero
// hash, and sections, ending with the right trailer.
func build(t *testing.T, h changefile.FileHeader, sections ...[]byte) []byte {
	t.Helper()
	h.HashTableEntries = 1
	b, err := h.AppendBinary(nil)
	require.NoError(t, err)
	hasher := ident.NewHasher()
	table := make([]byte, ident.Size)
	b = append(b, table...)
	hasher.Write(table)
	for _, s := range sections {
		b = append(b, s...)
		if changefile.SectionType(s[0]).Hashed() {
			hasher.Write(s)
		}
	}
	sum := hasher.Sum()
	return append(b, sum[:]...)
}

func TestParseWorkedExample(t *testing.T) {
	b := changefixture.WorkedExample()
	f, err := changefile.Parse(b, newDecoder(t))
	require.NoError(t, err)

	require.Equal(t, changefixture.WorkedExampleHash, f.Hash.String())
	require.Equal(t, b, f.Bytes())
	require.Equal(t, changefixture.WorkedExampleContent, string(f.Content()))
	require.Equal(t, changefile.Version2, f.Header.Version)
	require.Equal(t, uint32(1), f.Header.ContentChunks)
	require.Equal(t, 1, f.Table.Len())
	zero, err := f.Table.At(0)
	require.NoError(t, err)
	require.True(t, zero.IsZero())

	require.Len(t, f.Sections, 3)
	require.Equal(t, changefile.SectionHeader, f.Sections[0].Type)
	require.Equal(t, changefixture.WorkedExampleHeader(), f.Sections[0].Payload)
	require.Equal(t, changefile.SectionDeps, f.Sections[1].Type)
	require.Equal(t, []byte{0}, f.Sections[1].Payload)
	require.Equal(t, changefile.SectionContent, f.Sections[2].Type)
	require.Equal(t, []byte(changefixture.WorkedExampleContent), f.Sections[2].Payload)
}

func TestVerifyWorkedExample(t *testing.T) {
	h, err := changefile.Verify(changefixture.WorkedExample())
	require.NoError(t, err)
	require.Equal(t, changefixture.WorkedExampleHash, h.String())
}

func TestReaderNext(t *testing.T) {
	b := changefixture.WorkedExample()
	r, err := changefile.NewReader(b, newDecoder(t))
	require.NoError(t, err)
	require.Equal(t, changefile.Version2, r.Header().Version)
	require.Equal(t, 1, r.Table().Len())

	s, ok, err := r.Next()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, changefile.SectionHeader, s.Type)
	require.Equal(t, int64(exHeaderSection), s.Offset)
	require.Equal(t, b[exHeaderSection:exHeaderSection+5], s.Frame)
	require.Equal(t, b[exHeaderSection+5:exDepsSection], s.Compressed)
	require.Equal(t, changefixture.WorkedExampleHeader(), s.Payload)
	require.Equal(t, changefile.ChunkInfo{}, s.Chunk)

	s, ok, err = r.Next()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, changefile.SectionDeps, s.Type)
	require.Equal(t, int64(exDepsSection), s.Offset)
	require.Equal(t, []byte{0}, s.Payload)

	s, ok, err = r.Next()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, changefile.SectionContent, s.Type)
	require.Equal(t, int64(exContentSection), s.Offset)
	require.Equal(t, b[exContentSection:exContentZstd], s.Frame)
	require.Equal(t, b[exContentZstd:exTrailer], s.Compressed)
	require.Equal(t, uint32(0), s.Chunk.Index)
	require.Equal(t, ident.Sum([]byte(changefixture.WorkedExampleContent)), s.Chunk.Hash)
	require.Equal(t, uint32(10), s.Chunk.UncompressedLen)

	_, ok, err = r.Next()
	require.NoError(t, err)
	require.False(t, ok)
	_, ok, err = r.Next()
	require.NoError(t, err)
	require.False(t, ok)

	f, err := r.Finish()
	require.NoError(t, err)
	require.Len(t, f.Sections, 3)
	require.Equal(t, workedExampleHash(t), f.Hash)

	again, err := r.Finish()
	require.NoError(t, err)
	require.Same(t, f, again)
}

func TestFinishReadsRemainingSections(t *testing.T) {
	r, err := changefile.NewReader(changefixture.WorkedExample(), newDecoder(t))
	require.NoError(t, err)
	_, ok, err := r.Next()
	require.NoError(t, err)
	require.True(t, ok)

	f, err := r.Finish()
	require.NoError(t, err)
	require.Len(t, f.Sections, 3)
	require.Equal(t, changefixture.WorkedExampleContent, string(f.Content()))
}

func TestNilDecompressorIsRefused(t *testing.T) {
	_, err := changefile.NewReader(changefixture.WorkedExample(), nil)
	require.ErrorIs(t, err, changefile.ErrNilDecompressor)
	_, err = changefile.Parse(changefixture.WorkedExample(), nil)
	require.ErrorIs(t, err, changefile.ErrNilDecompressor)
}

func TestFlippedPayloadByte(t *testing.T) {
	orig := workedExampleHash(t)
	b := changefixture.WorkedExample()
	b[exContentPayload] ^= 0x01
	computed := ident.Sum(b[changefile.HeaderSize:exTrailer])
	require.NotEqual(t, orig, computed)

	requireMismatch := func(t *testing.T, err error) {
		t.Helper()
		require.ErrorIs(t, err, changefile.ErrHashMismatch)
		var hm *changefile.HashMismatchError
		require.ErrorAs(t, err, &hm)
		require.Equal(t, orig, hm.Trailer)
		require.Equal(t, computed, hm.Computed)
	}

	t.Run("Parse", func(t *testing.T) {
		_, err := changefile.Parse(b, newDecoder(t))
		requireMismatch(t, err)
	})
	t.Run("Verify", func(t *testing.T) {
		_, err := changefile.Verify(b)
		requireMismatch(t, err)
	})
}

func TestUnhashedHeaderBytesLeaveTheHashUnchanged(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(b []byte)
	}{
		{"version 2 to 1", func(b []byte) { b[4] = 1 }},
		{"first reserved byte", func(b []byte) { b[36] = 0xff }},
		{"last reserved byte", func(b []byte) { b[63] = 0x5a }},
		{"total_uncompressed", func(b []byte) { b[28] = 0x7f }},
		{"unknown flag bit", func(b []byte) { b[8] |= 0x10 }},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := changefixture.WorkedExample()
			tc.mutate(b)

			h, err := changefile.Verify(b)
			require.NoError(t, err)
			require.Equal(t, changefixture.WorkedExampleHash, h.String())

			f, err := changefile.Parse(b, newDecoder(t))
			require.NoError(t, err)
			require.Equal(t, changefixture.WorkedExampleHash, f.Hash.String())
		})
	}
}

func TestTruncatedAtASectionStart(t *testing.T) {
	// The file ends where DEPS would start: the section loop ends without
	// an error and the trailer is missing.
	b := changefixture.WorkedExample()[:exDepsSection]
	r, err := changefile.NewReader(b, newDecoder(t))
	require.NoError(t, err)

	_, ok, err := r.Next()
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = r.Next()
	require.NoError(t, err)
	require.False(t, ok)

	_, err = r.Finish()
	fe := requireFormatError(t, err, changefile.ErrTruncated, exDepsSection)
	require.Equal(t, changefile.SectionType(0), fe.Section)

	_, err = changefile.Verify(b)
	requireFormatError(t, err, changefile.ErrTruncated, exDepsSection)
}

func TestTruncatedMidFrame(t *testing.T) {
	for _, n := range []int{exDepsSection + 2, exDepsZstd + 3, exContentSection + 20, exContentPayload + 1} {
		b := changefixture.WorkedExample()[:n]
		r, err := changefile.NewReader(b, newDecoder(t))
		require.NoError(t, err)

		_, ok, err := r.Next()
		require.NoError(t, err)
		require.True(t, ok)
		for err == nil && ok {
			_, ok, err = r.Next()
		}
		fe := requireFormatError(t, err, changefile.ErrTruncated, int64(n))
		require.NotEqual(t, changefile.SectionType(0), fe.Section)

		_, err = changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrTruncated, int64(n))
	}
}

func TestTruncatedTableAndTrailer(t *testing.T) {
	_, err := changefile.Verify(changefixture.WorkedExample()[:80])
	requireFormatError(t, err, changefile.ErrTruncated, 80)

	_, err = changefile.Verify(changefixture.WorkedExample()[:exLen-1])
	requireFormatError(t, err, changefile.ErrTruncated, exLen-1)

	_, err = changefile.Verify(changefixture.WorkedExample()[:3])
	requireFormatError(t, err, changefile.ErrTruncated, 3)
}

func TestAppendedByte(t *testing.T) {
	b := append(changefixture.WorkedExample(), 0)

	_, err := changefile.Parse(b, newDecoder(t))
	requireFormatError(t, err, changefile.ErrTrailingBytes, exLen)
	_, err = changefile.Verify(b)
	requireFormatError(t, err, changefile.ErrTrailingBytes, exLen)

	f, err := changefile.Parse(b, newDecoder(t), changefile.AllowTrailingBytes())
	require.NoError(t, err)
	require.Equal(t, b[:exLen], f.Bytes())
	require.Equal(t, changefixture.WorkedExampleHash, f.Hash.String())

	h, err := changefile.Verify(b, changefile.AllowTrailingBytes())
	require.NoError(t, err)
	require.Equal(t, changefixture.WorkedExampleHash, h.String())
}

func TestDepsBeforeHeader(t *testing.T) {
	header := section(changefile.SectionHeader, changefixture.WorkedExampleHeader())
	deps := section(changefile.SectionDeps, []byte{0})
	headerAt := int64(changefile.HeaderSize + ident.Size + len(deps))

	t.Run("version 2 refuses it", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version2}, deps, header)
		r, err := changefile.NewReader(b, newDecoder(t))
		require.NoError(t, err)
		s, ok, err := r.Next()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, changefile.SectionDeps, s.Type)

		_, _, err = r.Next()
		fe := requireFormatError(t, err, changefile.ErrUnexpectedSection, headerAt)
		require.Equal(t, changefile.SectionHeader, fe.Section)

		_, err = r.Finish()
		require.Same(t, fe, errAs(t, err))

		_, err = changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrUnexpectedSection, headerAt)
	})

	t.Run("version 1 accepts it", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version1}, deps, header)
		f, err := changefile.Parse(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, []changefile.SectionType{changefile.SectionDeps, changefile.SectionHeader}, types(f))
		_, err = changefile.Verify(b)
		require.NoError(t, err)
	})
}

func errAs(t *testing.T, err error) *changefile.FormatError {
	t.Helper()
	var fe *changefile.FormatError
	require.ErrorAs(t, err, &fe)
	return fe
}

func types(f *changefile.File) []changefile.SectionType {
	var ts []changefile.SectionType
	for _, s := range f.Sections {
		ts = append(ts, s.Type)
	}
	return ts
}

func TestSpanSection(t *testing.T) {
	header := section(changefile.SectionHeader, changefixture.WorkedExampleHeader())
	deps := section(changefile.SectionDeps, []byte{0})
	span := section(changefile.SectionSpan, []byte("ATSP\x01\x00\x00"))
	spanAt := int64(changefile.HeaderSize + ident.Size + len(header) + len(deps))

	// A version 1 or 2 header frames a SPAN section only through another
	// count; ContentChunks 1 frames the third section.
	for _, v := range []changefile.Version{changefile.Version1, changefile.Version2} {
		b := build(t, changefile.FileHeader{Version: v, ContentChunks: 1}, header, deps, span)
		_, err := changefile.Parse(b, newDecoder(t))
		fe := requireFormatError(t, err, changefile.ErrUnexpectedSection, spanAt)
		require.Equal(t, changefile.SectionSpan, fe.Section)
		_, err = changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrUnexpectedSection, spanAt)
	}

	t.Run("version 3 accepts one", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version3}, header, deps, span)
		f, err := changefile.Parse(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, []byte("ATSP\x01\x00\x00"), f.Sections[2].Payload)
	})

	t.Run("version 3 refuses two", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version3, GraphSections: 1}, header, deps, span, span)
		_, err := changefile.Verify(b)
		fe := requireFormatError(t, err, changefile.ErrUnexpectedSection, spanAt+int64(len(span)))
		require.Equal(t, changefile.SectionSpan, fe.Section)
	})

	t.Run("version 3 requires one", func(t *testing.T) {
		// GraphSections and the SPAN count frame four sections; a CONTENT
		// section stands where SPAN should be.
		content := changefixture.WorkedExample()[exContentSection:exTrailer]
		graph := section(changefile.SectionGraph, nil)
		b := build(t, changefile.FileHeader{Version: changefile.Version3, GraphSections: 1}, header, deps, graph, content)
		_, err := changefile.Verify(b)
		fe := requireFormatError(t, err, changefile.ErrInvalidHeader, -1)
		require.Equal(t, changefile.SectionSpan, fe.Section)
	})
}

func TestVersion1YieldsRepeatedSections(t *testing.T) {
	header := section(changefile.SectionHeader, changefixture.WorkedExampleHeader())
	first := section(changefile.SectionDeps, []byte{0})
	second := section(changefile.SectionDeps, []byte{1, 1})
	b := build(t, changefile.FileHeader{Version: changefile.Version1, ContentChunks: 1}, header, first, second)

	r, err := changefile.NewReader(b, newDecoder(t))
	require.NoError(t, err)
	var deps [][]byte
	for {
		s, ok, err := r.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		if s.Type == changefile.SectionDeps {
			deps = append(deps, s.Payload)
		}
	}
	require.Equal(t, [][]byte{{0}, {1, 1}}, deps)
	f, err := r.Finish()
	require.NoError(t, err)
	require.Len(t, f.Sections, 3)

	t.Run("version 2 refuses them", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version2, ContentChunks: 1}, header, first, second)
		_, err := changefile.Verify(b)
		fe := requireFormatError(t, err, changefile.ErrUnexpectedSection, int64(changefile.HeaderSize+ident.Size+len(header)+len(first)))
		require.Equal(t, changefile.SectionDeps, fe.Section)
	})
}

func TestVersion2Duplicates(t *testing.T) {
	header := section(changefile.SectionHeader, changefixture.WorkedExampleHeader())
	deps := section(changefile.SectionDeps, []byte{0})
	for _, typ := range []changefile.SectionType{changefile.SectionProvenance, changefile.SectionUnhashed} {
		t.Run(typ.String(), func(t *testing.T) {
			dup := section(typ, []byte{0})
			b := build(t, changefile.FileHeader{Version: changefile.Version2, ContentChunks: 2}, header, deps, dup, dup)
			_, err := changefile.Verify(b)
			fe := requireFormatError(t, err, changefile.ErrUnexpectedSection, int64(changefile.HeaderSize+ident.Size+len(header)+len(deps)+len(dup)))
			require.Equal(t, typ, fe.Section)
		})
	}
	t.Run("HEADER", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version2}, header, header)
		_, err := changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrUnexpectedSection, int64(changefile.HeaderSize+ident.Size+len(header)))
	})
}

func TestSectionManifest(t *testing.T) {
	header := section(changefile.SectionHeader, changefixture.WorkedExampleHeader())
	deps := section(changefile.SectionDeps, []byte{0})
	graph := section(changefile.SectionGraph, nil)
	content := changefixture.WorkedExample()[exContentSection:exTrailer]

	t.Run("version 2 checks the counts", func(t *testing.T) {
		// GraphSections frames the CONTENT section, which the header does
		// not declare.
		b := build(t, changefile.FileHeader{Version: changefile.Version2, GraphSections: 1}, header, deps, content)
		_, err := changefile.Parse(b, newDecoder(t))
		fe := requireFormatError(t, err, changefile.ErrInvalidHeader, -1)
		require.Equal(t, changefile.SectionGraph, fe.Section)
	})

	t.Run("version 2 checks the flags", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version2, Flags: changefile.FlagUnhashed}, header, deps, graph)
		_, err := changefile.Verify(b)
		fe := requireFormatError(t, err, changefile.ErrInvalidHeader, -1)
		require.Equal(t, changefile.SectionGraph, fe.Section)
	})

	t.Run("version 2 checks HEADER", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version2, GraphSections: 1}, deps, graph, graph)
		_, err := changefile.Verify(b)
		fe := requireFormatError(t, err, changefile.ErrInvalidHeader, -1)
		require.Equal(t, changefile.SectionHeader, fe.Section)
	})

	t.Run("version 1 does not check the counts", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version1, GraphSections: 1}, header, deps, content)
		f, err := changefile.Parse(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, changefixture.WorkedExampleContent, string(f.Content()))
	})

	t.Run("version 1 requires HEADER", func(t *testing.T) {
		b := build(t, changefile.FileHeader{Version: changefile.Version1}, deps, deps)
		_, err := changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrInvalidHeader, -1)
	})

	t.Run("signed and unhashed sections", func(t *testing.T) {
		sig := section(changefile.SectionSignature, []byte("sig"))
		unhashed := section(changefile.SectionUnhashed, []byte("{}"))
		h := changefile.FileHeader{Version: changefile.Version2, Flags: changefile.FlagSignature | changefile.FlagUnhashed}
		b := build(t, h, header, deps, sig, unhashed)
		f, err := changefile.Parse(b, newDecoder(t))
		require.NoError(t, err)
		require.Equal(t, ident.Sum(slices.Concat(make([]byte, ident.Size), header, deps)), f.Hash)
		require.Equal(t, []byte("{}"), f.Sections[3].Payload)
	})
}

func TestLegacyFormat(t *testing.T) {
	for _, v := range []uint32{1, 2} {
		b := changefixture.WorkedExample()
		binary.LittleEndian.PutUint32(b, v)
		_, err := changefile.Parse(b, newDecoder(t))
		requireFormatError(t, err, changefile.ErrLegacyFormat, 0)
		_, err = changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrLegacyFormat, 0)
		require.NotErrorIs(t, err, changefile.ErrInvalidMagic)
	}
	for _, v := range []uint32{0, 3, 0xffffffff} {
		b := changefixture.WorkedExample()
		binary.LittleEndian.PutUint32(b, v)
		_, err := changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrInvalidMagic, 0)
	}
}

func TestHeaderAndTableChecks(t *testing.T) {
	t.Run("unsupported version", func(t *testing.T) {
		b := changefixture.WorkedExample()
		b[4] = 4
		_, err := changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrUnsupportedVersion, 4)
	})
	t.Run("semantic flag without sections", func(t *testing.T) {
		b := changefixture.WorkedExample()
		b[8] = byte(changefile.FlagSemantic)
		_, err := changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrInvalidHeader, 8)
	})
	t.Run("duplicate table entry", func(t *testing.T) {
		b := changefixture.WorkedExample()
		b[12] = 2
		b = slices.Insert(b, changefile.HeaderSize, make([]byte, ident.Size)...)
		_, err := changefile.Verify(b)
		requireFormatError(t, err, changefile.ErrInvalidHeader, changefile.HeaderSize+ident.Size)
	})
	t.Run("unknown section type", func(t *testing.T) {
		b := changefixture.WorkedExample()
		b[exHeaderSection] = 0x21
		_, err := changefile.Verify(b)
		fe := requireFormatError(t, err, changefile.ErrInvalidSectionType, exHeaderSection)
		require.Contains(t, fe.Error(), "0x21")
	})
}

func TestSmallestFile(t *testing.T) {
	// docs/remote/change-format/container.md#file-layout: the header, a
	// one-entry table, two empty section frames and the trailer.
	b := build(t, changefile.FileHeader{Version: changefile.Version2},
		rawSection(changefile.SectionHeader, nil),
		rawSection(changefile.SectionDeps, nil),
	)
	require.Len(t, b, 138)

	dec := &countingDecompressor{dec: newDecoder(t)}
	f, err := changefile.Parse(b, dec)
	require.NoError(t, err)
	require.Zero(t, dec.calls)
	require.Len(t, f.Sections, 2)
	for _, s := range f.Sections {
		require.NotNil(t, s.Payload)
		require.Empty(t, s.Payload)
		require.Empty(t, s.Compressed)
	}
	require.Nil(t, f.Content())
	require.Equal(t, ident.Sum(b[changefile.HeaderSize:138-changefile.TrailerSize]), f.Hash)

	h, err := changefile.Verify(b)
	require.NoError(t, err)
	require.Equal(t, f.Hash, h)

	t.Run("the worked example calls it once per section", func(t *testing.T) {
		dec := &countingDecompressor{dec: newDecoder(t)}
		_, err := changefile.Parse(changefixture.WorkedExample(), dec)
		require.NoError(t, err)
		require.Equal(t, 3, dec.calls)
	})
}

func TestDecompressErrorComesFirst(t *testing.T) {
	// A DEPS payload that will not decompress, followed by a section of an
	// unknown type: the reader decompresses DEPS before it reads the next
	// type byte.
	b := changefixture.WorkedExample()
	b[exDepsZstd] ^= 0xff
	b[exContentSection] = 0x21

	_, err := changefile.Parse(b, newDecoder(t))
	fe := requireFormatError(t, err, changefile.ErrDecompress, exDepsSection)
	require.Equal(t, changefile.SectionDeps, fe.Section)
	require.Error(t, fe.Err)

	// Verify decompresses nothing, so it reaches the type byte.
	_, err = changefile.Verify(b)
	requireFormatError(t, err, changefile.ErrInvalidSectionType, exContentSection)
}

func TestDecompressorErrorIsKept(t *testing.T) {
	cause := errors.New("boom")
	_, err := changefile.Parse(changefixture.WorkedExample(), failingDecompressor{err: cause})
	requireFormatError(t, err, changefile.ErrDecompress, exHeaderSection)
	require.ErrorIs(t, err, cause)
}

func TestErrorsAreSticky(t *testing.T) {
	b := changefixture.WorkedExample()
	b[exContentSection] = 0x21
	r, err := changefile.NewReader(b, newDecoder(t))
	require.NoError(t, err)
	var nextErr error
	for nextErr == nil {
		_, _, nextErr = r.Next()
	}
	_, ok, err := r.Next()
	require.False(t, ok)
	require.Equal(t, nextErr, err)
	_, err = r.Finish()
	require.Equal(t, nextErr, err)

	// A failing Finish is sticky too.
	b = changefixture.WorkedExample()
	b[exContentPayload] ^= 1
	r, err = changefile.NewReader(b, newDecoder(t))
	require.NoError(t, err)
	_, err = r.Finish()
	require.ErrorIs(t, err, changefile.ErrHashMismatch)
	_, err2 := r.Finish()
	require.Equal(t, err, err2)
	_, _, err2 = r.Next()
	require.Equal(t, err, err2)
}

func TestCheckChunks(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(b []byte)
		verify bool // whether Verify, which has no payload, finds it too
	}{
		{"chunk hash", func(b []byte) { b[exChunkHash] ^= 0x01 }, false},
		{"chunk index", func(b []byte) { b[exChunkIndex] = 1 }, true},
		{"uncompressed length", func(b []byte) { b[exUncompressed] = 9 }, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := changefixture.WorkedExample()
			tc.mutate(b)
			retrail(b)

			// Atomic's reader checks none of it.
			_, err := changefile.Parse(b, newDecoder(t))
			require.NoError(t, err)

			_, err = changefile.Parse(b, newDecoder(t), changefile.CheckChunks())
			fe := requireFormatError(t, err, changefile.ErrChunkMismatch, exContentSection)
			require.Equal(t, changefile.SectionContent, fe.Section)

			_, err = changefile.Verify(b, changefile.CheckChunks())
			if tc.verify {
				requireFormatError(t, err, changefile.ErrChunkMismatch, exContentSection)
			} else {
				require.NoError(t, err)
			}
		})
	}

	t.Run("the worked example passes", func(t *testing.T) {
		_, err := changefile.Parse(changefixture.WorkedExample(), newDecoder(t), changefile.CheckChunks())
		require.NoError(t, err)
		_, err = changefile.Verify(changefixture.WorkedExample(), changefile.CheckChunks())
		require.NoError(t, err)
	})

	t.Run("indices count from 0", func(t *testing.T) {
		header := section(changefile.SectionHeader, changefixture.WorkedExampleHeader())
		deps := section(changefile.SectionDeps, []byte{0})
		chunk := changefixture.WorkedExample()[exContentSection:exTrailer]
		h := changefile.FileHeader{Version: changefile.Version2, ContentChunks: 2}
		b := build(t, h, header, deps, chunk, chunk)
		_, err := changefile.Verify(b)
		require.NoError(t, err)
		_, err = changefile.Verify(b, changefile.CheckChunks())
		requireFormatError(t, err, changefile.ErrChunkMismatch, int64(changefile.HeaderSize+ident.Size+len(header)+len(deps)+len(chunk)))
	})
}

func TestVerifyDoesNotDecompress(t *testing.T) {
	b := changefixture.WorkedExample()
	for i := exContentZstd; i < exTrailer; i++ {
		b[i] = 0xee
	}
	// The file has no unhashed sections, so its hash input is everything
	// from the table to the trailer.
	want := ident.Sum(b[changefile.HeaderSize : len(b)-changefile.TrailerSize])
	copy(b[len(b)-changefile.TrailerSize:], want[:])

	h, err := changefile.Verify(b)
	require.NoError(t, err)
	require.Equal(t, want, h)

	_, err = changefile.Parse(b, newDecoder(t))
	fe := requireFormatError(t, err, changefile.ErrDecompress, exContentSection)
	require.Equal(t, changefile.SectionContent, fe.Section)
}

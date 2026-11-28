package changefile_test

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
)

// workedExampleHeader is bytes 0 to 63 of span_opaque_v2_reference.hex, as
// docs/remote/change-format/container.md#worked-example-a-version-2-file
// lays them out.
const workedExampleHeader = "41544f4d" + // magic
	"02000000" + // version 2
	"00000000" + // flags
	"01000000" + // hash_table_entries
	"00000000" + // graph_section_count
	"00000000" + // semantic_section_count
	"01000000" + // contents_chunks
	"0000000000000000" + // total_uncompressed
	"00000000000000000000000000000000000000000000000000000000" // reserved

func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

// headerBytes returns the worked example's header with each u32 field at
// the given offset replaced.
func headerBytes(t testing.TB, fields map[int]uint32) []byte {
	t.Helper()
	b := mustHex(t, workedExampleHeader)
	for off, v := range fields {
		binary.LittleEndian.PutUint32(b[off:], v)
	}
	return b
}

func requireFormatError(t *testing.T, err error, kind error, offset int64) *changefile.FormatError {
	t.Helper()
	require.ErrorIs(t, err, kind)
	var fe *changefile.FormatError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, kind, fe.Kind)
	require.Equal(t, offset, fe.Offset)
	return fe
}

func TestConstants(t *testing.T) {
	require.Equal(t, "ATOM", changefile.Magic)
	require.Equal(t, 64, changefile.HeaderSize)
	require.Equal(t, 32, changefile.TrailerSize)
	require.Equal(t, 65534, changefile.MaxHashTableEntries)
	require.Equal(t, changefile.Version(1), changefile.Version1)
	require.Equal(t, changefile.Version(2), changefile.Version2)
	require.Equal(t, changefile.Version(3), changefile.Version3)
	require.Equal(t, changefile.Flags(0x01), changefile.FlagProvenance)
	require.Equal(t, changefile.Flags(0x02), changefile.FlagSemantic)
	require.Equal(t, changefile.Flags(0x04), changefile.FlagUnhashed)
	require.Equal(t, changefile.Flags(0x08), changefile.FlagSignature)
}

func TestParseWorkedExampleHeader(t *testing.T) {
	b := mustHex(t, workedExampleHeader)
	require.Len(t, b, changefile.HeaderSize)

	h, err := changefile.ParseFileHeader(b)
	require.NoError(t, err)
	require.Equal(t, changefile.FileHeader{
		Version:          changefile.Version2,
		HashTableEntries: 1,
		ContentChunks:    1,
	}, h)
	require.NoError(t, h.Validate())
	require.Equal(t, uint64(3), h.SectionCount())

	out, err := h.AppendBinary(nil)
	require.NoError(t, err)
	require.Equal(t, b, out)

	// AppendBinary appends rather than overwriting.
	out, err = h.AppendBinary([]byte("prefix"))
	require.NoError(t, err)
	require.Equal(t, append([]byte("prefix"), b...), out)
}

func TestParseFileHeaderIgnoresFollowingBytes(t *testing.T) {
	b := append(mustHex(t, workedExampleHeader), 0xde, 0xad)
	h, err := changefile.ParseFileHeader(b)
	require.NoError(t, err)
	require.Equal(t, uint32(1), h.HashTableEntries)
}

// The 0.19.1 fixture's header: flags 0x0a (HAS_SEMANTIC | HAS_SIGNATURE),
// one GRAPH, one SEMANTIC and one CONTENT section
// (docs/remote/change-format/container.md#other-fixtures).
func TestSectionCountSignedLegacyFile(t *testing.T) {
	b := headerBytes(t, map[int]uint32{4: 1, 8: 0x0a, 16: 1, 20: 1, 24: 1})
	h, err := changefile.ParseFileHeader(b)
	require.NoError(t, err)
	require.Equal(t, changefile.Version1, h.Version)
	require.True(t, h.Flags.Has(changefile.FlagSemantic))
	require.True(t, h.Flags.Has(changefile.FlagSignature))
	require.False(t, h.Flags.Has(changefile.FlagProvenance))
	require.False(t, h.Flags.Has(changefile.FlagUnhashed))
	require.NoError(t, h.Validate())
	require.Equal(t, uint64(6), h.SectionCount())
}

func TestSectionCount(t *testing.T) {
	testCases := []struct {
		name string
		h    changefile.FileHeader
		want uint64
	}{
		{"bare v2", changefile.FileHeader{Version: changefile.Version2}, 2},
		{"v3 adds SPAN", changefile.FileHeader{Version: changefile.Version3}, 3},
		{"v1 has no SPAN", changefile.FileHeader{Version: changefile.Version1}, 2},
		{
			"every flag",
			changefile.FileHeader{
				Version:          changefile.Version2,
				Flags:            changefile.FlagProvenance | changefile.FlagSemantic | changefile.FlagUnhashed | changefile.FlagSignature,
				SemanticSections: 1,
			},
			6,
		},
		{"unknown bits add nothing", changefile.FileHeader{Version: changefile.Version2, Flags: 0xFFFFFFF0}, 2},
		{
			"counts do not overflow",
			changefile.FileHeader{
				Version:          changefile.Version3,
				Flags:            changefile.FlagProvenance | changefile.FlagSemantic | changefile.FlagUnhashed | changefile.FlagSignature,
				GraphSections:    0xFFFFFFFF,
				SemanticSections: 0xFFFFFFFF,
				ContentChunks:    0xFFFFFFFF,
			},
			6 + 3*0xFFFFFFFF,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.h.SectionCount())
		})
	}
}

func TestUnknownFlagsAndReservedSurvive(t *testing.T) {
	b := headerBytes(t, map[int]uint32{8: 0xA5A5A5F0 | uint32(changefile.FlagProvenance)})
	binary.LittleEndian.PutUint64(b[28:], 0x0102030405060708)
	for i := 36; i < 64; i++ {
		b[i] = byte(i)
	}

	h, err := changefile.ParseFileHeader(b)
	require.NoError(t, err)
	require.Equal(t, changefile.Flags(0xA5A5A5F1), h.Flags)
	require.Equal(t, changefile.Flags(0xA5A5A5F0), h.Flags.Unknown())
	require.True(t, h.Flags.Has(changefile.FlagProvenance))
	require.Equal(t, uint64(0x0102030405060708), h.TotalUncompressed)
	for i, v := range h.Reserved {
		require.Equal(t, byte(36+i), v)
	}
	require.NoError(t, h.Validate())
	require.Equal(t, uint64(4), h.SectionCount())

	out, err := h.AppendBinary(nil)
	require.NoError(t, err)
	require.Equal(t, b, out)
}

func TestParseFileHeaderVersions(t *testing.T) {
	for _, v := range []uint32{1, 2, 3} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			h, err := changefile.ParseFileHeader(headerBytes(t, map[int]uint32{4: v}))
			require.NoError(t, err)
			require.Equal(t, changefile.Version(v), h.Version)
			require.True(t, h.Version.Supported())
		})
	}
	for _, v := range []uint32{0, 4, 5, 0xFFFFFFFF} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			_, err := changefile.ParseFileHeader(headerBytes(t, map[int]uint32{4: v}))
			fe := requireFormatError(t, err, changefile.ErrUnsupportedVersion, 4)
			require.Contains(t, fe.Error(), fmt.Sprintf("got %d", v))
			require.False(t, changefile.Version(v).Supported())

			err = changefile.FileHeader{Version: changefile.Version(v)}.Validate()
			requireFormatError(t, err, changefile.ErrUnsupportedVersion, 4)
		})
	}
}

func TestParseFileHeaderInvalidMagic(t *testing.T) {
	testCases := map[string][]byte{
		"lower case":     []byte("atom"),
		"legacy bincode": {0x02, 0x00, 0x00, 0x00},
		"empty magic":    {0, 0, 0, 0},
	}
	for name, magic := range testCases {
		t.Run(name, func(t *testing.T) {
			b := mustHex(t, workedExampleHeader)
			copy(b, magic)
			_, err := changefile.ParseFileHeader(b)
			requireFormatError(t, err, changefile.ErrInvalidMagic, 0)
			require.NotErrorIs(t, err, changefile.ErrLegacyFormat)
		})
	}

	// A short input with the wrong magic is refused for its magic.
	_, err := changefile.ParseFileHeader([]byte("ATOX"))
	requireFormatError(t, err, changefile.ErrInvalidMagic, 0)
}

func TestParseFileHeaderTruncated(t *testing.T) {
	b := mustHex(t, workedExampleHeader)
	for _, n := range []int{0, 3, 4, 63} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			_, err := changefile.ParseFileHeader(b[:n])
			requireFormatError(t, err, changefile.ErrTruncated, int64(n))
		})
	}
}

func TestValidateHashTableEntries(t *testing.T) {
	h := changefile.FileHeader{Version: changefile.Version2, HashTableEntries: changefile.MaxHashTableEntries}
	require.NoError(t, h.Validate())

	h.HashTableEntries = 65535
	fe := requireFormatError(t, h.Validate(), changefile.ErrInvalidHeader, 12)
	require.Contains(t, fe.Error(), "65535")

	// The same header read from bytes parses, and is refused by Validate.
	parsed, err := changefile.ParseFileHeader(headerBytes(t, map[int]uint32{12: 65535}))
	require.NoError(t, err)
	requireFormatError(t, parsed.Validate(), changefile.ErrInvalidHeader, 12)
}

func TestValidateSemanticFlag(t *testing.T) {
	t.Run("flag without sections", func(t *testing.T) {
		h := changefile.FileHeader{Version: changefile.Version2, Flags: changefile.FlagSemantic}
		fe := requireFormatError(t, h.Validate(), changefile.ErrInvalidHeader, 8)
		require.Contains(t, fe.Error(), "HAS_SEMANTIC")
	})
	t.Run("sections without flag", func(t *testing.T) {
		h := changefile.FileHeader{Version: changefile.Version2, SemanticSections: 2}
		fe := requireFormatError(t, h.Validate(), changefile.ErrInvalidHeader, 20)
		require.Contains(t, fe.Error(), "HAS_SEMANTIC")
	})
	t.Run("both", func(t *testing.T) {
		h := changefile.FileHeader{Version: changefile.Version2, Flags: changefile.FlagSemantic, SemanticSections: 2}
		require.NoError(t, h.Validate())
	})
	t.Run("other flags are not compared", func(t *testing.T) {
		h := changefile.FileHeader{Version: changefile.Version2, Flags: changefile.FlagProvenance | changefile.FlagUnhashed | changefile.FlagSignature | 0x100}
		require.NoError(t, h.Validate())
	})
}

func TestFormatErrorIs(t *testing.T) {
	_, err := changefile.ParseFileHeader(headerBytes(t, map[int]uint32{4: 9}))
	require.ErrorIs(t, err, changefile.ErrUnsupportedVersion)
	require.NotErrorIs(t, err, changefile.ErrInvalidHeader)

	wrapped := fmt.Errorf("reading change: %w", err)
	require.ErrorIs(t, wrapped, changefile.ErrUnsupportedVersion)
	var fe *changefile.FormatError
	require.ErrorAs(t, wrapped, &fe)
	require.Equal(t, int64(4), fe.Offset)

	cause := errors.New("zstd: corrupt block")
	fe = &changefile.FormatError{
		Kind:    changefile.ErrDecompress,
		Section: changefile.SectionContent,
		Offset:  179,
		Detail:  "chunk 0",
		Err:     cause,
	}
	require.ErrorIs(t, fe, changefile.ErrDecompress)
	require.ErrorIs(t, fe, cause)
	require.NotErrorIs(t, fe, changefile.ErrCompress)
	require.Equal(t, "changefile: decompression error in CONTENT section at offset 179: chunk 0: zstd: corrupt block", fe.Error())

	// Offset -1 and Section 0 are left out of the text.
	fe = &changefile.FormatError{Kind: changefile.ErrHashNotFound, Offset: -1}
	require.Equal(t, "changefile: hash not found in dedup table", fe.Error())

	// A FormatError without a Kind matches no sentinel, not even nil.
	fe = &changefile.FormatError{Offset: -1}
	require.NotErrorIs(t, fe, changefile.ErrInvalidHeader)
	require.True(t, strings.HasPrefix(fe.Error(), "changefile: "))
}

func TestHashMismatchError(t *testing.T) {
	e := &changefile.HashMismatchError{Computed: [32]byte{1}}
	require.ErrorIs(t, e, changefile.ErrHashMismatch)
	require.NotErrorIs(t, e, changefile.ErrInvalidHeader)
	require.Contains(t, e.Error(), e.Trailer.String())
	require.Contains(t, e.Error(), e.Computed.String())

	// The reader reports one as the cause of a FormatError.
	fe := &changefile.FormatError{Kind: changefile.ErrHashMismatch, Offset: 243, Err: e}
	require.ErrorIs(t, fe, changefile.ErrHashMismatch)
	var hm *changefile.HashMismatchError
	require.ErrorAs(t, fe, &hm)
	require.Equal(t, e.Computed, hm.Computed)
}

func TestSentinelsAreDistinct(t *testing.T) {
	all := []error{
		changefile.ErrInvalidMagic, changefile.ErrLegacyFormat, changefile.ErrUnsupportedVersion,
		changefile.ErrInvalidHeader, changefile.ErrInvalidSectionType, changefile.ErrUnexpectedSection,
		changefile.ErrTruncated, changefile.ErrDecompress, changefile.ErrCompress,
		changefile.ErrHashMismatch, changefile.ErrHashTableFull, changefile.ErrHashIndexOutOfBounds,
		changefile.ErrHashNotFound, changefile.ErrInvalidChangeMetadata, changefile.ErrInvalidGraphOp,
		changefile.ErrPayload, changefile.ErrValidation, changefile.ErrTrailingBytes,
		changefile.ErrChunkMismatch,
	}
	texts := map[string]bool{}
	for i, a := range all {
		texts[a.Error()] = true
		for j, b := range all {
			if i != j {
				require.NotErrorIs(t, a, b)
			}
		}
	}
	require.Len(t, texts, len(all))
}

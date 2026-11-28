package changefixture_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/changefixture"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	require.NoError(t, err)
	return b
}

func TestWorkedExampleHeader(t *testing.T) {
	// docs/remote/change-format/sections.md#header-example
	want := mustHex(t, "11"+
		"6f 70 61 71 75 65 20 76 32 20"+
		"66 69 78 74 75 72 65"+
		"00"+
		"14"+
		"32 30 32 33 2d 31 31 2d 31 34 54"+
		"32 32 3a 31 33 3a 32 30 5a"+
		"00 00 00 00 00 00"+
		"07 41 54 53 50 01 00 00")
	got := changefixture.WorkedExampleHeader()
	require.Len(t, got, 54)
	require.Equal(t, want, got)
}

func TestWorkedExampleLayout(t *testing.T) {
	// docs/remote/change-format/container.md#worked-example-a-version-2-file
	b := changefixture.WorkedExample()
	require.Len(t, b, 275)

	testCases := []struct {
		name string
		off  int
		hex  string
	}{
		{"magic", 0, "41 54 4f 4d"},
		{"version", 4, "02 00 00 00"},
		{"flags", 8, "00 00 00 00"},
		{"hash_table_entries", 12, "01 00 00 00"},
		{"graph_section_count", 16, "00 00 00 00"},
		{"semantic_section_count", 20, "00 00 00 00"},
		{"contents_chunks", 24, "01 00 00 00"},
		{"total_uncompressed", 28, strings.Repeat("00", 8)},
		{"reserved", 36, strings.Repeat("00", 28)},
		{"table slot 0", 64, strings.Repeat("00", 32)},
		{"HEADER frame", 96, "01 3f 00 00 00"},
		{"HEADER zstd frame and block header", 101, "28 b5 2f fd 00 58 b1 01 00"},
		{"HEADER payload start", 110, "11 6f 70 61"},
		{"DEPS frame", 164, "02 0a 00 00 00"},
		{"DEPS zstd frame, block header and payload", 169, "28 b5 2f fd 00 58 09 00 00 00"},
		{"CONTENT type and chunk_index", 179, "20 00 00 00 00"},
		{"chunk_hash start", 184, "37 f9 71 49"},
		{"chunk_hash end", 212, "ce 9a 08 e6"},
		{"uncompressed_len and compressed_len", 216, "0a 00 00 00 13 00 00 00"},
		{"CONTENT zstd frame and block header", 224, "28 b5 2f fd 00 58 51 00 00"},
		{"CONTENT payload", 233, "6f 6c 64 20 62 79 74 65 73 0a"},
		{"trailer", 243, "cf90221730dcd4c8f3088b9b00001c317c251a0f66536aabd8a75dd902fcef8e"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			want := mustHex(t, tc.hex)
			require.Equal(t, want, b[tc.off:tc.off+len(want)])
		})
	}
}

func TestWorkedExampleHash(t *testing.T) {
	b := changefixture.WorkedExample()
	h, err := ident.ParseHash(changefixture.WorkedExampleHash)
	require.NoError(t, err)
	require.Equal(t, h[:], b[len(b)-32:])
	require.Equal(t, h, ident.Sum(b[64:243]))
	require.Equal(t, changefixture.WorkedExampleHash, h.String())
}

func TestWorkedExampleContent(t *testing.T) {
	b := changefixture.WorkedExample()
	require.Equal(t, changefixture.WorkedExampleContent, string(b[233:243]))
	sum := ident.Sum([]byte(changefixture.WorkedExampleContent))
	require.Equal(t, sum[:], b[184:216])
}

func TestWorkedExampleReturnsANewSlice(t *testing.T) {
	a := changefixture.WorkedExample()
	a[0] = 'X'
	require.Equal(t, byte('A'), changefixture.WorkedExample()[0])

	h := changefixture.WorkedExampleHeader()
	h[0] = 0
	require.Equal(t, byte(17), changefixture.WorkedExampleHeader()[0])
}

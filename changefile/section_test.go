package changefile_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/zstdframe"
)

// zstdframe's codecs are what the reader and writer are given.
var (
	_ changefile.Decompressor = (*zstdframe.Decoder)(nil)
	_ changefile.Compressor   = (*zstdframe.Encoder)(nil)
	_ changefile.Compressor   = zstdframe.Stored{}
)

func TestSectionTypes(t *testing.T) {
	// docs/remote/change-format/container.md#section-order-and-multiplicity
	testCases := []struct {
		t      changefile.SectionType
		b      byte
		name   string
		order  int
		hashed bool
		frame  int
	}{
		{changefile.SectionHeader, 0x01, "HEADER", 0, true, 5},
		{changefile.SectionDeps, 0x02, "DEPS", 1, true, 5},
		{changefile.SectionProvenance, 0x03, "PROVENANCE", 2, true, 5},
		{changefile.SectionSignature, 0x04, "SIGNATURE", 3, false, 5},
		{changefile.SectionGraph, 0x10, "GRAPH", 4, true, 5},
		{changefile.SectionSpan, 0x11, "SPAN", 5, true, 5},
		{changefile.SectionSemantic, 0x30, "SEMANTIC", 6, true, 5},
		{changefile.SectionContent, 0x20, "CONTENT", 7, true, 45},
		{changefile.SectionUnhashed, 0xF0, "UNHASHED", 8, false, 5},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, changefile.SectionType(tc.b), tc.t)
			require.True(t, tc.t.Known())
			require.Equal(t, tc.name, tc.t.String())
			require.Equal(t, tc.order, tc.t.Order())
			require.Equal(t, tc.hashed, tc.t.Hashed())
			require.Equal(t, tc.frame, tc.t.FrameSize())
		})
	}
	require.Equal(t, 5, changefile.FrameSize)
	require.Equal(t, 45, changefile.ChunkFrameSize)
}

func TestUnknownSectionTypes(t *testing.T) {
	// The bytes Atomic's own test refuses.
	for _, b := range []byte{0x00, 0x0F, 0x12, 0x21, 0x31, 0xFF} {
		st := changefile.SectionType(b)
		require.False(t, st.Known(), "%#02x", b)
		require.False(t, st.Hashed(), "%#02x", b)
		require.Equal(t, -1, st.Order(), "%#02x", b)
		require.Equal(t, 5, st.FrameSize(), "%#02x", b)
	}
	require.Equal(t, "SectionType(0x0f)", changefile.SectionType(0x0F).String())
	require.Equal(t, "SectionType(0x00)", changefile.SectionType(0).String())

	// Exactly nine of the 256 bytes are section types.
	known := 0
	for b := range 256 {
		if changefile.SectionType(b).Known() {
			known++
		}
	}
	require.Equal(t, 9, known)
}

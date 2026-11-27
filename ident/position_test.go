package ident_test

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
)

func mustParseHash(t *testing.T, s string) ident.Hash {
	t.Helper()
	h, err := ident.ParseHash(s)
	require.NoError(t, err)
	return h
}

func TestParsePosition(t *testing.T) {
	change := mustParseHash(t, goldenBase32)

	for _, tc := range []struct {
		name string
		in   string
		want uint64
	}{
		{"DE is 100", goldenBase32 + ".DE", 100},
		{"A is 0", goldenBase32 + ".A", 0},
		{"leading A's are accepted", goldenBase32 + ".AADE", 100},
		{"an empty offset is 0", goldenBase32 + ".", 0},
		{"the offset is read in either case", goldenBase32 + ".de", 100},
		{"mixed case", goldenBase32 + ".dE", 100},
		{"a digit group", goldenBase32 + ".B2", 32 + 26},
		{"the largest offset", goldenBase32 + ".P777777777777", math.MaxUint64},
		{"the largest offset with leading A's", goldenBase32 + ".AAP777777777777", math.MaxUint64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ident.ParsePosition(tc.in)
			require.NoError(t, err)
			require.Equal(t, ident.Position{Change: change, Offset: tc.want}, got)
		})
	}

	t.Run("the change part is read leniently", func(t *testing.T) {
		got, err := ident.ParsePosition(strings.ToLower(goldenBase32) + ".DE")
		require.NoError(t, err)
		require.Equal(t, ident.Position{Change: change, Offset: 100}, got)
	})

	t.Run("the text is split at the first dot", func(t *testing.T) {
		in := goldenBase32 + ".D.E"
		_, err := ident.ParsePosition(in)
		requireParseError(t, err, ident.ErrSyntax, len(goldenBase32)+2)
	})

	dot := len(goldenBase32) + 1
	for _, tc := range []struct {
		name   string
		in     string
		err    error
		offset int
	}{
		{"an offset above 2^64-1", goldenBase32 + ".QAAAAAAAAAAAA", ident.ErrRange, dot + 12},
		{"an offset of 14 groups", goldenBase32 + ".BAAAAAAAAAAAAA", ident.ErrRange, dot + 13},
		{"no dot", goldenBase32, ident.ErrSyntax, len(goldenBase32)},
		{"the empty string", "", ident.ErrSyntax, 0},
		{"a byte outside the alphabet", goldenBase32 + ".D1", ident.ErrSyntax, dot + 1},
		{"padding", goldenBase32 + ".DE=", ident.ErrSyntax, dot + 2},
		{"a decimal offset", goldenBase32 + ".100", ident.ErrSyntax, dot},
		{"the display form", goldenBase32 + ":100", ident.ErrSyntax, len(goldenBase32) + 4},
		{"a short change part", goldenBase32[:51] + ".DE", ident.ErrLength, 51},
		{"an empty change part", ".DE", ident.ErrLength, 0},
		{"a change part outside the alphabet", "1" + goldenBase32[1:] + ".DE", ident.ErrSyntax, 0},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			_, err := ident.ParsePosition(tc.in)
			requireParseError(t, err, tc.err, tc.offset)

			var pe *ident.ParseError
			require.ErrorAs(t, err, &pe)
			require.Equal(t, tc.in, pe.Input)
			require.Equal(t, "position", pe.What)
		})
	}
}

func TestPositionMarshalText(t *testing.T) {
	change := mustParseHash(t, goldenBase32)

	for _, tc := range []struct {
		offset uint64
		want   string
	}{
		{0, "A"},
		{1, "B"},
		{31, "7"},
		{32, "BA"},
		{100, "DE"},
		{math.MaxUint64, "P777777777777"},
	} {
		t.Run(strconv.FormatUint(tc.offset, 10), func(t *testing.T) {
			got, err := ident.Position{Change: change, Offset: tc.offset}.MarshalText()
			require.NoError(t, err)
			require.Equal(t, goldenBase32+"."+tc.want, string(got))
		})
	}

	t.Run("AppendText appends", func(t *testing.T) {
		got, err := ident.Position{Change: change, Offset: 100}.AppendText([]byte("x="))
		require.NoError(t, err)
		require.Equal(t, "x="+goldenBase32+".DE", string(got))
	})
}

func TestPositionUnmarshalText(t *testing.T) {
	change := mustParseHash(t, goldenBase32)

	t.Run("reads DE back as 100", func(t *testing.T) {
		var p ident.Position
		require.NoError(t, p.UnmarshalText([]byte(goldenBase32+".DE")))
		require.Equal(t, ident.Position{Change: change, Offset: 100}, p)
	})

	t.Run("reads A back as 0", func(t *testing.T) {
		p := ident.Position{Offset: 7}
		require.NoError(t, p.UnmarshalText([]byte(goldenBase32+".A")))
		require.Equal(t, ident.Position{Change: change, Offset: 0}, p)
	})

	t.Run("leaves the position unchanged on failure", func(t *testing.T) {
		before := ident.Position{Change: change, Offset: 7}
		p := before
		err := p.UnmarshalText([]byte(goldenBase32 + ".QAAAAAAAAAAAA"))
		require.ErrorIs(t, err, ident.ErrRange)
		require.Equal(t, before, p)
	})
}

func TestPositionString(t *testing.T) {
	change := mustParseHash(t, goldenBase32)
	require.Equal(t, goldenBase32+":100", ident.Position{Change: change, Offset: 100}.String())
	require.Equal(t, goldenBase32+":0", ident.Position{Change: change}.String())
	require.Equal(t, strings.Repeat("A", ident.Base32Len)+":18446744073709551615",
		ident.Position{Offset: math.MaxUint64}.String())
}

func TestPositionRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	for range 1000 {
		var p ident.Position
		for i := range p.Change {
			p.Change[i] = byte(r.Uint32())
		}
		p.Offset = r.Uint64() >> r.IntN(64)

		text, err := p.MarshalText()
		require.NoError(t, err)

		var got ident.Position
		require.NoError(t, got.UnmarshalText(text))
		require.Equal(t, p, got)

		// The offset's text has no leading 'A' unless it is 0.
		_, off, ok := strings.Cut(string(text), ".")
		require.True(t, ok)
		require.True(t, off == "A" || off[0] != 'A', "offset text %q", off)
	}
}

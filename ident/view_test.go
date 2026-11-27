package ident_test

import (
	"math"
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
)

func TestParseScope(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ident.Scope
	}{
		{"shared", ident.ScopeShared},
		{"SHARED", ident.ScopeShared},
		{"Shared", ident.ScopeShared},
		{"draft", ident.ScopeDraft},
		{"DrAfT", ident.ScopeDraft},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ident.ParseScope(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	for _, in := range []string{"", "public", " draft", "draft\n", "drafts"} {
		t.Run("refuses "+strconv.Quote(in), func(t *testing.T) {
			_, err := ident.ParseScope(in)
			requireParseError(t, err, ident.ErrSyntax, 0)
		})
	}
}

func TestScopeIsDraft(t *testing.T) {
	require.True(t, ident.ScopeDraft.IsDraft())
	require.True(t, ident.Scope("DRAFT").IsDraft())
	require.True(t, ident.Scope("Draft").IsDraft())
	require.False(t, ident.ScopeShared.IsDraft())
	require.False(t, ident.Scope("drafts").IsDraft())
	require.False(t, ident.Scope("").IsDraft())
}

func TestParseSequence(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ident.Sequence
	}{
		{"0", 0},
		{"5", 5},
		{"+5", 5},
		{"007", 7},
		{"18446744073709551615", math.MaxUint64},
		{"+18446744073709551615", math.MaxUint64},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ident.ParseSequence(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	for _, tc := range []struct {
		in     string
		err    error
		offset int
	}{
		{"-1", ident.ErrSyntax, 0},
		{" 5", ident.ErrSyntax, 0},
		{"5 ", ident.ErrSyntax, 1},
		{"", ident.ErrSyntax, 0},
		{"+", ident.ErrSyntax, 1},
		{"++5", ident.ErrSyntax, 1},
		{"1_000", ident.ErrSyntax, 1},
		{"0x10", ident.ErrSyntax, 1},
		{"18446744073709551616", ident.ErrRange, 19},
		{"+18446744073709551616", ident.ErrRange, 20},
		{"99999999999999999999", ident.ErrRange, 19},
	} {
		t.Run("refuses "+strconv.Quote(tc.in), func(t *testing.T) {
			_, err := ident.ParseSequence(tc.in)
			requireParseError(t, err, tc.err, tc.offset)
		})
	}
}

func TestSequenceRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(23, 24))
	for range 1000 {
		n := ident.Sequence(r.Uint64() >> r.IntN(64))
		require.Equal(t, strconv.FormatUint(uint64(n), 10), n.String())

		got, err := ident.ParseSequence(n.String())
		require.NoError(t, err)
		require.Equal(t, n, got)

		got, err = ident.ParseSequence("+" + n.String())
		require.NoError(t, err)
		require.Equal(t, n, got)
	}
}

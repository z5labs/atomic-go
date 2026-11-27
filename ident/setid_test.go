package ident_test

import (
	"encoding/binary"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
)

func TestSetIDAdd(t *testing.T) {
	t.Run("adds the domain-separated hash lane by lane", func(t *testing.T) {
		r := rand.New(rand.NewPCG(11, 12))
		h := randomHash(r)
		e := ident.Sum(append([]byte(ident.SetIDDomain), h[:]...))

		require.Equal(t, ident.SetID(e), ident.SetID{}.Add(h))
	})

	t.Run("wraps each lane without carrying into the next", func(t *testing.T) {
		r := rand.New(rand.NewPCG(13, 14))
		h := randomHash(r)
		e := ident.Sum(append([]byte(ident.SetIDDomain), h[:]...))

		var start ident.SetID
		for i := range start {
			start[i] = 0xff
		}
		got := start.Add(h)
		for lane := 0; lane < ident.Size; lane += 8 {
			want := binary.LittleEndian.Uint64(e[lane:]) - 1
			require.Equal(t, want, binary.LittleEndian.Uint64(got[lane:]), "lane %d", lane/8)
		}
	})

	t.Run("adding twice differs from adding once", func(t *testing.T) {
		r := rand.New(rand.NewPCG(15, 16))
		for range 100 {
			s := ident.SetIDOf(randomHashes(r, r.IntN(5)))
			h := randomHash(r)
			require.NotEqual(t, s.Add(h), s.Add(h).Add(h))
		}
	})
}

func TestSetIDOf(t *testing.T) {
	t.Run("the empty set is zero", func(t *testing.T) {
		require.True(t, ident.SetIDOf(nil).IsZero())
		require.True(t, ident.SetIDOf([]ident.Hash{}).IsZero())
		require.Equal(t, strings.Repeat("A", ident.Base32Len), ident.SetIDOf(nil).String())
	})

	t.Run("is the sum of its members", func(t *testing.T) {
		r := rand.New(rand.NewPCG(17, 18))
		a, b := randomHash(r), randomHash(r)
		require.Equal(t, ident.SetID{}.Add(a).Add(b), ident.SetIDOf([]ident.Hash{a, b}))
		require.False(t, ident.SetIDOf([]ident.Hash{a}).IsZero())
	})

	t.Run("is order-invariant", func(t *testing.T) {
		r := rand.New(rand.NewPCG(19, 20))
		for range 100 {
			members := randomHashes(r, 1+r.IntN(10))
			shuffled := append([]ident.Hash{}, members...)
			r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			require.Equal(t, ident.SetIDOf(members), ident.SetIDOf(shuffled))
		}
	})

	t.Run("counts a repeated member once", func(t *testing.T) {
		r := rand.New(rand.NewPCG(21, 22))
		for range 100 {
			members := randomHashes(r, 1+r.IntN(10))
			repeated := append([]ident.Hash{}, members...)
			for range 1 + r.IntN(10) {
				repeated = append(repeated, members[r.IntN(len(members))])
			}
			r.Shuffle(len(repeated), func(i, j int) { repeated[i], repeated[j] = repeated[j], repeated[i] })
			require.Equal(t, ident.SetIDOf(members), ident.SetIDOf(repeated))
		}
	})
}

func TestSetIDText(t *testing.T) {
	s, err := ident.ParseSetID(strings.ToLower(goldenBase32))
	require.NoError(t, err)
	require.False(t, s.IsZero())
	require.Equal(t, goldenBase32, s.String())
	require.Equal(t, goldenHex, s.Hex())

	fromHex, err := ident.ParseSetIDHex(strings.ToUpper(goldenHex))
	require.NoError(t, err)
	require.Equal(t, s, fromHex)

	text, err := s.MarshalText()
	require.NoError(t, err)
	require.Equal(t, goldenBase32, string(text))

	appended, err := s.AppendText([]byte("set="))
	require.NoError(t, err)
	require.Equal(t, "set="+goldenBase32, string(appended))

	var got ident.SetID
	require.NoError(t, got.UnmarshalText(text))
	require.Equal(t, s, got)

	err = got.UnmarshalText([]byte(goldenBase32 + "\n"))
	require.ErrorIs(t, err, ident.ErrLength)
	require.Equal(t, s, got)

	_, err = ident.ParseSetID(goldenBase32[:51] + "=")
	requireParseError(t, err, ident.ErrSyntax, 51)

	_, err = ident.ParseSetIDHex("+f" + goldenHex[2:])
	requireParseError(t, err, ident.ErrSyntax, 0)
}

package ident_test

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
)

func randomHash(r *rand.Rand) ident.Hash {
	var h ident.Hash
	for i := 0; i < ident.Size; i += 8 {
		v := r.Uint64()
		for j := 0; j < 8; j++ {
			h[i+j] = byte(v >> (8 * j))
		}
	}
	return h
}

func randomHashes(r *rand.Rand, n int) []ident.Hash {
	hs := make([]ident.Hash, n)
	for i := range hs {
		hs[i] = randomHash(r)
	}
	return hs
}

func TestNextState(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	state, change := randomHash(r), randomHash(r)

	concat := append(append([]byte{}, state[:]...), change[:]...)
	require.Equal(t, ident.Sum(concat), ident.NextState(state, change))
}

func TestFoldState(t *testing.T) {
	t.Run("an empty log is the all-zero seed", func(t *testing.T) {
		require.True(t, ident.FoldState(nil).IsZero())
	})

	t.Run("folds from the zero hash in log order", func(t *testing.T) {
		r := rand.New(rand.NewPCG(5, 6))
		log := randomHashes(r, 3)

		want := ident.NextState(ident.NextState(ident.NextState(ident.Hash{}, log[0]), log[1]), log[2])
		require.Equal(t, want, ident.FoldState(log))
	})

	t.Run("FoldStateFrom takes another seed", func(t *testing.T) {
		r := rand.New(rand.NewPCG(7, 8))
		seed := ident.Sum(nil)
		log := randomHashes(r, 4)

		require.Equal(t, seed, ident.FoldStateFrom(seed, nil))
		require.Equal(t, ident.FoldStateFrom(ident.NextState(seed, log[0]), log[1:]), ident.FoldStateFrom(seed, log))
		require.NotEqual(t, ident.FoldState(log), ident.FoldStateFrom(seed, log))
	})

	t.Run("is order-sensitive", func(t *testing.T) {
		r := rand.New(rand.NewPCG(9, 10))
		for range 100 {
			log := randomHashes(r, 2+r.IntN(8))
			shuffled := append([]ident.Hash{}, log...)
			for {
				r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
				if !equalHashes(log, shuffled) {
					break
				}
			}
			require.NotEqual(t, ident.FoldState(log), ident.FoldState(shuffled))
		}
	})
}

func equalHashes(a, b []ident.Hash) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseState(t *testing.T) {
	t.Run("a dash is an empty view", func(t *testing.T) {
		state, ok, err := ident.ParseState(ident.EmptyState)
		require.NoError(t, err)
		require.False(t, ok)
		require.True(t, state.IsZero())
		require.Equal(t, ident.EmptyState, ident.FormatState(state, ok))
	})

	t.Run("a hash is a state", func(t *testing.T) {
		state, ok, err := ident.ParseState(goldenBase32)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, goldenHex, state.Hex())
		require.Equal(t, goldenBase32, ident.FormatState(state, ok))
	})

	t.Run("the zero state is not the empty view", func(t *testing.T) {
		require.Equal(t, ident.Hash{}.String(), ident.FormatState(ident.Hash{}, true))
	})

	t.Run("refuses anything else", func(t *testing.T) {
		for _, s := range []string{"", "--", " -", goldenBase32[:51]} {
			_, ok, err := ident.ParseState(s)
			require.Error(t, err, "%q", s)
			require.False(t, ok)
			var pe *ident.ParseError
			require.ErrorAs(t, err, &pe)
			require.Equal(t, "state", pe.What)
		}
	})
}

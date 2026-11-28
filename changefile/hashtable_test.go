package changefile_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
)

// distinct returns n distinct non-zero hashes.
func distinct(n int) []ident.Hash {
	hs := make([]ident.Hash, n)
	for i := range hs {
		binary.LittleEndian.PutUint32(hs[i][:], uint32(i+1))
	}
	return hs
}

func TestHashIndexConstants(t *testing.T) {
	require.Equal(t, changefile.HashIndex(0xFFFF), changefile.ThisChange)
}

func TestNewHashTable(t *testing.T) {
	a, b := ident.Sum([]byte("a")), ident.Sum([]byte("b"))
	entries := []ident.Hash{{}, a, b}
	tab, err := changefile.NewHashTable(entries)
	require.NoError(t, err)
	require.Equal(t, 3, tab.Len())

	// The table keeps its own copy.
	entries[1] = b
	got, err := tab.At(1)
	require.NoError(t, err)
	require.Equal(t, a, got)

	i, ok := tab.IndexOf(b)
	require.True(t, ok)
	require.Equal(t, changefile.HashIndex(2), i)
	_, ok = tab.IndexOf(ident.Sum([]byte("c")))
	require.False(t, ok)

	var idx []changefile.HashIndex
	var hs []ident.Hash
	for i, h := range tab.All() {
		idx = append(idx, i)
		hs = append(hs, h)
	}
	require.Equal(t, []changefile.HashIndex{0, 1, 2}, idx)
	require.Equal(t, []ident.Hash{{}, a, b}, hs)

	// All stops when asked to.
	n := 0
	for range tab.All() {
		n++
		break
	}
	require.Equal(t, 1, n)

	out, err := tab.AppendBinary([]byte{0xAA})
	require.NoError(t, err)
	want := append([]byte{0xAA}, make([]byte, 32)...)
	want = append(want, a[:]...)
	want = append(want, b[:]...)
	require.Equal(t, want, out)
}

func TestHashTableAtAndReference(t *testing.T) {
	a := ident.Sum([]byte("a"))
	tab, err := changefile.NewHashTable([]ident.Hash{{}, a})
	require.NoError(t, err)

	h, err := tab.At(0)
	require.NoError(t, err)
	require.True(t, h.IsZero())

	h, err = tab.Reference(1)
	require.NoError(t, err)
	require.Equal(t, a, h)

	// Slot 0's zero hash is no reference.
	_, err = tab.Reference(0)
	requireFormatError(t, err, changefile.ErrInvalidChangeMetadata, -1)

	for _, i := range []changefile.HashIndex{2, 0xFFFE, changefile.ThisChange} {
		_, err = tab.At(i)
		fe := requireFormatError(t, err, changefile.ErrHashIndexOutOfBounds, -1)
		require.Contains(t, fe.Error(), "table has 2 entries")
		_, err = tab.Reference(i)
		requireFormatError(t, err, changefile.ErrHashIndexOutOfBounds, -1)
	}

	// Index 0 resolves through the table, whatever slot 0 holds.
	tab, err = changefile.NewHashTable([]ident.Hash{a})
	require.NoError(t, err)
	h, err = tab.Reference(0)
	require.NoError(t, err)
	require.Equal(t, a, h)
}

func TestNewHashTableRefusals(t *testing.T) {
	t.Run("repeat", func(t *testing.T) {
		a := ident.Sum([]byte("a"))
		_, err := changefile.NewHashTable([]ident.Hash{{}, a, {}, a})
		fe := requireFormatError(t, err, changefile.ErrInvalidHeader, 64+32*2)
		require.Contains(t, fe.Error(), "duplicate hash in dedup table at index 2")
	})
	t.Run("full", func(t *testing.T) {
		tab, err := changefile.NewHashTable(distinct(changefile.MaxHashTableEntries))
		require.NoError(t, err)
		require.Equal(t, changefile.MaxHashTableEntries, tab.Len())

		_, err = changefile.NewHashTable(distinct(changefile.MaxHashTableEntries + 1))
		requireFormatError(t, err, changefile.ErrHashTableFull, -1)
	})
}

func TestZeroHashTable(t *testing.T) {
	var tab changefile.HashTable
	require.Equal(t, 0, tab.Len())
	_, ok := tab.IndexOf(ident.Hash{})
	require.False(t, ok)
	_, err := tab.At(0)
	requireFormatError(t, err, changefile.ErrHashIndexOutOfBounds, -1)
	out, err := tab.AppendBinary(nil)
	require.NoError(t, err)
	require.Empty(t, out)

	tab, err = changefile.NewHashTable(nil)
	require.NoError(t, err)
	require.Equal(t, 0, tab.Len())
}

func TestTableBuilder(t *testing.T) {
	a, b := ident.Sum([]byte("a")), ident.Sum([]byte("b"))
	var tb changefile.TableBuilder

	// Slot 0 is the zero hash before anything is added.
	i, err := tb.IndexOf(ident.Hash{})
	require.NoError(t, err)
	require.Equal(t, changefile.HashIndex(0), i)
	require.Equal(t, 1, tb.Table().Len())

	i, err = tb.Add(b)
	require.NoError(t, err)
	require.Equal(t, changefile.HashIndex(1), i)
	i, err = tb.Add(a)
	require.NoError(t, err)
	require.Equal(t, changefile.HashIndex(2), i)

	// A repeat keeps its first index, and the zero hash is slot 0.
	i, err = tb.Add(b)
	require.NoError(t, err)
	require.Equal(t, changefile.HashIndex(1), i)
	i, err = tb.Add(ident.Hash{})
	require.NoError(t, err)
	require.Equal(t, changefile.HashIndex(0), i)

	i, err = tb.IndexOf(a)
	require.NoError(t, err)
	require.Equal(t, changefile.HashIndex(2), i)

	tab := tb.Table()
	require.Equal(t, 3, tab.Len())
	got, err := tab.At(1)
	require.NoError(t, err)
	require.Equal(t, b, got)
	j, ok := tab.IndexOf(a)
	require.True(t, ok)
	require.Equal(t, changefile.HashIndex(2), j)

	// Hashes added later do not change a table already taken.
	c := ident.Sum([]byte("c"))
	_, err = tb.Add(c)
	require.NoError(t, err)
	require.Equal(t, 3, tab.Len())
	_, ok = tab.IndexOf(c)
	require.False(t, ok)
	require.Equal(t, 4, tb.Table().Len())
}

func TestTableBuilderNotFound(t *testing.T) {
	var tb changefile.TableBuilder
	missing := ident.Sum([]byte("missing"))
	_, err := tb.IndexOf(missing)
	fe := requireFormatError(t, err, changefile.ErrHashNotFound, -1)
	require.Contains(t, fe.Error(), missing.String())
}

func TestTableBuilderFull(t *testing.T) {
	var tb changefile.TableBuilder
	hs := distinct(changefile.MaxHashTableEntries)
	// Slot 0 is taken, so 65533 more fill the table.
	for k, h := range hs[:changefile.MaxHashTableEntries-1] {
		i, err := tb.Add(h)
		require.NoError(t, err)
		require.Equal(t, changefile.HashIndex(k+1), i)
	}
	require.Equal(t, changefile.MaxHashTableEntries, tb.Table().Len())

	_, err := tb.Add(hs[changefile.MaxHashTableEntries-1])
	requireFormatError(t, err, changefile.ErrHashTableFull, -1)

	// A hash already present still resolves when the table is full.
	i, err := tb.Add(hs[0])
	require.NoError(t, err)
	require.Equal(t, changefile.HashIndex(1), i)

	// What it built is a table a reader accepts.
	tab := tb.Table()
	var entries []ident.Hash
	for _, h := range tab.All() {
		entries = append(entries, h)
	}
	_, err = changefile.NewHashTable(entries)
	require.NoError(t, err)
}

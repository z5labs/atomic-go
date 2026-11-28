package changefile

import (
	"fmt"
	"iter"
	"maps"
	"slices"

	"github.com/z5labs/atomic-go/ident"
)

// HashIndex names a [HashTable] slot inside a section payload, where it is
// encoded as a postcard varint.
//
// Index 0 is whatever slot 0 holds. Atomic's writer puts the zero hash
// there, so in its files index 0 is the virtual ROOT, whatever the names in
// Atomic's source say.
type HashIndex uint16

// ThisChange is the index a GRAPH payload names the change the file holds
// by. It is never a slot: a HEADER or DEPS index of ThisChange is refused.
const ThisChange HashIndex = 0xFFFF

// HashTable is a change file's hash dedup table: the hashes its payloads
// name by [HashIndex], in index order. Its entries are distinct, and there
// are at most [MaxHashTableEntries] of them. The zero value is an empty
// table. A HashTable is never changed once it is built, so it is safe to
// copy and to share.
type HashTable struct {
	entries []ident.Hash
	index   map[ident.Hash]HashIndex
}

// NewHashTable returns the table of entries, in that order, keeping a copy
// of the slice. It fails with a *[FormatError] of kind [ErrHashTableFull]
// for more than [MaxHashTableEntries] entries, and of kind
// [ErrInvalidHeader] for an entry equal to an earlier one, as Atomic's
// reader does.
func NewHashTable(entries []ident.Hash) (HashTable, error) {
	if len(entries) > MaxHashTableEntries {
		return HashTable{}, formatError(ErrHashTableFull, 0, -1, fmt.Sprintf("cannot store more than %d unique hashes, got %d", MaxHashTableEntries, len(entries)))
	}
	t := HashTable{
		entries: slices.Clone(entries),
		index:   make(map[ident.Hash]HashIndex, len(entries)),
	}
	for i, h := range t.entries {
		if _, dup := t.index[h]; dup {
			return HashTable{}, formatError(ErrInvalidHeader, 0, int64(HeaderSize+ident.Size*i), fmt.Sprintf("duplicate hash in dedup table at index %d", i))
		}
		t.index[h] = HashIndex(i)
	}
	return t, nil
}

// Len returns the number of entries.
func (t HashTable) Len() int {
	return len(t.entries)
}

// At returns the hash in slot i. It fails with a *[FormatError] of kind
// [ErrHashIndexOutOfBounds] when i is not below [HashTable.Len], which
// [ThisChange] never is.
func (t HashTable) At(i HashIndex) (ident.Hash, error) {
	if int(i) >= len(t.entries) {
		return ident.Hash{}, formatError(ErrHashIndexOutOfBounds, 0, -1, fmt.Sprintf("hash index %d is out of bounds (table has %d entries)", i, len(t.entries)))
	}
	return t.entries[i], nil
}

// Reference returns the hash in slot i, where a HEADER or DEPS payload
// requires one. It fails as [HashTable.At] does, and with a *[FormatError]
// of kind [ErrInvalidChangeMetadata] when the slot holds the zero hash, as
// slot 0 does in every file Atomic writes.
func (t HashTable) Reference(i HashIndex) (ident.Hash, error) {
	h, err := t.At(i)
	if err != nil {
		return ident.Hash{}, err
	}
	if h.IsZero() {
		return ident.Hash{}, formatError(ErrInvalidChangeMetadata, 0, -1, fmt.Sprintf("hash index %d names the zero hash", i))
	}
	return h, nil
}

// IndexOf returns the index of h, and whether the table holds it.
func (t HashTable) IndexOf(h ident.Hash) (HashIndex, bool) {
	i, ok := t.index[h]
	return i, ok
}

// All yields each index and its hash, in index order.
func (t HashTable) All() iter.Seq2[HashIndex, ident.Hash] {
	return func(yield func(HashIndex, ident.Hash) bool) {
		for i, h := range t.entries {
			if !yield(HashIndex(i), h) {
				return
			}
		}
	}
}

// AppendBinary appends the table as it is in a file to b: each entry's 32
// bytes in index order, with no count and no framing. It never returns an
// error; it implements [encoding.BinaryAppender].
func (t HashTable) AppendBinary(b []byte) ([]byte, error) {
	for _, h := range t.entries {
		b = append(b, h[:]...)
	}
	return b, nil
}

// TableBuilder builds the [HashTable] of a change being written. Slot 0
// holds the zero hash, as in every file Atomic writes, and each hash added
// takes the next slot the first time it is added. The zero value is ready
// to use. A TableBuilder must not be copied after its first use: a copy
// shares its state, and adding to both corrupts both. Pass a *TableBuilder.
type TableBuilder struct {
	entries []ident.Hash
	index   map[ident.Hash]HashIndex
}

func (b *TableBuilder) init() {
	if b.entries == nil {
		b.entries = []ident.Hash{{}}
		b.index = map[ident.Hash]HashIndex{{}: 0}
	}
}

// Add returns the index of h, adding it to the table if it is not there
// already. Adding the zero hash returns 0. It fails with a *[FormatError]
// of kind [ErrHashTableFull] when h is new and the table already holds
// [MaxHashTableEntries] entries.
func (b *TableBuilder) Add(h ident.Hash) (HashIndex, error) {
	b.init()
	if i, ok := b.index[h]; ok {
		return i, nil
	}
	if len(b.entries) >= MaxHashTableEntries {
		return 0, formatError(ErrHashTableFull, 0, -1, fmt.Sprintf("cannot store more than %d unique hashes", MaxHashTableEntries))
	}
	i := HashIndex(len(b.entries))
	b.entries = append(b.entries, h)
	b.index[h] = i
	return i, nil
}

// IndexOf returns the index of h. It fails with a *[FormatError] of kind
// [ErrHashNotFound] when h was never added. The zero hash is always at 0.
func (b *TableBuilder) IndexOf(h ident.Hash) (HashIndex, error) {
	b.init()
	i, ok := b.index[h]
	if !ok {
		return 0, formatError(ErrHashNotFound, 0, -1, h.String())
	}
	return i, nil
}

// Table returns the table built so far. Hashes added afterwards do not
// change it.
func (b *TableBuilder) Table() HashTable {
	b.init()
	return HashTable{
		entries: slices.Clone(b.entries),
		index:   maps.Clone(b.index),
	}
}

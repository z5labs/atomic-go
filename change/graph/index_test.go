package graph

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
)

const this = changefile.ThisChange

// zeroTable is the table Atomic's writer starts every file with: the zero
// hash in slot 0.
func zeroTable(t *testing.T) changefile.HashTable {
	t.Helper()
	table, err := changefile.NewHashTable([]ident.Hash{{}})
	require.NoError(t, err)
	return table
}

// threeTable is a table of ROOT in slot 0 and two other changes.
func threeTable(t *testing.T) changefile.HashTable {
	t.Helper()
	table, err := changefile.NewHashTable([]ident.Hash{{}, {1}, {2}})
	require.NoError(t, err)
	return table
}

func TestResolve(t *testing.T) {
	t.Run("on a 1-entry zero table, 0 is ROOT", func(t *testing.T) {
		ref, err := Resolve(zeroTable(t), 0)
		require.NoError(t, err)
		require.Equal(t, Ref{}, ref)
		require.True(t, ref.IsRoot())
	})

	t.Run("on a 1-entry zero table, 0xFFFF is This", func(t *testing.T) {
		ref, err := Resolve(zeroTable(t), this)
		require.NoError(t, err)
		require.Equal(t, Ref{This: true}, ref)
		require.False(t, ref.IsRoot())
	})

	t.Run("on a 1-entry zero table, 1 is out of bounds", func(t *testing.T) {
		_, err := Resolve(zeroTable(t), 1)
		requireFormatError(t, err, changefile.ErrHashIndexOutOfBounds)
	})

	t.Run("slot 0 is whatever hash it holds", func(t *testing.T) {
		table, err := changefile.NewHashTable([]ident.Hash{{7}})
		require.NoError(t, err)
		ref, err := Resolve(table, 0)
		require.NoError(t, err)
		require.Equal(t, Ref{Hash: ident.Hash{7}}, ref)
		require.False(t, ref.IsRoot())
	})

	t.Run("a later slot is its hash", func(t *testing.T) {
		ref, err := Resolve(threeTable(t), 2)
		require.NoError(t, err)
		require.Equal(t, Ref{Hash: ident.Hash{2}}, ref)
	})

	t.Run("an empty table holds nothing but This", func(t *testing.T) {
		_, err := Resolve(changefile.HashTable{}, 0)
		requireFormatError(t, err, changefile.ErrHashIndexOutOfBounds)
		ref, err := Resolve(changefile.HashTable{}, this)
		require.NoError(t, err)
		require.True(t, ref.This)
	})
}

func pos(change changefile.HashIndex) Position {
	return Position{Change: change}
}

func TestIndices(t *testing.T) {
	t.Run("FileAdd goes predecessors, successors, inode for each insertion, and skips ThisChange", func(t *testing.T) {
		op := FileAdd{
			AddName: Insertion{
				Predecessors: []Position{pos(1), pos(2)},
				Successors:   []Position{pos(3)},
				Inode:        pos(4),
			},
			AddInode: Insertion{
				Predecessors: []Position{pos(this)},
				Successors:   []Position{pos(5)},
				Inode:        pos(6),
			},
			Contents: &Insertion{
				Predecessors: []Position{pos(7)},
				Inode:        pos(this),
			},
			Path:     "a.txt",
			Encoding: ptr(Utf8),
		}
		require.Equal(t, []changefile.HashIndex{1, 2, 3, 4, 5, 6, 7}, slices.Collect(Indices(op)))
	})

	t.Run("NewEdge goes from, to, introduced_by, then the update's inode", func(t *testing.T) {
		op := DirDel{
			Del: EdgeUpdate{
				Edges: []NewEdge{
					{From: pos(1), To: Node{Change: 2}, IntroducedBy: 3},
					{From: pos(4), To: Node{Change: this}, IntroducedBy: 5},
				},
				Inode: pos(6),
			},
			Path: "d",
		}
		require.Equal(t, []changefile.HashIndex{1, 2, 3, 4, 5, 6}, slices.Collect(Indices(op)))
	})

	t.Run("SetAttr yields its inode", func(t *testing.T) {
		op := SetAttr{Inode: pos(2), Path: "a", Value: Mode(0o644)}
		require.Equal(t, []changefile.HashIndex{2}, slices.Collect(Indices(op)))
	})

	t.Run("an index held twice is yielded twice", func(t *testing.T) {
		op := AddRoot{Name: Insertion{Inode: pos(1)}, Inode: Insertion{Inode: pos(1)}}
		require.Equal(t, []changefile.HashIndex{1, 1}, slices.Collect(Indices(op)))
	})

	t.Run("Edit follows its atom", func(t *testing.T) {
		ins := Edit{Change: Insertion{Predecessors: []Position{pos(1)}, Inode: pos(2)}}
		require.Equal(t, []changefile.HashIndex{1, 2}, slices.Collect(Indices(ins)))
		upd := Edit{Change: EdgeUpdate{Edges: []NewEdge{{From: pos(1), To: Node{Change: 2}, IntroducedBy: 3}}, Inode: pos(4)}}
		require.Equal(t, []changefile.HashIndex{1, 2, 3, 4}, slices.Collect(Indices(upd)))
	})

	t.Run("stops when the consumer does", func(t *testing.T) {
		op := FileMove{
			Del: EdgeUpdate{Edges: []NewEdge{{From: pos(1), To: Node{Change: 2}, IntroducedBy: 3}}, Inode: pos(4)},
			Add: Insertion{Inode: pos(5)},
		}
		var got []changefile.HashIndex
		for i := range Indices(op) {
			got = append(got, i)
			if len(got) == 2 {
				break
			}
		}
		require.Equal(t, []changefile.HashIndex{1, 2}, got)
	})

	t.Run("a nil op yields nothing", func(t *testing.T) {
		require.Empty(t, slices.Collect(Indices(nil)))
	})
}

func TestRemap(t *testing.T) {
	t.Run("a permutation renumbers every index and keeps ThisChange", func(t *testing.T) {
		perm := map[changefile.HashIndex]changefile.HashIndex{0: 0, 1: 3, 2: 1, 3: 2}
		f := func(i changefile.HashIndex) changefile.HashIndex {
			j, ok := perm[i]
			require.True(t, ok, "f called with %d", i)
			return j
		}
		op := FileAdd{
			AddName: Insertion{
				Predecessors: []Position{pos(0)},
				Successors:   []Position{pos(this)},
				Flag:         Folder | Block,
				Start:        0,
				End:          5,
				Inode:        pos(1),
			},
			AddInode: Insertion{Predecessors: []Position{pos(this)}, Inode: pos(2)},
			Contents: &Insertion{Predecessors: []Position{pos(3)}, Inode: pos(this)},
			Path:     "a.txt",
			Encoding: ptr(Latin1),
		}

		got := Remap(op, f)

		want := FileAdd{
			AddName: Insertion{
				Predecessors: []Position{pos(0)},
				Successors:   []Position{pos(this)},
				Flag:         Folder | Block,
				Start:        0,
				End:          5,
				Inode:        pos(3),
			},
			AddInode: Insertion{Predecessors: []Position{pos(this)}, Inode: pos(1)},
			Contents: &Insertion{Predecessors: []Position{pos(2)}, Inode: pos(this)},
			Path:     "a.txt",
			Encoding: ptr(Latin1),
		}
		require.Equal(t, want, got)
	})

	t.Run("returns a copy and leaves the op alone", func(t *testing.T) {
		op := Replacement{
			Change:      EdgeUpdate{Edges: []NewEdge{{From: pos(1), To: Node{Change: 1}, IntroducedBy: 1}}, Inode: pos(1)},
			Replacement: Insertion{Predecessors: []Position{pos(1)}, Successors: []Position{pos(1)}, Inode: pos(1)},
			Local:       Local{Path: "a", Line: 1},
			Encoding:    ptr(Utf8),
		}
		orig := Replacement{
			Change:      EdgeUpdate{Edges: []NewEdge{{From: pos(1), To: Node{Change: 1}, IntroducedBy: 1}}, Inode: pos(1)},
			Replacement: Insertion{Predecessors: []Position{pos(1)}, Successors: []Position{pos(1)}, Inode: pos(1)},
			Local:       Local{Path: "a", Line: 1},
			Encoding:    ptr(Utf8),
		}

		got := Remap(op, func(changefile.HashIndex) changefile.HashIndex { return 2 }).(Replacement)

		require.Equal(t, orig, op)
		require.Equal(t, []changefile.HashIndex{2, 2, 2, 2, 2, 2, 2}, slices.Collect(Indices(got)))
		*got.Encoding = Binary
		require.Equal(t, Utf8, *op.Encoding)
	})

	t.Run("FileDel contents are renumbered and copied", func(t *testing.T) {
		op := FileDel{
			Del:      EdgeUpdate{Inode: pos(1)},
			Contents: &EdgeUpdate{Edges: []NewEdge{{From: pos(1), To: Node{Change: this}, IntroducedBy: 1}}, Inode: pos(1)},
			Path:     "a",
		}
		got := Remap(op, func(changefile.HashIndex) changefile.HashIndex { return 2 }).(FileDel)
		require.Equal(t, FileDel{
			Del:      EdgeUpdate{Inode: pos(2)},
			Contents: &EdgeUpdate{Edges: []NewEdge{{From: pos(2), To: Node{Change: this}, IntroducedBy: 2}}, Inode: pos(2)},
			Path:     "a",
		}, got)
		require.NotSame(t, op.Contents, got.Contents)
		require.Equal(t, changefile.HashIndex(1), op.Contents.Edges[0].From.Change)
	})

	t.Run("a nil FileDel contents stays nil", func(t *testing.T) {
		op := FileDel{Del: EdgeUpdate{Inode: pos(1)}, Path: "a"}
		got := Remap(op, func(changefile.HashIndex) changefile.HashIndex { return 2 })
		require.Equal(t, FileDel{Del: EdgeUpdate{Inode: pos(2)}, Path: "a"}, got)
	})

	t.Run("an Edit keeps its atom's type", func(t *testing.T) {
		op := Edit{Change: EdgeUpdate{Inode: pos(1)}, Local: Local{Path: "a"}}
		got := Remap(op, func(changefile.HashIndex) changefile.HashIndex { return 4 })
		require.Equal(t, Edit{Change: EdgeUpdate{Inode: pos(4)}, Local: Local{Path: "a"}}, got)
	})

	t.Run("a nil op is returned as is", func(t *testing.T) {
		require.Nil(t, Remap(nil, func(i changefile.HashIndex) changefile.HashIndex { return i }))
	})
}

// nameEdge is a well-formed losing name claim of a SolveNameConflict whose
// indices are all change 1.
func nameEdge() NewEdge {
	return NewEdge{Previous: Folder | Block, Flag: Folder | Block | Deleted, From: pos(1), To: Node{Change: 1, Start: 0, End: 2}, IntroducedBy: 1}
}

func TestCheck(t *testing.T) {
	withEdge := func(f func(*NewEdge)) SolveNameConflict {
		e := nameEdge()
		f(&e)
		return SolveNameConflict{Name: EdgeUpdate{Edges: []NewEdge{e}, Inode: pos(1)}, Path: "a"}
	}

	accepted := []struct {
		name string
		op   Op
	}{
		{"a SetAttr on another change", SetAttr{Inode: pos(1), Path: "a", Value: Mode(0o644)}},
		{"a SetAttr on this change", SetAttr{Inode: pos(this), Path: "a", Value: Mode(0o644)}},
		{"a well-formed SolveNameConflict", withEdge(func(*NewEdge) {})},
		{"a from of ROOT", withEdge(func(e *NewEdge) { e.From = pos(0) })},
		{"an UnsolveNameConflict with a from of ROOT", UnsolveNameConflict{
			Name: EdgeUpdate{Edges: []NewEdge{{Previous: Folder | Block | Deleted, Flag: Folder | Block, From: pos(0), To: Node{Change: 2, End: 1}, IntroducedBy: 2}}, Inode: pos(2)},
			Path: "a",
		}},
		{"a v1 edge-less marker naming this change", SolveNameConflict{Name: EdgeUpdate{Inode: pos(this)}, Path: "a"}},
		{"an op with no checks naming ROOT and this change", FileAdd{AddName: Insertion{Predecessors: []Position{pos(0)}, Inode: pos(this)}, Path: "a"}},
	}
	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			require.NoError(t, Check(tc.op, threeTable(t)))
		})
	}

	refused := []struct {
		name string
		op   Op
		kind error
	}{
		{"a SetAttr on ROOT", SetAttr{Inode: pos(0), Path: "a", Value: Mode(0o644)}, changefile.ErrInvalidGraphOp},
		{"a name.inode of ThisChange", SolveNameConflict{Name: EdgeUpdate{Edges: []NewEdge{nameEdge()}, Inode: pos(this)}, Path: "a"}, changefile.ErrInvalidGraphOp},
		{"a name.inode of ROOT", SolveNameConflict{Name: EdgeUpdate{Edges: []NewEdge{nameEdge()}, Inode: pos(0)}, Path: "a"}, changefile.ErrInvalidGraphOp},
		{"a to of ROOT", withEdge(func(e *NewEdge) { e.To.Change = 0 }), changefile.ErrInvalidGraphOp},
		{"a to of ThisChange", withEdge(func(e *NewEdge) { e.To.Change = this }), changefile.ErrInvalidGraphOp},
		{"an introduced_by of ThisChange", withEdge(func(e *NewEdge) { e.IntroducedBy = this }), changefile.ErrInvalidGraphOp},
		{"an introduced_by of ROOT", withEdge(func(e *NewEdge) { e.IntroducedBy = 0 }), changefile.ErrInvalidGraphOp},
		{"a from of ThisChange", withEdge(func(e *NewEdge) { e.From = pos(this) }), changefile.ErrInvalidGraphOp},
		{"an UnsolveNameConflict with a to of ROOT", UnsolveNameConflict{
			Name: EdgeUpdate{Edges: []NewEdge{{Previous: Folder | Block | Deleted, Flag: Folder | Block, From: pos(1), To: Node{Change: 0, End: 1}, IntroducedBy: 1}}, Inode: pos(1)},
			Path: "a",
		}, changefile.ErrInvalidGraphOp},
		{"an index out of bounds", FileAdd{AddName: Insertion{Inode: pos(3)}, Path: "a"}, changefile.ErrHashIndexOutOfBounds},
		{"a nil op", nil, changefile.ErrInvalidGraphOp},
		{"a pointer op", &SetAttr{Inode: pos(0), Path: "a", Value: Mode(0o644)}, changefile.ErrInvalidGraphOp},
		{"an Edit with a pointer atom", Edit{Change: &Insertion{Inode: pos(3)}, Local: Local{Path: "a"}}, changefile.ErrInvalidGraphOp},
		{"an Edit with a nil atom", Edit{Local: Local{Path: "a"}}, changefile.ErrInvalidGraphOp},
	}
	for _, tc := range refused {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			requireFormatError(t, Check(tc.op, threeTable(t)), tc.kind)
		})
	}

	t.Run("ROOT is a zero slot, not index 0", func(t *testing.T) {
		table, err := changefile.NewHashTable([]ident.Hash{{9}})
		require.NoError(t, err)
		require.NoError(t, Check(SetAttr{Inode: pos(0), Path: "a", Value: Mode(0o644)}, table))
	})
}

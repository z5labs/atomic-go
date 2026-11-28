package graph

import (
	"fmt"
	"iter"
	"slices"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
)

// Ref is what a GRAPH hash index names once it is resolved against the
// file's hash table: this change, or the change whose hash is Hash. A Ref
// whose Hash is zero, and which is not This, is the virtual ROOT
// (docs/remote/change-format/graph.md#what-an-index-means).
type Ref struct {
	// This reports that the index is [changefile.ThisChange]: the change
	// the file holds. Hash is then zero.
	This bool

	// Hash is the hash in the slot the index names.
	Hash ident.Hash
}

// IsRoot reports whether r is the virtual ROOT: a slot holding the zero
// hash, as slot 0 does in every file Atomic writes.
func (r Ref) IsRoot() bool {
	return !r.This && r.Hash.IsZero()
}

// other reports whether r references another change: neither this change
// nor ROOT.
func (r Ref) other() bool {
	return !r.This && !r.IsRoot()
}

// Resolve returns what i names in t, as Atomic's compactor expands it
// (docs/remote/change-format/container.md#hashindex):
// [changefile.ThisChange] is this change, and any other index is the hash
// in that slot, index 0 included, so index 0 is ROOT only when slot 0 holds
// the zero hash. An index that is not below t.Len() fails with a
// *[changefile.FormatError] of kind [changefile.ErrHashIndexOutOfBounds]
// in [changefile.SectionGraph], with no offset.
func Resolve(t changefile.HashTable, i changefile.HashIndex) (Ref, error) {
	if i == changefile.ThisChange {
		return Ref{This: true}, nil
	}
	h, err := t.At(i)
	if err != nil {
		return Ref{}, formatError(changefile.ErrHashIndexOutOfBounds, fmt.Sprintf("hash index %d is out of bounds (table has %d entries)", i, t.Len()), nil)
	}
	return Ref{Hash: h}, nil
}

// Indices yields every hash index op holds, other than
// [changefile.ThisChange], in the order Atomic's Change::serialize adds the
// hashes they name to the table: the op's fields in wire order, an
// insertion's predecessors, successors and then inode, an edge update's
// edges and then inode, and an edge's from, to and then introduced_by
// (docs/remote/change-format/graph.md#assigning-indices-when-writing). An
// index held more than once is yielded each time. An op that is nil or not
// one of this package's op values, and an [Edit] whose Change is not an
// [Insertion] or an [EdgeUpdate] value, yield nothing of their own.
func Indices(op Op) iter.Seq[changefile.HashIndex] {
	return func(yield func(changefile.HashIndex) bool) {
		eachIndex(op, func(i *changefile.HashIndex) bool {
			if *i == changefile.ThisChange {
				return true
			}
			return yield(*i)
		})
	}
}

// Remap returns a copy of op with every hash index other than
// [changefile.ThisChange] replaced by f of it, the indices visited in the
// order [Indices] yields them. Nothing the copy holds is shared with op.
// Renumbering an op into a new file's table is f mapping each old index to
// the new index of the same hash; [Indices] gives the order to add those
// hashes to the new table in. An op that is nil or not one of this
// package's op values is returned as it is.
func Remap(op Op, f func(changefile.HashIndex) changefile.HashIndex) Op {
	out, _ := eachIndex(op, func(i *changefile.HashIndex) bool {
		if *i != changefile.ThisChange {
			*i = f(*i)
		}
		return true
	})
	return out
}

// Check runs the checks Atomic's reader makes after expanding op through
// t (docs/remote/change-format/graph.md#compactgraphop), and fails with a
// *[changefile.FormatError] in [changefile.SectionGraph], with no offset.
//
// It resolves every index op holds first, in [Indices] order, and fails as
// [Resolve] does on the first that is out of bounds, as expansion does.
// Then a [SetAttr] whose Inode is ROOT is of kind
// [changefile.ErrInvalidGraphOp]. So is a [SolveNameConflict] or
// [UnsolveNameConflict] whose Name.Inode, or any edge's To or IntroducedBy,
// is this change or ROOT, or any edge's From is this change; From may be
// ROOT. A name-conflict op with no edges is the legacy marker a version 1
// file may hold, which [Decode] accepts only from version 1, and is not
// checked, as Atomic does not check it. So is an op that is nil or not one
// of this package's op values. Every other variant has no checks here.
//
// The checks [Decode] and [AppendPayload] run before expansion are not
// repeated.
func Check(op Op, t changefile.HashTable) error {
	for i := range Indices(op) {
		if _, err := Resolve(t, i); err != nil {
			return err
		}
	}
	ref := func(i changefile.HashIndex) Ref {
		r, _ := Resolve(t, i)
		return r
	}
	invalid := func(format string, args ...any) error {
		return formatError(changefile.ErrInvalidGraphOp, fmt.Sprintf(format, args...), nil)
	}

	switch op := op.(type) {
	case SetAttr:
		if ref(op.Inode.Change).IsRoot() {
			return invalid("SetAttr inode must not be ROOT")
		}
	case SolveNameConflict:
		return checkNameConflict(op.Variant(), op.Name, ref, invalid)
	case UnsolveNameConflict:
		return checkNameConflict(op.Variant(), op.Name, ref, invalid)
	}
	return nil
}

// checkNameConflict runs Check's rules on the Name of a SolveNameConflict
// or UnsolveNameConflict.
func checkNameConflict(v OpVariant, name EdgeUpdate, ref func(changefile.HashIndex) Ref, invalid func(string, ...any) error) error {
	if len(name.Edges) == 0 {
		return nil
	}
	if !ref(name.Inode.Change).other() {
		return invalid("%s name inode must reference another change, not this change or ROOT", v)
	}
	for k, edge := range name.Edges {
		if ref(edge.From.Change).This {
			return invalid("%s edge %d from must not be this change", v, k)
		}
		if !ref(edge.To.Change).other() {
			return invalid("%s edge %d to must reference another change, not this change or ROOT", v, k)
		}
		if !ref(edge.IntroducedBy).other() {
			return invalid("%s edge %d introduced_by must reference another change, not this change or ROOT", v, k)
		}
	}
	return nil
}

// eachIndex copies op, calls visit with a pointer to each hash index in
// the copy, in [Indices] order, and returns the copy. It stops early, and
// reports false, when visit returns false; the copy is then partial and
// must be discarded. Otherwise every slice and pointer the copy holds is a
// fresh one, so visit may change the index it is given.
func eachIndex(op Op, visit func(*changefile.HashIndex) bool) (Op, bool) {
	ok := true
	step := func(f func() bool) {
		if ok {
			ok = f()
		}
	}
	switch op := op.(type) {
	case FileAdd:
		step(func() bool { return visitInsertion(&op.AddName, visit) })
		step(func() bool { return visitInsertion(&op.AddInode, visit) })
		op.Contents = cloneVisit(op.Contents, &ok, func(ins *Insertion) bool { return visitInsertion(ins, visit) })
		op.Encoding = clonePtr(op.Encoding)
		return op, ok
	case DirAdd:
		step(func() bool { return visitInsertion(&op.AddName, visit) })
		step(func() bool { return visitInsertion(&op.AddInode, visit) })
		return op, ok
	case DirDel:
		step(func() bool { return visitEdgeUpdate(&op.Del, visit) })
		return op, ok
	case DirUndel:
		step(func() bool { return visitEdgeUpdate(&op.Undel, visit) })
		return op, ok
	case FileDel:
		step(func() bool { return visitEdgeUpdate(&op.Del, visit) })
		op.Contents = cloneVisit(op.Contents, &ok, func(u *EdgeUpdate) bool { return visitEdgeUpdate(u, visit) })
		op.Encoding = clonePtr(op.Encoding)
		return op, ok
	case FileUndel:
		step(func() bool { return visitEdgeUpdate(&op.Undel, visit) })
		op.Contents = cloneVisit(op.Contents, &ok, func(u *EdgeUpdate) bool { return visitEdgeUpdate(u, visit) })
		op.Encoding = clonePtr(op.Encoding)
		return op, ok
	case FileMove:
		step(func() bool { return visitEdgeUpdate(&op.Del, visit) })
		step(func() bool { return visitInsertion(&op.Add, visit) })
		return op, ok
	case Edit:
		switch a := op.Change.(type) {
		case Insertion:
			step(func() bool { return visitInsertion(&a, visit) })
			op.Change = a
		case EdgeUpdate:
			step(func() bool { return visitEdgeUpdate(&a, visit) })
			op.Change = a
		}
		op.Encoding = clonePtr(op.Encoding)
		return op, ok
	case Replacement:
		step(func() bool { return visitEdgeUpdate(&op.Change, visit) })
		step(func() bool { return visitInsertion(&op.Replacement, visit) })
		op.Encoding = clonePtr(op.Encoding)
		return op, ok
	case SolveNameConflict:
		step(func() bool { return visitEdgeUpdate(&op.Name, visit) })
		return op, ok
	case UnsolveNameConflict:
		step(func() bool { return visitEdgeUpdate(&op.Name, visit) })
		return op, ok
	case SolveOrderConflict:
		step(func() bool { return visitEdgeUpdate(&op.Change, visit) })
		return op, ok
	case UnsolveOrderConflict:
		step(func() bool { return visitEdgeUpdate(&op.Change, visit) })
		return op, ok
	case ResurrectZombies:
		step(func() bool { return visitEdgeUpdate(&op.Change, visit) })
		op.Encoding = clonePtr(op.Encoding)
		return op, ok
	case AddRoot:
		step(func() bool { return visitInsertion(&op.Name, visit) })
		step(func() bool { return visitInsertion(&op.Inode, visit) })
		return op, ok
	case DelRoot:
		step(func() bool { return visitEdgeUpdate(&op.Name, visit) })
		step(func() bool { return visitEdgeUpdate(&op.Inode, visit) })
		return op, ok
	case SetAttr:
		step(func() bool { return visit(&op.Inode.Change) })
		return op, ok
	default:
		return op, true
	}
}

// visitInsertion clones ins's slices and visits its predecessors,
// successors and inode.
func visitInsertion(ins *Insertion, visit func(*changefile.HashIndex) bool) bool {
	ins.Predecessors = slices.Clone(ins.Predecessors)
	ins.Successors = slices.Clone(ins.Successors)
	for k := range ins.Predecessors {
		if !visit(&ins.Predecessors[k].Change) {
			return false
		}
	}
	for k := range ins.Successors {
		if !visit(&ins.Successors[k].Change) {
			return false
		}
	}
	return visit(&ins.Inode.Change)
}

// visitEdgeUpdate clones u's edges and visits each edge's from, to and
// introduced_by, then u's inode.
func visitEdgeUpdate(u *EdgeUpdate, visit func(*changefile.HashIndex) bool) bool {
	u.Edges = slices.Clone(u.Edges)
	for k := range u.Edges {
		e := &u.Edges[k]
		if !visit(&e.From.Change) || !visit(&e.To.Change) || !visit(&e.IntroducedBy) {
			return false
		}
	}
	return visit(&u.Inode.Change)
}

// cloneVisit returns a copy of *p, or nil for a nil p, after running f on
// the copy while *ok holds, setting *ok to its result.
func cloneVisit[T any](p *T, ok *bool, f func(*T) bool) *T {
	if p == nil {
		return nil
	}
	c := *p
	if *ok {
		*ok = f(&c)
	}
	return &c
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

package graph

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/internal/postcard"
)

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func ptr[T any](v T) *T {
	return &v
}

// versions returns the schema versions a GRAPH payload can come from.
func versions() []changefile.Version {
	return []changefile.Version{changefile.Version1, changefile.Version2, changefile.Version3}
}

// examplePayload is the 97-byte GRAPH payload of
// docs/remote/change-format/graph.md#example-graph-payload: a FileAdd of
// a.txt and two Edits, with a content_end of 22.
func examplePayload() []byte {
	return cat(
		[]byte{0x00, 0x03},
		// op 0: FileAdd
		[]byte{0x00},
		[]byte{0x01, 0x00, 0x00, 0x00, 0x11, 0x00, 0x05, 0x00, 0x00},
		[]byte{0x01, 0xff, 0xff, 0x03, 0x05, 0x00, 0x11, 0x05, 0x05, 0xff, 0xff, 0x03, 0x05},
		[]byte{0x01, 0x01, 0xff, 0xff, 0x03, 0x05, 0x00, 0x01, 0x05, 0x0b, 0xff, 0xff, 0x03, 0x05},
		[]byte{0x05, 0x61, 0x2e, 0x74, 0x78, 0x74},
		[]byte{0x01, 0x00},
		// op 1: Edit
		[]byte{0x07, 0x00, 0x01, 0xff, 0xff, 0x03, 0x0b, 0x00, 0x01, 0x0b, 0x10, 0xff, 0xff, 0x03, 0x05},
		[]byte{0x05, 0x61, 0x2e, 0x74, 0x78, 0x74, 0x02, 0x01, 0x00},
		// op 2: Edit
		[]byte{0x07, 0x00, 0x01, 0xff, 0xff, 0x03, 0x10, 0x00, 0x01, 0x10, 0x16, 0xff, 0xff, 0x03, 0x05},
		[]byte{0x05, 0x61, 0x2e, 0x74, 0x78, 0x74, 0x03, 0x01, 0x00},
		// content_start, content_end
		[]byte{0x00, 0x16},
	)
}

func exampleValue() Payload {
	this := changefile.ThisChange
	inode := Position{Change: this, Pos: 5}
	return Payload{
		Ops: []Op{
			FileAdd{
				AddName: Insertion{
					Predecessors: []Position{{Change: 0, Pos: 0}},
					Flag:         Folder | Block,
					Start:        0,
					End:          5,
					Inode:        Position{Change: 0, Pos: 0},
				},
				AddInode: Insertion{
					Predecessors: []Position{inode},
					Flag:         Folder | Block,
					Start:        5,
					End:          5,
					Inode:        inode,
				},
				Contents: &Insertion{
					Predecessors: []Position{inode},
					Flag:         Block,
					Start:        5,
					End:          11,
					Inode:        inode,
				},
				Path:     "a.txt",
				Encoding: ptr(Utf8),
			},
			Edit{
				Change: Insertion{
					Predecessors: []Position{{Change: this, Pos: 11}},
					Flag:         Block,
					Start:        11,
					End:          16,
					Inode:        inode,
				},
				Local:    Local{Path: "a.txt", Line: 2},
				Encoding: ptr(Utf8),
			},
			Edit{
				Change: Insertion{
					Predecessors: []Position{{Change: this, Pos: 16}},
					Flag:         Block,
					Start:        16,
					End:          22,
					Inode:        inode,
				},
				Local:    Local{Path: "a.txt", Line: 3},
				Encoding: ptr(Utf8),
			},
		},
		ContentStart: 0,
		ContentEnd:   22,
	}
}

func TestDecode_Example(t *testing.T) {
	b := examplePayload()
	require.Len(t, b, 97)

	for _, v := range versions() {
		t.Run(fmt.Sprintf("version %d", v), func(t *testing.T) {
			p, err := Decode(b, v)
			require.NoError(t, err)
			require.Equal(t, exampleValue(), p)

			out, err := AppendPayload(nil, p)
			require.NoError(t, err)
			require.Equal(t, b, out)
		})
	}
}

func TestDecode_IgnoresTrailingBytes(t *testing.T) {
	p, err := Decode(cat(examplePayload(), []byte{0xa5}), changefile.Version2)
	require.NoError(t, err)
	require.Equal(t, exampleValue(), p)
}

func TestDecode_DoesNotAliasInput(t *testing.T) {
	b := examplePayload()
	p, err := Decode(b, changefile.Version2)
	require.NoError(t, err)
	for i := range b {
		b[i] = 0
	}
	require.Equal(t, exampleValue(), p)
}

func TestDecode_EmptyPayload(t *testing.T) {
	// The test payload Atomic sizes: path src/main.rs, no ops, 0..1024.
	b := cat([]byte{0x0b}, []byte("src/main.rs"), []byte{0x00, 0x00, 0x80, 0x08})
	p, err := Decode(b, changefile.Version3)
	require.NoError(t, err)
	require.Equal(t, Payload{Path: "src/main.rs", ContentEnd: 1024}, p)
	require.Nil(t, p.Ops)

	out, err := AppendPayload(nil, p)
	require.NoError(t, err)
	require.Equal(t, b, out)
}

func TestDecode_KeepsUnknownFlagBits(t *testing.T) {
	b := []byte{
		0x00, 0x02,
		// An Edit inserting with flag 0x02, a bit Atomic does not define.
		0x07, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00,
		// A DirDel whose one edge goes from 0x02 to 0x83 (BLOCK | DELETED
		// plus 0x02), from {0, 0} to {1, 0..1}, introduced by 1.
		0x02, 0x01, 0x02, 0x83, 0x00, 0x00, 0x01, 0x00, 0x01, 0x01, 0x01, 0x00,
		0x01, 'd',
		0x00, 0x00,
	}
	p, err := Decode(b, changefile.Version2)
	require.NoError(t, err)
	require.Len(t, p.Ops, 2)

	edit, ok := p.Ops[0].(Edit)
	require.True(t, ok)
	ins, ok := edit.Change.(Insertion)
	require.True(t, ok)
	require.Equal(t, EdgeFlags(0x02), ins.Flag)
	require.Equal(t, EdgeFlags(0), ins.Flag.Known())

	del, ok := p.Ops[1].(DirDel)
	require.True(t, ok)
	require.Len(t, del.Del.Edges, 1)
	edge := del.Del.Edges[0]
	require.Equal(t, EdgeFlags(0x02), edge.Previous)
	require.Equal(t, EdgeFlags(0x83), edge.Flag)
	require.Equal(t, EdgeFlags(0), edge.Previous.Known())
	require.Equal(t, Block|Deleted, edge.Flag.Known())

	out, err := AppendPayload(nil, p)
	require.NoError(t, err)
	require.Equal(t, b, out)
}

func TestEdgeFlags_Known(t *testing.T) {
	require.Equal(t, EdgeFlags(0), EdgeFlags(0x02).Known())
	require.Equal(t, Block|Pseudo|Folder|Parent|Deleted, EdgeFlags(0xff).Known())
	require.Equal(t, Folder|Block|Deleted, EdgeFlags(0x91).Known())
}

func TestDecode_FileDelFieldOrder(t *testing.T) {
	// Written by hand from the CompactEdgeUpdate and CompactNewEdge tables,
	// so that the edge fields are pinned to the documented order rather
	// than to whatever the encoder does.
	b := []byte{
		0x00, 0x01,
		0x04,
		// del: one edge, BLOCK -> BLOCK|DELETED, from {0, 0} to {1, 0..5},
		// introduced_by 1; inode {1, 0}.
		0x01, 0x01, 0x81, 0x00, 0x00, 0x01, 0x00, 0x05, 0x01, 0x01, 0x00,
		// contents: Some, no edges, inode {1, 7}
		0x01, 0x00, 0x01, 0x07,
		// path "x", encoding None
		0x01, 'x', 0x00,
		0x00, 0x00,
	}
	p, err := Decode(b, changefile.Version2)
	require.NoError(t, err)
	require.Equal(t, Payload{Ops: []Op{FileDel{
		Del: EdgeUpdate{
			Edges: []NewEdge{{
				Previous:     Block,
				Flag:         Block | Deleted,
				From:         Position{Change: 0, Pos: 0},
				To:           Node{Change: 1, Start: 0, End: 5},
				IntroducedBy: 1,
			}},
			Inode: Position{Change: 1, Pos: 0},
		},
		Contents: &EdgeUpdate{Inode: Position{Change: 1, Pos: 7}},
		Path:     "x",
	}}}, p)

	out, err := AppendPayload(nil, p)
	require.NoError(t, err)
	require.Equal(t, b, out)
}

func TestDecode_FieldOrder(t *testing.T) {
	// Each op is written by hand from the CompactGraphOp table, with
	// distinct values in every field, so that a field order swapped the
	// same way in the decoder and the encoder still fails here.
	insA := []byte{0x01, 0x01, 0x02, 0x00, 0x10, 0x03, 0x04, 0x01, 0x05}
	insAValue := Insertion{Predecessors: []Position{{Change: 1, Pos: 2}}, Flag: Folder, Start: 3, End: 4, Inode: Position{Change: 1, Pos: 5}}
	insB := []byte{0x00, 0x01, 0x02, 0x06, 0x01, 0x07, 0x08, 0x02, 0x09}
	insBValue := Insertion{Successors: []Position{{Change: 2, Pos: 6}}, Flag: Block, Start: 7, End: 8, Inode: Position{Change: 2, Pos: 9}}
	updA := []byte{0x01, 0x01, 0x81, 0x03, 0x01, 0x03, 0x01, 0x02, 0x03, 0x03, 0x04}
	updAValue := EdgeUpdate{
		Edges: []NewEdge{{Previous: Block, Flag: Block | Deleted, From: Position{Change: 3, Pos: 1}, To: Node{Change: 3, Start: 1, End: 2}, IntroducedBy: 3}},
		Inode: Position{Change: 3, Pos: 4},
	}
	updB := []byte{0x00, 0x04, 0x05}
	updBValue := EdgeUpdate{Inode: Position{Change: 4, Pos: 5}}
	// One name edge, 0x11 -> 0x91 (0x91 -> 0x11 for Unsolve), from {0, 6}
	// to {5, 1..7}, introduced by 5; inode {5, 8}.
	nameSolve := []byte{0x01, 0x11, 0x91, 0x00, 0x06, 0x05, 0x01, 0x07, 0x05, 0x05, 0x08}
	nameUnsolve := []byte{0x01, 0x91, 0x11, 0x00, 0x06, 0x05, 0x01, 0x07, 0x05, 0x05, 0x08}
	nameEdge := NewEdge{From: Position{Change: 0, Pos: 6}, To: Node{Change: 5, Start: 1, End: 7}, IntroducedBy: 5}
	solveEdge, unsolveEdge := nameEdge, nameEdge
	solveEdge.Previous, solveEdge.Flag = Folder|Block, Folder|Block|Deleted
	unsolveEdge.Previous, unsolveEdge.Flag = Folder|Block|Deleted, Folder|Block
	nameSolveValue := EdgeUpdate{Edges: []NewEdge{solveEdge}, Inode: Position{Change: 5, Pos: 8}}
	nameUnsolveValue := EdgeUpdate{Edges: []NewEdge{unsolveEdge}, Inode: Position{Change: 5, Pos: 8}}

	testCases := []struct {
		name string
		op   []byte
		want Op
	}{
		{name: "DirAdd", op: cat([]byte{0x01}, insA, insB, []byte{0x01, 'd'}), want: DirAdd{AddName: insAValue, AddInode: insBValue, Path: "d"}},
		{name: "DirDel", op: cat([]byte{0x02}, updA, []byte{0x01, 'd'}), want: DirDel{Del: updAValue, Path: "d"}},
		{name: "DirUndel", op: cat([]byte{0x03}, updA, []byte{0x01, 'd'}), want: DirUndel{Undel: updAValue, Path: "d"}},
		{name: "FileUndel", op: cat([]byte{0x05}, updA, []byte{0x01}, updB, []byte{0x01, 'f', 0x01, 0x03}), want: FileUndel{Undel: updAValue, Contents: &updBValue, Path: "f", Encoding: ptr(Latin1)}},
		{name: "FileMove", op: cat([]byte{0x06}, updA, insA, []byte{0x01, 'g'}), want: FileMove{Del: updAValue, Add: insAValue, Path: "g"}},
		{name: "Edit EdgeUpdate", op: cat([]byte{0x07, 0x01}, updA, []byte{0x01, 'f', 0x09, 0x00}), want: Edit{Change: updAValue, Local: Local{Path: "f", Line: 9}}},
		{name: "Replacement", op: cat([]byte{0x08}, updA, insB, []byte{0x01, 'f', 0x0a, 0x01, 0x04}), want: Replacement{Change: updAValue, Replacement: insBValue, Local: Local{Path: "f", Line: 10}, Encoding: ptr(Binary)}},
		{name: "SolveNameConflict", op: cat([]byte{0x09}, nameSolve, []byte{0x01, 'n'}), want: SolveNameConflict{Name: nameSolveValue, Path: "n"}},
		{name: "UnsolveNameConflict", op: cat([]byte{0x0a}, nameUnsolve, []byte{0x01, 'n'}), want: UnsolveNameConflict{Name: nameUnsolveValue, Path: "n"}},
		{name: "SolveOrderConflict", op: cat([]byte{0x0b}, updA, []byte{0x01, 'f', 0x0b}), want: SolveOrderConflict{Change: updAValue, Local: Local{Path: "f", Line: 11}}},
		{name: "UnsolveOrderConflict", op: cat([]byte{0x0c}, updA, []byte{0x01, 'f', 0x0c}), want: UnsolveOrderConflict{Change: updAValue, Local: Local{Path: "f", Line: 12}}},
		{name: "ResurrectZombies", op: cat([]byte{0x0d}, updA, []byte{0x01, 'f', 0x0d, 0x01, 0x02}), want: ResurrectZombies{Change: updAValue, Local: Local{Path: "f", Line: 13}, Encoding: ptr(Utf16Be)}},
		{name: "AddRoot", op: cat([]byte{0x0e}, insA, insB), want: AddRoot{Name: insAValue, Inode: insBValue}},
		{name: "DelRoot", op: cat([]byte{0x0f}, updA, updB), want: DelRoot{Name: updAValue, Inode: updBValue}},
		// inode {1, 7}, path "link", value Kind(Symlink): the round trip
		// Atomic's own test makes.
		{name: "SetAttr Kind", op: []byte{0x10, 0x01, 0x07, 0x04, 'l', 'i', 'n', 'k', 0x01, 0x01}, want: SetAttr{Inode: Position{Change: 1, Pos: 7}, Path: "link", Value: Symlink}},
		// value Mode(0o644), a two-byte varint.
		{name: "SetAttr Mode", op: []byte{0x10, 0x02, 0x08, 0x01, 'm', 0x00, 0xa4, 0x03}, want: SetAttr{Inode: Position{Change: 2, Pos: 8}, Path: "m", Value: Mode(0o644)}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := cat([]byte{0x00, 0x01}, tc.op, []byte{0x00, 0x00})
			p, err := Decode(b, changefile.Version2)
			require.NoError(t, err)
			require.Equal(t, Payload{Ops: []Op{tc.want}}, p)

			out, err := AppendPayload(nil, p)
			require.NoError(t, err)
			require.Equal(t, b, out)
		})
	}
}

func TestAtom_Variants(t *testing.T) {
	ins := Insertion{
		Predecessors: []Position{{Change: changefile.ThisChange, Pos: 3}},
		Successors:   []Position{{Change: 2, Pos: 9}},
		Flag:         Block,
		Start:        3,
		End:          7,
		Inode:        Position{Change: 2, Pos: 1},
	}
	upd := EdgeUpdate{
		Edges: []NewEdge{{
			Previous:     Block,
			Flag:         Block | Deleted,
			From:         Position{Change: 2, Pos: 4},
			To:           Node{Change: 2, Start: 4, End: 8},
			IntroducedBy: 2,
		}},
		Inode: Position{Change: 2, Pos: 1},
	}

	testCases := []struct {
		name    string
		atom    Atom
		variant byte
	}{
		{name: "Insertion", atom: ins, variant: 0},
		{name: "EdgeUpdate", atom: upd, variant: 1},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := Payload{Ops: []Op{Edit{Change: tc.atom, Local: Local{Path: "f", Line: 4}}}}
			b, err := AppendPayload(nil, p)
			require.NoError(t, err)
			// path "", one op, Edit, then the atom's variant index.
			require.Equal(t, []byte{0x00, 0x01, 0x07, tc.variant}, b[:4])

			got, err := Decode(b, changefile.Version2)
			require.NoError(t, err)
			require.Equal(t, p, got)
		})
	}

	t.Run("unknown variant", func(t *testing.T) {
		b := []byte{0x00, 0x01, 0x07, 0x02}
		_, err := Decode(b, changefile.Version2)
		requireFormatError(t, err, changefile.ErrPayload)
		require.ErrorIs(t, err, postcard.ErrUnknownVariant)
	})
}

func TestRoundTrip_EveryVariant(t *testing.T) {
	this := changefile.ThisChange
	ins := func(start, end uint32) Insertion {
		return Insertion{
			Predecessors: []Position{{Change: 1, Pos: 2}, {Change: this, Pos: 0}},
			Successors:   []Position{{Change: 300, Pos: 70000}},
			Flag:         Folder | Block,
			Start:        start,
			End:          end,
			Inode:        Position{Change: this, Pos: start},
		}
	}
	upd := func(n uint32) EdgeUpdate {
		return EdgeUpdate{
			Edges: []NewEdge{
				{Previous: Block, Flag: Block | Deleted, From: Position{Change: 1, Pos: n}, To: Node{Change: 1, Start: n, End: n + 4}, IntroducedBy: 1},
				{Previous: Folder | Block, Flag: Folder | Block | Deleted, From: Position{Change: 0, Pos: 0}, To: Node{Change: 2, Start: 0, End: 3}, IntroducedBy: 2},
			},
			Inode: Position{Change: 1, Pos: n},
		}
	}
	names := func(previous, flag EdgeFlags) EdgeUpdate {
		return EdgeUpdate{
			Edges: []NewEdge{
				{Previous: previous, Flag: flag, From: Position{Change: 0, Pos: 0}, To: Node{Change: 1, Start: 0, End: 3}, IntroducedBy: 1},
				{Previous: previous, Flag: flag, From: Position{Change: 0, Pos: 0}, To: Node{Change: 2, Start: 4, End: 9}, IntroducedBy: 2},
			},
			Inode: Position{Change: 1, Pos: 3},
		}
	}

	testCases := []struct {
		variant OpVariant
		op      Op
	}{
		{OpFileAdd, FileAdd{AddName: ins(0, 3), AddInode: ins(3, 3), Contents: ptr(ins(3, 9)), Path: "dir/f", Encoding: ptr(Latin1)}},
		{OpFileAdd, FileAdd{AddName: ins(0, 3), AddInode: ins(3, 3), Path: "empty"}},
		{OpDirAdd, DirAdd{AddName: ins(0, 3), AddInode: ins(3, 3), Path: "dir"}},
		{OpDirDel, DirDel{Del: upd(5), Path: "dir"}},
		{OpDirUndel, DirUndel{Undel: upd(6), Path: "dir"}},
		{OpFileDel, FileDel{Del: upd(7), Contents: ptr(upd(8)), Path: "f", Encoding: ptr(Binary)}},
		{OpFileDel, FileDel{Del: upd(7), Path: "f"}},
		{OpFileUndel, FileUndel{Undel: upd(9), Contents: ptr(upd(10)), Path: "f", Encoding: ptr(Utf16Be)}},
		{OpFileUndel, FileUndel{Undel: upd(9), Path: "f"}},
		{OpFileMove, FileMove{Del: upd(11), Add: ins(0, 4), Path: "g"}},
		{OpEdit, Edit{Change: ins(4, 9), Local: Local{Path: "f", Line: 12}, Encoding: ptr(Utf16Le)}},
		{OpEdit, Edit{Change: upd(12), Local: Local{Path: "f", Line: 1 << 40}}},
		{OpReplacement, Replacement{Change: upd(13), Replacement: ins(9, 12), Local: Local{Path: "f", Line: 3}, Encoding: ptr(Utf8)}},
		{OpReplacement, Replacement{Change: upd(13), Replacement: ins(9, 12), Local: Local{Path: "f", Line: 3}}},
		{OpSolveNameConflict, SolveNameConflict{Name: names(Folder|Block, Folder|Block|Deleted), Path: "f"}},
		{OpUnsolveNameConflict, UnsolveNameConflict{Name: names(Folder|Block|Deleted, Folder|Block), Path: "f"}},
		{OpSolveOrderConflict, SolveOrderConflict{Change: upd(14), Local: Local{Path: "f", Line: 5}}},
		{OpUnsolveOrderConflict, UnsolveOrderConflict{Change: upd(15), Local: Local{Path: "f", Line: 6}}},
		{OpResurrectZombies, ResurrectZombies{Change: upd(16), Local: Local{Path: "f", Line: 7}, Encoding: ptr(Utf8)}},
		{OpResurrectZombies, ResurrectZombies{Change: upd(16), Local: Local{Path: "f", Line: 7}}},
		{OpAddRoot, AddRoot{Name: ins(0, 0), Inode: ins(0, 0)}},
		{OpDelRoot, DelRoot{Name: upd(17), Inode: upd(18)}},
		{OpSetAttr, SetAttr{Inode: Position{Change: 1, Pos: 7}, Path: "f", Value: Mode(0o755)}},
		{OpSetAttr, SetAttr{Inode: Position{Change: 1, Pos: 7}, Path: "f", Value: MaxMode}},
		{OpSetAttr, SetAttr{Inode: Position{Change: this, Pos: 7}, Path: "f", Value: Regular}},
		{OpSetAttr, SetAttr{Inode: Position{Change: 1, Pos: 7}, Path: "f", Value: Gitlink}},
	}
	for _, tc := range testCases {
		t.Run(tc.variant.String(), func(t *testing.T) {
			require.Equal(t, tc.variant, tc.op.Variant())

			p := Payload{Path: "p", Ops: []Op{tc.op}, ContentStart: 1, ContentEnd: 1 << 33}
			b, err := AppendPayload(nil, p)
			require.NoError(t, err)
			// path "p", one op, then the op's variant index.
			require.Equal(t, []byte{0x01, 'p', 0x01, byte(tc.variant)}, b[:4])

			got, err := Decode(b, changefile.Version3)
			require.NoError(t, err)
			require.Equal(t, p, got)

			again, err := AppendPayload(nil, got)
			require.NoError(t, err)
			require.Equal(t, b, again)
		})
	}
}

func TestDecode_RefusesPrototypeContext(t *testing.T) {
	// Variant 17 exists only in an experimental build of Atomic; 18 is
	// beyond every build.
	for _, v := range []OpVariant{OpSetAttr + 1, OpSetAttr + 2} {
		t.Run(v.String(), func(t *testing.T) {
			b := []byte{0x00, 0x01, byte(v), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
			for _, ver := range versions() {
				_, err := Decode(b, ver)
				requireFormatError(t, err, changefile.ErrPayload)
				require.ErrorIs(t, err, postcard.ErrUnknownVariant)

				var pe *postcard.Error
				require.ErrorAs(t, err, &pe)
				require.Equal(t, int64(2), pe.Offset)
			}
		})
	}
}

func TestDecode_Errors(t *testing.T) {
	example := examplePayload()

	testCases := []struct {
		name  string
		b     []byte
		cause error
	}{
		{name: "empty", b: nil, cause: postcard.ErrUnexpectedEnd},
		{name: "truncated", b: example[:60], cause: postcard.ErrUnexpectedEnd},
		{name: "no content_end", b: example[:96], cause: postcard.ErrUnexpectedEnd},
		{name: "path not UTF-8", b: []byte{0x01, 0xff, 0x00, 0x00, 0x00}, cause: postcard.ErrBadUTF8},
		// Offset 25 is FileAdd's contents option tag.
		{name: "bad option tag", b: cat(example[:25], []byte{0x02}), cause: postcard.ErrBadOption},
		{name: "unknown encoding", b: []byte{
			0x00, 0x01,
			0x07, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x01, 0x05,
			0x00, 0x00,
		}, cause: postcard.ErrUnknownVariant},
		{name: "hash index overflows u16", b: []byte{
			0x00, 0x01,
			0x07, 0x00, 0x01, 0xff, 0xff, 0x04,
		}, cause: postcard.ErrBadVarint},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Decode(tc.b, changefile.Version2)
			requireFormatError(t, err, changefile.ErrPayload)
			require.ErrorIs(t, err, tc.cause)
			require.Equal(t, Payload{}, p)
		})
	}
}

func TestDecode_UnsupportedVersion(t *testing.T) {
	for _, v := range []changefile.Version{0, 4} {
		_, err := Decode(examplePayload(), v)
		requireFormatError(t, err, changefile.ErrUnsupportedVersion)
	}
}

func TestAppendPayload_Errors(t *testing.T) {
	bad := string([]byte{0xff})
	ins := Insertion{Flag: Block}

	testCases := []struct {
		name string
		p    Payload
		kind error
	}{
		{name: "payload path not UTF-8", p: Payload{Path: bad}, kind: changefile.ErrPayload},
		{name: "nil op", p: Payload{Ops: []Op{nil}}, kind: changefile.ErrInvalidGraphOp},
		{name: "pointer op", p: Payload{Ops: []Op{&DirAdd{Path: "d"}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "op path not UTF-8", p: Payload{Ops: []Op{DirDel{Path: bad}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "local path not UTF-8", p: Payload{Ops: []Op{Edit{Change: ins, Local: Local{Path: bad}}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "nil atom", p: Payload{Ops: []Op{Edit{Local: Local{Path: "f"}}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "pointer atom", p: Payload{Ops: []Op{Edit{Change: &ins}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "unknown encoding", p: Payload{Ops: []Op{FileAdd{Path: "f", Encoding: ptr(Encoding(5))}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "second op invalid", p: Payload{Ops: []Op{DirAdd{Path: "d"}, Replacement{Encoding: ptr(Encoding(9))}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "name conflict path not UTF-8", p: Payload{Ops: []Op{SolveNameConflict{Path: bad}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "order conflict local path not UTF-8", p: Payload{Ops: []Op{UnsolveOrderConflict{Local: Local{Path: bad}}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "zombie unknown encoding", p: Payload{Ops: []Op{ResurrectZombies{Local: Local{Path: "f"}, Encoding: ptr(Encoding(5))}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "SetAttr path not UTF-8", p: Payload{Ops: []Op{SetAttr{Path: bad, Value: Mode(0)}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "SetAttr nil value", p: Payload{Ops: []Op{SetAttr{Path: "f"}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "SetAttr pointer value", p: Payload{Ops: []Op{SetAttr{Path: "f", Value: ptr(Mode(0))}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "SetAttr unknown kind", p: Payload{Ops: []Op{SetAttr{Path: "f", Value: InodeKind(3)}}}, kind: changefile.ErrInvalidGraphOp},
		{name: "pointer SetAttr", p: Payload{Ops: []Op{&SetAttr{Path: "f", Value: Mode(0)}}}, kind: changefile.ErrInvalidGraphOp},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dst := []byte{0xaa}
			out, err := AppendPayload(dst, tc.p)
			requireFormatError(t, err, tc.kind)
			require.Equal(t, []byte{0xaa}, out)
		})
	}
}

func TestAppendPayload_Appends(t *testing.T) {
	out, err := AppendPayload([]byte{0xaa}, exampleValue())
	require.NoError(t, err)
	require.Equal(t, cat([]byte{0xaa}, examplePayload()), out)
}

func TestOpVariant_String(t *testing.T) {
	names := []string{
		"FileAdd", "DirAdd", "DirDel", "DirUndel", "FileDel", "FileUndel",
		"FileMove", "Edit", "Replacement", "SolveNameConflict",
		"UnsolveNameConflict", "SolveOrderConflict", "UnsolveOrderConflict",
		"ResurrectZombies", "AddRoot", "DelRoot", "SetAttr",
	}
	require.Equal(t, OpVariant(len(names)-1), OpSetAttr)
	for i, name := range names {
		require.Equal(t, name, OpVariant(i).String())
	}
	require.Equal(t, "OpVariant(17)", OpVariant(17).String())
}

func TestEnum_Strings(t *testing.T) {
	require.Equal(t, []string{"Utf8", "Utf16Le", "Utf16Be", "Latin1", "Binary", "Encoding(5)"},
		[]string{Utf8.String(), Utf16Le.String(), Utf16Be.String(), Latin1.String(), Binary.String(), Encoding(5).String()})
	require.Equal(t, []string{"Regular", "Symlink", "Gitlink", "InodeKind(3)"},
		[]string{Regular.String(), Symlink.String(), Gitlink.String(), InodeKind(3).String()})

	var attrs []InodeAttr
	attrs = append(attrs, Mode(0o644), Symlink)
	require.Len(t, attrs, 2)
}

// rawPayload writes ops with an empty path and content range, without the
// checks AppendPayload runs, so that a test can hand Decode an op that
// breaks one of them. Every SetAttr value must be a Mode or an InodeKind.
func rawPayload(ops ...Op) []byte {
	e := postcard.NewEncoder(nil)
	e.Str("")
	postcard.PutSeq(e, ops, putOp)
	e.U64(0)
	e.U64(0)
	return e.Bytes()
}

// requireRule asserts that ops break one of Atomic's pre-expansion checks:
// Decode refuses them in every version given, and AppendPayload refuses
// them.
func requireRule(t *testing.T, ops []Op, vs ...changefile.Version) {
	t.Helper()
	b := rawPayload(ops...)
	for _, v := range vs {
		p, err := Decode(b, v)
		requireFormatError(t, err, changefile.ErrInvalidGraphOp)
		require.Equal(t, Payload{}, p)
	}
	out, err := AppendPayload([]byte{0xaa}, Payload{Ops: ops})
	requireFormatError(t, err, changefile.ErrInvalidGraphOp)
	require.Equal(t, []byte{0xaa}, out)
}

// requirePasses asserts that ops pass Atomic's pre-expansion checks: they
// encode, and decode in every version back to themselves.
func requirePasses(t *testing.T, ops []Op) {
	t.Helper()
	p := Payload{Ops: ops}
	b, err := AppendPayload(nil, p)
	require.NoError(t, err)
	require.Equal(t, rawPayload(ops...), b)
	for _, v := range versions() {
		got, err := Decode(b, v)
		require.NoError(t, err)
		require.Equal(t, p, got)
	}
}

func TestSetAttr_Rules(t *testing.T) {
	inode := Position{Change: 1, Pos: 7}

	t.Run("Kind(Symlink) round-trips", func(t *testing.T) {
		requirePasses(t, []Op{SetAttr{Inode: inode, Path: "link", Value: Symlink}})
	})
	t.Run("Mode 0o777 passes", func(t *testing.T) {
		requirePasses(t, []Op{SetAttr{Inode: inode, Path: "f", Value: Mode(0o777)}})
	})
	t.Run("Mode 0o1000 is refused", func(t *testing.T) {
		requireRule(t, []Op{SetAttr{Inode: inode, Path: "f", Value: Mode(0o1000)}}, versions()...)
	})
	t.Run("Mode 0xffff is refused", func(t *testing.T) {
		requireRule(t, []Op{SetAttr{Inode: inode, Path: "f", Value: Mode(0xffff)}}, versions()...)
	})
	t.Run("empty path is refused", func(t *testing.T) {
		requireRule(t, []Op{SetAttr{Inode: inode, Value: Symlink}}, versions()...)
	})
}

func TestSetAttr_DecodeErrors(t *testing.T) {
	// A payload of one SetAttr, inode {1, 7}, path "f", whose value starts
	// at offset 7. Each tail is the rest of the payload: the value as far
	// as it goes, then the content range where the value is whole.
	testCases := []struct {
		name   string
		tail   []byte
		cause  error
		offset int64
	}{
		{name: "unknown attribute variant", tail: []byte{0x02, 0x00, 0x00, 0x00}, cause: postcard.ErrUnknownVariant, offset: 7},
		{name: "unknown inode kind", tail: []byte{0x01, 0x03, 0x00, 0x00}, cause: postcard.ErrUnknownVariant, offset: 8},
		{name: "mode overflows u16", tail: []byte{0x00, 0xff, 0xff, 0x04, 0x00, 0x00}, cause: postcard.ErrBadVarint, offset: 8},
		{name: "value missing", tail: nil, cause: postcard.ErrUnexpectedEnd, offset: 7},
		{name: "mode missing", tail: []byte{0x00}, cause: postcard.ErrUnexpectedEnd, offset: 8},
		{name: "mode truncated", tail: []byte{0x00, 0xa4}, cause: postcard.ErrUnexpectedEnd, offset: 8},
		{name: "kind missing", tail: []byte{0x01}, cause: postcard.ErrUnexpectedEnd, offset: 8},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := cat([]byte{0x00, 0x01, 0x10, 0x01, 0x07, 0x01, 'f'}, tc.tail)
			p, err := Decode(b, changefile.Version2)
			requireFormatError(t, err, changefile.ErrPayload)
			require.ErrorIs(t, err, tc.cause)
			require.Equal(t, Payload{}, p)

			var pe *postcard.Error
			require.ErrorAs(t, err, &pe)
			require.Equal(t, tc.offset, pe.Offset)
		})
	}
}

func TestNameConflict_Rules(t *testing.T) {
	alive, deleted := Folder|Block, Folder|Block|Deleted
	edge := func(previous, flag EdgeFlags, start, end uint32) NewEdge {
		return NewEdge{Previous: previous, Flag: flag, From: Position{Change: 0, Pos: 0}, To: Node{Change: 1, Start: start, End: end}, IntroducedBy: 1}
	}
	from := func(e NewEdge, p Position) NewEdge {
		e.From = p
		return e
	}
	introducedBy := func(e NewEdge, h changefile.HashIndex) NewEdge {
		e.IntroducedBy = h
		return e
	}
	update := func(edges ...NewEdge) EdgeUpdate {
		return EdgeUpdate{Edges: edges, Inode: Position{Change: 1, Pos: 9}}
	}
	solve := func(path string, edges ...NewEdge) Op {
		return SolveNameConflict{Name: update(edges...), Path: path}
	}
	unsolve := func(path string, edges ...NewEdge) Op {
		return UnsolveNameConflict{Name: update(edges...), Path: path}
	}

	passes := []struct {
		name string
		op   Op
	}{
		{name: "Solve 0x11 to 0x91", op: solve("f", edge(alive, deleted, 0, 3))},
		{name: "Unsolve 0x91 to 0x11", op: unsolve("f", edge(deleted, alive, 0, 3))},
		{name: "Solve with distinct edges", op: solve("f", edge(alive, deleted, 0, 3), edge(alive, deleted, 3, 5))},
		{name: "Unsolve with distinct edges", op: unsolve("f", edge(deleted, alive, 0, 3), edge(deleted, alive, 3, 5))},
		// A repeat is the whole edge: the same To with another From or
		// IntroducedBy is a distinct edge.
		{name: "Solve same To, other From", op: solve("f", edge(alive, deleted, 0, 3), from(edge(alive, deleted, 0, 3), Position{Change: 2, Pos: 1}))},
		{name: "Solve same To, other IntroducedBy", op: solve("f", edge(alive, deleted, 0, 3), introducedBy(edge(alive, deleted, 0, 3), 2))},
	}
	for _, tc := range passes {
		t.Run(tc.name, func(t *testing.T) {
			requirePasses(t, []Op{tc.op})
		})
	}

	// Every one fails in every version: a legacy marker has no edges and a
	// path, and none of these is one.
	refused := []struct {
		name string
		op   Op
	}{
		{name: "Solve 0x91 to 0x11", op: solve("f", edge(deleted, alive, 0, 3))},
		{name: "Unsolve 0x11 to 0x91", op: unsolve("f", edge(alive, deleted, 0, 3))},
		{name: "Solve with an undefined bit", op: solve("f", edge(alive|0x02, deleted, 0, 3))},
		{name: "Unsolve with an undefined bit", op: unsolve("f", edge(deleted, alive|0x40, 0, 3))},
		{name: "Solve 0x01 to 0x81", op: solve("f", edge(Block, Block|Deleted, 0, 3))},
		{name: "Solve with a bad second edge", op: solve("f", edge(alive, deleted, 0, 3), edge(alive, alive, 3, 5))},
		{name: "Solve empty path", op: solve("", edge(alive, deleted, 0, 3))},
		{name: "Unsolve empty path", op: unsolve("", edge(deleted, alive, 0, 3))},
		{name: "Solve empty path and edges", op: solve("")},
		{name: "Solve to.start == to.end", op: solve("f", edge(alive, deleted, 3, 3))},
		{name: "Unsolve to.start == to.end", op: unsolve("f", edge(deleted, alive, 3, 3))},
		{name: "Solve to.start > to.end", op: solve("f", edge(alive, deleted, 4, 3))},
		{name: "Solve repeated edge", op: solve("f", edge(alive, deleted, 0, 3), edge(alive, deleted, 0, 3))},
		{name: "Unsolve repeated edge", op: unsolve("f", edge(deleted, alive, 0, 3), edge(deleted, alive, 3, 5), edge(deleted, alive, 0, 3))},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			requireRule(t, []Op{tc.op}, versions()...)
		})
	}

	t.Run("empty edges", func(t *testing.T) {
		// Refused in versions 2 and 3 and on encode; version 1 takes it as
		// a legacy marker.
		requireRule(t, []Op{solve("f")}, changefile.Version2, changefile.Version3)
		requireRule(t, []Op{unsolve("f")}, changefile.Version2, changefile.Version3)
	})
}

func TestNameConflict_LegacyMarkers(t *testing.T) {
	for _, op := range []Op{
		SolveNameConflict{Name: EdgeUpdate{Inode: Position{Change: 1, Pos: 2}}, Path: "f"},
		UnsolveNameConflict{Name: EdgeUpdate{Inode: Position{Change: 1, Pos: 2}}, Path: "f"},
	} {
		t.Run(op.Variant().String(), func(t *testing.T) {
			b := rawPayload(op)

			p, err := Decode(b, changefile.Version1)
			require.NoError(t, err)
			require.Equal(t, Payload{Ops: []Op{op}}, p)

			for _, v := range []changefile.Version{changefile.Version2, changefile.Version3} {
				_, err := Decode(b, v)
				requireFormatError(t, err, changefile.ErrInvalidGraphOp)
			}

			// A decoded marker is not written back: no writer may produce
			// one.
			out, err := AppendPayload(nil, p)
			requireFormatError(t, err, changefile.ErrInvalidGraphOp)
			require.Nil(t, out)
		})
	}

	t.Run("empty path in version 1", func(t *testing.T) {
		b := rawPayload(SolveNameConflict{})
		_, err := Decode(b, changefile.Version1)
		requireFormatError(t, err, changefile.ErrInvalidGraphOp)
	})

	t.Run("edges still checked in version 1", func(t *testing.T) {
		op := SolveNameConflict{
			Name: EdgeUpdate{Edges: []NewEdge{{Previous: Folder | Block | Deleted, Flag: Folder | Block, To: Node{Change: 1, Start: 0, End: 3}, IntroducedBy: 1}}},
			Path: "f",
		}
		_, err := Decode(rawPayload(op), changefile.Version1)
		requireFormatError(t, err, changefile.ErrInvalidGraphOp)
	})
}

func TestDecode_RuleErrorNamesOp(t *testing.T) {
	ops := []Op{
		DirAdd{Path: "d"},
		SetAttr{Path: "f", Value: Mode(0o1000)},
		SetAttr{Value: Mode(0)},
	}
	_, err := Decode(rawPayload(ops...), changefile.Version2)
	requireFormatError(t, err, changefile.ErrInvalidGraphOp)
	var fe *changefile.FormatError
	require.ErrorAs(t, err, &fe)
	require.Contains(t, fe.Detail, "op 1:")

	_, err = AppendPayload(nil, Payload{Ops: ops})
	require.ErrorAs(t, err, &fe)
	require.Contains(t, fe.Detail, "op 1:")
}

func TestDecode_PayloadErrorBeatsRuleError(t *testing.T) {
	// The whole payload decodes before any op is checked, as in Atomic, so
	// an op that breaks a rule does not hide a later one that does not
	// decode.
	b := []byte{
		0x00, 0x02,
		// op 0: SetAttr, inode {0, 0}, path "", Mode(0): breaks a rule.
		0x10, 0x00, 0x00, 0x00, 0x00, 0x00,
		// op 1: variant 17, which does not decode.
		0x11,
	}
	_, err := Decode(b, changefile.Version2)
	requireFormatError(t, err, changefile.ErrPayload)
	require.ErrorIs(t, err, postcard.ErrUnknownVariant)
}

func requireFormatError(t *testing.T, err error, kind error) {
	t.Helper()
	require.Error(t, err)
	var fe *changefile.FormatError
	require.True(t, errors.As(err, &fe), "error %v is not a *changefile.FormatError", err)
	require.ErrorIs(t, err, kind)
	require.Equal(t, changefile.SectionGraph, fe.Section)
	require.Equal(t, int64(-1), fe.Offset)
}

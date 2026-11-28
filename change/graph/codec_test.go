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

var versions = []changefile.Version{changefile.Version1, changefile.Version2, changefile.Version3}

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

	for _, v := range versions {
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
	// An Edit inserting with flag 0x03: BLOCK plus the undefined bit 0x02.
	b := []byte{
		0x00, 0x01,
		0x07, 0x00, 0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00,
		0x00, 0x00,
	}
	p, err := Decode(b, changefile.Version2)
	require.NoError(t, err)
	require.Len(t, p.Ops, 1)
	edit, ok := p.Ops[0].(Edit)
	require.True(t, ok)
	ins, ok := edit.Change.(Insertion)
	require.True(t, ok)
	require.Equal(t, EdgeFlags(0x03), ins.Flag)
	require.Equal(t, Block, ins.Flag.Known())

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

func TestDecode_RefusesVariantsNotYetDecoded(t *testing.T) {
	for v := OpSolveNameConflict; v <= OpSetAttr+1; v++ {
		t.Run(v.String(), func(t *testing.T) {
			b := []byte{0x00, 0x01, byte(v), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
			_, err := Decode(b, changefile.Version2)
			requireFormatError(t, err, changefile.ErrPayload)
			require.ErrorIs(t, err, postcard.ErrUnknownVariant)

			var pe *postcard.Error
			require.ErrorAs(t, err, &pe)
			require.Equal(t, int64(2), pe.Offset)
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
		{name: "bad option tag", b: cat(example[:2], []byte{0x00}, example[3:25], []byte{0x02}), cause: postcard.ErrBadOption},
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

func requireFormatError(t *testing.T, err error, kind error) {
	t.Helper()
	require.Error(t, err)
	var fe *changefile.FormatError
	require.True(t, errors.As(err, &fe), "error %v is not a *changefile.FormatError", err)
	require.ErrorIs(t, err, kind)
	require.Equal(t, changefile.SectionGraph, fe.Section)
	require.Equal(t, int64(-1), fe.Offset)
}

package semantic

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/change/graph"
	"github.com/z5labs/atomic-go/change/span"
	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
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

func filled(b byte) ident.Hash {
	var h ident.Hash
	for i := range h {
		h[i] = b
	}
	return h
}

// examplePayload is the 83-byte SEMANTIC payload of
// docs/remote/change-format/graph.md#example-semantic-payload: one FileOps
// for a.txt holding a TrunkCreate and three BranchInserts.
func examplePayload() []byte {
	return cat(
		[]byte{0x01},       // Vec<FileOps>: 1 entry
		[]byte{0x00, 0x00}, // trunk_id {0, 0}
		[]byte{0x05, 0x61, 0x2e, 0x74, 0x78, 0x74}, // path "a.txt"
		[]byte{0x01, 0x00},                         // trunk_op: Some(Create)
		[]byte{0x05, 0x61, 0x2e, 0x74, 0x78, 0x74}, //   path "a.txt"
		[]byte{0x01, 0x00},                         //   encoding: Some(Utf8)
		[]byte{0x03},                               // line_ops: 3
		// line {0, 0}
		[]byte{0x00, 0x00},
		[]byte{0x00, 0x00},
		[]byte{0x01},
		[]byte{0x00, 0x00, 0x00},
		[]byte{0x05, 0x61, 0x6c, 0x70, 0x68, 0x61},
		[]byte{0x00},
		[]byte{0x01, 0x01},
		[]byte{0x01, 0x05, 0x0b},
		// line {0, 1}
		[]byte{0x00, 0x01},
		[]byte{0x00, 0x01, 0x00, 0x00},
		[]byte{0x01, 0x00, 0x00, 0x00, 0x04, 0x62, 0x65, 0x74, 0x61},
		[]byte{0x00, 0x01, 0x02},
		[]byte{0x01, 0x0b, 0x10},
		// line {0, 2}
		[]byte{0x00, 0x02, 0x00, 0x01, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x05, 0x67, 0x61, 0x6d, 0x6d, 0x61, 0x00, 0x01, 0x03, 0x01, 0x10, 0x16},
	)
}

func exampleLine(idx uint32, after *BranchID, word string, line, start, end uint64) LineOps {
	return LineOps{
		Branch: BranchID{Change: ThisChange, BranchIdx: idx},
		Op: BranchInsert{
			After:   after,
			Content: []LeafOp{LeafInsert{Kind: Word, Content: []byte(word)}},
		},
		NewLine:      ptr(line),
		ContentRange: &Range{Start: start, End: end},
	}
}

func exampleValue() []FileOps {
	return []FileOps{{
		Trunk:   TrunkID{Change: ThisChange, FileIdx: 0},
		Path:    "a.txt",
		TrunkOp: TrunkCreate{Path: "a.txt", Encoding: ptr(graph.Utf8)},
		LineOps: []LineOps{
			exampleLine(0, nil, "alpha", 1, 5, 11),
			exampleLine(1, &BranchID{Change: ThisChange, BranchIdx: 0}, "beta", 2, 11, 16),
			exampleLine(2, &BranchID{Change: ThisChange, BranchIdx: 1}, "gamma", 3, 16, 22),
		},
	}}
}

func TestDecode_Example(t *testing.T) {
	b := examplePayload()
	require.Len(t, b, 83)

	ops, err := Decode(b)
	require.NoError(t, err)
	require.Equal(t, exampleValue(), ops)

	out, err := AppendPayload(nil, ops)
	require.NoError(t, err)
	require.Equal(t, b, out)
}

func TestDecode_IgnoresTrailingBytes(t *testing.T) {
	ops, err := Decode(cat(examplePayload(), []byte{0xde, 0xad, 0xbe, 0xef}))
	require.NoError(t, err)
	require.Equal(t, exampleValue(), ops)
}

func TestDecode_EmptyList(t *testing.T) {
	ops, err := Decode([]byte{0x00})
	require.NoError(t, err)
	require.Nil(t, ops)

	out, err := AppendPayload(nil, nil)
	require.NoError(t, err)
	require.Equal(t, []byte{0x00}, out)
}

// A SemanticSectionPayload, which Atomic defines but never writes, opens
// with its path. Read as a Vec<FileOps>, as Atomic reads every SEMANTIC
// payload, an empty path is a count of 0 and the rest is trailing bytes
// (docs/remote/change-format/graph.md#semanticsectionpayload).
func TestDecode_SemanticSectionPayloadWithEmptyPath(t *testing.T) {
	fileOps := examplePayload()[1:] // the example's one FileOps
	b := cat(
		[]byte{0x00}, // path: ""
		fileOps,      // file_ops
		[]byte{0x05}, // content_start
		[]byte{0x16}, // content_end
	)
	ops, err := Decode(b)
	require.NoError(t, err)
	require.Empty(t, ops)
}

func TestDecode_DoesNotAlias(t *testing.T) {
	b := examplePayload()
	ops, err := Decode(b)
	require.NoError(t, err)
	for i := range b {
		b[i] = 0xff
	}
	require.Equal(t, exampleValue(), ops)
}

// fileWith returns a one-entry list whose FileOps has trunk op op and the
// given line ops, every ID non-zero so that a field written in the wrong
// place shows.
func fileWith(op TrunkOp, lines ...LineOps) []FileOps {
	return []FileOps{{
		Trunk:   TrunkID{Change: 7, FileIdx: 3},
		Path:    "dir/f.go",
		TrunkOp: op,
		LineOps: lines,
	}}
}

func TestRoundTrip_TrunkOps(t *testing.T) {
	trunk := TrunkID{Change: 300, FileIdx: 2}
	tests := []struct {
		name    string
		op      TrunkOp
		variant byte
	}{
		{name: "None", op: nil},
		{name: "Create", op: TrunkCreate{Path: "dir/f.go", Encoding: ptr(graph.Latin1)}, variant: 0},
		{name: "Create binary", op: TrunkCreate{Path: "img.png"}, variant: 0},
		{name: "Delete", op: TrunkDelete{Trunk: trunk}, variant: 1},
		{name: "Move", op: TrunkMove{Trunk: trunk, NewPath: "new/f.go"}, variant: 2},
		{name: "Undelete", op: TrunkUndelete{Trunk: trunk}, variant: 3},
		{name: "SetMode", op: TrunkSetMode{Trunk: trunk, Mode: 0o755}, variant: 4},
		{name: "SetKind", op: TrunkSetKind{Trunk: trunk, Kind: graph.Gitlink}, variant: 5},
		{name: "ProjectedSpanEdit", op: ProjectedSpanEdit{
			File:     span.ExternalFile{Owner: filled(0xaa), Ordinal: 9},
			Baseline: span.ProjectionID(filled(0xbb)),
		}, variant: 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := fileWith(tc.op)
			b, err := AppendPayload(nil, in)
			require.NoError(t, err)

			// 01 entry, trunk {7, 3}, path "dir/f.go", then the option tag.
			prefix := cat([]byte{0x01, 0x07, 0x03, 0x08}, []byte("dir/f.go"))
			require.Equal(t, prefix, b[:len(prefix)])
			if tc.op == nil {
				require.Equal(t, byte(0x00), b[len(prefix)])
			} else {
				require.Equal(t, []byte{0x01, tc.variant}, b[len(prefix):len(prefix)+2])
			}

			out, err := Decode(b)
			require.NoError(t, err)
			require.Equal(t, in, out)
		})
	}
}

func TestRoundTrip_TrunkOpBytes(t *testing.T) {
	b, err := AppendPayload(nil, fileWith(TrunkSetMode{Trunk: TrunkID{Change: 300, FileIdx: 2}, Mode: 0o755}))
	require.NoError(t, err)
	// ... 01 04, trunk {300 = ac 02, 2}, mode 0o755 = 493 = ed 03, no line ops.
	require.Equal(t, []byte{0x01, 0x04, 0xac, 0x02, 0x02, 0xed, 0x03, 0x00}, b[len(b)-8:])

	b, err = AppendPayload(nil, fileWith(ProjectedSpanEdit{
		File:     span.ExternalFile{Owner: filled(0xaa), Ordinal: 9},
		Baseline: span.ProjectionID(filled(0xbb)),
	}))
	require.NoError(t, err)
	owner, baseline := filled(0xaa), filled(0xbb)
	want := cat([]byte{0x01, 0x06}, owner[:], []byte{0x09}, baseline[:], []byte{0x00})
	require.Equal(t, want, b[len(b)-len(want):])
}

func TestRoundTrip_BranchOps(t *testing.T) {
	branch := BranchID{Change: 5, BranchIdx: 6}
	after := &BranchID{Change: 0, BranchIdx: 4}
	tokens := []LeafOp{LeafInsert{Kind: Word, Content: []byte("x")}, LeafDelete{Leaf: LeafID{Change: 2, LeafIdx: 1}}}
	tests := []struct {
		name    string
		op      BranchOp
		variant byte
	}{
		{name: "Insert at start", op: BranchInsert{Content: tokens}, variant: 0},
		{name: "Insert after", op: BranchInsert{After: after}, variant: 0},
		{name: "Delete", op: BranchDelete{Branch: branch, Content: tokens}, variant: 1},
		{name: "Delete without content", op: BranchDelete{Branch: branch}, variant: 1},
		{name: "Modify", op: BranchModify{Branch: branch, OldContent: tokens, NewContent: tokens[:1]}, variant: 2},
		{name: "Restore", op: BranchRestore{Branch: branch}, variant: 3},
		{name: "Reparent to start", op: BranchReparent{Branch: branch}, variant: 4},
		{name: "Reparent after", op: BranchReparent{Branch: branch, NewAfter: after}, variant: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := LineOps{
				Branch:       BranchID{Change: 1, BranchIdx: 2},
				Op:           tc.op,
				OldLine:      ptr(uint64(10)),
				NewLine:      ptr(uint64(200)),
				ContentRange: &Range{Start: 3, End: 4},
			}
			in := fileWith(nil, line)
			b, err := AppendPayload(nil, in)
			require.NoError(t, err)

			// ... no trunk op, 1 line op, branch {1, 2}, then the variant.
			prefix := cat([]byte{0x01, 0x07, 0x03, 0x08}, []byte("dir/f.go"), []byte{0x00, 0x01, 0x01, 0x02})
			require.Equal(t, prefix, b[:len(prefix)])
			require.Equal(t, tc.variant, b[len(prefix)])
			// old 10, new 200 (c8 01), range (3, 4).
			require.Equal(t, []byte{0x01, 0x0a, 0x01, 0xc8, 0x01, 0x01, 0x03, 0x04}, b[len(b)-8:])

			out, err := Decode(b)
			require.NoError(t, err)
			require.Equal(t, in, out)
		})
	}
}

func TestRoundTrip_LineOpsOptions(t *testing.T) {
	in := fileWith(nil, LineOps{Branch: BranchID{Change: 1, BranchIdx: 2}, Op: BranchRestore{Branch: BranchID{Change: 3, BranchIdx: 4}}})
	b, err := AppendPayload(nil, in)
	require.NoError(t, err)
	require.Equal(t, []byte{0x03, 0x03, 0x04, 0x00, 0x00, 0x00}, b[len(b)-6:])

	out, err := Decode(b)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestRoundTrip_LeafOps(t *testing.T) {
	leaf := LeafID{Change: 8, LeafIdx: 9}
	tests := []struct {
		name string
		op   LeafOp
		want []byte
	}{
		{name: "Insert at start", op: LeafInsert{Kind: Operator, Content: []byte("==")}, want: []byte{0x00, 0x00, 0x02, 0x02, '=', '='}},
		{name: "Insert after", op: LeafInsert{After: &leaf, Kind: Newline, Content: []byte("\n")}, want: []byte{0x00, 0x01, 0x08, 0x09, 0x07, 0x01, '\n'}},
		{name: "Insert empty", op: LeafInsert{Kind: Other}, want: []byte{0x00, 0x00, 0x08, 0x00}},
		{name: "Delete", op: LeafDelete{Leaf: leaf}, want: []byte{0x01, 0x08, 0x09}},
		{name: "Replace", op: LeafReplace{Leaf: leaf, NewContent: []byte("y")}, want: []byte{0x02, 0x08, 0x09, 0x01, 'y'}},
		{name: "Replace with empty", op: LeafReplace{Leaf: leaf}, want: []byte{0x02, 0x08, 0x09, 0x00}},
		{name: "Restore", op: LeafRestore{Leaf: leaf}, want: []byte{0x03, 0x08, 0x09}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := fileWith(nil, LineOps{Op: BranchInsert{Content: []LeafOp{tc.op}}})
			b, err := AppendPayload(nil, in)
			require.NoError(t, err)

			// ... branch {0, 0}, Insert, after None, 1 leaf op, the op, then
			// three Nones.
			want := cat([]byte{0x00, 0x00, 0x00, 0x00, 0x01}, tc.want, []byte{0x00, 0x00, 0x00})
			require.Equal(t, want, b[len(b)-len(want):])

			out, err := Decode(b)
			require.NoError(t, err)
			require.Equal(t, in, out)
		})
	}
}

func TestRoundTrip_TokenKinds(t *testing.T) {
	for k := Word; k <= Other; k++ {
		t.Run(k.String(), func(t *testing.T) {
			in := fileWith(nil, LineOps{Op: BranchInsert{Content: []LeafOp{LeafInsert{Kind: k, Content: []byte("t")}}}})
			b, err := AppendPayload(nil, in)
			require.NoError(t, err)
			require.Equal(t, []byte{0x00, 0x00, byte(k), 0x01, 't', 0x00, 0x00, 0x00}, b[len(b)-8:])

			out, err := Decode(b)
			require.NoError(t, err)
			require.Equal(t, in, out)
		})
	}
}

func TestRoundTrip_SeveralFiles(t *testing.T) {
	in := append(exampleValue(), FileOps{
		Trunk:   TrunkID{Change: 12, FileIdx: 1},
		Path:    "b.txt",
		TrunkOp: TrunkMove{Trunk: TrunkID{Change: 12, FileIdx: 1}, NewPath: "c.txt"},
	}, FileOps{
		Path:    "spans.txt",
		TrunkOp: ProjectedSpanEdit{File: span.ExternalFile{Owner: filled(1)}, Baseline: span.ProjectionID(filled(2))},
	})
	b, err := AppendPayload([]byte("prefix"), in)
	require.NoError(t, err)
	require.Equal(t, []byte("prefix"), b[:6])

	out, err := Decode(b[6:])
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestTokenKindString(t *testing.T) {
	require.Equal(t,
		[]string{"Word", "Whitespace", "Operator", "Punctuation", "String", "Number", "Comment", "Newline", "Other", "TokenKind(9)"},
		[]string{Word.String(), Whitespace.String(), Operator.String(), Punctuation.String(), String.String(), Number.String(), Comment.String(), Newline.String(), Other.String(), TokenKind(9).String()})
}

func TestNodeID(t *testing.T) {
	require.Equal(t, NodeID(0), ThisChange)
	require.True(t, ThisChange.IsThisChange())
	require.False(t, NodeID(1).IsThisChange())
}

func requireFormatError(t *testing.T, err error) *changefile.FormatError {
	t.Helper()
	var fe *changefile.FormatError
	require.ErrorAs(t, err, &fe)
	require.ErrorIs(t, err, changefile.ErrPayload)
	require.Equal(t, changefile.SectionSemantic, fe.Section)
	require.Equal(t, int64(-1), fe.Offset)
	return fe
}

func TestDecode_Refuses(t *testing.T) {
	fileHead := cat([]byte{0x01, 0x00, 0x00, 0x01}, []byte("f"))
	lineHead := cat(fileHead, []byte{0x00, 0x01, 0x00, 0x00})
	// offset is where the failed value begins, or -1 when the test does
	// not pin it.
	tests := []struct {
		name   string
		b      []byte
		cause  error
		offset int64
	}{
		{name: "empty", b: nil, cause: postcard.ErrUnexpectedEnd, offset: 0},
		{name: "truncated", b: examplePayload()[:40], cause: postcard.ErrUnexpectedEnd, offset: -1},
		{name: "count beyond input", b: []byte{0x05, 0x00}, cause: postcard.ErrUnexpectedEnd, offset: 0},
		{name: "path not UTF-8", b: []byte{0x01, 0x00, 0x00, 0x01, 0xff, 0x00, 0x00}, cause: postcard.ErrBadUTF8, offset: 3},
		{name: "bad trunk op tag", b: cat(fileHead, []byte{0x02}), cause: postcard.ErrBadOption, offset: 5},
		{name: "unknown trunk op", b: cat(fileHead, []byte{0x01, 0x07}), cause: postcard.ErrUnknownVariant, offset: 6},
		{name: "unknown encoding", b: cat(fileHead, []byte{0x01, 0x00, 0x00, 0x01, 0x05, 0x00}), cause: postcard.ErrUnknownVariant, offset: 9},
		{name: "unknown inode kind", b: cat(fileHead, []byte{0x01, 0x05, 0x00, 0x00, 0x03, 0x00}), cause: postcard.ErrUnknownVariant, offset: 9},
		{name: "unknown branch op", b: cat(fileHead, []byte{0x00, 0x01, 0x00, 0x00, 0x05}), cause: postcard.ErrUnknownVariant, offset: 9},
		{name: "unknown leaf op", b: cat(lineHead, []byte{0x00, 0x00, 0x01, 0x04}), cause: postcard.ErrUnknownVariant, offset: 12},
		{name: "unknown token kind", b: cat(lineHead, []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x09}), cause: postcard.ErrUnknownVariant, offset: 14},
		{name: "bad line option tag", b: cat(lineHead, []byte{0x03, 0x00, 0x00, 0x02}), cause: postcard.ErrBadOption, offset: 12},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ops, err := Decode(tc.b)
			require.Nil(t, ops)
			requireFormatError(t, err)
			require.ErrorIs(t, err, tc.cause)
			var pe *postcard.Error
			require.True(t, errors.As(err, &pe))
			if tc.offset >= 0 {
				require.Equal(t, tc.offset, pe.Offset)
			}
		})
	}
}

type otherTrunkOp struct{}

func (otherTrunkOp) trunkOp() {}

type otherBranchOp struct{}

func (otherBranchOp) branchOp() {}

type otherLeafOp struct{}

func (otherLeafOp) leafOp() {}

func TestAppendPayload_Refuses(t *testing.T) {
	insert := func(ops ...LeafOp) LineOps { return LineOps{Op: BranchInsert{Content: ops}} }
	tests := []struct {
		name   string
		ops    []FileOps
		detail string
	}{
		{name: "path not UTF-8", ops: []FileOps{{Path: "\xff"}}, detail: "file ops 0: path"},
		{name: "create path not UTF-8", ops: fileWith(TrunkCreate{Path: "\xff"}), detail: "file ops 0: trunk op: path"},
		{name: "move path not UTF-8", ops: fileWith(TrunkMove{NewPath: "\xff"}), detail: "file ops 0: trunk op: new path"},
		{name: "unknown encoding", ops: fileWith(TrunkCreate{Path: "f", Encoding: ptr(graph.Encoding(5))}), detail: "encoding Encoding(5)"},
		{name: "unknown inode kind", ops: fileWith(TrunkSetKind{Kind: graph.InodeKind(3)}), detail: "inode kind InodeKind(3)"},
		{name: "foreign trunk op", ops: fileWith(otherTrunkOp{}), detail: "semantic.otherTrunkOp"},
		{name: "pointer trunk op", ops: fileWith(&TrunkDelete{}), detail: "*semantic.TrunkDelete"},
		{name: "nil branch op", ops: fileWith(nil, LineOps{}), detail: "file ops 0: line op 0: branch op is nil"},
		{name: "foreign branch op", ops: fileWith(nil, insert(), LineOps{Op: otherBranchOp{}}), detail: "line op 1: branch op is a semantic.otherBranchOp"},
		{name: "nil leaf op", ops: fileWith(nil, insert(LeafRestore{}, nil)), detail: "line op 0: content leaf op 1: leaf op is nil"},
		{name: "foreign leaf op", ops: fileWith(nil, LineOps{Op: BranchDelete{Content: []LeafOp{otherLeafOp{}}}}), detail: "semantic.otherLeafOp"},
		{name: "modify old content", ops: fileWith(nil, LineOps{Op: BranchModify{OldContent: []LeafOp{nil}}}), detail: "old content leaf op 0"},
		{name: "modify new content", ops: fileWith(nil, LineOps{Op: BranchModify{NewContent: []LeafOp{nil}}}), detail: "new content leaf op 0"},
		{name: "unknown token kind", ops: fileWith(nil, insert(LeafInsert{Kind: TokenKind(9)})), detail: "token kind TokenKind(9)"},
		{name: "second file", ops: append(exampleValue(), FileOps{Path: "\xff"}), detail: "file ops 1:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := []byte("keep")
			out, err := AppendPayload(dst, tc.ops)
			require.Equal(t, []byte("keep"), out)
			fe := requireFormatError(t, err)
			require.Contains(t, fe.Detail, tc.detail)
		})
	}
}

func ExampleDecode() {
	b := []byte{
		0x01, 0x00, 0x00, 0x01, 'f', // one FileOps: trunk {0, 0}, path "f"
		0x00,                         // trunk op: None
		0x01,                         // one line op
		0x00, 0x00, 0x03, 0x00, 0x00, // branch {0, 0}, Restore {0, 0}
		0x00, 0x00, 0x00, // no line numbers, no content range
	}
	ops, err := Decode(b)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s: %T\n", ops[0].Path, ops[0].LineOps[0].Op)
	// Output: f: semantic.BranchRestore
}

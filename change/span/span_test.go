package span

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

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

func filled(b byte) ident.Hash {
	var h ident.Hash
	for i := range h {
		h[i] = b
	}
	return h
}

// ownerExample is the version 2 payload from
// docs/remote/change-format/sections.md#span-section: no prior metadata,
// no operations, and one semantic owner, authored_id 300 and an owner of 32
// 0xA5 bytes.
func ownerExample() []byte {
	return cat(
		[]byte{0x41, 0x54, 0x53, 0x50, 0x02, 0x00, 0x00, 0x01, 0xac, 0x02},
		bytes.Repeat([]byte{0xa5}, 32),
	)
}

// legacyExample is the version 1 payload of Atomic's
// legacy_span_payload_roundtrips_without_changing_its_bytes test: prior
// metadata "legacy" and one Root for "file", ordinal 0, range 0 to 4.
func legacyExample() []byte {
	return []byte{
		0x41, 0x54, 0x53, 0x50, 0x01, 0x06, 0x6c, 0x65, 0x67, 0x61, 0x63, 0x79,
		0x01, 0x00, 0x04, 0x66, 0x69, 0x6c, 0x65, 0x00, 0x00, 0x04,
	}
}

func TestOwnerExample(t *testing.T) {
	want := Patch{
		Version:        V2,
		SemanticOwners: []SemanticOwner{{AuthoredID: 300, Owner: filled(0xa5)}},
	}

	got, err := Decode(ownerExample())
	require.NoError(t, err)
	require.Equal(t, want, got)

	b, err := AppendPayload(nil, want)
	require.NoError(t, err)
	require.Equal(t, ownerExample(), b)

	require.NoError(t, got.ValidateContentRanges(0))
}

func TestLegacyV1RoundTrip(t *testing.T) {
	want := Patch{
		Version:       V1,
		PriorMetadata: []byte("legacy"),
		Ops:           []Op{Root{Path: "file", Ordinal: 0, Start: 0, End: 4}},
	}

	got, err := Decode(legacyExample())
	require.NoError(t, err)
	require.Equal(t, want, got)

	b, err := AppendPayload(nil, got)
	require.NoError(t, err)
	require.Equal(t, legacyExample(), b)

	require.NoError(t, got.ValidateContentRanges(4))
}

func TestEditRoundTrip(t *testing.T) {
	p := Patch{
		Version:       LegacySource,
		PriorMetadata: []byte{0xff},
		Ops: []Op{
			Root{Path: "a/b.txt", Ordinal: 300, Start: 1, End: 1 << 20},
			Edit{
				File:        ExternalFile{Owner: filled(0x11), Ordinal: 7},
				Target:      ExternalSpan{Owner: filled(0x22), Start: 128, End: 200},
				Start:       2,
				End:         72,
				InsertStart: 5,
				InsertEnd:   1 << 20,
			},
		},
		SemanticOwners: []SemanticOwner{
			{AuthoredID: 1, Owner: filled(0x33)},
			{AuthoredID: 1 << 40, Owner: filled(0x44)},
		},
	}

	want := cat(
		[]byte("ATSP"), []byte{0x03}, // version 3
		[]byte{0x01, 0xff}, // prior metadata
		[]byte{0x02},       // two ops
		[]byte{0x00, 0x07}, []byte("a/b.txt"), []byte{0xac, 0x02}, []byte{0x01}, []byte{0x80, 0x80, 0x40},
		[]byte{0x01}, bytes.Repeat([]byte{0x11}, 32), []byte{0x07},
		bytes.Repeat([]byte{0x22}, 32), []byte{0x80, 0x01}, []byte{0xc8, 0x01},
		[]byte{0x02}, []byte{0x48}, []byte{0x05}, []byte{0x80, 0x80, 0x40},
		[]byte{0x02}, // two owners
		[]byte{0x01}, bytes.Repeat([]byte{0x33}, 32),
		[]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x20}, bytes.Repeat([]byte{0x44}, 32),
	)

	b, err := AppendPayload(nil, p)
	require.NoError(t, err)
	require.Equal(t, want, b)

	got, err := Decode(b)
	require.NoError(t, err)
	require.Equal(t, p, got)
	require.NoError(t, got.ValidateContentRanges(1<<20))
}

func TestAppendPayloadAppends(t *testing.T) {
	b, err := AppendPayload([]byte("prefix"), Patch{Version: V2})
	require.NoError(t, err)
	require.Equal(t, []byte("prefixATSP\x02\x00\x00\x00"), b)
}

func TestDecodeDoesNotAlias(t *testing.T) {
	in := legacyExample()
	got, err := Decode(in)
	require.NoError(t, err)
	in[6] = 'X'
	require.Equal(t, []byte("legacy"), got.PriorMetadata)
}

func TestDecodeRefuses(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    []error
		msg     string
	}{
		{
			name:    "an overlong prior metadata length",
			payload: []byte{0x41, 0x54, 0x53, 0x50, 0x02, 0x80, 0x00, 0x00, 0x00},
			want:    []error{ErrNotCanonical},
			msg:     "re-encodes to different bytes",
		},
		{
			name:    "an overlong authored_id",
			payload: cat([]byte{0x41, 0x54, 0x53, 0x50, 0x02, 0x00, 0x00, 0x01, 0xac, 0x82, 0x00}, bytes.Repeat([]byte{0xa5}, 32)),
			want:    []error{ErrNotCanonical},
			msg:     "re-encodes to different bytes",
		},
		{
			name:    "a trailing byte after a V2 patch",
			payload: append(ownerExample(), 0x00),
			want:    []error{ErrNotCanonical, postcard.ErrTrailingBytes},
		},
		{
			name:    "a V1 patch followed by an owner list",
			payload: append(legacyExample(), 0x00),
			want:    []error{ErrNotCanonical, postcard.ErrTrailingBytes},
		},
		{
			name:    "a truncated patch",
			payload: ownerExample()[:20],
			want:    []error{ErrNotCanonical, postcard.ErrUnexpectedEnd},
		},
		{
			name:    "no version byte",
			payload: []byte("ATSP"),
			want:    []error{ErrNotCanonical, postcard.ErrUnexpectedEnd},
		},
		{
			name:    "an unknown op variant",
			payload: []byte{0x41, 0x54, 0x53, 0x50, 0x02, 0x00, 0x01, 0x02},
			want:    []error{ErrNotCanonical, postcard.ErrUnknownVariant},
		},
		{
			name:    "a path that is not UTF-8",
			payload: []byte{0x41, 0x54, 0x53, 0x50, 0x02, 0x00, 0x01, 0x00, 0x01, 0xff, 0x00, 0x00, 0x00, 0x00},
			want:    []error{ErrNotCanonical, postcard.ErrBadUTF8},
		},
		{
			name:    "bad magic",
			payload: append([]byte("ATSQ"), ownerExample()[4:]...),
			want:    []error{ErrMagic},
		},
		{
			name:    "a short magic",
			payload: []byte("ATS"),
			want:    []error{ErrMagic},
		},
		{
			name:    "an empty payload",
			payload: nil,
			want:    []error{ErrMagic},
		},
		{
			name:    "version 4",
			payload: []byte{0x41, 0x54, 0x53, 0x50, 0x04, 0x00, 0x00, 0x00},
			want:    []error{ErrVersion},
		},
		{
			name:    "version 0",
			payload: []byte{0x41, 0x54, 0x53, 0x50, 0x00, 0x00, 0x00, 0x00},
			want:    []error{ErrVersion},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Decode(tc.payload)
			for _, want := range tc.want {
				require.ErrorIs(t, err, want)
			}
			if tc.msg != "" {
				require.ErrorContains(t, err, tc.msg)
			}
			require.Zero(t, got)
		})
	}
}

func TestDecodeReportsPayloadOffset(t *testing.T) {
	_, err := Decode(append(ownerExample(), 0x00))
	var pe *postcard.Error
	require.ErrorAs(t, err, &pe)
	require.Equal(t, int64(len(ownerExample())), pe.Offset)
}

type otherOp struct{}

func (otherOp) spanOp() {}

func TestAppendPayloadRefuses(t *testing.T) {
	owners := []SemanticOwner{{AuthoredID: 1, Owner: filled(1)}}
	tests := []struct {
		name  string
		patch Patch
		want  error
	}{
		{"V1 with owners", Patch{Version: V1, SemanticOwners: owners}, ErrInvalidOwners},
		{"version 0", Patch{}, ErrVersion},
		{"version 4", Patch{Version: 4}, ErrVersion},
		{"a nil op", Patch{Version: V2, Ops: []Op{nil}}, ErrInvalidOp},
		{"a *Root op", Patch{Version: V2, Ops: []Op{&Root{}}}, ErrInvalidOp},
		{"a foreign op", Patch{Version: V2, Ops: []Op{otherOp{}}}, ErrInvalidOp},
		{"a path that is not UTF-8", Patch{Version: V2, Ops: []Op{Root{Path: "\xff"}}}, ErrInvalidOp},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := []byte("keep")
			got, err := AppendPayload(dst, tc.patch)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, []byte("keep"), got)
		})
	}
}

func TestAppendPayloadV1WithoutOwners(t *testing.T) {
	b, err := AppendPayload(nil, Patch{Version: V1, SemanticOwners: []SemanticOwner{}})
	require.NoError(t, err)
	require.Equal(t, []byte("ATSP\x01\x00\x00"), b)
}

// validEdit returns an edit that passes against 10 content bytes.
func validEdit() Edit {
	return Edit{
		File:        ExternalFile{Owner: filled(1), Ordinal: 0},
		Target:      ExternalSpan{Owner: filled(2), Start: 10, End: 20},
		Start:       0,
		End:         10,
		InsertStart: 0,
		InsertEnd:   10,
	}
}

func TestValidateContentRanges(t *testing.T) {
	edit := func(f func(*Edit)) Op {
		e := validEdit()
		f(&e)
		return e
	}
	owner := func(id uint64, b byte) SemanticOwner {
		return SemanticOwner{AuthoredID: id, Owner: filled(b)}
	}

	tests := []struct {
		name   string
		ops    []Op
		owners []SemanticOwner
		want   error
		msg    string
	}{
		{name: "valid", ops: []Op{Root{Start: 0, End: 10}, validEdit()}, owners: []SemanticOwner{owner(1, 1), owner(2, 2)}},
		{name: "empty ranges", ops: []Op{Root{Start: 10, End: 10}, edit(func(e *Edit) {
			e.Target = ExternalSpan{Owner: filled(2), Start: 5, End: 5}
			e.Start, e.End, e.InsertStart, e.InsertEnd = 0, 0, 10, 10
		})}},
		{name: "reversed root", ops: []Op{Root{Start: 5, End: 4}}, want: ErrInvalidOp, msg: "span: invalid span operation 0: "},
		{name: "root past the content", ops: []Op{Root{Start: 0, End: 11}}, want: ErrInvalidOp},
		{name: "zero file owner", ops: []Op{Root{End: 1}, edit(func(e *Edit) { e.File.Owner = ident.Hash{} })}, want: ErrInvalidOp, msg: "span: invalid span operation 1: "},
		{name: "zero target owner", ops: []Op{edit(func(e *Edit) { e.Target.Owner = ident.Hash{} })}, want: ErrInvalidOp},
		{name: "reversed target", ops: []Op{edit(func(e *Edit) { e.Target.Start, e.Target.End = 20, 10 })}, want: ErrInvalidOp},
		{name: "reversed edit", ops: []Op{edit(func(e *Edit) { e.Start, e.End = 3, 2 })}, want: ErrInvalidOp},
		{name: "edit past the target", ops: []Op{edit(func(e *Edit) { e.End = 11 })}, want: ErrInvalidOp},
		{name: "reversed insert", ops: []Op{edit(func(e *Edit) { e.InsertStart, e.InsertEnd = 3, 2 })}, want: ErrInvalidOp},
		{name: "insert past the content", ops: []Op{edit(func(e *Edit) { e.InsertEnd = 11 })}, want: ErrInvalidOp},
		{name: "nil op", ops: []Op{nil}, want: ErrInvalidOp},
		{name: "foreign op", ops: []Op{otherOp{}}, want: ErrInvalidOp, msg: "span.otherOp, not a span.Root or span.Edit"},
		{name: "*Root op", ops: []Op{&Root{}}, want: ErrInvalidOp},
		{name: "authored_id 0", owners: []SemanticOwner{owner(0, 1)}, want: ErrInvalidOwners},
		{name: "zero owner hash", owners: []SemanticOwner{{AuthoredID: 1}}, want: ErrInvalidOwners},
		{name: "unsorted authored_ids", owners: []SemanticOwner{owner(2, 1), owner(1, 2)}, want: ErrInvalidOwners},
		{name: "repeated authored_id", owners: []SemanticOwner{owner(1, 1), owner(1, 2)}, want: ErrInvalidOwners},
		{name: "duplicate owner", owners: []SemanticOwner{owner(1, 1), owner(2, 2), owner(3, 1)}, want: ErrInvalidOwners},
		{name: "ops are checked before owners", ops: []Op{Root{Start: 1, End: 0}}, owners: []SemanticOwner{owner(0, 1)}, want: ErrInvalidOp},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Patch{Version: V2, Ops: tc.ops, SemanticOwners: tc.owners}
			err := p.ValidateContentRanges(10)
			if tc.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.want)
			if tc.msg != "" {
				require.Contains(t, err.Error(), tc.msg)
			}
		})
	}
}

func TestProjectionID(t *testing.T) {
	require.True(t, ProjectionID{}.IsZero())
	id := ProjectionID(filled(0x5a))
	require.False(t, id.IsZero())
	require.Equal(t, filled(0x5a).String(), id.String())
}

func TestVersionString(t *testing.T) {
	require.Equal(t, "V1", V1.String())
	require.Equal(t, "V2", V2.String())
	require.Equal(t, "LegacySource", LegacySource.String())
	require.Equal(t, "Version(4)", Version(4).String())
}

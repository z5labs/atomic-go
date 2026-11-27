package syncproto_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/syncproto"
)

func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	require.NoError(t, err)
	return b
}

func ptr(s string) *string { return &s }

// The goldens of docs/remote/endpoints/project.md#syncpack.
const (
	advertiseGolden = "01 03 64 65 76 00 01"
	packGolden      = `
		01
		04 02 62 62
		02 7b 7d
		02
		03 64 65 76 00
		02 62 62
		01 66 01 02 62 62
		02 63 63`
)

func goldenPack() syncproto.Pack {
	return syncproto.Pack{
		Objects: []syncproto.ObjectRecord{
			{Family: syncproto.View, Key: "bb", Bytes: []byte("{}")},
		},
		Refs: []syncproto.RefRecord{
			{Name: "dev", NewTarget: "bb"},
			{Name: "f", ExpectOld: ptr("bb"), NewTarget: "cc"},
		},
	}
}

func TestConstants(t *testing.T) {
	require.Equal(t, "Atomic-Protocol", syncproto.ProtocolHeader)
	require.Equal(t, "sync/1", syncproto.ProtocolVersion)
	require.Equal(t, "application/vnd.atomic.sync.v1", syncproto.ContentType)
	require.EqualValues(t, int64(2)<<30, syncproto.MaxDecodedSize)
}

func TestFamily(t *testing.T) {
	cases := []struct {
		f       syncproto.Family
		index   uint32
		name    string
		segment string
	}{
		{syncproto.Change, 0, "Change", "changes"},
		{syncproto.Tag, 1, "Tag", "tags"},
		{syncproto.Provenance, 2, "Provenance", "provenance"},
		{syncproto.Attest, 3, "Attest", "attest"},
		{syncproto.View, 4, "View", "views"},
		{syncproto.Conflict, 5, "Conflict", "conflicts"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.index, uint32(c.f))
			require.True(t, c.f.Known())
			require.Equal(t, c.name, c.f.String())
			require.Equal(t, c.segment, c.f.Segment())
		})
	}
	t.Run("unknown", func(t *testing.T) {
		f := syncproto.Family(9)
		require.False(t, f.Known())
		require.Equal(t, "Family(9)", f.String())
		require.Empty(t, f.Segment())
		require.False(t, syncproto.Family(6).Known())
	})
}

func TestWantsConstructors(t *testing.T) {
	require.Equal(t, syncproto.Wants{Refs: []string{"dev", "main"}, RefsOnly: true}, syncproto.Advertise("dev", "main"))
	require.Equal(t, syncproto.Wants{RefsOnly: true}, syncproto.Advertise())
	require.Equal(t, syncproto.Wants{Haves: []string{"aa", "bb"}}, syncproto.All([]string{"aa", "bb"}))
	require.Equal(t, syncproto.Wants{}, syncproto.All(nil))

	t.Run("the result does not share the caller's slice", func(t *testing.T) {
		views := []string{"dev"}
		w := syncproto.Advertise(views...)
		views[0] = "main"
		require.Equal(t, []string{"dev"}, w.Refs)

		haves := []string{"aa"}
		w = syncproto.All(haves)
		haves[0] = "bb"
		require.Equal(t, []string{"aa"}, w.Haves)
	})
}

func TestWantsGolden(t *testing.T) {
	golden := mustHex(t, advertiseGolden)

	got, err := syncproto.Advertise("dev").AppendPostcard(nil)
	require.NoError(t, err)
	require.Equal(t, golden, got)

	var w syncproto.Wants
	require.NoError(t, w.UnmarshalPostcard(golden))
	require.Equal(t, syncproto.Advertise("dev"), w)

	t.Run("appends to dst", func(t *testing.T) {
		got, err := syncproto.Advertise("dev").AppendPostcard([]byte{0xaa})
		require.NoError(t, err)
		require.Equal(t, append([]byte{0xaa}, golden...), got)
	})

	t.Run("every truncation fails", func(t *testing.T) {
		for i := range golden {
			var w syncproto.Wants
			err := w.UnmarshalPostcard(golden[:i])
			requireDecodeError(t, err, syncproto.StagePostcard)
		}
	})

	t.Run("trailing bytes are ignored", func(t *testing.T) {
		var w syncproto.Wants
		require.NoError(t, w.UnmarshalPostcard(append(golden, 0xff, 0xff)))
		require.Equal(t, syncproto.Advertise("dev"), w)
	})
}

func TestPackGolden(t *testing.T) {
	golden := mustHex(t, packGolden)

	got, err := goldenPack().AppendPostcard(nil)
	require.NoError(t, err)
	require.Equal(t, golden, got)

	var p syncproto.Pack
	require.NoError(t, p.UnmarshalPostcard(golden))
	require.Equal(t, goldenPack(), p)

	t.Run("decoded bytes do not share the input", func(t *testing.T) {
		in := mustHex(t, packGolden)
		var p syncproto.Pack
		require.NoError(t, p.UnmarshalPostcard(in))
		clear(in)
		require.Equal(t, goldenPack(), p)
	})

	t.Run("every truncation fails", func(t *testing.T) {
		for i := range golden {
			p := goldenPack()
			err := p.UnmarshalPostcard(golden[:i])
			requireDecodeError(t, err, syncproto.StagePostcard)
			require.Equal(t, goldenPack(), p, "a failed decode leaves the value unchanged")
		}
	})

	t.Run("trailing bytes are ignored", func(t *testing.T) {
		var p syncproto.Pack
		require.NoError(t, p.UnmarshalPostcard(append(golden, 0x00, 0xff)))
		require.Equal(t, goldenPack(), p)
	})
}

func TestRoundTrip(t *testing.T) {
	packs := map[string]syncproto.Pack{
		"empty": {},
		"nil ExpectOld": {
			Refs: []syncproto.RefRecord{{Name: "dev", NewTarget: "aa"}},
		},
		"non-nil ExpectOld": {
			Refs: []syncproto.RefRecord{{Name: "dev", ExpectOld: ptr("aa"), NewTarget: "bb"}},
		},
		"empty ExpectOld": {
			Refs: []syncproto.RefRecord{{Name: "dev", ExpectOld: ptr(""), NewTarget: "bb"}},
		},
		"every family": {
			Objects: []syncproto.ObjectRecord{
				{Family: syncproto.Change, Key: "c", Bytes: []byte{0}},
				{Family: syncproto.Tag, Key: "t", Bytes: []byte{1}},
				{Family: syncproto.Provenance, Key: "p", Bytes: []byte{2}},
				{Family: syncproto.Attest, Key: "a", Bytes: []byte{3}},
				{Family: syncproto.View, Key: "v", Bytes: []byte("{}")},
				{Family: syncproto.Conflict, Key: "x", Bytes: []byte{5}},
			},
			Refs: []syncproto.RefRecord{
				{Name: "dev", NewTarget: "v"},
				{Name: "ünïcode", ExpectOld: ptr("v"), NewTarget: "w"},
			},
		},
		"a large object": {
			Objects: []syncproto.ObjectRecord{
				{Family: syncproto.Change, Key: "big", Bytes: make([]byte, 300)},
			},
		},
	}
	for name, want := range packs {
		t.Run(name, func(t *testing.T) {
			b, err := want.AppendPostcard(nil)
			require.NoError(t, err)
			var got syncproto.Pack
			require.NoError(t, got.UnmarshalPostcard(b))
			require.Equal(t, want, got)
		})
	}

	wants := map[string]syncproto.Wants{
		"zero":      {},
		"advertise": syncproto.Advertise("a", "b"),
		"all":       syncproto.All([]string{"x", "y", "z"}),
		"both":      {Refs: []string{"dev"}, Haves: []string{"aa"}, RefsOnly: true},
	}
	for name, want := range wants {
		t.Run("wants "+name, func(t *testing.T) {
			b, err := want.AppendPostcard(nil)
			require.NoError(t, err)
			var got syncproto.Wants
			require.NoError(t, got.UnmarshalPostcard(b))
			require.Equal(t, want, got)
		})
	}
}

func TestUnknownFamily(t *testing.T) {
	// The golden pack with its object's family changed from View (4) to 9.
	in := mustHex(t, packGolden)
	require.Equal(t, byte(0x04), in[1])
	in[1] = 0x09

	var p syncproto.Pack
	require.NoError(t, p.UnmarshalPostcard(in))
	require.Len(t, p.Objects, 1)
	require.Equal(t, syncproto.Family(9), p.Objects[0].Family)
	require.False(t, p.Objects[0].Family.Known())
	require.Equal(t, "bb", p.Objects[0].Key)

	dst := []byte{0xaa}
	got, err := p.AppendPostcard(dst)
	require.ErrorIs(t, err, syncproto.ErrUnknownFamily)
	require.Equal(t, dst, got, "nothing is appended on failure")

	_, err = syncproto.NewCodec().EncodePack(p)
	require.ErrorIs(t, err, syncproto.ErrUnknownFamily)
}

func TestInvalidUTF8(t *testing.T) {
	const bad = "\xff"
	packs := map[string]syncproto.Pack{
		"key":        {Objects: []syncproto.ObjectRecord{{Family: syncproto.Change, Key: bad}}},
		"name":       {Refs: []syncproto.RefRecord{{Name: bad, NewTarget: "a"}}},
		"expect old": {Refs: []syncproto.RefRecord{{Name: "a", ExpectOld: ptr(bad), NewTarget: "a"}}},
		"new target": {Refs: []syncproto.RefRecord{{Name: "a", NewTarget: bad}}},
	}
	for name, p := range packs {
		t.Run(name, func(t *testing.T) {
			got, err := p.AppendPostcard(nil)
			require.ErrorIs(t, err, syncproto.ErrInvalidUTF8)
			require.Empty(t, got)
		})
	}
	t.Run("object bytes need not be UTF-8", func(t *testing.T) {
		p := syncproto.Pack{Objects: []syncproto.ObjectRecord{{Family: syncproto.Change, Key: "k", Bytes: []byte(bad)}}}
		_, err := p.AppendPostcard(nil)
		require.NoError(t, err)
	})

	wants := map[string]syncproto.Wants{
		"ref":  {Refs: []string{"ok", bad}},
		"have": {Haves: []string{bad}},
	}
	for name, w := range wants {
		t.Run("wants "+name, func(t *testing.T) {
			got, err := w.AppendPostcard(nil)
			require.ErrorIs(t, err, syncproto.ErrInvalidUTF8)
			require.Empty(t, got)
			_, err = syncproto.NewCodec().EncodeWants(w)
			require.ErrorIs(t, err, syncproto.ErrInvalidUTF8)
		})
	}

	t.Run("decoding refuses invalid UTF-8", func(t *testing.T) {
		// Advertise("dev") with the view name's first byte made 0xff.
		in := mustHex(t, advertiseGolden)
		in[2] = 0xff
		var w syncproto.Wants
		requireDecodeError(t, w.UnmarshalPostcard(in), syncproto.StagePostcard)
	})
}

func TestDecodeErrorText(t *testing.T) {
	var w syncproto.Wants
	err := w.UnmarshalPostcard(nil)
	require.ErrorContains(t, err, "syncproto: postcard decode: ")
}

func requireDecodeError(t *testing.T, err error, stage string) {
	t.Helper()
	require.Error(t, err)
	var de *syncproto.DecodeError
	require.ErrorAs(t, err, &de)
	require.Equal(t, stage, de.Stage)
	require.NotNil(t, de.Err)
	require.ErrorIs(t, err, de.Err)
}

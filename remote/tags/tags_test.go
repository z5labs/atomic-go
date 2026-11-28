package tags_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"lukechampine.com/blake3"

	"github.com/z5labs/atomic-go/change"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/tags"
)

const goldenTimestamp = "2023-11-14T22:13:20Z"

// recordBytes assembles a TagRecord by hand, field by field, as the table
// under TagRecord in docs/remote/endpoints/project.md lays it out: the
// Release tag v1 on view dev at sequence 3, with a zero state and a change
// hash of 32 0x11 bytes, the timestamp text ts, and then tail, the bytes
// from the author option on.
func recordBytes(ts string, tail ...byte) []byte {
	var b []byte
	b = append(b, 0x02, 'v', '1')
	b = append(b, 0x03, 'd', 'e', 'v')
	b = append(b, 0x03)
	b = append(b, make([]byte, 32)...)
	b = append(b, bytes.Repeat([]byte{0x11}, 32)...)
	b = append(b, byte(len(ts)))
	b = append(b, ts...)
	return append(b, tail...)
}

// golden is the 97-byte example under TagRecord: no author, message or
// metadata.
var golden = recordBytes(goldenTimestamp, 0x00, 0x00, 0x00, 0x00)

func goldenRecord() tags.Record {
	return tags.Record{
		Name:          "v1",
		View:          "dev",
		Sequence:      3,
		ChangeHash:    ident.Hash(bytes.Repeat([]byte{0x11}, 32)),
		Timestamp:     time.Unix(1700000000, 0).UTC(),
		TimestampText: goldenTimestamp,
		Kind:          tags.Release,
	}
}

func ptr[T any](v T) *T { return &v }

func TestGoldenRoundTrip(t *testing.T) {
	require.Len(t, golden, 97)

	var r tags.Record
	require.NoError(t, r.UnmarshalPostcard(golden))
	require.Equal(t, goldenRecord(), r)

	b, err := r.MarshalPostcard()
	require.NoError(t, err)
	require.Equal(t, golden, b)

	t.Run("from the instant alone", func(t *testing.T) {
		r := goldenRecord()
		r.TimestampText = ""
		b, err := r.MarshalPostcard()
		require.NoError(t, err)
		require.Equal(t, golden, b)
	})
}

func TestTimestampText(t *testing.T) {
	base := time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)
	cases := []struct {
		name string
		text string
		nsec int
		// chrono is whether chrono writes text for the instant, so that
		// formatting the instant alone gives the same bytes.
		chrono bool
	}{
		{name: "6 digits", text: "2023-11-14T22:13:20.451350Z", nsec: 451350000, chrono: true},
		{name: "3 digits", text: "2023-11-14T22:13:20.451Z", nsec: 451000000, chrono: true},
		{name: "9 digits", text: "2023-11-14T22:13:20.451350001Z", nsec: 451350001, chrono: true},
		{name: "0 digits", text: "2023-11-14T22:13:20Z", nsec: 0, chrono: true},
		{name: "a spelling chrono does not write", text: "2023-11-14T22:13:20.100000Z", nsec: 100000000},
		{name: "an offset", text: "2023-11-15T00:13:20.451+02:00", nsec: 451000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := recordBytes(tc.text, 0x00, 0x00, 0x00, 0x00)

			var r tags.Record
			require.NoError(t, r.UnmarshalPostcard(in))
			require.Equal(t, tc.text, r.TimestampText)
			require.Equal(t, base.Add(time.Duration(tc.nsec)), r.Timestamp)

			b, err := r.MarshalPostcard()
			require.NoError(t, err)
			require.Equal(t, in, b)

			r.TimestampText = ""
			b, err = r.MarshalPostcard()
			require.NoError(t, err)
			if tc.chrono {
				require.Equal(t, in, b)
			} else {
				require.NotEqual(t, in, b)
			}
		})
	}
}

func TestRecordEndingAfterKind(t *testing.T) {
	cut := golden[:len(golden)-1]

	var r tags.Record
	require.NoError(t, r.UnmarshalPostcard(cut))
	require.Equal(t, goldenRecord(), r)
	require.Nil(t, r.Metadata)

	b, err := r.MarshalPostcard()
	require.NoError(t, err)
	require.Equal(t, golden, b, "a writer emits all ten fields")

	t.Run("keeps the timestamp text", func(t *testing.T) {
		const text = "2023-11-14T22:13:20.100000Z"
		var r tags.Record
		require.NoError(t, r.UnmarshalPostcard(recordBytes(text, 0x00, 0x00, 0x02)))
		require.Equal(t, text, r.TimestampText)
		require.Equal(t, tags.Custom, r.Kind)
	})
}

func TestMetadata(t *testing.T) {
	const meta = `{"ci":"ok"}`
	in := recordBytes(goldenTimestamp, append([]byte{0x00, 0x00, 0x00, 0x01, byte(len(meta))}, meta...)...)

	var r tags.Record
	require.NoError(t, r.UnmarshalPostcard(in))
	require.Equal(t, json.RawMessage(meta), r.Metadata)

	want := goldenRecord()
	want.Metadata = json.RawMessage(meta)
	require.Equal(t, want, r)

	b, err := r.MarshalPostcard()
	require.NoError(t, err)
	require.Equal(t, in, b)

	t.Run("empty is none", func(t *testing.T) {
		r := goldenRecord()
		r.Metadata = json.RawMessage{}
		b, err := r.MarshalPostcard()
		require.NoError(t, err)
		require.Equal(t, golden, b)
	})

	t.Run("refused when not JSON", func(t *testing.T) {
		r := goldenRecord()
		r.Metadata = json.RawMessage(`{"ci":`)
		_, err := r.MarshalPostcard()
		require.ErrorIs(t, err, tags.ErrInvalidRecord)
	})

	t.Run("refused when not UTF-8", func(t *testing.T) {
		r := goldenRecord()
		r.Metadata = json.RawMessage("\"\xff\"")
		_, err := r.MarshalPostcard()
		require.ErrorIs(t, err, tags.ErrInvalidRecord)
	})

	t.Run("refused on read when not JSON", func(t *testing.T) {
		const bad = `{"ci":`
		in := recordBytes(goldenTimestamp, append([]byte{0x00, 0x00, 0x00, 0x01, byte(len(bad))}, bad...)...)
		var r tags.Record
		err := r.UnmarshalPostcard(in)
		require.ErrorIs(t, err, tags.ErrMalformed)
		require.Equal(t, tags.Record{}, r)
	})
}

func TestAuthor(t *testing.T) {
	var tail []byte
	tail = append(tail, 0x01, 0x03)
	tail = append(tail, "Ann"...)
	tail = append(tail, 0x01, 0x0f)
	tail = append(tail, "ann@example.com"...)
	tail = append(tail, 0x01, 0x03)
	tail = append(tail, "KEY"...)
	tail = append(tail, 0x01, 0x04)
	tail = append(tail, "note"...)
	tail = append(tail, 0x01, 0x00)
	in := recordBytes(goldenTimestamp, tail...)

	var r tags.Record
	require.NoError(t, r.UnmarshalPostcard(in))

	want := goldenRecord()
	want.Author = &change.Author{Name: "Ann", Email: ptr("ann@example.com"), Identity: ptr("KEY")}
	want.Message = ptr("note")
	want.Kind = tags.ReviewGate
	require.Equal(t, want, r)

	b, err := r.MarshalPostcard()
	require.NoError(t, err)
	require.Equal(t, in, b)
}

func TestContentHashGolden(t *testing.T) {
	var in []byte
	in = append(in, "v1"...)
	in = append(in, "dev"...)
	in = binary.LittleEndian.AppendUint64(in, 3)
	in = append(in, make([]byte, 32)...)
	in = append(in, bytes.Repeat([]byte{0x11}, 32)...)
	in = binary.LittleEndian.AppendUint64(in, uint64(int64(1700000000)))
	in = append(in, 0x00) // kind: Release
	want := ident.Hash(blake3.Sum256(in))

	var r tags.Record
	require.NoError(t, r.UnmarshalPostcard(golden))
	require.Equal(t, want, r.ContentHash())
	require.Equal(t, want.String(), r.Key())
}

func TestContentHashFields(t *testing.T) {
	var in []byte
	in = append(in, "v1"...)
	in = append(in, "dev"...)
	in = binary.LittleEndian.AppendUint64(in, 3)
	in = append(in, make([]byte, 32)...)
	in = append(in, bytes.Repeat([]byte{0x11}, 32)...)
	in = binary.LittleEndian.AppendUint64(in, uint64(int64(1700000000)))
	in = append(in, "Ann"...)
	in = append(in, "ann@example.com"...)
	in = append(in, "note"...)
	in = append(in, 0x02) // kind: Custom

	r := goldenRecord()
	r.Author = &change.Author{Name: "Ann", Email: ptr("ann@example.com"), Identity: ptr("KEY")}
	r.Message = ptr("note")
	r.Kind = tags.Custom
	require.Equal(t, ident.Hash(blake3.Sum256(in)), r.ContentHash())
}

func TestKeyIgnores(t *testing.T) {
	// withAuthor gives the golden record an author with an email, so the
	// identity test below changes nothing but the identity.
	withAuthor := func(identity *string) tags.Record {
		r := goldenRecord()
		r.Author = &change.Author{Name: "Ann", Email: ptr("ann@example.com"), Identity: identity}
		return r
	}

	cases := map[string]struct{ base, changed tags.Record }{
		"metadata": {
			base: goldenRecord(),
			changed: func() tags.Record {
				r := goldenRecord()
				r.Metadata = json.RawMessage(`{"ci":"ok"}`)
				return r
			}(),
		},
		"author identity": {
			base:    withAuthor(nil),
			changed: withAuthor(ptr("KEY")),
		},
		"sub-second timestamp text": {
			base: goldenRecord(),
			changed: func() tags.Record {
				r := goldenRecord()
				r.TimestampText = "2023-11-14T22:13:20.999999999Z"
				return r
			}(),
		},
		"sub-second instant": {
			base: goldenRecord(),
			changed: func() tags.Record {
				r := goldenRecord()
				r.TimestampText = ""
				r.Timestamp = r.Timestamp.Add(450 * time.Millisecond)
				return r
			}(),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.base.Key(), tc.changed.Key())
		})
	}

	key := goldenRecord().Key()

	t.Run("name", func(t *testing.T) {
		r := goldenRecord()
		r.Name = "v2"
		require.NotEqual(t, key, r.Key())
	})

	t.Run("whole seconds", func(t *testing.T) {
		r := goldenRecord()
		r.TimestampText = "2023-11-14T22:13:21Z"
		require.NotEqual(t, key, r.Key())
	})

	t.Run("author email", func(t *testing.T) {
		r := withAuthor(nil)
		r.Author.Email = nil
		require.NotEqual(t, withAuthor(nil).Key(), r.Key())
	})
}

func TestUnmarshalTruncated(t *testing.T) {
	for i := range len(golden) - 1 {
		var r tags.Record
		err := r.UnmarshalPostcard(golden[:i])
		require.ErrorIs(t, err, tags.ErrMalformed, "prefix of %d bytes", i)
		require.Equal(t, tags.Record{}, r)
	}

	t.Run("trailing bytes are ignored", func(t *testing.T) {
		var r tags.Record
		require.NoError(t, r.UnmarshalPostcard(append(bytes.Clone(golden), 0xff, 0xff)))
		require.Equal(t, goldenRecord(), r)
	})
}

func TestUnmarshalRefuses(t *testing.T) {
	cases := map[string][]byte{
		"unknown kind":      recordBytes(goldenTimestamp, 0x00, 0x00, 0x03, 0x00),
		"bad option tag":    recordBytes(goldenTimestamp, 0x02, 0x00, 0x00, 0x00),
		"bad metadata tag":  recordBytes(goldenTimestamp, 0x00, 0x00, 0x00, 0x02),
		"not a timestamp":   recordBytes("yesterday", 0x00, 0x00, 0x00, 0x00),
		"too many fraction": recordBytes("2023-11-14T22:13:20.1234567890Z", 0x00, 0x00, 0x00, 0x00),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			r := goldenRecord()
			err := r.UnmarshalPostcard(in)
			require.ErrorIs(t, err, tags.ErrMalformed)
			require.Equal(t, goldenRecord(), r, "a failure leaves the record unchanged")
		})
	}
}

func TestMarshalRefuses(t *testing.T) {
	cases := map[string]func(*tags.Record){
		"unknown kind":           func(r *tags.Record) { r.Kind = 3 },
		"name not UTF-8":         func(r *tags.Record) { r.Name = "\xff" },
		"view not UTF-8":         func(r *tags.Record) { r.View = "\xff" },
		"message not UTF-8":      func(r *tags.Record) { r.Message = ptr("\xff") },
		"author name not UTF-8":  func(r *tags.Record) { r.Author = &change.Author{Name: "\xff"} },
		"author email not UTF-8": func(r *tags.Record) { r.Author = &change.Author{Email: ptr("\xff")} },
		"identity not UTF-8":     func(r *tags.Record) { r.Author = &change.Author{Identity: ptr("\xff")} },
		"timestamp text":         func(r *tags.Record) { r.TimestampText = "yesterday" },
		"year out of range": func(r *tags.Record) {
			r.TimestampText = ""
			r.Timestamp = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := goldenRecord()
			mutate(&r)
			b, err := r.MarshalPostcard()
			require.ErrorIs(t, err, tags.ErrInvalidRecord)
			require.Nil(t, b)
		})
	}
}

func TestKindString(t *testing.T) {
	require.Equal(t, "Release", tags.Release.String())
	require.Equal(t, "ReviewGate", tags.ReviewGate.String())
	require.Equal(t, "Custom", tags.Custom.String())
	require.Equal(t, "Kind(7)", tags.Kind(7).String())
}

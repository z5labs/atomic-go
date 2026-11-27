package postcard_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/internal/postcard"
)

func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	require.NoError(t, err)
	return b
}

func requireError(t *testing.T, err error, want error, offset int64) {
	t.Helper()
	require.ErrorIs(t, err, want)
	var pe *postcard.Error
	require.True(t, errors.As(err, &pe), "error %v is not a *postcard.Error", err)
	require.Equal(t, offset, pe.Offset)
}

func encode(f func(*postcard.Encoder)) []byte {
	e := postcard.NewEncoder(nil)
	f(e)
	return e.Bytes()
}

func ptr[T any](v T) *T { return &v }

// The pack example of docs/remote/endpoints/project.md#syncpack: one View
// object keyed "bb" with bytes "{}", a genesis ref dev -> bb and a ref f
// moving from bb to cc.
const packHex = "01 04 02 6262 02 7b7d 02 03 646576 00 02 6262 01 66 01 02 6262 02 6363"

func packValue() syncPack {
	return syncPack{
		Objects: []objectRecord{{Family: 4, Key: "bb", Bytes: []byte("{}")}},
		Refs: []refRecord{
			{Name: "dev", NewTarget: "bb"},
			{Name: "f", ExpectOld: ptr("bb"), NewTarget: "cc"},
		},
	}
}

func TestGoldens(t *testing.T) {
	t.Run("CompactPosition {0, 0} and {0, 42}", func(t *testing.T) {
		for _, tc := range []struct {
			v   compactPosition
			hex string
		}{
			{compactPosition{0, 0}, "00 00"},
			{compactPosition{0, 42}, "00 2a"},
		} {
			b := mustHex(t, tc.hex)
			require.Equal(t, b, encode(func(e *postcard.Encoder) { encodeCompactPosition(e, tc.v) }))
			got, err := postcard.DecodeExact(b, decodeCompactPosition)
			require.NoError(t, err)
			require.Equal(t, tc.v, got)
		}
	})

	t.Run("Vec<u16> [1, 2, 3]", func(t *testing.T) {
		b := mustHex(t, "03 01 02 03")
		require.Equal(t, b, encode(func(e *postcard.Encoder) {
			postcard.PutSeq(e, []uint16{1, 2, 3}, (*postcard.Encoder).U16)
		}))
		got, err := postcard.DecodeExact(b, func(d *postcard.Decoder) []uint16 {
			return postcard.Seq(d, (*postcard.Decoder).U16)
		})
		require.NoError(t, err)
		require.Equal(t, []uint16{1, 2, 3}, got)
	})

	t.Run("u16 0xFFFF", func(t *testing.T) {
		b := mustHex(t, "ff ff 03")
		require.Equal(t, b, encode(func(e *postcard.Encoder) { e.U16(0xFFFF) }))
		got, err := postcard.DecodeExact(b, (*postcard.Decoder).U16)
		require.NoError(t, err)
		require.Equal(t, uint16(0xFFFF), got)
	})

	t.Run("i64 1794500600", func(t *testing.T) {
		b := mustHex(t, "f0 9f af af 0d")
		require.Equal(t, b, encode(func(e *postcard.Encoder) { e.I64(1794500600) }))
		got, err := postcard.DecodeExact(b, (*postcard.Decoder).I64)
		require.NoError(t, err)
		require.Equal(t, int64(1794500600), got)
	})

	t.Run(`string "ed25519"`, func(t *testing.T) {
		b := mustHex(t, "07 65 64 32 35 35 31 39")
		require.Equal(t, b, encode(func(e *postcard.Encoder) { e.Str("ed25519") }))
		got, err := postcard.DecodeExact(b, (*postcard.Decoder).Str)
		require.NoError(t, err)
		require.Equal(t, "ed25519", got)
	})

	t.Run("300 as u16, u32 and u64", func(t *testing.T) {
		b := mustHex(t, "ac 02")
		require.Equal(t, b, encode(func(e *postcard.Encoder) { e.U16(300) }))
		require.Equal(t, b, encode(func(e *postcard.Encoder) { e.U32(300) }))
		require.Equal(t, b, encode(func(e *postcard.Encoder) { e.U64(300) }))
		u16, err := postcard.DecodeExact(b, (*postcard.Decoder).U16)
		require.NoError(t, err)
		require.Equal(t, uint16(300), u16)
		u32, err := postcard.DecodeExact(b, (*postcard.Decoder).U32)
		require.NoError(t, err)
		require.Equal(t, uint32(300), u32)
		u64, err := postcard.DecodeExact(b, (*postcard.Decoder).U64)
		require.NoError(t, err)
		require.Equal(t, uint64(300), u64)
	})

	t.Run(`SyncWants::advertise(["dev"])`, func(t *testing.T) {
		b := mustHex(t, "01 03 64 65 76 00 01")
		w := syncWants{Refs: []string{"dev"}, Haves: []string{}, RefsOnly: true}
		require.Equal(t, b, encode(func(e *postcard.Encoder) { encodeSyncWants(e, w) }))
		got, err := postcard.DecodeExact(b, decodeSyncWants)
		require.NoError(t, err)
		require.Equal(t, w, got)
	})

	t.Run("SyncPack example", func(t *testing.T) {
		b := mustHex(t, packHex)
		require.Equal(t, b, encode(func(e *postcard.Encoder) { encodeSyncPack(e, packValue()) }))
		got, err := postcard.DecodeExact(b, decodeSyncPack)
		require.NoError(t, err)
		require.Equal(t, packValue(), got)
	})

	t.Run("SyncPack example from a stream", func(t *testing.T) {
		b := mustHex(t, packHex)
		r := postcard.NewReader(iotest.OneByteReader(bytes.NewReader(b)), 1024)
		got := readSyncPack(r)
		require.NoError(t, r.Err())
		require.Equal(t, packValue(), got)
		require.Equal(t, int64(len(b)), r.Offset())
	})
}

func TestVarintLimits(t *testing.T) {
	t.Run("u16 accepts an in-range overlong encoding", func(t *testing.T) {
		d := postcard.NewDecoder(mustHex(t, "80 00"))
		require.Equal(t, uint16(0), d.U16())
		require.NoError(t, d.Err())
		require.Equal(t, int64(2), d.Offset())

		d = postcard.NewDecoder(mustHex(t, "80 80 03"))
		require.Equal(t, uint16(0xC000), d.U16())
		require.NoError(t, d.Err())
	})

	for _, tc := range []struct {
		name string
		hex  string
		dec  func(*postcard.Decoder)
	}{
		{"u16 of four bytes", "80 80 80 00", func(d *postcard.Decoder) { d.U16() }},
		{"u16 third byte above 0x03", "80 80 04", func(d *postcard.Decoder) { d.U16() }},
		{"u16 third byte with the continuation bit", "ff ff 83 00", func(d *postcard.Decoder) { d.U16() }},
		{"u32 fifth byte above 0x0f", "ff ff ff ff 10", func(d *postcard.Decoder) { d.U32() }},
		{"u32 of six bytes", "80 80 80 80 80 00", func(d *postcard.Decoder) { d.U32() }},
		{"u64 tenth byte above 0x01", "ff ff ff ff ff ff ff ff ff 02", func(d *postcard.Decoder) { d.U64() }},
		{"u64 of eleven bytes", "80 80 80 80 80 80 80 80 80 80 00", func(d *postcard.Decoder) { d.U64() }},
		{"i16 through the u16 rule", "80 80 04", func(d *postcard.Decoder) { d.I16() }},
		{"i32 through the u32 rule", "ff ff ff ff 10", func(d *postcard.Decoder) { d.I32() }},
		{"i64 through the u64 rule", "ff ff ff ff ff ff ff ff ff 02", func(d *postcard.Decoder) { d.I64() }},
		{"variant index through the u32 rule", "ff ff ff ff 10", func(d *postcard.Decoder) { d.Variant(math.MaxUint32) }},
		{"sequence length through the u64 rule", "ff ff ff ff ff ff ff ff ff 02", func(d *postcard.Decoder) { d.SeqLen() }},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			d := postcard.NewDecoder(append([]byte{0x2a}, mustHex(t, tc.hex)...))
			d.U8()
			tc.dec(d)
			requireError(t, d.Err(), postcard.ErrBadVarint, 1)
		})
	}

	t.Run("accepts each type's largest last byte", func(t *testing.T) {
		u32, err := postcard.DecodeExact(mustHex(t, "ff ff ff ff 0f"), (*postcard.Decoder).U32)
		require.NoError(t, err)
		require.Equal(t, uint32(math.MaxUint32), u32)

		u64, err := postcard.DecodeExact(mustHex(t, "ff ff ff ff ff ff ff ff ff 01"), (*postcard.Decoder).U64)
		require.NoError(t, err)
		require.Equal(t, uint64(math.MaxUint64), u64)
	})

	t.Run("a varint cut short is ErrUnexpectedEnd", func(t *testing.T) {
		_, err := postcard.Decode(mustHex(t, "ff ff"), (*postcard.Decoder).U32)
		requireError(t, err, postcard.ErrUnexpectedEnd, 0)
	})

	t.Run("the Reader follows the same rules", func(t *testing.T) {
		r := postcard.NewReader(bytes.NewReader(mustHex(t, "80 00 ff ff ff ff 10")), 0)
		require.Equal(t, uint32(0), r.U32())
		require.NoError(t, r.Err())
		r.U32()
		requireError(t, r.Err(), postcard.ErrBadVarint, 2)

		r = postcard.NewReader(bytes.NewReader(mustHex(t, "ff ff ff ff ff ff ff ff ff 02")), 0)
		r.U64()
		requireError(t, r.Err(), postcard.ErrBadVarint, 0)
	})
}

func TestSignedAndFloat(t *testing.T) {
	t.Run("zigzag", func(t *testing.T) {
		require.Equal(t, []byte{0x00}, encode(func(e *postcard.Encoder) { e.I32(0) }))
		require.Equal(t, []byte{0x01}, encode(func(e *postcard.Encoder) { e.I32(-1) }))
		require.Equal(t, []byte{0x02}, encode(func(e *postcard.Encoder) { e.I32(1) }))
		require.Equal(t, []byte{0x03}, encode(func(e *postcard.Encoder) { e.I16(-2) }))
	})

	t.Run("extremes round-trip", func(t *testing.T) {
		for _, v := range []int16{math.MinInt16, -1, 0, 1, math.MaxInt16} {
			got, err := postcard.DecodeExact(encode(func(e *postcard.Encoder) { e.I16(v) }), (*postcard.Decoder).I16)
			require.NoError(t, err)
			require.Equal(t, v, got)
		}
		for _, v := range []int32{math.MinInt32, -1, 0, 1, math.MaxInt32} {
			got, err := postcard.DecodeExact(encode(func(e *postcard.Encoder) { e.I32(v) }), (*postcard.Decoder).I32)
			require.NoError(t, err)
			require.Equal(t, v, got)
		}
		for _, v := range []int64{math.MinInt64, -1, 0, 1, math.MaxInt64} {
			got, err := postcard.DecodeExact(encode(func(e *postcard.Encoder) { e.I64(v) }), (*postcard.Decoder).I64)
			require.NoError(t, err)
			require.Equal(t, v, got)
		}
		for _, v := range []int8{math.MinInt8, -1, 0, 1, math.MaxInt8} {
			got, err := postcard.DecodeExact(encode(func(e *postcard.Encoder) { e.I8(v) }), (*postcard.Decoder).I8)
			require.NoError(t, err)
			require.Equal(t, v, got)
		}
	})

	t.Run("floats are little-endian IEEE 754", func(t *testing.T) {
		require.Equal(t, mustHex(t, "0000803f"), encode(func(e *postcard.Encoder) { e.F32(1) }))
		require.Equal(t, mustHex(t, "000000000000f03f"), encode(func(e *postcard.Encoder) { e.F64(1) }))

		f32, err := postcard.DecodeExact(mustHex(t, "0000c0bf"), (*postcard.Decoder).F32)
		require.NoError(t, err)
		require.Equal(t, float32(-1.5), f32)
		f64, err := postcard.DecodeExact(mustHex(t, "000000000000f8bf"), (*postcard.Decoder).F64)
		require.NoError(t, err)
		require.Equal(t, -1.5, f64)
	})
}

func TestRefusals(t *testing.T) {
	t.Run("bool tag 0x02", func(t *testing.T) {
		_, err := postcard.Decode([]byte{0x02}, (*postcard.Decoder).Bool)
		requireError(t, err, postcard.ErrBadBool, 0)

		r := postcard.NewReader(bytes.NewReader([]byte{0x02}), 0)
		r.Bool()
		requireError(t, r.Err(), postcard.ErrBadBool, 0)
	})

	t.Run("option tag 0x02", func(t *testing.T) {
		_, err := postcard.Decode([]byte{0x02, 0x00}, func(d *postcard.Decoder) *uint32 {
			return postcard.Option(d, (*postcard.Decoder).U32)
		})
		requireError(t, err, postcard.ErrBadOption, 0)

		r := postcard.NewReader(bytes.NewReader([]byte{0x02}), 0)
		r.OptionTag()
		requireError(t, r.Err(), postcard.ErrBadOption, 0)
	})

	t.Run("invalid UTF-8", func(t *testing.T) {
		b := mustHex(t, "00 02 c3 28")
		d := postcard.NewDecoder(b)
		d.U8()
		require.Empty(t, d.Str())
		requireError(t, d.Err(), postcard.ErrBadUTF8, 1)

		r := postcard.NewReader(bytes.NewReader(b), 16)
		r.Bool()
		require.Empty(t, r.Str())
		requireError(t, r.Err(), postcard.ErrBadUTF8, 1)
	})

	t.Run("unknown variant", func(t *testing.T) {
		_, err := postcard.Decode([]byte{0x06}, func(d *postcard.Decoder) uint32 { return d.Variant(objectFamilies) })
		requireError(t, err, postcard.ErrUnknownVariant, 0)

		got, err := postcard.Decode([]byte{0x05}, func(d *postcard.Decoder) uint32 { return d.Variant(objectFamilies) })
		require.NoError(t, err)
		require.Equal(t, uint32(5), got)

		r := postcard.NewReader(bytes.NewReader([]byte{0x06}), 0)
		r.Variant(objectFamilies)
		requireError(t, r.Err(), postcard.ErrUnknownVariant, 0)
	})

	t.Run("byte sequence and string longer than the input", func(t *testing.T) {
		_, err := postcard.Decode(mustHex(t, "05 61 62"), (*postcard.Decoder).ByteSeq)
		requireError(t, err, postcard.ErrUnexpectedEnd, 0)
		_, err = postcard.Decode(mustHex(t, "05 61 62"), (*postcard.Decoder).Str)
		requireError(t, err, postcard.ErrUnexpectedEnd, 0)
	})

	t.Run("fixed array longer than the input", func(t *testing.T) {
		_, err := postcard.Decode(make([]byte, 31), (*postcard.Decoder).Array32)
		requireError(t, err, postcard.ErrUnexpectedEnd, 0)
		_, err = postcard.Decode([]byte{1, 2}, func(d *postcard.Decoder) []byte { return d.Fixed(3) })
		requireError(t, err, postcard.ErrUnexpectedEnd, 0)
		_, err = postcard.Decode([]byte{1, 2}, func(d *postcard.Decoder) []byte { return d.Fixed(-1) })
		requireError(t, err, postcard.ErrBadLength, 0)
	})

	t.Run("empty input", func(t *testing.T) {
		for name, dec := range map[string]func(*postcard.Decoder){
			"U8": func(d *postcard.Decoder) { d.U8() }, "Bool": func(d *postcard.Decoder) { d.Bool() },
			"F32": func(d *postcard.Decoder) { d.F32() }, "F64": func(d *postcard.Decoder) { d.F64() },
			"OptionTag": func(d *postcard.Decoder) { d.OptionTag() }, "SeqLen": func(d *postcard.Decoder) { d.SeqLen() },
		} {
			t.Run(name, func(t *testing.T) {
				d := postcard.NewDecoder(nil)
				dec(d)
				requireError(t, d.Err(), postcard.ErrUnexpectedEnd, 0)
			})
		}
	})
}

func TestHostileCount(t *testing.T) {
	// A count of 2^30 eight-byte elements would allocate 8 GiB if the
	// count were trusted.
	b := encode(func(e *postcard.Encoder) { e.SeqLen(1 << 30); e.U64(1) })

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := postcard.Decode(b, func(d *postcard.Decoder) []uint64 {
		return postcard.Seq(d, (*postcard.Decoder).U64)
	})
	runtime.ReadMemStats(&after)

	requireError(t, err, postcard.ErrUnexpectedEnd, 0)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))

	t.Run("SeqLen fails once the count exceeds Remaining", func(t *testing.T) {
		d := postcard.NewDecoder([]byte{0x03, 0x01, 0x02})
		require.Equal(t, 0, d.SeqLen())
		requireError(t, d.Err(), postcard.ErrUnexpectedEnd, 0)

		d = postcard.NewDecoder([]byte{0x02, 0x01, 0x02})
		require.Equal(t, 2, d.SeqLen())
		require.NoError(t, d.Err())
		require.Equal(t, 2, d.Remaining())
	})
}

func TestReaderLimits(t *testing.T) {
	declared := encode(func(e *postcard.Encoder) { e.U64(1 << 40) })

	t.Run("a declared length of 2^40 against maxLen 1024", func(t *testing.T) {
		for name, read := range map[string]func(*postcard.Reader){
			"ByteSeq": func(r *postcard.Reader) { require.Nil(t, r.ByteSeq()) },
			"Str":     func(r *postcard.Reader) { require.Empty(t, r.Str()) },
			"SeqLen":  func(r *postcard.Reader) { require.Zero(t, r.SeqLen()) },
		} {
			t.Run(name, func(t *testing.T) {
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				r := postcard.NewReader(bytes.NewReader(declared), 1024)
				read(r)
				runtime.ReadMemStats(&after)
				requireError(t, r.Err(), postcard.ErrTooLong, 0)
				require.Equal(t, int64(len(declared)), r.Offset())
				require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
			})
		}
	})

	t.Run("a length at the limit is read", func(t *testing.T) {
		b := encode(func(e *postcard.Encoder) { e.ByteSeq([]byte("abcd")) })
		r := postcard.NewReader(bytes.NewReader(b), 4)
		require.Equal(t, []byte("abcd"), r.ByteSeq())
		require.NoError(t, r.Err())

		r = postcard.NewReader(bytes.NewReader(b), 3)
		r.ByteSeq()
		requireError(t, r.Err(), postcard.ErrTooLong, 0)
	})

	t.Run("the underlying reader's error is reachable", func(t *testing.T) {
		boom := errors.New("boom")
		r := postcard.NewReader(io.MultiReader(bytes.NewReader([]byte{0x05, 0x61}), iotest.ErrReader(boom)), 16)
		r.ByteSeq()
		requireError(t, r.Err(), boom, 0)

		r = postcard.NewReader(iotest.ErrReader(boom), 16)
		r.U32()
		requireError(t, r.Err(), boom, 0)
	})

	t.Run("input that ends inside a value", func(t *testing.T) {
		r := postcard.NewReader(bytes.NewReader([]byte{0x2a, 0x05, 0x61}), 16)
		require.Equal(t, uint64(42), r.U64())
		r.Str()
		requireError(t, r.Err(), postcard.ErrUnexpectedEnd, 1)
		require.ErrorIs(t, r.Err(), io.ErrUnexpectedEOF)

		r = postcard.NewReader(bytes.NewReader(nil), 16)
		r.Bool()
		requireError(t, r.Err(), postcard.ErrUnexpectedEnd, 0)
		require.ErrorIs(t, r.Err(), io.EOF)
	})

	t.Run("errors are sticky", func(t *testing.T) {
		r := postcard.NewReader(bytes.NewReader([]byte{0x02, 0x01, 0x01}), 16)
		r.Bool()
		first := r.Err()
		require.False(t, r.Bool())
		require.Zero(t, r.U64())
		require.Same(t, first, r.Err())
		require.Equal(t, int64(1), r.Offset())
	})

	t.Run("a negative limit panics", func(t *testing.T) {
		require.Panics(t, func() { postcard.NewReader(bytes.NewReader(nil), -1) })
	})
}

func TestDecoder(t *testing.T) {
	t.Run("errors are sticky", func(t *testing.T) {
		d := postcard.NewDecoder([]byte{0x01, 0x02, 0x01, 0x05})
		require.True(t, d.Bool())
		require.False(t, d.Bool())
		requireError(t, d.Err(), postcard.ErrBadBool, 1)
		first := d.Err()

		require.False(t, d.Bool())
		require.Zero(t, d.U8())
		require.Nil(t, d.ByteSeq())
		require.Equal(t, [32]byte{}, d.Array32())
		d.Fail(postcard.ErrBadLength)
		require.Same(t, first, d.Err())
		require.Equal(t, int64(1), d.Offset())
		require.Equal(t, 3, d.Remaining())
	})

	t.Run("Fail records a caller's rule at the current offset", func(t *testing.T) {
		b := encode(func(e *postcard.Encoder) { e.U8(7); e.ByteSeq(make([]byte, 63)) })
		d := postcard.NewDecoder(b)
		d.U8()
		d.Fail(nil)
		require.NoError(t, d.Err())
		if sig := d.ByteSeq(); len(sig) != 64 {
			d.Fail(postcard.ErrBadLength)
		}
		requireError(t, d.Err(), postcard.ErrBadLength, int64(len(b)))
		require.EqualError(t, d.Err(), "postcard: bad length at offset 65")
	})

	t.Run("byte sequences and fixed arrays alias the input", func(t *testing.T) {
		b := mustHex(t, "02 61 62 63 64")
		d := postcard.NewDecoder(b)
		seq := d.ByteSeq()
		fixed := d.Fixed(2)
		require.NoError(t, d.Err())
		require.Equal(t, []byte("ab"), seq)
		require.Equal(t, []byte("cd"), fixed)
		b[1], b[3] = 'x', 'y'
		require.Equal(t, []byte("xb"), seq)
		require.Equal(t, []byte("yd"), fixed)
		require.Equal(t, 2, cap(seq), "appending to a result must not overwrite the input")
	})

	t.Run("Array32 reads 32 bytes with no prefix", func(t *testing.T) {
		var want [32]byte
		for i := range want {
			want[i] = byte(i)
		}
		b := encode(func(e *postcard.Encoder) { e.Array32(want) })
		require.Equal(t, want[:], b)
		got, err := postcard.DecodeExact(b, (*postcard.Decoder).Array32)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("Option and Seq", func(t *testing.T) {
		some := encode(func(e *postcard.Encoder) { postcard.PutOption(e, ptr(uint32(300)), (*postcard.Encoder).U32) })
		require.Equal(t, mustHex(t, "01 ac 02"), some)
		none := encode(func(e *postcard.Encoder) { postcard.PutOption(e, (*uint32)(nil), (*postcard.Encoder).U32) })
		require.Equal(t, []byte{0x00}, none)

		opt := func(d *postcard.Decoder) *uint32 { return postcard.Option(d, (*postcard.Decoder).U32) }
		got, err := postcard.DecodeExact(some, opt)
		require.NoError(t, err)
		require.Equal(t, ptr(uint32(300)), got)
		got, err = postcard.DecodeExact(none, opt)
		require.NoError(t, err)
		require.Nil(t, got)

		_, err = postcard.Decode(mustHex(t, "01 ff"), opt)
		requireError(t, err, postcard.ErrUnexpectedEnd, 1)

		seq := func(d *postcard.Decoder) []string { return postcard.Seq(d, (*postcard.Decoder).Str) }
		_, err = postcard.Decode(mustHex(t, "02 01 61 01 ff"), seq)
		requireError(t, err, postcard.ErrBadUTF8, 3)

		empty, err := postcard.DecodeExact([]byte{0x00}, seq)
		require.NoError(t, err)
		require.Equal(t, []string{}, empty)
	})

	t.Run("Decode ignores trailing bytes and DecodeExact refuses them", func(t *testing.T) {
		got, err := postcard.Decode([]byte{0x2a, 0xff}, (*postcard.Decoder).U8)
		require.NoError(t, err)
		require.Equal(t, uint8(42), got)

		got, err = postcard.DecodeExact([]byte{0x2a, 0xff}, (*postcard.Decoder).U8)
		requireError(t, err, postcard.ErrTrailingBytes, 1)
		require.Zero(t, got)
	})
}

func TestEncoder(t *testing.T) {
	t.Run("appends to the buffer it is given", func(t *testing.T) {
		e := postcard.NewEncoder([]byte{0xaa})
		e.U8(1)
		e.Variant(4)
		e.OptionTag(false)
		e.Fixed([]byte{9, 9})
		require.Equal(t, []byte{0xaa, 0x01, 0x04, 0x00, 0x09, 0x09}, e.Bytes())
	})

	t.Run("a negative sequence length panics", func(t *testing.T) {
		require.Panics(t, func() { postcard.NewEncoder(nil).SeqLen(-1) })
	})

	t.Run("varints are minimal", func(t *testing.T) {
		for _, tc := range []struct {
			v   uint64
			hex string
		}{
			{0, "00"}, {127, "7f"}, {128, "80 01"}, {16383, "ff 7f"}, {16384, "80 80 01"},
			{math.MaxUint32, "ff ff ff ff 0f"}, {math.MaxUint64, "ff ff ff ff ff ff ff ff ff 01"},
		} {
			require.Equal(t, mustHex(t, tc.hex), encode(func(e *postcard.Encoder) { e.U64(tc.v) }))
		}
	})
}

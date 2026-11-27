package postcard_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/internal/postcard"
)

// roundTrip decodes a value from b and re-encodes it, reporting how many
// bytes the decode consumed.
type roundTrip func(b []byte) (out []byte, consumed int64, err error)

func schema[T any](dec func(*postcard.Decoder) T, enc func(*postcard.Encoder, T)) roundTrip {
	return func(b []byte) ([]byte, int64, error) {
		d := postcard.NewDecoder(b)
		v := dec(d)
		if err := d.Err(); err != nil {
			return nil, d.Offset(), err
		}
		e := postcard.NewEncoder(nil)
		enc(e, v)
		return e.Bytes(), d.Offset(), nil
	}
}

// fuzzSchemas returns the types FuzzDecode decodes. The first byte of the
// fuzz input picks one, and the rest is its encoding.
func fuzzSchemas() []roundTrip {
	return []roundTrip{
		schema(decodeCompactPosition, encodeCompactPosition),
		schema(func(d *postcard.Decoder) []uint16 { return postcard.Seq(d, (*postcard.Decoder).U16) },
			func(e *postcard.Encoder, v []uint16) { postcard.PutSeq(e, v, (*postcard.Encoder).U16) }),
		schema((*postcard.Decoder).I64, (*postcard.Encoder).I64),
		schema((*postcard.Decoder).Str, (*postcard.Encoder).Str),
		schema((*postcard.Decoder).U32, (*postcard.Encoder).U32),
		schema(decodeSyncWants, encodeSyncWants),
		schema(decodeSyncPack, encodeSyncPack),
		schema(decodeMixed, encodeMixed),
	}
}

const packSchema = 6

func FuzzDecode(f *testing.F) {
	for _, seed := range []struct {
		schema byte
		hex    string
	}{
		{0, "00 00"},
		{0, "00 2a"},
		{1, "03 01 02 03"},
		{1, "01 ff ff 03"},
		{1, "01 80 00"},
		{1, "01 80 80 80 00"},
		{2, "f0 9f af af 0d"},
		{3, "07 65 64 32 35 35 31 39"},
		{3, "02 c3 28"},
		{4, "ac 02"},
		{4, "ff ff ff ff 10"},
		{5, "01 03 64 65 76 00 01"},
		{5, "01 03 64 65 76 00 02"},
		{packSchema, packHex},
		{packSchema, "ff ff ff ff 0f"},
	} {
		f.Add(append([]byte{seed.schema}, mustHex(f, seed.hex)...))
	}
	f.Add(append([]byte{7}, encode(func(e *postcard.Encoder) {
		encodeMixed(e, mixed{M: "x", N: []byte{1, 2, 3}, P: ptr(uint64(1)), T: ptr("y"), S: []compactPosition{{1, 2}}})
	})...))

	schemas := fuzzSchemas()
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		sel := int(data[0]) % len(schemas)
		in := data[1:]
		out, consumed, err := schemas[sel](in)

		if sel == packSchema {
			r := postcard.NewReader(bytes.NewReader(in), len(in))
			got := readSyncPack(r)
			if err != nil {
				require.Error(t, r.Err())
			} else {
				require.NoError(t, r.Err())
				want, derr := postcard.Decode(in, decodeSyncPack)
				require.NoError(t, derr)
				require.Equal(t, want, got)
				require.Equal(t, consumed, r.Offset())
			}
		}

		if err != nil {
			var pe *postcard.Error
			require.True(t, errors.As(err, &pe), "error %v is not a *postcard.Error", err)
			require.GreaterOrEqual(t, pe.Offset, int64(0))
			require.LessOrEqual(t, pe.Offset, int64(len(in)))
			return
		}
		require.LessOrEqual(t, consumed, int64(len(in)))
		require.LessOrEqual(t, int64(len(out)), consumed, "re-encoding is never longer than the input")
		if int64(len(out)) == consumed {
			// Only an overlong varint shortens on re-encoding, so input of
			// the same length was minimal and must come back identical.
			require.Equal(t, in[:consumed], out)
		}

		again, againConsumed, err := schemas[sel](out)
		require.NoError(t, err)
		require.Equal(t, int64(len(out)), againConsumed)
		require.Equal(t, out, again)
	})
}

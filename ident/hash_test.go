package ident_test

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
)

// From atomic-remote's upload.rs test; docs/remote/types.md#base32-hash-text.
const (
	goldenBase32 = "GJGC6OK6JT77OB2PDGFYEC2BUISJWQNQVT345SNIZ2CKKXGV652A"
	goldenHex    = "324c2f395e4cfff7074f198b820b41a2249b41b0acf7cec9a8ce84a55cd5f774"

	secondBase32 = "Z6ICEFZQ3TKMR4YIRONQAAA4GF6CKGQPMZJWVK6YU5O5SAX456HA"
	secondHex    = "cf90221730dcd4c8f3088b9b00001c317c251a0f66536aabd8a75dd902fcef8e"

	// The author identity of docs/remote/change-format/sections.md#changeheader-example
	// and the signer_did #changesignature gives for it.
	authorIdentity = "TKX4DF2DBALZPZRTUORRAZ74CX5BTFCLMBIRPF27PWYD3TDFUNYQ"
	authorSigner   = "45LZQUIZNX5VFO7UXZHBOU4LMUB6U62NZEUN6QP2VPQ7ANLLTC2Q"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func requireParseError(t *testing.T, err error, want error, offset int) {
	t.Helper()
	require.ErrorIs(t, err, want)
	var pe *ident.ParseError
	require.True(t, errors.As(err, &pe), "error %v is not a *ParseError", err)
	require.Equal(t, offset, pe.Offset)
}

func TestHashGolden(t *testing.T) {
	t.Run("base32 and hex of the upload.rs hash agree", func(t *testing.T) {
		h, err := ident.ParseHash(goldenBase32)
		require.NoError(t, err)
		require.Equal(t, mustHex(t, goldenHex), h[:])
		require.Equal(t, goldenBase32, h.String())
		require.Equal(t, goldenHex, h.Hex())

		fromHex, err := ident.ParseHashHex(goldenHex)
		require.NoError(t, err)
		require.Equal(t, h, fromHex)
	})

	t.Run("base32 and hex of a second hash agree", func(t *testing.T) {
		h, err := ident.ParseHash(secondBase32)
		require.NoError(t, err)
		require.Equal(t, mustHex(t, secondHex), h[:])
		require.Equal(t, secondBase32, h.String())

		fromHex, err := ident.ParseHashHex(secondHex)
		require.NoError(t, err)
		require.Equal(t, secondBase32, fromHex.String())
	})

	t.Run("the zero hash is 52 A characters", func(t *testing.T) {
		var zero ident.Hash
		require.True(t, zero.IsZero())
		require.Equal(t, strings.Repeat("A", ident.Base32Len), zero.String())

		h, err := ident.ParseHash(strings.Repeat("A", ident.Base32Len))
		require.NoError(t, err)
		require.True(t, h.IsZero())
	})

	t.Run("lower case decodes to the same hash", func(t *testing.T) {
		h, err := ident.ParseHash(strings.ToLower(goldenBase32))
		require.NoError(t, err)
		require.Equal(t, goldenBase32, h.String())
	})

	t.Run("the signer of the example author identity", func(t *testing.T) {
		key, err := ident.DecodeBase32Strict(authorIdentity, ident.Size)
		require.NoError(t, err)
		require.Len(t, key, ident.Size)
		require.Equal(t, authorSigner, ident.Sum(key).String())
	})
}

func TestDecodeBase32(t *testing.T) {
	t.Run("a last character other than A or Q is lenient but not strict", func(t *testing.T) {
		s := goldenBase32[:ident.Base32Len-1] + "B"

		b, err := ident.DecodeBase32(s)
		require.NoError(t, err)
		require.Equal(t, mustHex(t, goldenHex), b[:])

		_, err = ident.DecodeBase32Strict(s, ident.Size)
		requireParseError(t, err, ident.ErrSyntax, ident.Base32Len-1)

		strict, err := ident.DecodeBase32Strict(goldenBase32, ident.Size)
		require.NoError(t, err)
		require.Equal(t, mustHex(t, goldenHex), strict)
	})

	t.Run("refuses the wrong length", func(t *testing.T) {
		_, err := ident.DecodeBase32(goldenBase32[:51])
		requireParseError(t, err, ident.ErrLength, 51)

		_, err = ident.DecodeBase32(goldenBase32 + "A")
		requireParseError(t, err, ident.ErrLength, 52)

		_, err = ident.DecodeBase32("")
		requireParseError(t, err, ident.ErrLength, 0)
	})

	for _, bad := range []string{"=", "-", "\n", "\r", " ", "1", "8", "\x00", "é"} {
		t.Run("refuses "+strings.ReplaceAll(strings.ReplaceAll(bad, "\n", `\n`), "\r", `\r`), func(t *testing.T) {
			at := 10
			s := goldenBase32[:at] + bad + goldenBase32[at+len(bad):]
			_, err := ident.DecodeBase32(s)
			requireParseError(t, err, ident.ErrSyntax, at)
		})
	}

	t.Run("refuses trailing padding and whitespace", func(t *testing.T) {
		for _, suffix := range []string{"=", "\n", " ", "\r\n"} {
			_, err := ident.DecodeBase32(goldenBase32[:51] + suffix)
			require.Error(t, err)
			_, err = ident.ParseHash(goldenBase32 + suffix)
			require.Error(t, err)
		}
	})

	t.Run("errors name what was parsed", func(t *testing.T) {
		_, err := ident.ParseHash("nope")
		var pe *ident.ParseError
		require.True(t, errors.As(err, &pe))
		require.Equal(t, "hash", pe.What)
		require.Equal(t, "nope", pe.Input)
		require.Equal(t, ident.ErrLength, pe.Unwrap())
		require.Contains(t, err.Error(), `"nope"`)
	})
}

func TestDecodeBase32Strict(t *testing.T) {
	t.Run("refuses lower case", func(t *testing.T) {
		_, err := ident.DecodeBase32Strict(strings.ToLower(goldenBase32), ident.Size)
		requireParseError(t, err, ident.ErrSyntax, 0)
	})

	t.Run("refuses padding", func(t *testing.T) {
		_, err := ident.DecodeBase32Strict("ME======", 1)
		requireParseError(t, err, ident.ErrLength, 2)
		_, err = ident.DecodeBase32Strict("M=", 1)
		requireParseError(t, err, ident.ErrSyntax, 1)
	})

	t.Run("requires exactly n bytes", func(t *testing.T) {
		_, err := ident.DecodeBase32Strict(goldenBase32, 31)
		requireParseError(t, err, ident.ErrLength, 50)
		_, err = ident.DecodeBase32Strict(goldenBase32, 33)
		requireParseError(t, err, ident.ErrLength, 52)
	})

	t.Run("refuses a negative length", func(t *testing.T) {
		_, err := ident.DecodeBase32Strict("", -1)
		requireParseError(t, err, ident.ErrRange, 0)
	})

	t.Run("refuses a length too large to encode", func(t *testing.T) {
		_, err := ident.DecodeBase32Strict(goldenBase32, math.MaxInt)
		requireParseError(t, err, ident.ErrRange, 0)
		_, err = ident.DecodeBase32Strict(goldenBase32, math.MaxInt/8+1)
		requireParseError(t, err, ident.ErrRange, 0)
	})

	t.Run("a large n with a short input is a length error", func(t *testing.T) {
		_, err := ident.DecodeBase32Strict(goldenBase32, 1<<40)
		requireParseError(t, err, ident.ErrLength, ident.Base32Len)
	})

	t.Run("round-trips every length", func(t *testing.T) {
		r := rand.New(rand.NewPCG(1, 2))
		for n := 0; n <= 40; n++ {
			b := make([]byte, n)
			for i := range b {
				b[i] = byte(r.Uint32())
			}
			s := ident.EncodeBase32(b)
			require.Equal(t, base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), s)

			got, err := ident.DecodeBase32Strict(s, n)
			require.NoError(t, err)
			require.Equal(t, b, got)
		}
	})
}

func TestAppendBase32(t *testing.T) {
	h, err := ident.ParseHash(goldenBase32)
	require.NoError(t, err)
	require.Equal(t, "x="+goldenBase32, string(ident.AppendBase32([]byte("x="), h[:])))
}

func TestParseHashHex(t *testing.T) {
	t.Run("accepts upper case", func(t *testing.T) {
		h, err := ident.ParseHashHex(strings.ToUpper(goldenHex))
		require.NoError(t, err)
		require.Equal(t, goldenHex, h.Hex())
	})

	t.Run("refuses a plus sign in a pair", func(t *testing.T) {
		_, err := ident.ParseHashHex("+f" + goldenHex[2:])
		requireParseError(t, err, ident.ErrSyntax, 0)
	})

	t.Run("refuses the wrong length", func(t *testing.T) {
		_, err := ident.ParseHashHex(goldenHex[:62])
		requireParseError(t, err, ident.ErrLength, 62)
		_, err = ident.ParseHashHex(goldenHex + "00")
		requireParseError(t, err, ident.ErrLength, 64)
	})

	t.Run("refuses a non-hex digit", func(t *testing.T) {
		_, err := ident.ParseHashHex(goldenHex[:5] + "g" + goldenHex[6:])
		requireParseError(t, err, ident.ErrSyntax, 5)
	})
}

func TestHashMethods(t *testing.T) {
	a, err := ident.ParseHash(goldenBase32)
	require.NoError(t, err)
	b, err := ident.ParseHash(secondBase32)
	require.NoError(t, err)

	t.Run("Compare orders by bytes", func(t *testing.T) {
		require.Equal(t, -1, a.Compare(b))
		require.Equal(t, 1, b.Compare(a))
		require.Equal(t, 0, a.Compare(a))
	})

	t.Run("text methods round-trip", func(t *testing.T) {
		text, err := a.MarshalText()
		require.NoError(t, err)
		require.Equal(t, goldenBase32, string(text))

		appended, err := a.AppendText([]byte("h:"))
		require.NoError(t, err)
		require.Equal(t, "h:"+goldenBase32, string(appended))

		var got ident.Hash
		require.NoError(t, got.UnmarshalText([]byte(strings.ToLower(goldenBase32))))
		require.Equal(t, a, got)
	})

	t.Run("UnmarshalText leaves the hash unchanged on failure", func(t *testing.T) {
		got := a
		err := got.UnmarshalText([]byte("-"))
		require.ErrorIs(t, err, ident.ErrSyntax)
		require.Equal(t, a, got)
	})
}

func TestHasher(t *testing.T) {
	data := []byte("atomic hashes are blake3")

	h := ident.NewHasher()
	require.Equal(t, ident.Sum(nil), h.Sum())

	n, err := h.Write(data[:7])
	require.NoError(t, err)
	require.Equal(t, 7, n)
	require.Equal(t, ident.Sum(data[:7]), h.Sum())

	_, err = h.Write(data[7:])
	require.NoError(t, err)
	require.Equal(t, ident.Sum(data), h.Sum())
	require.Equal(t, ident.Sum(data), h.Sum(), "Sum must not change the state")
}

func TestSumKnownAnswer(t *testing.T) {
	// BLAKE3 of the empty input, from the BLAKE3 specification's test vectors.
	require.Equal(t,
		"af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262",
		ident.Sum(nil).Hex())
}

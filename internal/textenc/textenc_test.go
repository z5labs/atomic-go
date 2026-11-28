package textenc_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/internal/textenc"
)

func TestBase58Vectors(t *testing.T) {
	for _, tc := range []struct {
		hex, text string
	}{
		{"", ""},
		{"61", "2g"},
		{"626262", "a3gV"},
		{"636363", "aPEr"},
		{"00000000000000000000", "1111111111"},
		{"0000287fb4cd", "11233QC4"},
		{"00eb15231dfceb60925886b67d065299925915aeb172c06647", "1NS17iag9jJgTHD1VXjvLCEnZuQ3rJDE9L"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			b, err := hex.DecodeString(tc.hex)
			require.NoError(t, err)

			require.Equal(t, tc.text, textenc.EncodeBase58(b))

			got, err := textenc.DecodeBase58(tc.text)
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, b, got)
		})
	}
}

func TestBase58RoundTrip(t *testing.T) {
	for _, b := range [][]byte{
		{0},
		{0, 0, 1},
		{1, 0},
		{0xff, 0xff, 0xff, 0xff},
		{0xed, 0x01, 0x00, 0xff},
	} {
		got, err := textenc.DecodeBase58(textenc.EncodeBase58(b))
		require.NoError(t, err)
		require.Equal(t, b, got)
	}
}

func TestDecodeBase58Refuses(t *testing.T) {
	for _, s := range []string{"0", "O", "I", "l", "2g=", " 2g", "2g\n", "+", "é"} {
		t.Run(s, func(t *testing.T) {
			_, err := textenc.DecodeBase58(s)
			require.ErrorIs(t, err, textenc.ErrInvalid)
		})
	}
}

func TestBase64URL(t *testing.T) {
	t.Run("round-trips without padding", func(t *testing.T) {
		for _, tc := range []struct {
			in   []byte
			want string
		}{
			{[]byte{}, ""},
			{[]byte("f"), "Zg"},
			{[]byte("fo"), "Zm8"},
			{[]byte("foo"), "Zm9v"},
			{[]byte{0xfb, 0xff}, "-_8"},
		} {
			require.Equal(t, tc.want, textenc.EncodeBase64URL(tc.in))
			got, err := textenc.DecodeBase64URL(tc.want)
			require.NoError(t, err)
			require.Equal(t, tc.in, got)
		}
	})

	t.Run("refuses non-canonical text", func(t *testing.T) {
		for name, s := range map[string]string{
			"one padding":        "Zm8=",
			"two padding":        "Zg==",
			"plus":               "+_8",
			"slash":              "-/8",
			"nonzero tail bits":  "Zh",
			"line feed":          "Zm\n9v",
			"carriage return":    "Zm9v\r",
			"impossible length":  "Z",
			"space":              "Zm 9v",
			"standard alphabet":  "+/8",
			"trailing line feed": "Zm9v\n",
		} {
			t.Run(name, func(t *testing.T) {
				_, err := textenc.DecodeBase64URL(s)
				require.ErrorIs(t, err, textenc.ErrInvalid)
			})
		}
	})
}

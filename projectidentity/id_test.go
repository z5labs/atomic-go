package projectidentity_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/projectidentity"
)

func TestNewProjectID(t *testing.T) {
	t.Run("a bytes.Reader gives a deterministic id", func(t *testing.T) {
		seed := bytes.Repeat([]byte{0x5a}, 32)
		seed[0], seed[31] = 0x01, 0xfe

		a, err := projectidentity.NewProjectID(bytes.NewReader(seed))
		require.NoError(t, err)
		b, err := projectidentity.NewProjectID(bytes.NewReader(seed))
		require.NoError(t, err)

		require.Equal(t, a, b)
		require.Equal(t, seed, a[:])
	})

	t.Run("reads exactly 32 bytes", func(t *testing.T) {
		r := bytes.NewReader(bytes.Repeat([]byte{7}, 40))
		_, err := projectidentity.NewProjectID(r)
		require.NoError(t, err)
		require.Equal(t, 8, r.Len())
	})

	t.Run("a nil reader is refused", func(t *testing.T) {
		_, err := projectidentity.NewProjectID(nil)
		require.ErrorIs(t, err, projectidentity.ErrInvalid)
	})

	t.Run("a short reader is an unexpected EOF", func(t *testing.T) {
		_, err := projectidentity.NewProjectID(bytes.NewReader(make([]byte, 31)))
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("an empty reader is an EOF", func(t *testing.T) {
		_, err := projectidentity.NewProjectID(bytes.NewReader(nil))
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("a read error is returned wrapped", func(t *testing.T) {
		boom := errors.New("boom")
		_, err := projectidentity.NewProjectID(iotest.ErrReader(boom))
		require.ErrorIs(t, err, boom)
	})

}

func TestParseProjectID(t *testing.T) {
	zero := strings.Repeat("A", ident.Base32Len)

	t.Run("the zero id is 52 A's", func(t *testing.T) {
		id, err := projectidentity.ParseProjectID(zero)
		require.NoError(t, err)
		require.True(t, id.IsZero())
		require.Equal(t, zero, id.String())
	})

	t.Run("round-trips", func(t *testing.T) {
		var id projectidentity.ProjectID
		for i := range id {
			id[i] = byte(i * 7)
		}
		got, err := projectidentity.ParseProjectID(id.String())
		require.NoError(t, err)
		require.Equal(t, id, got)
		require.False(t, got.IsZero())
	})

	for name, s := range map[string]string{
		"empty":                "",
		"lower case":           strings.Repeat("a", ident.Base32Len),
		"too short":            zero[1:],
		"too long":             zero + "A",
		"padding":              zero[:ident.Base32Len-1] + "=",
		"white space":          " " + zero[1:],
		"non-zero unused bits": zero[:ident.Base32Len-1] + "B",
		"outside the alphabet": zero[:ident.Base32Len-1] + "1",
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			_, err := projectidentity.ParseProjectID(s)
			require.ErrorIs(t, err, projectidentity.ErrInvalid)
			var pe *ident.ParseError
			require.ErrorAs(t, err, &pe)
		})
	}
}

func TestProjectIDText(t *testing.T) {
	var id projectidentity.ProjectID
	id[0] = 0xff

	b, err := id.MarshalText()
	require.NoError(t, err)
	require.Equal(t, id.String(), string(b))

	b, err = id.AppendText([]byte("x"))
	require.NoError(t, err)
	require.Equal(t, "x"+id.String(), string(b))

	var got projectidentity.ProjectID
	require.NoError(t, got.UnmarshalText([]byte(id.String())))
	require.Equal(t, id, got)

	err = got.UnmarshalText([]byte("nope"))
	require.ErrorIs(t, err, projectidentity.ErrInvalid)
	require.Equal(t, id, got, "unchanged on failure")
}

package wire_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/internal/wire"
)

func TestQueryAdd(t *testing.T) {
	t.Run("parameters are joined with &", func(t *testing.T) {
		var q wire.Query
		require.NoError(t, q.Add("view", "dev"))
		require.NoError(t, q.Add("state", ""))
		require.Equal(t, "view=dev&state=", q.String())
	})

	for _, tc := range []struct {
		name, value, want string
	}{
		{"space", "a b", "a%20b"},
		{"non-ASCII", "é", "%C3%A9"},
		{"percent, plus and equals stay raw", "%41+=", "%41+="},
		{"other raw bytes", "a/b?c~!$()*,;:@[]^`{|}\\", "a/b?c~!$()*,;:@[]^`{|}\\"},
		{"quote, apostrophe and angle brackets", `"'<>`, "%22%27%3C%3E"},
		{"inner C0 and DEL", "a\x00b\x1fc\x7fd", "a%00b%1Fc%7Fd"},
		{"high bytes", "a\x80\xff", "a%80%FF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var q wire.Query
			require.NoError(t, q.Add("view", tc.value))
			require.Equal(t, "view="+tc.want, q.String())
		})
	}
}

func TestQueryAddRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
	}{
		{"hash", "view", "feat#1"},
		{"ampersand", "view", "a&b"},
		{"tab", "view", "a\tb"},
		{"line feed", "view", "a\nb"},
		{"carriage return", "view", "a\rb"},
		{"leading space", "view", " a"},
		{"trailing space", "view", "a "},
		{"leading C0", "view", "\x01a"},
		{"trailing C0", "view", "a\x1f"},
		{"empty key", "", "a"},
		{"equals in key", "a=b", "c"},
		{"hash in key", "a#", "c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var q wire.Query
			require.NoError(t, q.Add("first", "1"))
			err := q.Add(tc.key, tc.value)
			require.ErrorIs(t, err, wire.ErrUnsafeQueryValue)
			require.Equal(t, "first=1", q.String(), "nothing is appended")
		})
	}
}

func TestQueryFlag(t *testing.T) {
	var q wire.Query
	q.Flag("views")
	require.Equal(t, "views", q.String())

	q.Flag("x")
	require.NoError(t, q.Add("view", "dev"))
	require.Equal(t, "views&x&view=dev", q.String())

	require.Panics(t, func() { q.Flag("") })
	require.Panics(t, func() { q.Flag("a&b") })
}

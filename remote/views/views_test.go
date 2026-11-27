package views_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/views"
)

// The rows of docs/remote/types.md#remoteviewinfo that both parsers accept.
var bothParse = []struct {
	line string
	want views.Info
}{
	{
		"dev\tshared\t-\t3\t2AAAAAAAAAAA",
		views.Info{Name: "dev", Scope: ident.ScopeShared, ChangeCount: 3, State: "2AAAAAAAAAAA"},
	},
	{
		"feature\tdraft\tdev\t2\tXYZ",
		views.Info{Name: "feature", Scope: ident.ScopeDraft, Parent: "dev", ChangeCount: 2, State: "XYZ"},
	},
	{
		"fresh\tshared\t-\t0\t-",
		views.Info{Name: "fresh", Scope: ident.ScopeShared},
	},
	{
		"dev\tshared\t-\t3\tAAAA\tSETID",
		views.Info{Name: "dev", Scope: ident.ScopeShared, ChangeCount: 3, State: "AAAA", SetID: "SETID"},
	},
	{
		// An empty state survives before a non-empty SetId.
		"fresh\tshared\t-\t0\t\tSETID",
		views.Info{Name: "fresh", Scope: ident.ScopeShared, SetID: "SETID"},
	},
	{
		// Scope is kept as sent, the line and each field are trimmed, an
		// empty parent and a '-' SetId read as none, and Rust's u64 parser
		// accepts a leading '+'.
		"  Dev \t DRAFT\t\t+7 \tS\t-\r\n",
		views.Info{Name: "Dev", Scope: "DRAFT", ChangeCount: 7, State: "S"},
	},
}

func TestParseInfoStrict(t *testing.T) {
	for _, tc := range bothParse {
		t.Run(strconv.Quote(tc.line), func(t *testing.T) {
			got, err := views.ParseInfoStrict(tc.line)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	for _, tc := range []struct {
		line   string
		reason string
	}{
		// The three refusals of docs/remote/endpoints/project.md#list_view_refs.
		{"dev\tshared\t-\tnot-a-count\t-", "invalid change count 'not-a-count'"},
		{"dev\tunknown\t-\t0\t-", "unsupported view scope 'unknown'"},
		{`{"workspace":"w"}`, "expected 5 or 6 tab-separated fields, got 1"},

		// Trimming the line drops the empty state's tab.
		{"fresh\tshared\t-\t0\t", "expected 5 or 6 tab-separated fields, got 4"},

		{"", "row is empty"},
		{" \t \r", "row is empty"},
		{"dev\tshared", "expected 5 or 6 tab-separated fields, got 2"},
		{"dev\tshared\t-\t0\t-\tS\textra", "expected 5 or 6 tab-separated fields, got 7"},
		{"x\t \tshared\t-\t0\t-", "unsupported view scope ''"},
		{"x\t\t-\t0\t-", "unsupported view scope ''"},
		{"x\tſhared\t-\t0\t-", "unsupported view scope 'ſhared'"},
		{"dev\tshared\t-\t\t-\tS", "invalid change count ''"},
		{"dev\tshared\t-\t-1\t-", "invalid change count '-1'"},
		{"dev\tshared\t-\t18446744073709551616\t-", "invalid change count '18446744073709551616'"},
	} {
		t.Run("refuses "+strconv.Quote(tc.line), func(t *testing.T) {
			_, err := views.ParseInfoStrict(tc.line)
			require.EqualError(t, err, tc.reason)
		})
	}
}

func TestParseInfo(t *testing.T) {
	for _, tc := range bothParse {
		t.Run(strconv.Quote(tc.line), func(t *testing.T) {
			got, ok := views.ParseInfo(tc.line)
			require.True(t, ok)
			require.Equal(t, tc.want, got)
		})
	}

	for _, tc := range []struct {
		line string
		want views.Info
	}{
		{"dev\tshared", views.Info{Name: "dev", Scope: ident.ScopeShared}},
		{"dev\tshared\t-\tx\t-", views.Info{Name: "dev", Scope: ident.ScopeShared}},
		{"dev\tshared\tmain", views.Info{Name: "dev", Scope: ident.ScopeShared, Parent: "main"}},
		{"dev\tpublic\t-\t4\t-", views.Info{Name: "dev", Scope: "public", ChangeCount: 4}},
		{
			"dev\tshared\t-\t3\tS\tID\tseventh\teighth",
			views.Info{Name: "dev", Scope: ident.ScopeShared, ChangeCount: 3, State: "S", SetID: "ID"},
		},
		{
			// Strict refuses an empty state at the end of the row; lenient
			// reads it as none.
			"fresh\tshared\t-\t0\t",
			views.Info{Name: "fresh", Scope: ident.ScopeShared},
		},
	} {
		t.Run(strconv.Quote(tc.line), func(t *testing.T) {
			got, ok := views.ParseInfo(tc.line)
			require.True(t, ok)
			require.Equal(t, tc.want, got)
		})
	}

	for _, line := range []string{
		"",
		" \r\n",
		"\tshared",
		"dev",
		"dev\t",
		"dev\t \t-\t0\t-",
		`{"workspace":"w"}`,
	} {
		t.Run("skips "+strconv.Quote(line), func(t *testing.T) {
			got, ok := views.ParseInfo(line)
			require.False(t, ok)
			require.Equal(t, views.Info{}, got)
		})
	}
}

func TestParseInventory(t *testing.T) {
	t.Run("parses every row and skips blank lines", func(t *testing.T) {
		body := "dev\tshared\t-\t3\t2AAAAAAAAAAA\n\n  \r\nfeature\tdraft\tdev\t2\tXYZ\r\nfresh\tshared\t-\t0\t-\n"
		got, err := views.ParseInventory(body)
		require.NoError(t, err)
		require.Equal(t, []views.Info{
			{Name: "dev", Scope: ident.ScopeShared, ChangeCount: 3, State: "2AAAAAAAAAAA"},
			{Name: "feature", Scope: ident.ScopeDraft, Parent: "dev", ChangeCount: 2, State: "XYZ"},
			{Name: "fresh", Scope: ident.ScopeShared},
		}, got)
	})

	t.Run("an empty body has no rows", func(t *testing.T) {
		for _, body := range []string{"", "\n", "\n \n"} {
			got, err := views.ParseInventory(body)
			require.NoError(t, err)
			require.Empty(t, got)
		}
	})

	t.Run("names the first refused row, blank lines counted", func(t *testing.T) {
		body := "dev\tshared\t-\t3\t2AAAAAAAAAAA\n\ndev\tunknown\t-\t0\t-\n{\"workspace\":\"w\"}\n"
		got, err := views.ParseInventory(body)
		require.Nil(t, got)

		var invErr *views.InventoryError
		require.ErrorAs(t, err, &invErr)
		require.Equal(t, 3, invErr.Line)
		require.EqualError(t, invErr.Err, "unsupported view scope 'unknown'")
		require.EqualError(t, err, "Invalid view inventory row 3: unsupported view scope 'unknown'")
		require.Equal(t, invErr.Err, errors.Unwrap(err))
	})

	t.Run("refuses an older server's JSON answer", func(t *testing.T) {
		_, err := views.ParseInventory(`{"workspace":"w"}`)
		var invErr *views.InventoryError
		require.ErrorAs(t, err, &invErr)
		require.Equal(t, 1, invErr.Line)
		require.EqualError(t, invErr.Err, "expected 5 or 6 tab-separated fields, got 1")
	})
}

func TestInventoryErrorIs(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := error(&views.InventoryError{Line: 2, Err: sentinel})
	require.ErrorIs(t, err, sentinel)
	require.EqualError(t, err, "Invalid view inventory row 2: sentinel")
}

func TestInfoAppendText(t *testing.T) {
	for _, tc := range []struct {
		info views.Info
		want string
	}{
		{
			views.Info{Name: "dev", Scope: ident.ScopeShared, ChangeCount: 3, State: "2AAAAAAAAAAA"},
			"dev\tshared\t-\t3\t2AAAAAAAAAAA",
		},
		{
			views.Info{Name: "feature", Scope: ident.ScopeDraft, Parent: "dev", ChangeCount: 2, State: "XYZ"},
			"feature\tdraft\tdev\t2\tXYZ",
		},
		{
			views.Info{Name: "fresh", Scope: ident.ScopeShared},
			"fresh\tshared\t-\t0\t-",
		},
		{
			views.Info{Name: "fresh", Scope: ident.ScopeShared, SetID: "SETID"},
			"fresh\tshared\t-\t0\t-\tSETID",
		},
		{
			views.Info{Name: "big", Scope: "Draft", Parent: "p", ChangeCount: 18446744073709551615, State: "S", SetID: "I"},
			"big\tDraft\tp\t18446744073709551615\tS\tI",
		},
		{
			// Scope is not checked, so a fake server can send a row only
			// the lenient parser accepts.
			views.Info{Name: "odd", Scope: "public"},
			"odd\tpublic\t-\t0\t-",
		},
	} {
		t.Run(tc.want, func(t *testing.T) {
			got, err := tc.info.AppendText([]byte("prefix:"))
			require.NoError(t, err)
			require.Equal(t, "prefix:"+tc.want, string(got))

			text, err := tc.info.MarshalText()
			require.NoError(t, err)
			require.Equal(t, tc.want, string(text))

			lenient, ok := views.ParseInfo(string(text))
			require.True(t, ok)
			require.Equal(t, tc.info, lenient)

			if tc.info.Scope != "public" {
				strict, err := views.ParseInfoStrict(string(text))
				require.NoError(t, err)
				require.Equal(t, tc.info, strict)
			}
		})
	}

	for _, tc := range []struct {
		name string
		info views.Info
	}{
		{"empty name", views.Info{Scope: ident.ScopeShared}},
		{"empty scope", views.Info{Name: "dev"}},
		{"tab in name", views.Info{Name: "d\tev", Scope: ident.ScopeShared}},
		{"LF in parent", views.Info{Name: "dev", Scope: ident.ScopeShared, Parent: "a\nb"}},
		{"CR in state", views.Info{Name: "dev", Scope: ident.ScopeShared, State: "S\r"}},
		{"space around scope", views.Info{Name: "dev", Scope: " shared"}},
		{"space around set id", views.Info{Name: "dev", Scope: ident.ScopeShared, SetID: "I "}},
		{"dash parent", views.Info{Name: "dev", Scope: ident.ScopeShared, Parent: "-"}},
		{"dash state", views.Info{Name: "dev", Scope: ident.ScopeShared, State: "-"}},
		{"dash set id", views.Info{Name: "dev", Scope: ident.ScopeShared, SetID: "-"}},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			got, err := tc.info.AppendText([]byte("prefix:"))
			require.ErrorIs(t, err, views.ErrUnwritable)
			require.Equal(t, "prefix:", string(got))
		})
	}

	t.Run("a view named '-' is written", func(t *testing.T) {
		info := views.Info{Name: "-", Scope: ident.ScopeShared}
		text, err := info.MarshalText()
		require.NoError(t, err)
		got, err := views.ParseInfoStrict(string(text))
		require.NoError(t, err)
		require.Equal(t, info, got)
	})
}

func TestInventoryRoundTrip(t *testing.T) {
	infos := []views.Info{
		{Name: "dev", Scope: ident.ScopeShared, ChangeCount: 3, State: "2AAAAAAAAAAA", SetID: "SET"},
		{Name: "feature", Scope: ident.ScopeDraft, Parent: "dev", ChangeCount: 2, State: "XYZ"},
		{Name: "fresh", Scope: ident.ScopeShared},
	}
	var body []byte
	for _, info := range infos {
		var err error
		body, err = info.AppendText(body)
		require.NoError(t, err)
		body = append(body, '\n')
	}
	got, err := views.ParseInventory(string(body))
	require.NoError(t, err)
	require.Equal(t, infos, got)
}

package canonical_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/canonical"
)

func canonicalOf(t *testing.T, in string) string {
	t.Helper()
	d, err := canonical.Admit([]byte(in))
	require.NoError(t, err)
	b, err := d.Canonical()
	require.NoError(t, err)
	return string(b)
}

func TestRFC8785Sorting(t *testing.T) {
	// RFC 8785 §3.2.3, with every name written as a \u escape.
	const in = `{
  "\u20ac": "Euro Sign",
  "\r": "Carriage Return",
  "\ufb33": "Hebrew Letter Dalet With Dagesh",
  "1": "One",
  "\ud83d\ude00": "Emoji: Grinning Face",
  "\u0080": "Control",
  "\u00f6": "Latin Small Letter O With Diaeresis"
}`
	// Sorted by UTF-16 code units: CR, "1", U+0080, U+00F6, U+20AC,
	// U+1F600 (the surrogates D83D DE00), U+FB33. Only CR is escaped.
	const want = `{"\r":"Carriage Return",` +
		`"1":"One",` +
		"\"\u0080\":\"Control\"," +
		"\"\u00f6\":\"Latin Small Letter O With Diaeresis\"," +
		"\"\u20ac\":\"Euro Sign\"," +
		"\"\U0001F600\":\"Emoji: Grinning Face\"," +
		"\"\ufb33\":\"Hebrew Letter Dalet With Dagesh\"}"
	require.Equal(t, want, canonicalOf(t, in))
}

func TestRFC8785Escaping(t *testing.T) {
	// RFC 8785 §3.2.2 without its "numbers" member, which admission
	// refuses.
	const in = `{
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`
	const want = "{\"literals\":[null,true,false],\"string\":\"\u20ac" + `$\u000f\nA'B\"\\\\\"/"}`
	require.Equal(t, want, canonicalOf(t, in))
}

func TestCanonicalLeavesJavaScriptAndHTMLCharactersRaw(t *testing.T) {
	got := canonicalOf(t, `{"s":"\u2028<&>"}`)
	require.Equal(t, "{\"s\":\"\u2028<&>\"}", got)
}

func TestCanonicalIsStable(t *testing.T) {
	once := canonicalOf(t, `{ "b" : [ 1 , { "z":true, "a":null } ], "a" : -0 }`)
	require.Equal(t, `{"a":0,"b":[1,{"a":null,"z":true}]}`, once)
	require.Equal(t, once, canonicalOf(t, once))
}

func TestAdmitRefuses(t *testing.T) {
	testCases := []struct {
		name string
		in   string
	}{
		{name: "duplicate name", in: `{"a":1,"a":2}`},
		{name: "nested duplicate name", in: `{"a":{"b":1,"b":1}}`},
		{name: "invalid UTF-8", in: "{\"a\":\"\xff\"}"},
		{name: "invalid UTF-8 in a name", in: "{\"\xc3\":1}"},
		{name: "lone surrogate escape", in: `{"a":"\ud800"}`},
		{name: "fraction", in: `{"a":1.5}`},
		{name: "exponent", in: `{"a":1e2}`},
		{name: "integer above MaxInteger", in: `{"a":9007199254740992}`},
		{name: "integer below -MaxInteger", in: `{"a":-9007199254740992}`},
		{name: "integer beyond int64", in: `{"a":[99999999999999999999]}`},
		{name: "top-level array", in: `[]`},
		{name: "top-level string", in: `"a"`},
		{name: "top-level null", in: `null`},
		{name: "empty input", in: ``},
		{name: "whitespace only", in: ` `},
		{name: "trailing value", in: `{"a":1} {}`},
		{name: "trailing garbage", in: `{"a":1}x`},
		{name: "unterminated object", in: `{"a":1`},
		{name: "depth 129", in: nested(129)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := canonical.Admit([]byte(tc.in))
			require.ErrorIs(t, err, canonical.ErrNotAdmitted)
		})
	}
}

func TestAdmitAccepts(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{name: "MaxInteger", in: `{"a":9007199254740991}`, want: `{"a":9007199254740991}`},
		{name: "-MaxInteger", in: `{"a":-9007199254740991}`, want: `{"a":-9007199254740991}`},
		{name: "empty object", in: ` {} `, want: `{}`},
		{name: "depth 128", in: nested(128), want: nested(128)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, canonicalOf(t, tc.in))
		})
	}
}

// nested returns an object whose nesting depth is depth: the top-level
// object, then depth-1 arrays inside its "a" member.
func nested(depth int) string {
	return `{"a":` + strings.Repeat("[", depth-1) + strings.Repeat("]", depth-1) + `}`
}

func TestMembers(t *testing.T) {
	d, err := canonical.Admit([]byte(`{"name":"alice","n":7,"obj":{ "y":1, "x":2 }}`))
	require.NoError(t, err)

	require.Equal(t, 3, d.Len())
	require.True(t, d.Has("name"))
	require.False(t, d.Has("missing"))

	m, ok := d.Member("obj")
	require.True(t, ok)
	require.Equal(t, `{"x":2,"y":1}`, string(m))
	_, ok = d.Member("missing")
	require.False(t, ok)

	s, ok := d.Text("name")
	require.True(t, ok)
	require.Equal(t, "alice", s)
	_, ok = d.Text("n")
	require.False(t, ok)
	_, ok = d.Text("missing")
	require.False(t, ok)

	var n int
	require.NoError(t, d.Decode("n", &n))
	require.Equal(t, 7, n)
	require.ErrorIs(t, d.Decode("missing", &n), canonical.ErrNoMember)
	require.Error(t, d.Decode("name", &n))
}

func TestMemberReturnsACopy(t *testing.T) {
	d, err := canonical.Admit([]byte(`{"a":"x"}`))
	require.NoError(t, err)

	m, ok := d.Member("a")
	require.True(t, ok)
	m[1] = 'y'

	again, ok := d.Member("a")
	require.True(t, ok)
	require.Equal(t, `"x"`, string(again))
}

func TestWithAndWithoutDoNotMutate(t *testing.T) {
	d, err := canonical.Admit([]byte(`{"a":1,"b":2}`))
	require.NoError(t, err)

	with, err := d.With("c", []string{"x"})
	require.NoError(t, err)
	replaced, err := d.With("a", jsontext.Value(`{ "k" : true }`))
	require.NoError(t, err)
	without := d.Without("a", "missing")

	b, err := d.Canonical()
	require.NoError(t, err)
	require.Equal(t, `{"a":1,"b":2}`, string(b))

	b, err = with.Canonical()
	require.NoError(t, err)
	require.Equal(t, `{"a":1,"b":2,"c":["x"]}`, string(b))

	b, err = replaced.Canonical()
	require.NoError(t, err)
	require.Equal(t, `{"a":{"k":true},"b":2}`, string(b))

	b, err = without.Canonical()
	require.NoError(t, err)
	require.Equal(t, `{"b":2}`, string(b))
}

func TestWithRefuses(t *testing.T) {
	d, err := canonical.Admit([]byte(`{"a":1}`))
	require.NoError(t, err)

	testCases := []struct {
		name string
		v    any
	}{
		{name: "fraction", v: 1.5},
		{name: "integer above MaxInteger", v: int64(canonical.MaxInteger + 1)},
		{name: "duplicate name", v: jsontext.Value(`{"x":1,"x":2}`)},
		// A member value sits one level below the top-level object, so 128
		// arrays make depth 129.
		{name: "depth 129", v: jsontext.Value(strings.Repeat("[", 128) + strings.Repeat("]", 128))},
	}
	_, err = d.With("\xff", 1)
	require.ErrorIs(t, err, canonical.ErrNotAdmitted)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := d.With("b", tc.v)
			require.ErrorIs(t, err, canonical.ErrNotAdmitted)

			b, err := d.Canonical()
			require.NoError(t, err)
			require.Equal(t, `{"a":1}`, string(b))
		})
	}

	_, err = d.With("b", jsontext.Value(strings.Repeat("[", 127)+strings.Repeat("]", 127)))
	require.NoError(t, err)
	_, err = d.With("b", int64(-canonical.MaxInteger))
	require.NoError(t, err)
}

func TestFromValue(t *testing.T) {
	type doc struct {
		Type  string `json:"@type"`
		Count int    `json:"count"`
	}
	d, err := canonical.FromValue(doc{Type: "AgentDelegation", Count: 2})
	require.NoError(t, err)
	b, err := d.Canonical()
	require.NoError(t, err)
	require.Equal(t, `{"@type":"AgentDelegation","count":2}`, string(b))

	_, err = canonical.FromValue([]int{1})
	require.ErrorIs(t, err, canonical.ErrNotAdmitted)
	_, err = canonical.FromValue(map[string]float64{"x": 0.5})
	require.ErrorIs(t, err, canonical.ErrNotAdmitted)
	_, err = canonical.FromValue(make(chan int))
	require.Error(t, err)
}

func TestJSONRoundTrip(t *testing.T) {
	type envelope struct {
		Doc canonical.Document `json:"doc"`
	}
	var e envelope
	require.NoError(t, json.Unmarshal([]byte(`{"doc":{"z":"\u00e9","a":[1,2]}}`), &e))
	require.Equal(t, 2, e.Doc.Len())

	b, err := e.Doc.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, "{\"a\":[1,2],\"z\":\"\u00e9\"}", string(b))

	b, err = json.Marshal(e)
	require.NoError(t, err)
	require.Equal(t, "{\"doc\":{\"a\":[1,2],\"z\":\"\u00e9\"}}", string(b))

	var refused envelope
	err = json.Unmarshal([]byte(`{"doc":{"a":1.5}}`), &refused)
	require.ErrorIs(t, err, canonical.ErrNotAdmitted)
	require.Equal(t, 0, refused.Doc.Len())

	var null canonical.Document
	require.ErrorIs(t, null.UnmarshalJSON([]byte(`null`)), canonical.ErrNotAdmitted)
}

func TestZeroDocumentIsEmptyObject(t *testing.T) {
	var d canonical.Document
	b, err := d.Canonical()
	require.NoError(t, err)
	require.Equal(t, `{}`, string(b))
	require.False(t, d.Has("a"))

	d2, err := d.With("a", "b")
	require.NoError(t, err)
	b, err = d2.Canonical()
	require.NoError(t, err)
	require.Equal(t, `{"a":"b"}`, string(b))
}

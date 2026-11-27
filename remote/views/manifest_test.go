package views_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/views"
)

// fold is the test's own Merkle fold, written out from
// docs/remote/types.md#merkle-state rather than taken from ident: start from
// 32 zero bytes and hash state || change for each change, in order.
func fold(log ...ident.Hash) ident.Hash {
	var state ident.Hash
	for _, h := range log {
		buf := append(append([]byte{}, state[:]...), h[:]...)
		state = ident.Sum(buf)
	}
	return state
}

func requireManifestError(t *testing.T, err error, kind views.ManifestErrorKind, line int, text string) {
	t.Helper()
	var me *views.ManifestError
	require.True(t, errors.As(err, &me), "error %v is not a *ManifestError", err)
	require.Equal(t, views.ManifestError{Kind: kind, Line: line, Text: text}, *me)
}

func TestManifestMarshalTextEmptyRoot(t *testing.T) {
	m := views.Manifest{Name: "dev", Scope: ident.ScopeShared}

	got, err := m.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "dev\tshared\t-\t-\n", string(got))

	back, err := views.ParseManifest(string(got))
	require.NoError(t, err)
	require.Equal(t, m, back)
	require.Nil(t, back.Fold())
	require.NoError(t, back.Verify())
}

func TestManifestDraftRoundTrip(t *testing.T) {
	c1 := ident.Sum([]byte("change one"))
	c2 := ident.Sum([]byte("change two"))
	state := fold(c1, c2)
	m := views.Manifest{
		Name:    "feature",
		Scope:   ident.ScopeDraft,
		Parent:  "dev",
		State:   &state,
		Changes: []ident.Hash{c1, c2},
	}

	got, err := m.MarshalText()
	require.NoError(t, err)
	require.Equal(t,
		"feature\tdraft\tdev\t"+state.String()+"\n"+c1.String()+"\n"+c2.String()+"\n",
		string(got))

	back, err := views.ParseManifest(string(got))
	require.NoError(t, err)
	require.Equal(t, m, back)

	f := back.Fold()
	require.NotNil(t, f)
	require.Equal(t, state, *f)
	require.NoError(t, back.Verify())
}

func TestManifestMarshalTextLowersScope(t *testing.T) {
	got, err := views.Manifest{Name: "dev", Scope: "SHARED"}.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "dev\tshared\t-\t-\n", string(got))
}

func TestManifestMarshalTextRefuses(t *testing.T) {
	for _, m := range []views.Manifest{
		{Scope: ident.ScopeShared},
		{Name: "dev"},
		{Name: "dev", Scope: "public"},
		{Name: "a\tb", Scope: ident.ScopeShared},
		{Name: "dev\n", Scope: ident.ScopeShared},
		{Name: " dev", Scope: ident.ScopeShared},
		{Name: "dev", Scope: ident.ScopeShared, Parent: "p\rq"},
		{Name: "dev", Scope: ident.ScopeShared, Parent: "-"},
	} {
		t.Run(strconv.Quote(m.Name)+"/"+strconv.Quote(string(m.Scope))+"/"+strconv.Quote(m.Parent), func(t *testing.T) {
			_, err := m.MarshalText()
			require.ErrorIs(t, err, views.ErrUnwritable)
		})
	}
}

func TestParseManifest(t *testing.T) {
	c1 := ident.Sum([]byte("one"))
	c2 := ident.Sum([]byte("two"))
	state := fold(c1, c2)

	for _, tc := range []struct {
		name string
		text string
		want views.Manifest
	}{
		{
			name: "five-field header",
			text: "dev\tshared\t-\t-\textra\n",
			want: views.Manifest{Name: "dev", Scope: ident.ScopeShared},
		},
		{
			name: "empty parent reads as none",
			text: "feature\tdraft\t\t-\n",
			want: views.Manifest{Name: "feature", Scope: ident.ScopeDraft},
		},
		{
			name: "empty state reads as none",
			text: "dev\tshared\t-\t\n",
			want: views.Manifest{Name: "dev", Scope: ident.ScopeShared},
		},
		{
			name: "scope in any case, fields trimmed, blank lines skipped",
			text: "\n \r\n  dev \t DRAFT \t main \t " + state.String() + " \r\n\n  " + c1.String() + " \r\n\n" + c2.String(),
			want: views.Manifest{Name: "dev", Scope: ident.ScopeDraft, Parent: "main", State: &state, Changes: []ident.Hash{c1, c2}},
		},
		{
			name: "no trailing newline",
			text: "dev\tshared\t-\t-",
			want: views.Manifest{Name: "dev", Scope: ident.ScopeShared},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := views.ParseManifest(tc.text)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseManifestErrors(t *testing.T) {
	good := ident.Sum([]byte("good")).String()

	for _, tc := range []struct {
		name string
		text string
		kind views.ManifestErrorKind
		line int
		etxt string
	}{
		{"Empty", "", views.ManifestEmpty, 0, ""},
		{"Empty/blank lines", "\n  \r\n\t\n", views.ManifestEmpty, 0, ""},
		{"MalformedHeader/three fields", "dev\tshared\t-\n", views.ManifestMalformedHeader, 1, "dev\tshared\t-"},
		{"MalformedHeader/empty name", " \tshared\t-\t-\n", views.ManifestMalformedHeader, 1, " \tshared\t-\t-"},
		{"MalformedHeader/empty scope", "dev\t \t-\t-\n", views.ManifestMalformedHeader, 1, "dev\t \t-\t-"},
		{"MalformedHeader/after blank lines", "\n\ndev\n", views.ManifestMalformedHeader, 1, "dev"},
		{"UnknownScope", "dev\tpublic\t-\t-\n", views.ManifestUnknownScope, 1, "public"},
		{"UnknownScope/not ASCII folded", "dev\tſhared\t-\t-\n", views.ManifestUnknownScope, 1, "ſhared"},
		{"InvalidState", "dev\tshared\t-\tnot base32!\n", views.ManifestInvalidState, 1, "not base32!"},
		{"InvalidHash", "dev\tshared\t-\t-\n" + good + "\nnope\n", views.ManifestInvalidHash, 3, "nope"},
		{
			"InvalidHash/blank line counted",
			"\n\ndev\tshared\t-\t-\n" + good + "\n\n  \n  bad!  \n",
			views.ManifestInvalidHash, 5, "bad!",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := views.ParseManifest(tc.text)
			requireManifestError(t, err, tc.kind, tc.line, tc.etxt)
		})
	}
}

func TestManifestVerifyStateMismatch(t *testing.T) {
	c1 := ident.Sum([]byte("one"))
	c2 := ident.Sum([]byte("two"))
	right := fold(c1, c2)
	wrong := fold(c2, c1)

	for _, tc := range []struct {
		name string
		m    views.Manifest
		text string
	}{
		{
			"wrong order",
			views.Manifest{Name: "dev", Scope: ident.ScopeShared, State: &wrong, Changes: []ident.Hash{c1, c2}},
			"declared " + wrong.String() + ", computed " + right.String(),
		},
		{
			"state on an empty log",
			views.Manifest{Name: "dev", Scope: ident.ScopeShared, State: &right},
			"declared " + right.String() + ", computed -",
		},
		{
			"no state on a log",
			views.Manifest{Name: "dev", Scope: ident.ScopeShared, Changes: []ident.Hash{c1}},
			"declared -, computed " + fold(c1).String(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Verify()
			requireManifestError(t, err, views.ManifestStateMismatch, 0, tc.text)
		})
	}
}

func TestManifestErrorString(t *testing.T) {
	require.Equal(t, "views: manifest is empty", (&views.ManifestError{Kind: views.ManifestEmpty}).Error())
	require.Equal(t,
		`views: manifest line 3: invalid change hash "nope"`,
		(&views.ManifestError{Kind: views.ManifestInvalidHash, Line: 3, Text: "nope"}).Error())
	require.Equal(t, "InvalidHash", views.ManifestInvalidHash.String())
	require.Equal(t, "ManifestErrorKind(0)", views.ManifestErrorKind(0).String())
}

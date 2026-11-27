package views_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"lukechampine.com/blake3"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/views"
)

// golden is the shared root view with one change from
// docs/remote/endpoints/project.md#viewsnapshot.
const golden = `{"scope":"shared","own_changes":["C1"],"own_set_id":"SETID","merkle_state":"MERKLE"}`

func TestSnapshotGolden(t *testing.T) {
	s := views.Snapshot{
		Scope:       ident.ScopeShared,
		OwnChanges:  []string{"C1"},
		OwnSetID:    "SETID",
		MerkleState: "MERKLE",
	}
	require.Equal(t, golden, string(s.MarshalCanonical()))

	sum := blake3.Sum256([]byte(golden))
	require.Equal(t, hex.EncodeToString(sum[:]), s.Key())

	back, err := views.ParseSnapshot([]byte(golden))
	require.NoError(t, err)
	require.Equal(t, s, back)
}

func TestSnapshotMarshalCanonicalAllMembers(t *testing.T) {
	s := views.Snapshot{
		Scope:       ident.ScopeDraft,
		ParentView:  "dev",
		Prev:        []string{"aa", "bb"},
		OwnSetID:    "S",
		MerkleState: "M",
	}
	require.Equal(t,
		`{"scope":"draft","parent_view":"dev","prev":["aa","bb"],"own_changes":[],"own_set_id":"S"`+
			`,"merkle_state":"M"}`,
		string(s.MarshalCanonical()))
}

func TestSnapshotMarshalCanonicalEscapes(t *testing.T) {
	s := views.Snapshot{
		Scope:      ident.ScopeShared,
		ParentView: "<&>  \n\x01\"\\\b\t\f\r\x1f\x7fé",
		OwnSetID:   "S",
	}
	require.Equal(t,
		`{"scope":"shared","parent_view":"<&>`+"  "+`\n\u0001\"\\\b\t\f\r\u001f`+"\x7fé"+`","own_changes":[],"own_set_id":"S"}`,
		string(s.MarshalCanonical()))
}

func TestSnapshotMarshalCanonicalInvalidUTF8(t *testing.T) {
	s := views.Snapshot{Scope: ident.ScopeShared, ParentView: "a\xffb", OwnSetID: "S"}
	require.Equal(t,
		`{"scope":"shared","parent_view":"a`+"�"+`b","own_changes":[],"own_set_id":"S"}`,
		string(s.MarshalCanonical()))
}

func TestParseSnapshotRoundTrip(t *testing.T) {
	for _, in := range []string{
		golden,
		`{"scope":"shared","own_changes":[],"own_set_id":"AAAA"}`,
		`{"scope":"draft","parent_view":"dev","prev":["0f","1e"],"own_changes":["A","B"],"own_set_id":"S","merkle_state":"M"}`,
		`{"scope":"Draft","own_changes":[],"own_set_id":"S"}`,
		`{"scope":"shared","parent_view":"<&>` + " " + `\n\u0001\"\\","own_changes":[],"own_set_id":"S"}`,
	} {
		t.Run(in, func(t *testing.T) {
			s, err := views.ParseSnapshot([]byte(in))
			require.NoError(t, err)
			require.Equal(t, in, string(s.MarshalCanonical()))
		})
	}
}

func TestParseSnapshotLenient(t *testing.T) {
	s, err := views.ParseSnapshot([]byte(` { "own_set_id" : "S", "extra": {"x": 1}, "prev": null,
		"merkle_state": null, "parent_view": null, "own_changes": [], "scope": "shared" } `))
	require.NoError(t, err)
	require.Equal(t, views.Snapshot{Scope: ident.ScopeShared, OwnSetID: "S"}, s)
	require.Equal(t, `{"scope":"shared","own_changes":[],"own_set_id":"S"}`, string(s.MarshalCanonical()))
}

func TestParseSnapshotErrors(t *testing.T) {
	for _, in := range []string{
		``,
		`null`,
		`[]`,
		`{"scope":"shared","own_changes":[]}`,
		`{"own_changes":[],"own_set_id":"S"}`,
		`{"scope":"shared","own_set_id":"S"}`,
		`{"scope":null,"own_changes":[],"own_set_id":"S"}`,
		`{"Scope":"shared","own_changes":[],"own_set_id":"S"}`,
		`{"scope":1,"own_changes":[],"own_set_id":"S"}`,
		`{"scope":"shared","own_changes":"A","own_set_id":"S"}`,
		`{"scope":"shared","own_changes":[],"own_set_id":"S"} x`,
		"{\"scope\":\"sh\xffared\",\"own_changes\":[],\"own_set_id\":\"S\"}",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := views.ParseSnapshot([]byte(in))
			require.ErrorIs(t, err, views.ErrInvalidSnapshot)
		})
	}
}

func TestSnapshotIsDraft(t *testing.T) {
	require.True(t, views.Snapshot{Scope: "draft"}.IsDraft())
	require.False(t, views.Snapshot{Scope: "Draft"}.IsDraft())
	require.False(t, views.Snapshot{Scope: "shared"}.IsDraft())
	require.False(t, views.Snapshot{}.IsDraft())
}

func TestFromManifest(t *testing.T) {
	c1 := ident.Sum([]byte("one"))
	c2 := ident.Sum([]byte("two"))
	state := fold(c1, c2)

	t.Run("shared root, empty log, first snapshot", func(t *testing.T) {
		s := views.FromManifest(views.Manifest{Name: "dev", Scope: ident.ScopeShared}, "")
		require.Equal(t, views.Snapshot{Scope: ident.ScopeShared, OwnSetID: ident.SetID{}.String()}, s)
		require.Equal(t,
			`{"scope":"shared","own_changes":[],"own_set_id":"`+ident.SetID{}.String()+`"}`,
			string(s.MarshalCanonical()))
	})

	t.Run("draft with a log and a predecessor", func(t *testing.T) {
		m := views.Manifest{
			Name:    "feature",
			Scope:   ident.ScopeDraft,
			Parent:  "dev",
			State:   &state,
			Changes: []ident.Hash{c1, c2},
		}
		s := views.FromManifest(m, "abc123")
		require.Equal(t, views.Snapshot{
			Scope:       ident.ScopeDraft,
			ParentView:  "dev",
			Prev:        []string{"abc123"},
			OwnChanges:  []string{c1.String(), c2.String()},
			OwnSetID:    ident.SetIDOf([]ident.Hash{c1, c2}).String(),
			MerkleState: state.String(),
		}, s)

		back, err := s.Manifest("feature")
		require.NoError(t, err)
		require.Equal(t, m, back)
		require.NoError(t, back.Verify())
	})

	t.Run("scope Draft writes draft", func(t *testing.T) {
		s := views.FromManifest(views.Manifest{Name: "dev", Scope: "Draft"}, "")
		require.Equal(t, ident.ScopeDraft, s.Scope)
		require.True(t, s.IsDraft())
	})

	t.Run("any other scope writes shared", func(t *testing.T) {
		s := views.FromManifest(views.Manifest{Name: "dev", Scope: "SHARED"}, "")
		require.Equal(t, ident.ScopeShared, s.Scope)
	})
}

func TestSnapshotManifest(t *testing.T) {
	c1 := ident.Sum([]byte("one"))

	t.Run("non-draft scope reads as shared", func(t *testing.T) {
		m, err := views.Snapshot{Scope: "Draft", OwnChanges: []string{c1.String()}}.Manifest("renamed")
		require.NoError(t, err)
		require.Equal(t, views.Manifest{Name: "renamed", Scope: ident.ScopeShared, Changes: []ident.Hash{c1}}, m)
	})

	t.Run("invalid state", func(t *testing.T) {
		_, err := views.Snapshot{Scope: "shared", MerkleState: "MERKLE!"}.Manifest("dev")
		requireManifestError(t, err, views.ManifestInvalidState, 1, "MERKLE!")
	})

	t.Run("invalid change", func(t *testing.T) {
		_, err := views.Snapshot{Scope: "shared", OwnChanges: []string{c1.String(), "C1"}}.Manifest("dev")
		requireManifestError(t, err, views.ManifestInvalidHash, 3, "C1")
	})
}

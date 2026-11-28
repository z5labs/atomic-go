package delegation_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/delegation"
)

var allPermissions = []delegation.Permission{
	delegation.PermissionRead,
	delegation.PermissionRecord,
	delegation.PermissionPush,
	delegation.PermissionPull,
	delegation.PermissionManageViews,
	delegation.PermissionManageTags,
	delegation.PermissionAdmin,
	delegation.PermissionFull,
}

func TestImplicationMatrix(t *testing.T) {
	const (
		r = delegation.PermissionRead
		c = delegation.PermissionRecord
		u = delegation.PermissionPush
		l = delegation.PermissionPull
		v = delegation.PermissionManageViews
		g = delegation.PermissionManageTags
		a = delegation.PermissionAdmin
		f = delegation.PermissionFull
	)
	// implied[p] is every permission p implies (docs/remote/auth.md#scope).
	implied := map[delegation.Permission][]delegation.Permission{
		r: {r},
		c: {c},
		u: {u},
		l: {l},
		v: {v},
		g: {g},
		a: {r, v, g, a},
		f: allPermissions,
	}
	for _, p := range allPermissions {
		for _, q := range allPermissions {
			want := false
			for _, i := range implied[p] {
				want = want || i == q
			}
			require.Equal(t, want, p.Implies(q), "%s implies %s", p, q)
		}
	}
}

func TestImpliesUnknown(t *testing.T) {
	unknown := delegation.Permission("write")
	require.False(t, unknown.Implies(unknown))
	require.False(t, delegation.PermissionFull.Implies(unknown))
	require.False(t, delegation.PermissionFull.Implies(""))
	require.False(t, unknown.Implies(delegation.PermissionRead))
}

func TestParsePermission(t *testing.T) {
	for in, want := range map[string]delegation.Permission{
		"read":          delegation.PermissionRead,
		"record":        delegation.PermissionRecord,
		"push":          delegation.PermissionPush,
		"pull":          delegation.PermissionPull,
		"manage_views":  delegation.PermissionManageViews,
		"manage-views":  delegation.PermissionManageViews,
		"manage_stacks": delegation.PermissionManageViews,
		"manage-stacks": delegation.PermissionManageViews,
		"manage_tags":   delegation.PermissionManageTags,
		"Manage-Tags":   delegation.PermissionManageTags,
		"admin":         delegation.PermissionAdmin,
		" FULL\t":       delegation.PermissionFull,
	} {
		got, err := delegation.ParsePermission(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "write", "manage views", "manage__views", "read,push"} {
		_, err := delegation.ParsePermission(in)
		require.ErrorIs(t, err, delegation.ErrUnknownPermission, in)
	}
}

func TestPermissionText(t *testing.T) {
	for _, p := range allPermissions {
		b, err := p.MarshalText()
		require.NoError(t, err)
		require.Equal(t, string(p), string(b))

		var got delegation.Permission
		require.NoError(t, got.UnmarshalText(b))
		require.Equal(t, p, got)
	}

	// Only the eight wire values: none of ParsePermission's leniency.
	for _, in := range []string{"", "Read", "manage-views", "manage_stacks", " read", "write"} {
		got := delegation.PermissionPush
		err := got.UnmarshalText([]byte(in))
		require.ErrorIs(t, err, delegation.ErrUnknownPermission, in)
		require.Equal(t, delegation.PermissionPush, got, "unchanged on failure")
	}

	_, err := delegation.Permission("write").MarshalText()
	require.ErrorIs(t, err, delegation.ErrUnknownPermission)
	_, err = delegation.Permission("").MarshalText()
	require.ErrorIs(t, err, delegation.ErrUnknownPermission)

	b, err := json.Marshal([]delegation.Permission{delegation.PermissionManageTags, delegation.PermissionFull})
	require.NoError(t, err)
	require.Equal(t, `["manage_tags","full"]`, string(b))
}

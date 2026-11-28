package delegation_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/delegation"
)

func TestNewScope(t *testing.T) {
	require.Equal(t, delegation.Scope{Permissions: []delegation.Permission{delegation.PermissionRead}}, delegation.NewScope())

	s := delegation.NewScope(delegation.PermissionPush, delegation.PermissionRecord, delegation.PermissionPush)
	require.Equal(t, []delegation.Permission{delegation.PermissionPush, delegation.PermissionRecord}, s.Permissions)
	require.Nil(t, s.Servers)

	perms := []delegation.Permission{delegation.PermissionPull}
	s = delegation.NewScope(perms...)
	perms[0] = delegation.PermissionFull
	require.Equal(t, []delegation.Permission{delegation.PermissionPull}, s.Permissions, "NewScope copies its arguments")
}

func TestGrants(t *testing.T) {
	require.False(t, delegation.Scope{}.Grants(delegation.PermissionRead), "the zero scope grants nothing")

	s := delegation.NewScope(delegation.PermissionAdmin, delegation.PermissionPush)
	for p, want := range map[delegation.Permission]bool{
		delegation.PermissionRead:        true,
		delegation.PermissionRecord:      false,
		delegation.PermissionPush:        true,
		delegation.PermissionPull:        false,
		delegation.PermissionManageViews: true,
		delegation.PermissionManageTags:  true,
		delegation.PermissionAdmin:       true,
		delegation.PermissionFull:        false,
	} {
		require.Equal(t, want, s.Grants(p), p)
	}
}

func TestAllows(t *testing.T) {
	s := delegation.NewScope(delegation.PermissionRead, delegation.PermissionPush)
	s.Servers = []string{"https://atomic.storage"}
	s.Workspaces = []string{"acme"}
	s.Projects = []string{"acme/*"}
	s.Views = []string{"main", "release-?"}

	ok := delegation.ResourceRef{Server: "https://atomic.storage", Workspace: "acme", Project: "acme/api", View: "main"}
	require.True(t, s.Allows(delegation.PermissionPush, ok))
	require.True(t, s.Allows(delegation.PermissionRead, ok))
	require.False(t, s.Allows(delegation.PermissionAdmin, ok), "permission is checked first")

	tests := []struct {
		name string
		ref  delegation.ResourceRef
		want bool
	}{
		{"nothing set", delegation.ResourceRef{}, true},
		{"server case and trailing slash", delegation.ResourceRef{Server: "https://Atomic.Storage/"}, true},
		{"other server", delegation.ResourceRef{Server: "https://staging.atomic.storage"}, false},
		{"other workspace", delegation.ResourceRef{Workspace: "bob"}, false},
		{"project outside the glob", delegation.ResourceRef{Project: "bob/api"}, false},
		{"project without its slash", delegation.ResourceRef{Project: "acme"}, false},
		{"second view pattern", delegation.ResourceRef{View: "release-1"}, true},
		{"view not listed", delegation.ResourceRef{View: "dev"}, false},
		{"one dimension fails", delegation.ResourceRef{Server: "https://atomic.storage", Project: "acme/api", View: "dev"}, false},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, s.Allows(delegation.PermissionRead, tc.ref), tc.name)
	}
}

func TestAllowsEmptyListsAreUnrestricted(t *testing.T) {
	s := delegation.NewScope()
	ref := delegation.ResourceRef{Server: "https://elsewhere.example", Workspace: "w", Project: "anything/at/all", View: "some-view"}
	require.True(t, s.Allows(delegation.PermissionRead, ref))
	require.False(t, s.Allows(delegation.PermissionPush, ref))
}

func TestAllowsServersAreLiteral(t *testing.T) {
	s := delegation.NewScope()
	s.Servers = []string{"https://*.storage", "https://atomic.?"}
	require.False(t, s.Allows(delegation.PermissionRead, delegation.ResourceRef{Server: "https://atomic.storage"}))
	require.False(t, s.Allows(delegation.PermissionRead, delegation.ResourceRef{Server: "https://atomic.x"}))
	require.True(t, s.Allows(delegation.PermissionRead, delegation.ResourceRef{Server: "https://*.storage"}))
	require.True(t, s.Allows(delegation.PermissionRead, delegation.ResourceRef{Server: "HTTPS://ATOMIC.?/"}))
}

func TestScopeMarshalEmpty(t *testing.T) {
	b, err := json.Marshal(delegation.Scope{})
	require.NoError(t, err)
	require.Equal(t, `{"permissions":[],"servers":[],"workspaces":[],"projects":[],"views":[]}`, string(b))

	b, err = json.Marshal(delegation.NewScope())
	require.NoError(t, err)
	require.Equal(t, `{"permissions":["read"],"servers":[],"workspaces":[],"projects":[],"views":[]}`, string(b))
}

func TestScopeMarshalFull(t *testing.T) {
	n := uint64(100)
	s := delegation.Scope{
		Permissions: []delegation.Permission{delegation.PermissionRead, delegation.PermissionRecord},
		Servers:     []string{"https://atomic.storage"},
		Workspaces:  []string{"acme"},
		Projects:    []string{"acme/*"},
		Views:       []string{"main"},
		MaxChanges:  &n,
		Description: "CI <bot> & co",
	}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.Equal(t, `{"permissions":["read","record"],"servers":["https://atomic.storage"],"workspaces":["acme"],"projects":["acme/*"],"views":["main"],"max_changes":100,"description":"CI <bot> & co"}`, string(b))

	var got delegation.Scope
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, s, got)

	zero := uint64(0)
	b, err = json.Marshal(delegation.Scope{Permissions: []delegation.Permission{delegation.PermissionFull}, MaxChanges: &zero})
	require.NoError(t, err)
	require.Equal(t, `{"permissions":["full"],"servers":[],"workspaces":[],"projects":[],"views":[],"max_changes":0}`, string(b))

	_, err = json.Marshal(delegation.Scope{Permissions: []delegation.Permission{"write"}})
	require.ErrorIs(t, err, delegation.ErrUnknownPermission)
}

func TestScopeUnmarshal(t *testing.T) {
	var s delegation.Scope
	require.NoError(t, json.Unmarshal([]byte(`{"permissions":["push"]}`), &s))
	require.Equal(t, delegation.Scope{Permissions: []delegation.Permission{delegation.PermissionPush}}, s)

	require.NoError(t, json.Unmarshal([]byte(`{"permissions":[],"max_changes":null,"description":null,"unknown":{"x":1}}`), &s))
	require.Equal(t, delegation.Scope{Permissions: []delegation.Permission{}}, s)
}

func TestScopeUnmarshalAliases(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want delegation.Scope
	}{
		{
			`{"permissions":["read"],"repository_patterns":["acme/*"],"view_patterns":["main"]}`,
			delegation.Scope{Permissions: []delegation.Permission{delegation.PermissionRead}, Projects: []string{"acme/*"}, Views: []string{"main"}},
		},
		{
			`{"permissions":["read"],"stack_patterns":["dev"]}`,
			delegation.Scope{Permissions: []delegation.Permission{delegation.PermissionRead}, Views: []string{"dev"}},
		},
	} {
		var s delegation.Scope
		require.NoError(t, json.Unmarshal([]byte(tc.in), &s), tc.in)
		require.Equal(t, tc.want, s, tc.in)
	}
}

func TestScopeUnmarshalRefuses(t *testing.T) {
	for name, in := range map[string]string{
		"not an object":                   `["read"]`,
		"null":                            `null`,
		"missing permissions":             `{"servers":[]}`,
		"empty object":                    `{}`,
		"unknown permission":              `{"permissions":["write"]}`,
		"permission not in wire form":     `{"permissions":["manage-views"]}`,
		"legacy permission name":          `{"permissions":["manage_stacks"]}`,
		"null permissions":                `{"permissions":null}`,
		"null permission":                 `{"permissions":[null]}`,
		"projects and its alias":          `{"permissions":["read"],"projects":["a"],"repository_patterns":["b"]}`,
		"alias before its name":           `{"permissions":["read"],"repository_patterns":["b"],"projects":["a"]}`,
		"views and view_patterns":         `{"permissions":["read"],"views":["a"],"view_patterns":["b"]}`,
		"views and stack_patterns":        `{"permissions":["read"],"views":["a"],"stack_patterns":["b"]}`,
		"two view aliases":                `{"permissions":["read"],"view_patterns":["a"],"stack_patterns":["b"]}`,
		"repeated member":                 `{"permissions":["read"],"permissions":["push"]}`,
		"null list":                       `{"permissions":["read"],"servers":null}`,
		"non-string pattern":              `{"permissions":["read"],"projects":[1]}`,
		"list is a string":                `{"permissions":["read"],"views":"main"}`,
		"negative max_changes":            `{"permissions":["read"],"max_changes":-1}`,
		"fractional max_changes":          `{"permissions":["read"],"max_changes":1.5}`,
		"max_changes beyond 64 bits":      `{"permissions":["read"],"max_changes":18446744073709551616}`,
		"max_changes as a string":         `{"permissions":["read"],"max_changes":"1"}`,
		"description not a string":        `{"permissions":["read"],"description":1}`,
		"truncated":                       `{"permissions":["read"]`,
		"invalid permission element kind": `{"permissions":[["read"]]}`,
	} {
		before := delegation.NewScope(delegation.PermissionFull)
		s := before
		err := json.Unmarshal([]byte(in), &s)
		require.Error(t, err, name)
		require.Equal(t, before, s, "%s: unchanged on failure", name)
	}
}

func TestScopeUnmarshalErrorKinds(t *testing.T) {
	var s delegation.Scope
	err := s.UnmarshalJSON([]byte(`{"permissions":["write"]}`))
	require.ErrorIs(t, err, delegation.ErrInvalidScope)
	require.ErrorIs(t, err, delegation.ErrUnknownPermission)

	err = s.UnmarshalJSON([]byte(`{"servers":[]}`))
	require.ErrorIs(t, err, delegation.ErrInvalidScope)

	err = s.UnmarshalJSON([]byte(`{"permissions":["read"],"projects":[],"repository_patterns":[]}`))
	require.ErrorIs(t, err, delegation.ErrInvalidScope)
	require.ErrorContains(t, err, `"projects" and "repository_patterns"`)

	err = s.UnmarshalJSON([]byte(`{"permissions":["read"]} {}`))
	require.ErrorIs(t, err, delegation.ErrInvalidScope)
}

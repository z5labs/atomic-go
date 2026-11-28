package storage_test

import (
	"encoding/json/v2"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/storage"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

// memberID is the identity the member tests act on.
var memberID = uuid.MustParse("00000000-0000-0000-0000-000000000002")

// snakeMember is an OrgMemberInfo body in snake_case, as the Rust client's
// tests say the server writes it (docs/remote/endpoints/storage.md#orgmemberinfo).
const snakeMember = `{"org_id": "00000000-0000-0000-0000-000000000001", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "admin", "joined_at": "2024-01-02T00:00:00Z", "invited_by": "00000000-0000-0000-0000-000000000003", "name": "Alice", "public_key": "ABCDEF", "status": "active", "email": "alice@acme.com"}`

// bareMember is an OrgMemberInfo body without any of its optional members,
// as older servers write it.
const bareMember = `{"org_id": "00000000-0000-0000-0000-000000000001", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "member", "joined_at": "2024-01-02T00:00:00Z"}`

func wantSnakeMember() storage.OrgMemberInfo {
	return storage.OrgMemberInfo{
		OrgID:      uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		IdentityID: memberID,
		Role:       storage.OrgRoleAdmin,
		JoinedAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		InvitedBy:  ptr(uuid.MustParse("00000000-0000-0000-0000-000000000003")),
		Name:       ptr("Alice"),
		PublicKey:  ptr("ABCDEF"),
		Status:     ptr("active"),
		Email:      ptr("alice@acme.com"),
	}
}

func wantBareMember() storage.OrgMemberInfo {
	return storage.OrgMemberInfo{
		OrgID:      uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		IdentityID: memberID,
		Role:       storage.OrgRoleMember,
		JoinedAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestOrgMemberInfoReadsBothSpellings(t *testing.T) {
	tests := map[string]struct {
		body string
		want storage.OrgMemberInfo
	}{
		"snake_case": {body: snakeMember, want: wantSnakeMember()},
		"camelCase": {
			body: `{"orgId": "00000000-0000-0000-0000-000000000001", "identityId": "00000000-0000-0000-0000-000000000002", "role": "admin", "joinedAt": "2024-01-02T00:00:00Z", "invitedBy": "00000000-0000-0000-0000-000000000003", "name": "Alice", "publicKey": "ABCDEF", "status": "active", "email": "alice@acme.com"}`,
			want: wantSnakeMember(),
		},
		"without optional members": {body: bareMember, want: wantBareMember()},
		"optional members null": {
			body: `{"org_id": "00000000-0000-0000-0000-000000000001", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "member", "joined_at": "2024-01-02T00:00:00Z", "invited_by": null, "name": null, "public_key": null, "status": null, "email": null}`,
			want: wantBareMember(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.OrgMemberInfo
			require.NoError(t, json.Unmarshal([]byte(tt.body), &got))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOrgMemberInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing org_id":      `{"identity_id": "00000000-0000-0000-0000-000000000002", "role": "member", "joined_at": "2024-01-02T00:00:00Z"}`,
		"missing identity_id": `{"org_id": "00000000-0000-0000-0000-000000000001", "role": "member", "joined_at": "2024-01-02T00:00:00Z"}`,
		"missing role":        `{"org_id": "00000000-0000-0000-0000-000000000001", "identity_id": "00000000-0000-0000-0000-000000000002", "joined_at": "2024-01-02T00:00:00Z"}`,
		"missing joined_at":   `{"org_id": "00000000-0000-0000-0000-000000000001", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "member"}`,
		"null org_id":         `{"org_id": null, "identity_id": "00000000-0000-0000-0000-000000000002", "role": "member", "joined_at": "2024-01-02T00:00:00Z"}`,
		"null role":           `{"org_id": "00000000-0000-0000-0000-000000000001", "identity_id": "00000000-0000-0000-0000-000000000002", "role": null, "joined_at": "2024-01-02T00:00:00Z"}`,
		"both spellings":      `{"org_id": "00000000-0000-0000-0000-000000000001", "orgId": "00000000-0000-0000-0000-000000000001", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "member", "joined_at": "2024-01-02T00:00:00Z"}`,
		"bad identity_id":     `{"org_id": "00000000-0000-0000-0000-000000000001", "identity_id": "two", "role": "member", "joined_at": "2024-01-02T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.OrgMemberInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestOrgMembersList(t *testing.T) {
	c, got := serve(t, "GET /orgs/{slug}/members", raw(http.StatusOK, envelope("["+snakeMember+","+bareMember+"]")))
	list, err := c.OrgMembers().List(t.Context(), "acme")
	require.NoError(t, err)
	require.Equal(t, []storage.OrgMemberInfo{wantSnakeMember(), wantBareMember()}, list)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/orgs/acme/members", got.requestURI)
	require.Empty(t, got.body)
}

func TestOrgMembersAdd(t *testing.T) {
	c, got := serve(t, "POST /orgs/{slug}/members", raw(http.StatusCreated, envelope(bareMember)))
	member, err := c.OrgMembers().Add(t.Context(), "acme", storage.AddMemberRequest{
		IdentityID: memberID,
		Role:       storage.OrgRoleMember,
	})
	require.NoError(t, err)
	require.Equal(t, wantBareMember(), member)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/orgs/acme/members", got.requestURI)
	require.JSONEq(t, `{"identity_id": "00000000-0000-0000-0000-000000000002", "role": "member"}`, got.body)
}

func TestOrgMembersGet(t *testing.T) {
	c, got := serve(t, "GET /orgs/{slug}/members/{id}", raw(http.StatusOK, envelope(snakeMember)))
	member, err := c.OrgMembers().Get(t.Context(), "acme", memberID)
	require.NoError(t, err)
	require.Equal(t, wantSnakeMember(), member)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/orgs/acme/members/00000000-0000-0000-0000-000000000002", got.requestURI)
	require.Empty(t, got.body)
}

func TestOrgMembersGetNotFound(t *testing.T) {
	c, _ := serve(t, "GET /orgs/{slug}/members/{id}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "member not found"))
	})
	_, err := c.OrgMembers().Get(t.Context(), "acme", memberID)
	require.ErrorIs(t, err, remote.ErrNotFound)
}

func TestOrgMembersUpdateRole(t *testing.T) {
	c, got := serve(t, "PUT /orgs/{slug}/members/{id}", raw(http.StatusOK, envelope(snakeMember)))
	member, err := c.OrgMembers().UpdateRole(t.Context(), "acme", memberID, storage.UpdateMemberRoleRequest{Role: storage.OrgRoleAdmin})
	require.NoError(t, err)
	require.Equal(t, wantSnakeMember(), member)
	require.Equal(t, http.MethodPut, got.method)
	require.Equal(t, "/orgs/acme/members/00000000-0000-0000-0000-000000000002", got.requestURI)
	require.JSONEq(t, `{"role": "admin"}`, got.body)
}

func TestOrgMembersRemove(t *testing.T) {
	c, got := serve(t, "DELETE /orgs/{slug}/members/{id}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.OrgMembers().Remove(t.Context(), "acme", memberID))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/orgs/acme/members/00000000-0000-0000-0000-000000000002", got.requestURI)
	require.Empty(t, got.body)
}

func TestOrgMembersRemoveLastOwnerMatchesErrConflict(t *testing.T) {
	c, _ := serve(t, "DELETE /orgs/{slug}/members/{id}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusConflict, "CONFLICT", "cannot remove the last owner"))
	})
	err := c.OrgMembers().Remove(t.Context(), "acme", memberID)
	require.ErrorIs(t, err, remote.ErrConflict)
	var rerr *remote.Error
	require.ErrorAs(t, err, &rerr)
	require.Equal(t, http.StatusConflict, rerr.StatusCode)
	require.Equal(t, "CONFLICT", rerr.Code)
	require.Equal(t, "remove_member", rerr.Op)
}

func TestOrgMembersForbiddenMatchesErrForbidden(t *testing.T) {
	c, _ := serve(t, "PUT /orgs/{slug}/members/{id}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusForbidden, "FORBIDDEN", "not an admin"))
	})
	_, err := c.OrgMembers().UpdateRole(t.Context(), "acme", memberID, storage.UpdateMemberRoleRequest{Role: storage.OrgRoleOwner})
	require.ErrorIs(t, err, remote.ErrForbidden)
}

func TestOrgMembersRefuseBadSlugsWithNothingSent(t *testing.T) {
	for _, slug := range []string{"..", ".", "", "a/b", "%2e%2e", "a?b", "a#b"} {
		t.Run(slug, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(bareMember)))
			members := c.OrgMembers()

			_, err := members.List(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = members.Add(t.Context(), slug, storage.AddMemberRequest{IdentityID: memberID, Role: storage.OrgRoleMember})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = members.Get(t.Context(), slug, memberID)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = members.UpdateRole(t.Context(), slug, memberID, storage.UpdateMemberRoleRequest{Role: storage.OrgRoleOwner})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = members.Remove(t.Context(), slug, memberID)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

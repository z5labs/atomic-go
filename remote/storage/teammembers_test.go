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

// camelTeamMember is a TeamMemberInfo body in camelCase
// (docs/remote/endpoints/storage.md#teammemberinfo).
const camelTeamMember = `{"teamId": "00000000-0000-0000-0000-00000000000a", "identityId": "00000000-0000-0000-0000-000000000002", "role": "contributor", "addedAt": "2024-01-02T00:00:00Z", "addedBy": "00000000-0000-0000-0000-000000000003"}`

// snakeTeamMember is camelTeamMember in snake_case.
const snakeTeamMember = `{"team_id": "00000000-0000-0000-0000-00000000000a", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "contributor", "added_at": "2024-01-02T00:00:00Z", "added_by": "00000000-0000-0000-0000-000000000003"}`

func wantTeamMember() storage.TeamMemberInfo {
	return storage.TeamMemberInfo{
		TeamID:     uuid.MustParse("00000000-0000-0000-0000-00000000000a"),
		IdentityID: memberID,
		Role:       storage.TeamRoleContributor,
		AddedAt:    time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		AddedBy:    uuid.MustParse("00000000-0000-0000-0000-000000000003"),
	}
}

func TestTeamMemberInfoReadsBothSpellings(t *testing.T) {
	for name, body := range map[string]string{"camelCase": camelTeamMember, "snake_case": snakeTeamMember} {
		t.Run(name, func(t *testing.T) {
			var got storage.TeamMemberInfo
			require.NoError(t, json.Unmarshal([]byte(body), &got))
			require.Equal(t, wantTeamMember(), got)
		})
	}
}

func TestTeamMemberInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing team_id":     `{"identity_id": "00000000-0000-0000-0000-000000000002", "role": "contributor", "added_at": "2024-01-02T00:00:00Z", "added_by": "00000000-0000-0000-0000-000000000003"}`,
		"missing identity_id": `{"team_id": "00000000-0000-0000-0000-00000000000a", "role": "contributor", "added_at": "2024-01-02T00:00:00Z", "added_by": "00000000-0000-0000-0000-000000000003"}`,
		"missing role":        `{"team_id": "00000000-0000-0000-0000-00000000000a", "identity_id": "00000000-0000-0000-0000-000000000002", "added_at": "2024-01-02T00:00:00Z", "added_by": "00000000-0000-0000-0000-000000000003"}`,
		"missing added_at":    `{"team_id": "00000000-0000-0000-0000-00000000000a", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "contributor", "added_by": "00000000-0000-0000-0000-000000000003"}`,
		"missing added_by":    `{"team_id": "00000000-0000-0000-0000-00000000000a", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "contributor", "added_at": "2024-01-02T00:00:00Z"}`,
		"null added_by":       `{"team_id": "00000000-0000-0000-0000-00000000000a", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "contributor", "added_at": "2024-01-02T00:00:00Z", "added_by": null}`,
		"both spellings":      `{"team_id": "00000000-0000-0000-0000-00000000000a", "teamId": "00000000-0000-0000-0000-00000000000a", "identity_id": "00000000-0000-0000-0000-000000000002", "role": "contributor", "added_at": "2024-01-02T00:00:00Z", "added_by": "00000000-0000-0000-0000-000000000003"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.TeamMemberInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestTeamMembersList(t *testing.T) {
	c, got := serve(t, "GET /teams/{slug}/members", raw(http.StatusOK, envelope("["+camelTeamMember+","+snakeTeamMember+"]")))
	list, err := c.TeamMembers().List(t.Context(), "backend")
	require.NoError(t, err)
	require.Equal(t, []storage.TeamMemberInfo{wantTeamMember(), wantTeamMember()}, list)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/teams/backend/members", got.requestURI)
	require.Empty(t, got.body)
}

func TestTeamMembersAdd(t *testing.T) {
	c, got := serve(t, "POST /teams/{slug}/members", raw(http.StatusCreated, envelope(snakeTeamMember)))
	member, err := c.TeamMembers().Add(t.Context(), "backend", storage.AddTeamMemberRequest{
		IdentityID: memberID,
		Role:       storage.TeamRoleContributor,
	})
	require.NoError(t, err)
	require.Equal(t, wantTeamMember(), member)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/teams/backend/members", got.requestURI)
	require.JSONEq(t, `{"identity_id": "00000000-0000-0000-0000-000000000002", "role": "contributor"}`, got.body)
}

func TestTeamMembersUpdateRole(t *testing.T) {
	c, got := serve(t, "PUT /teams/{slug}/members/{id}", raw(http.StatusOK, envelope(camelTeamMember)))
	member, err := c.TeamMembers().UpdateRole(t.Context(), "backend", memberID, storage.UpdateTeamMemberRoleRequest{Role: storage.TeamRoleMaintainer})
	require.NoError(t, err)
	require.Equal(t, wantTeamMember(), member)
	require.Equal(t, http.MethodPut, got.method)
	require.Equal(t, "/teams/backend/members/00000000-0000-0000-0000-000000000002", got.requestURI)
	require.JSONEq(t, `{"role": "maintainer"}`, got.body)
}

// TestTeamMembersUpdateRoleNotFoundKeepsTheServersMessage checks that a 404
// is the server's, with its message, rather than a TeamNotFound or
// MemberNotFound the client decides on
// (docs/remote/endpoints/storage.md#open-questions).
func TestTeamMembersUpdateRoleNotFoundKeepsTheServersMessage(t *testing.T) {
	c, _ := serve(t, "PUT /teams/{slug}/members/{id}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "identity is not a member of this team"))
	})
	_, err := c.TeamMembers().UpdateRole(t.Context(), "backend", memberID, storage.UpdateTeamMemberRoleRequest{Role: storage.TeamRoleConsumer})
	require.ErrorIs(t, err, remote.ErrNotFound)
	var rerr *remote.Error
	require.ErrorAs(t, err, &rerr)
	require.Equal(t, http.StatusNotFound, rerr.StatusCode)
	require.Equal(t, "NOT_FOUND", rerr.Code)
	require.Equal(t, "identity is not a member of this team", rerr.Message)
	require.Equal(t, "update_team_member_role", rerr.Op)
}

func TestTeamMembersRemove(t *testing.T) {
	c, got := serve(t, "DELETE /teams/{slug}/members/{id}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.TeamMembers().Remove(t.Context(), "backend", memberID))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/teams/backend/members/00000000-0000-0000-0000-000000000002", got.requestURI)
	require.Empty(t, got.body)
}

func TestTeamMembersRefuseBadSlugsWithNothingSent(t *testing.T) {
	for _, slug := range []string{"..", ".", "", "a/b", "%2e%2e", "a?b", "a#b"} {
		t.Run(slug, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(camelTeamMember)))
			members := c.TeamMembers()

			_, err := members.List(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = members.Add(t.Context(), slug, storage.AddTeamMemberRequest{IdentityID: memberID, Role: storage.TeamRoleContributor})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = members.UpdateRole(t.Context(), slug, memberID, storage.UpdateTeamMemberRoleRequest{Role: storage.TeamRoleMaintainer})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = members.Remove(t.Context(), slug, memberID)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

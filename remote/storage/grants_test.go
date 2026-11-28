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

// grantSubject is the team or user the grant tests grant to and revoke.
var grantSubject = uuid.MustParse("00000000-0000-0000-0000-0000000000aa")

// orgGrant is a GrantInfo body in snake_case with every member the docs
// list (docs/remote/endpoints/storage.md#grantinfo).
const orgGrant = `{"id": "00000000-0000-0000-0000-000000000001", "subject_type": "team", "subject_id": "00000000-0000-0000-0000-0000000000aa", "relation": "write", "granted_by": "00000000-0000-0000-0000-000000000003", "granted_at": "2024-01-02T00:00:00Z"}`

// workspaceGrant is a WorkspaceGrantInfo body in snake_case with every
// member the docs list (docs/remote/endpoints/storage.md#workspacegrantinfo).
const workspaceGrant = `{"id": "00000000-0000-0000-0000-000000000002", "subject_type": "user", "subject_id": "00000000-0000-0000-0000-0000000000aa", "subject_relation": "member", "relation": "admin", "object_type": "workspace", "object_id": "00000000-0000-0000-0000-000000000009", "granted_at": "2024-01-02T00:00:00Z", "granted_by": "00000000-0000-0000-0000-000000000003"}`

// everyoneGrant is a public workspace's grant, with a null subject_id, as
// the Rust client's test parses it.
const everyoneGrant = `{"id": "00000000-0000-0000-0000-000000000004", "subject_type": "everyone", "subject_id": null, "subject_relation": null, "relation": "read", "object_type": "workspace", "object_id": "00000000-0000-0000-0000-000000000009", "granted_at": "2024-01-02T00:00:00Z", "granted_by": null}`

func wantOrgGrant() storage.GrantInfo {
	return storage.GrantInfo{
		ID:          uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		SubjectType: storage.SubjectTeam,
		SubjectID:   ptr(grantSubject),
		Relation:    storage.RelationWrite,
		GrantedBy:   ptr(uuid.MustParse("00000000-0000-0000-0000-000000000003")),
		GrantedAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func wantWorkspaceGrant() storage.WorkspaceGrantInfo {
	return storage.WorkspaceGrantInfo{
		ID:              uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		SubjectType:     storage.SubjectUser,
		SubjectID:       ptr(grantSubject),
		SubjectRelation: ptr("member"),
		Relation:        storage.RelationAdmin,
		ObjectType:      "workspace",
		ObjectID:        uuid.MustParse("00000000-0000-0000-0000-000000000009"),
		GrantedAt:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GrantedBy:       ptr(uuid.MustParse("00000000-0000-0000-0000-000000000003")),
	}
}

func wantEveryoneGrant() storage.WorkspaceGrantInfo {
	return storage.WorkspaceGrantInfo{
		ID:          uuid.MustParse("00000000-0000-0000-0000-000000000004"),
		SubjectType: storage.SubjectEveryone,
		Relation:    storage.RelationRead,
		ObjectType:  "workspace",
		ObjectID:    uuid.MustParse("00000000-0000-0000-0000-000000000009"),
		GrantedAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestGrantInfoReadsTheDocsBody(t *testing.T) {
	var got storage.GrantInfo
	require.NoError(t, json.Unmarshal([]byte(orgGrant), &got))
	require.Equal(t, wantOrgGrant(), got)

	var bare storage.GrantInfo
	require.NoError(t, json.Unmarshal([]byte(`{"id": "00000000-0000-0000-0000-000000000001", "subject_type": "everyone", "relation": "read", "granted_at": "2024-01-02T00:00:00Z"}`), &bare))
	require.Equal(t, storage.GrantInfo{
		ID:          uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		SubjectType: storage.SubjectEveryone,
		Relation:    storage.RelationRead,
		GrantedAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}, bare)
}

func TestWorkspaceGrantInfoReadsTheDocsBody(t *testing.T) {
	var got storage.WorkspaceGrantInfo
	require.NoError(t, json.Unmarshal([]byte(workspaceGrant), &got))
	require.Equal(t, wantWorkspaceGrant(), got)
}

func TestWorkspaceGrantInfoDecodesAnEveryoneGrantWithANullSubject(t *testing.T) {
	var got storage.WorkspaceGrantInfo
	require.NoError(t, json.Unmarshal([]byte(everyoneGrant), &got))
	require.Equal(t, wantEveryoneGrant(), got)
	require.Nil(t, got.SubjectID)
}

func TestGrantInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing id":           `{"subject_type": "team", "relation": "write", "granted_at": "2024-01-02T00:00:00Z"}`,
		"missing subject_type": `{"id": "00000000-0000-0000-0000-000000000001", "relation": "write", "granted_at": "2024-01-02T00:00:00Z"}`,
		"missing relation":     `{"id": "00000000-0000-0000-0000-000000000001", "subject_type": "team", "granted_at": "2024-01-02T00:00:00Z"}`,
		"missing granted_at":   `{"id": "00000000-0000-0000-0000-000000000001", "subject_type": "team", "relation": "write"}`,
		"null relation":        `{"id": "00000000-0000-0000-0000-000000000001", "subject_type": "team", "relation": null, "granted_at": "2024-01-02T00:00:00Z"}`,
		"camelCase grantedAt":  `{"id": "00000000-0000-0000-0000-000000000001", "subject_type": "team", "relation": "write", "grantedAt": "2024-01-02T00:00:00Z"}`,
		"written twice":        `{"id": "00000000-0000-0000-0000-000000000001", "id": "00000000-0000-0000-0000-000000000001", "subject_type": "team", "relation": "write", "granted_at": "2024-01-02T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.GrantInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestWorkspaceGrantInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing object_type": `{"id": "00000000-0000-0000-0000-000000000002", "subject_type": "user", "relation": "admin", "object_id": "00000000-0000-0000-0000-000000000009", "granted_at": "2024-01-02T00:00:00Z"}`,
		"missing object_id":   `{"id": "00000000-0000-0000-0000-000000000002", "subject_type": "user", "relation": "admin", "object_type": "workspace", "granted_at": "2024-01-02T00:00:00Z"}`,
		"camelCase objectId":  `{"id": "00000000-0000-0000-0000-000000000002", "subject_type": "user", "relation": "admin", "object_type": "workspace", "objectId": "00000000-0000-0000-0000-000000000009", "granted_at": "2024-01-02T00:00:00Z"}`,
		"bad subject_id":      `{"id": "00000000-0000-0000-0000-000000000002", "subject_type": "user", "subject_id": "one", "relation": "admin", "object_type": "workspace", "object_id": "00000000-0000-0000-0000-000000000009", "granted_at": "2024-01-02T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.WorkspaceGrantInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestOrgGrantsList(t *testing.T) {
	c, got := serve(t, "GET /orgs/{slug}/grants", raw(http.StatusOK, envelope("["+orgGrant+"]")))
	list, err := c.OrgGrants().List(t.Context(), "acme")
	require.NoError(t, err)
	require.Equal(t, []storage.GrantInfo{wantOrgGrant()}, list)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/orgs/acme/grants", got.requestURI)
	require.Empty(t, got.body)
}

func TestOrgGrantsAddSendsTheDocsBody(t *testing.T) {
	c, got := serve(t, "POST /orgs/{slug}/grants", raw(http.StatusCreated, envelope(orgGrant)))
	g, err := c.OrgGrants().Add(t.Context(), "acme", storage.AddGrantRequest{
		SubjectType: storage.SubjectTeam,
		SubjectID:   uuid.Nil,
		Relation:    storage.RelationWrite,
	})
	require.NoError(t, err)
	require.Equal(t, wantOrgGrant(), g)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/orgs/acme/grants", got.requestURI)
	require.JSONEq(t, `{"subject_type": "team", "subject_id": "00000000-0000-0000-0000-000000000000", "relation": "write"}`, got.body)
}

func TestOrgGrantsAddSendsOwner(t *testing.T) {
	c, got := serve(t, "POST /orgs/{slug}/grants", raw(http.StatusCreated, envelope(orgGrant)))
	_, err := c.OrgGrants().Add(t.Context(), "acme", storage.AddGrantRequest{
		SubjectType: storage.SubjectUser,
		SubjectID:   grantSubject,
		Relation:    storage.RelationOwner,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"subject_type": "user", "subject_id": "00000000-0000-0000-0000-0000000000aa", "relation": "owner"}`, got.body)
}

func TestOrgGrantsRevokePath(t *testing.T) {
	c, got := serve(t, "DELETE /orgs/{slug}/grants/{type}/{id}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.OrgGrants().Revoke(t.Context(), "acme", storage.SubjectTeam, grantSubject))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/orgs/acme/grants/team/00000000-0000-0000-0000-0000000000aa", got.requestURI)
	require.Empty(t, got.body)
}

func TestWorkspaceGrantsListDecodesAnEveryoneGrant(t *testing.T) {
	c, got := serve(t, "GET /workspaces/{slug}/grants", raw(http.StatusOK, envelope("["+workspaceGrant+","+everyoneGrant+"]")))
	list, err := c.WorkspaceGrants().List(t.Context(), "my-ws")
	require.NoError(t, err)
	require.Equal(t, []storage.WorkspaceGrantInfo{wantWorkspaceGrant(), wantEveryoneGrant()}, list)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/workspaces/my-ws/grants", got.requestURI)
	require.Empty(t, got.body)
}

func TestWorkspaceGrantsAddSendsTheBody(t *testing.T) {
	c, got := serve(t, "POST /workspaces/{slug}/grants", raw(http.StatusCreated, envelope(workspaceGrant)))
	g, err := c.WorkspaceGrants().Add(t.Context(), "my-ws", storage.AddGrantRequest{
		SubjectType: storage.SubjectUser,
		SubjectID:   grantSubject,
		Relation:    storage.RelationAdmin,
	})
	require.NoError(t, err)
	require.Equal(t, wantWorkspaceGrant(), g)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/workspaces/my-ws/grants", got.requestURI)
	require.JSONEq(t, `{"subject_type": "user", "subject_id": "00000000-0000-0000-0000-0000000000aa", "relation": "admin"}`, got.body)
}

func TestWorkspaceGrantsAddAcceptsOwner(t *testing.T) {
	c, got := serve(t, "POST /workspaces/{slug}/grants", raw(http.StatusCreated, envelope(workspaceGrant)))
	_, err := c.WorkspaceGrants().Add(t.Context(), "my-ws", storage.AddGrantRequest{
		SubjectType: storage.SubjectTeam,
		SubjectID:   grantSubject,
		Relation:    storage.RelationOwner,
	})
	require.NoError(t, err)
	require.Equal(t, 1, got.calls)
	require.JSONEq(t, `{"subject_type": "team", "subject_id": "00000000-0000-0000-0000-0000000000aa", "relation": "owner"}`, got.body)
}

func TestWorkspaceGrantsRevokePath(t *testing.T) {
	c, got := serve(t, "DELETE /workspaces/{slug}/grants/{type}/{id}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.WorkspaceGrants().Revoke(t.Context(), "my-ws", storage.SubjectUser, grantSubject))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/workspaces/my-ws/grants/user/00000000-0000-0000-0000-0000000000aa", got.requestURI)
	require.Empty(t, got.body)
}

func TestGrantsRefuseEveryoneWithNothingSent(t *testing.T) {
	everyone := storage.AddGrantRequest{SubjectType: storage.SubjectEveryone, SubjectID: grantSubject, Relation: storage.RelationRead}

	c, got := serve(t, "/", raw(http.StatusOK, envelope(orgGrant)))

	_, err := c.OrgGrants().Add(t.Context(), "acme", everyone)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)

	err = c.OrgGrants().Revoke(t.Context(), "acme", storage.SubjectEveryone, grantSubject)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)

	_, err = c.WorkspaceGrants().Add(t.Context(), "my-ws", everyone)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)

	err = c.WorkspaceGrants().Revoke(t.Context(), "my-ws", storage.SubjectEveryone, grantSubject)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)

	require.Zero(t, got.calls)
}

func TestGrantsRefuseUnknownSubjectsAndRelationsWithNothingSent(t *testing.T) {
	for name, req := range map[string]storage.AddGrantRequest{
		"empty subject type": {SubjectType: "", SubjectID: grantSubject, Relation: storage.RelationRead},
		"Team":               {SubjectType: "Team", SubjectID: grantSubject, Relation: storage.RelationRead},
		"public":             {SubjectType: "public", SubjectID: grantSubject, Relation: storage.RelationRead},
		"empty relation":     {SubjectType: storage.SubjectTeam, SubjectID: grantSubject, Relation: ""},
		"Write":              {SubjectType: storage.SubjectTeam, SubjectID: grantSubject, Relation: "Write"},
		"maintainer":         {SubjectType: storage.SubjectTeam, SubjectID: grantSubject, Relation: "maintainer"},
	} {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(orgGrant)))

			_, err := c.OrgGrants().Add(t.Context(), "acme", req)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = c.WorkspaceGrants().Add(t.Context(), "my-ws", req)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}

	for _, st := range []storage.GrantSubjectType{"", "Team", "user/..", "team/x"} {
		t.Run("revoke "+string(st), func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusNoContent, ""))

			err := c.OrgGrants().Revoke(t.Context(), "acme", st, grantSubject)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = c.WorkspaceGrants().Revoke(t.Context(), "my-ws", st, grantSubject)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

func TestGrantsRefuseBadSlugsWithNothingSent(t *testing.T) {
	add := storage.AddGrantRequest{SubjectType: storage.SubjectTeam, SubjectID: grantSubject, Relation: storage.RelationRead}
	for _, slug := range []string{"..", ".", "", "a/b", "%2e%2e", "a?b", "a#b"} {
		t.Run(slug, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(orgGrant)))

			_, err := c.OrgGrants().List(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = c.OrgGrants().Add(t.Context(), slug, add)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = c.OrgGrants().Revoke(t.Context(), slug, storage.SubjectTeam, grantSubject)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = c.WorkspaceGrants().List(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = c.WorkspaceGrants().Add(t.Context(), slug, add)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = c.WorkspaceGrants().Revoke(t.Context(), slug, storage.SubjectTeam, grantSubject)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

func TestGrantsNotFoundMatchesErrNotFound(t *testing.T) {
	notFound := func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "org not found"))
	}
	client := func(t *testing.T) *storage.Client {
		c, _ := serve(t, "/", notFound)
		return c
	}
	add := storage.AddGrantRequest{SubjectType: storage.SubjectTeam, SubjectID: grantSubject, Relation: storage.RelationRead}

	_, err := client(t).OrgGrants().List(t.Context(), "gone")
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = client(t).OrgGrants().Add(t.Context(), "gone", add)
	require.ErrorIs(t, err, remote.ErrNotFound)

	err = client(t).OrgGrants().Revoke(t.Context(), "gone", storage.SubjectTeam, grantSubject)
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = client(t).WorkspaceGrants().List(t.Context(), "gone")
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = client(t).WorkspaceGrants().Add(t.Context(), "gone", add)
	require.ErrorIs(t, err, remote.ErrNotFound)

	err = client(t).WorkspaceGrants().Revoke(t.Context(), "gone", storage.SubjectUser, grantSubject)
	require.ErrorIs(t, err, remote.ErrNotFound)
}

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

// docsOrg is an OrgInfo body with every member the docs list
// (docs/remote/endpoints/storage.md#orginfo).
const docsOrg = `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "acme-corp", "name": "Acme Corp", "email": "admin@acme.com", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-02T00:00:00Z"}`

func wantDocsOrg() storage.OrgInfo {
	return storage.OrgInfo{
		ID:        uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Slug:      "acme-corp",
		Name:      "Acme Corp",
		Email:     ptr("admin@acme.com"),
		Kind:      "team",
		Plan:      "free",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

// camelMyOrg is a MyOrgInfo body in camelCase, as the server writes it.
const camelMyOrg = `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "acme", "name": "Acme", "email": "admin@acme.com", "kind": "team", "plan": "enterprise", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z", "role": "owner", "joinedAt": "2024-01-02T00:00:00Z", "invitedBy": "00000000-0000-0000-0000-000000000009"}`

// snakeMyOrg is the snake_case MyOrgInfo body of the Rust client's alias
// test (docs/remote/endpoints/storage.md#myorginfo).
const snakeMyOrg = `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "acme", "name": "Acme", "email": null, "kind": "team", "plan": "free", "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z", "role": "member", "joined_at": "2024-01-02T00:00:00Z", "invited_by": null}`

// orgServe is serve with the request's headers recorded too.
func orgServe(t *testing.T, pattern string, h http.HandlerFunc) (*storage.Client, *served, *http.Header) {
	t.Helper()
	var header http.Header
	c, got := serve(t, pattern, func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		h(w, r)
	})
	return c, got, &header
}

func TestSlugify(t *testing.T) {
	for name, want := range map[string]string{
		"Acme Corp":         "acme-corp",
		"  Hello, World!! ": "hello-world",
		"Ünïcode Org":       "n-code-org",
		"!!!":               "",
		"":                  "",
		"already-a-slug":    "already-a-slug",
		"Team 42":           "team-42",
		"--a--b--":          "a-b",
		"Kelvin":            "kelvin",
		"İstanbul":          "i-stanbul",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, want, storage.Slugify(name))
		})
	}
}

func TestOrgInfoPlan(t *testing.T) {
	tests := map[string]string{
		"missing": `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "acme-corp", "name": "Acme Corp", "kind": "team", "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-02T00:00:00Z"}`,
		"null":    `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "acme-corp", "name": "Acme Corp", "email": null, "kind": "team", "plan": null, "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-02T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, "GET /orgs/{slug}", raw(http.StatusOK, envelope(body)))
			org, err := c.Orgs().Get(t.Context(), "acme-corp")
			require.NoError(t, err)
			want := wantDocsOrg()
			want.Email = nil
			want.Plan = ""
			require.Equal(t, want, org)
		})
	}
}

func TestOrgInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing slug":   `{"id": "550e8400-e29b-41d4-a716-446655440000", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z"}`,
		"null kind":      `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "a", "name": "A", "kind": null, "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z"}`,
		"both spellings": `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "a", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "created_at": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z"}`,
		"bad id":         `{"id": "one", "slug": "a", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z"}`,
		"plan a number":  `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "a", "name": "A", "kind": "team", "plan": 1, "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, "GET /orgs/{slug}", raw(http.StatusOK, envelope(body)))
			_, err := c.Orgs().Get(t.Context(), "a")
			var rerr *remote.Error
			require.ErrorAs(t, err, &rerr)
			require.Equal(t, remote.KindProtocol, rerr.Kind)
		})
	}
}

func TestMyOrgInfoReadsFlatBodies(t *testing.T) {
	joined := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := map[string]struct {
		body string
		want storage.MyOrgInfo
	}{
		"camelCase": {
			body: camelMyOrg,
			want: storage.MyOrgInfo{
				Org: storage.OrgInfo{
					ID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"), Slug: "acme", Name: "Acme",
					Email: ptr("admin@acme.com"), Kind: "team", Plan: "enterprise", CreatedAt: at, UpdatedAt: at,
				},
				Role:      storage.OrgRoleOwner,
				JoinedAt:  joined,
				InvitedBy: ptr(uuid.MustParse("00000000-0000-0000-0000-000000000009")),
			},
		},
		"snake_case": {
			body: snakeMyOrg,
			want: storage.MyOrgInfo{
				Org: storage.OrgInfo{
					ID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"), Slug: "acme", Name: "Acme",
					Kind: "team", Plan: "free", CreatedAt: at, UpdatedAt: at,
				},
				Role:     storage.OrgRoleMember,
				JoinedAt: joined,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.MyOrgInfo
			require.NoError(t, json.Unmarshal([]byte(tt.body), &got))
			require.Equal(t, tt.want, got)

			// It is written back flat, in camelCase, and reads back the same.
			b, err := json.Marshal(got)
			require.NoError(t, err)
			var members map[string]any
			require.NoError(t, json.Unmarshal(b, &members))
			require.NotContains(t, members, "org")
			require.NotContains(t, members, "Org")
			require.NotContains(t, members, "created_at")
			for _, key := range []string{"id", "slug", "name", "email", "kind", "plan", "createdAt", "updatedAt", "role", "joinedAt", "invitedBy"} {
				require.Contains(t, members, key)
			}

			var again storage.MyOrgInfo
			require.NoError(t, json.Unmarshal(b, &again))
			require.Equal(t, got, again)
		})
	}
}

func TestMyOrgInfoKeepsAnyRole(t *testing.T) {
	body := `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "a", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z", "role": "billing", "joinedAt": "2024-01-01T00:00:00Z"}`
	var got storage.MyOrgInfo
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	require.Equal(t, storage.OrgRole("billing"), got.Role)
	require.Nil(t, got.InvitedBy)
}

func TestMyOrgInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing role":       `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "a", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z", "joinedAt": "2024-01-01T00:00:00Z"}`,
		"missing joinedAt":   `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "a", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z", "role": "owner"}`,
		"missing slug":       `{"id": "550e8400-e29b-41d4-a716-446655440000", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z", "role": "owner", "joinedAt": "2024-01-01T00:00:00Z"}`,
		"both joined":        `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "a", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z", "role": "owner", "joinedAt": "2024-01-01T00:00:00Z", "joined_at": "2024-01-01T00:00:00Z"}`,
		"nested org":         `{"org": ` + docsOrg + `, "role": "owner", "joinedAt": "2024-01-01T00:00:00Z"}`,
		"invitedBy not uuid": `{"id": "550e8400-e29b-41d4-a716-446655440000", "slug": "a", "name": "A", "kind": "team", "plan": "free", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z", "role": "owner", "joinedAt": "2024-01-01T00:00:00Z", "invitedBy": "x"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.MyOrgInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestOrgsCreate(t *testing.T) {
	tests := map[string]struct {
		req  storage.CreateOrgRequest
		want string
	}{
		"with email": {
			req:  storage.CreateOrgRequest{Slug: "acme-corp", Name: "Acme Corp", Email: "admin@acme.com"},
			want: `{"slug": "acme-corp", "name": "Acme Corp", "email": "admin@acme.com"}`,
		},
		"without email": {
			req:  storage.CreateOrgRequest{Slug: "acme-corp", Name: "Acme Corp"},
			want: `{"slug": "acme-corp", "name": "Acme Corp"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "POST /orgs", raw(http.StatusCreated, envelope(docsOrg)))
			org, err := c.Orgs().Create(t.Context(), tt.req)
			require.NoError(t, err)
			require.Equal(t, wantDocsOrg(), org)
			require.Equal(t, "/orgs", got.requestURI)
			require.JSONEq(t, tt.want, got.body)
		})
	}
}

func TestOrgsCreateWithSlugifiedName(t *testing.T) {
	c, got := serve(t, "POST /orgs", raw(http.StatusCreated, envelope(docsOrg)))
	_, err := c.Orgs().Create(t.Context(), storage.CreateOrgRequest{Slug: storage.Slugify("Acme Corp"), Name: "Acme Corp"})
	require.NoError(t, err)
	require.JSONEq(t, `{"slug": "acme-corp", "name": "Acme Corp"}`, got.body)
}

func TestOrgsCreateRefusesAnEmptySlugWithNothingSent(t *testing.T) {
	c, got := serve(t, "/", raw(http.StatusOK, envelope(docsOrg)))
	_, err := c.Orgs().Create(t.Context(), storage.CreateOrgRequest{Slug: storage.Slugify("!!!"), Name: "!!!"})
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
	require.Zero(t, got.calls)
}

func TestOrgsCreateConflictMatchesErrConflict(t *testing.T) {
	c, _ := serve(t, "POST /orgs", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusConflict, "CONFLICT", "org exists"))
	})
	_, err := c.Orgs().Create(t.Context(), storage.CreateOrgRequest{Slug: "acme", Name: "Acme"})
	require.ErrorIs(t, err, remote.ErrConflict)
}

func TestOrgsListMine(t *testing.T) {
	c, got := serve(t, "GET /orgs", raw(http.StatusOK, envelope("["+camelMyOrg+","+snakeMyOrg+"]")))
	list, err := c.Orgs().ListMine(t.Context())
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, storage.OrgRoleOwner, list[0].Role)
	require.Equal(t, "enterprise", list[0].Org.Plan)
	require.Equal(t, storage.OrgRoleMember, list[1].Role)
	require.Nil(t, list[1].Org.Email)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/orgs", got.requestURI)
	require.Empty(t, got.body)
}

func TestOrgsListMineEmpty(t *testing.T) {
	c, _ := serve(t, "GET /orgs", raw(http.StatusOK, envelope("[]")))
	list, err := c.Orgs().ListMine(t.Context())
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestOrgsGet(t *testing.T) {
	c, got := serve(t, "GET /orgs/{slug}", raw(http.StatusOK, envelope(docsOrg)))
	org, err := c.Orgs().Get(t.Context(), "acme-corp")
	require.NoError(t, err)
	require.Equal(t, wantDocsOrg(), org)
	require.Equal(t, "/orgs/acme-corp", got.requestURI)
	require.Empty(t, got.body)
}

func TestOrgsGetNotFound(t *testing.T) {
	c, _ := serve(t, "GET /orgs/{slug}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "org not found"))
	})
	_, err := c.Orgs().Get(t.Context(), "gone")
	require.ErrorIs(t, err, remote.ErrNotFound)
}

func TestOrgsUpdate(t *testing.T) {
	tests := map[string]struct {
		req  storage.UpdateOrgRequest
		want string
	}{
		"empty": {
			want: `{}`,
		},
		"name only": {
			req:  storage.UpdateOrgRequest{Name: ptr("New Name")},
			want: `{"name": "New Name"}`,
		},
		"every field": {
			req:  storage.UpdateOrgRequest{Name: ptr("n"), Email: ptr("e@example.com")},
			want: `{"name": "n", "email": "e@example.com"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "PUT /orgs/{slug}", raw(http.StatusOK, envelope(docsOrg)))
			org, err := c.Orgs().Update(t.Context(), "acme-corp", tt.req)
			require.NoError(t, err)
			require.Equal(t, wantDocsOrg(), org)
			require.Equal(t, "/orgs/acme-corp", got.requestURI)
			require.JSONEq(t, tt.want, got.body)
		})
	}
}

func TestOrgsDelete(t *testing.T) {
	c, got := serve(t, "DELETE /orgs/{slug}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.Orgs().Delete(t.Context(), "acme-corp"))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/orgs/acme-corp", got.requestURI)
	require.Empty(t, got.body)
}

func TestOrgsDeleteForbidden(t *testing.T) {
	c, _ := serve(t, "DELETE /orgs/{slug}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusForbidden, "FORBIDDEN", "not an owner"))
	})
	err := c.Orgs().Delete(t.Context(), "acme-corp")
	require.ErrorIs(t, err, remote.ErrForbidden)
}

func TestOrgsUpgradeSendsNoBody(t *testing.T) {
	c, got, header := orgServe(t, "POST /orgs/{slug}/upgrade", raw(http.StatusOK, envelope(docsOrg)))
	org, err := c.Orgs().Upgrade(t.Context(), "acme-corp")
	require.NoError(t, err)
	require.Equal(t, wantDocsOrg(), org)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/orgs/acme-corp/upgrade", got.requestURI)
	require.Empty(t, got.body)
	require.Equal(t, []string{"application/json"}, header.Values("Content-Type"))
}

func TestOrgsRefuseBadSlugsWithNothingSent(t *testing.T) {
	for _, slug := range []string{"..", ".", "", "a/b", "%2e%2e", "a?b", "a#b"} {
		t.Run(slug, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(docsOrg)))
			orgs := c.Orgs()

			_, err := orgs.Get(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = orgs.Update(t.Context(), slug, storage.UpdateOrgRequest{})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = orgs.Delete(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = orgs.Upgrade(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

func TestOrgsSendSlugsAsOneSegment(t *testing.T) {
	c, got := serve(t, "/", raw(http.StatusOK, envelope(docsOrg)))
	_, err := c.Orgs().Upgrade(t.Context(), "my org")
	require.NoError(t, err)
	require.Equal(t, "/orgs/my%20org/upgrade", got.requestURI)
}

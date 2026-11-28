package storage_test

import (
	"encoding/json/v2"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/storage"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

// camelTeam is a TeamInfo body in camelCase
// (docs/remote/endpoints/storage.md#teaminfo).
const camelTeam = `{"id": "00000000-0000-0000-0000-00000000000a", "orgId": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "description": "Backend engineering", "visibility": "secret", "createdAt": "2024-01-02T00:00:00Z", "updatedAt": "2024-01-03T00:00:00Z"}`

// snakeTeam is camelTeam with every aliased member in snake_case and no
// description.
const snakeTeam = `{"id": "00000000-0000-0000-0000-00000000000a", "org_id": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "visibility": "visible", "created_at": "2024-01-02T00:00:00Z", "updated_at": "2024-01-03T00:00:00Z"}`

func wantCamelTeam() storage.TeamInfo {
	return storage.TeamInfo{
		ID:          uuid.MustParse("00000000-0000-0000-0000-00000000000a"),
		OrgID:       uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Slug:        "backend",
		Name:        "Backend",
		Description: ptr("Backend engineering"),
		Visibility:  storage.TeamVisibilitySecret,
		CreatedAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}
}

func wantSnakeTeam() storage.TeamInfo {
	return storage.TeamInfo{
		ID:         uuid.MustParse("00000000-0000-0000-0000-00000000000a"),
		OrgID:      uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Slug:       "backend",
		Name:       "Backend",
		Visibility: storage.TeamVisibilityVisible,
		CreatedAt:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}
}

func TestTeamInfoReadsBothSpellings(t *testing.T) {
	tests := map[string]struct {
		body string
		want storage.TeamInfo
	}{
		"camelCase":  {body: camelTeam, want: wantCamelTeam()},
		"snake_case": {body: snakeTeam, want: wantSnakeTeam()},
		"null description": {
			body: `{"id": "00000000-0000-0000-0000-00000000000a", "org_id": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "description": null, "visibility": "visible", "created_at": "2024-01-02T00:00:00Z", "updated_at": "2024-01-03T00:00:00Z"}`,
			want: wantSnakeTeam(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.TeamInfo
			require.NoError(t, json.Unmarshal([]byte(tt.body), &got))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTeamInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing id":         `{"orgId": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "visibility": "visible", "createdAt": "2024-01-02T00:00:00Z", "updatedAt": "2024-01-03T00:00:00Z"}`,
		"missing orgId":      `{"id": "00000000-0000-0000-0000-00000000000a", "slug": "backend", "name": "Backend", "visibility": "visible", "createdAt": "2024-01-02T00:00:00Z", "updatedAt": "2024-01-03T00:00:00Z"}`,
		"missing slug":       `{"id": "00000000-0000-0000-0000-00000000000a", "orgId": "00000000-0000-0000-0000-000000000001", "name": "Backend", "visibility": "visible", "createdAt": "2024-01-02T00:00:00Z", "updatedAt": "2024-01-03T00:00:00Z"}`,
		"missing name":       `{"id": "00000000-0000-0000-0000-00000000000a", "orgId": "00000000-0000-0000-0000-000000000001", "slug": "backend", "visibility": "visible", "createdAt": "2024-01-02T00:00:00Z", "updatedAt": "2024-01-03T00:00:00Z"}`,
		"missing visibility": `{"id": "00000000-0000-0000-0000-00000000000a", "orgId": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "createdAt": "2024-01-02T00:00:00Z", "updatedAt": "2024-01-03T00:00:00Z"}`,
		"missing createdAt":  `{"id": "00000000-0000-0000-0000-00000000000a", "orgId": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "visibility": "visible", "updatedAt": "2024-01-03T00:00:00Z"}`,
		"unknown visibility": `{"id": "00000000-0000-0000-0000-00000000000a", "orgId": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "visibility": "public", "createdAt": "2024-01-02T00:00:00Z", "updatedAt": "2024-01-03T00:00:00Z"}`,
		"missing updatedAt":  `{"id": "00000000-0000-0000-0000-00000000000a", "orgId": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "visibility": "visible", "createdAt": "2024-01-02T00:00:00Z"}`,
		"both spellings":     `{"id": "00000000-0000-0000-0000-00000000000a", "orgId": "00000000-0000-0000-0000-000000000001", "org_id": "00000000-0000-0000-0000-000000000001", "slug": "backend", "name": "Backend", "visibility": "visible", "createdAt": "2024-01-02T00:00:00Z", "updatedAt": "2024-01-03T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.TeamInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestTeamsList(t *testing.T) {
	c, got := serve(t, "GET /teams", raw(http.StatusOK, envelope("["+camelTeam+","+snakeTeam+"]")))
	list, err := c.Teams().List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []storage.TeamInfo{wantCamelTeam(), wantSnakeTeam()}, list)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/teams", got.requestURI)
	require.Empty(t, got.body)
}

func TestTeamsCreateBody(t *testing.T) {
	tests := map[string]struct {
		req  storage.CreateTeamRequest
		want string
	}{
		"with visibility": {
			req:  storage.CreateTeamRequest{Name: "Backend", Description: "Backend engineering", Visibility: storage.TeamVisibilitySecret},
			want: `{"name": "Backend", "description": "Backend engineering", "visibility": "secret"}`,
		},
		"without visibility": {
			req:  storage.CreateTeamRequest{Name: "Backend"},
			want: `{"name": "Backend"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "POST /teams", raw(http.StatusCreated, envelope(camelTeam)))
			team, err := c.Teams().Create(t.Context(), tt.req)
			require.NoError(t, err)
			require.Equal(t, wantCamelTeam(), team)
			require.Equal(t, http.MethodPost, got.method)
			require.Equal(t, "/teams", got.requestURI)
			require.JSONEq(t, tt.want, got.body)
		})
	}
}

func TestTeamsGet(t *testing.T) {
	c, got := serve(t, "GET /teams/{slug}", raw(http.StatusOK, envelope(camelTeam)))
	team, err := c.Teams().Get(t.Context(), "backend")
	require.NoError(t, err)
	require.Equal(t, wantCamelTeam(), team)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/teams/backend", got.requestURI)
	require.Empty(t, got.body)
}

func TestTeamsGetNotFound(t *testing.T) {
	c, _ := serve(t, "GET /teams/{slug}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "team not found"))
	})
	_, err := c.Teams().Get(t.Context(), "backend")
	require.ErrorIs(t, err, remote.ErrNotFound)
}

// TestTeamsGoToTheBaseURLsOrg checks that a team route carries no org, so
// that the same slug reaches the host of each Client's base
// (docs/remote/endpoints/storage.md#teams).
func TestTeamsGoToTheBaseURLsOrg(t *testing.T) {
	for _, base := range []string{"https://delta.example", "https://atomic.example"} {
		t.Run(base, func(t *testing.T) {
			var gotMethod, gotURL string
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotURL = "https://" + r.Host + r.RequestURI
				raw(http.StatusOK, envelope(camelTeam))(w, r)
			})
			c, err := storage.New(mustURL(t, base), remotetest.Client(h), remotetest.NewLogRecorder().Logger(), "0.22.1")
			require.NoError(t, err)

			_, err = c.Teams().Get(t.Context(), "engineering")
			require.NoError(t, err)
			require.Equal(t, http.MethodGet, gotMethod)
			require.Equal(t, base+"/teams/engineering", gotURL)
		})
	}
}

func TestTeamsUpdate(t *testing.T) {
	tests := map[string]struct {
		req  storage.UpdateTeamRequest
		want string
	}{
		"nothing set": {req: storage.UpdateTeamRequest{}, want: `{}`},
		"every field": {
			req: storage.UpdateTeamRequest{
				Name:        ptr("Backend"),
				Description: ptr(""),
				Visibility:  ptr(storage.TeamVisibilityVisible),
			},
			want: `{"name": "Backend", "description": "", "visibility": "visible"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "PUT /teams/{slug}", raw(http.StatusOK, envelope(camelTeam)))
			team, err := c.Teams().Update(t.Context(), "backend", tt.req)
			require.NoError(t, err)
			require.Equal(t, wantCamelTeam(), team)
			require.Equal(t, http.MethodPut, got.method)
			require.Equal(t, "/teams/backend", got.requestURI)
			require.JSONEq(t, tt.want, got.body)
		})
	}
}

func TestTeamsDelete(t *testing.T) {
	c, got := serve(t, "DELETE /teams/{slug}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.Teams().Delete(t.Context(), "backend"))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/teams/backend", got.requestURI)
	require.Empty(t, got.body)
}

func TestTeamsRefuseBadVisibilityWithNothingSent(t *testing.T) {
	for _, v := range []storage.TeamVisibility{"public", "Secret", "VISIBLE", " secret"} {
		t.Run(string(v), func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(camelTeam)))

			_, err := c.Teams().Create(t.Context(), storage.CreateTeamRequest{Name: "Backend", Visibility: v})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = c.Teams().Update(t.Context(), "backend", storage.UpdateTeamRequest{Visibility: ptr(v)})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

func TestTeamsRefuseBadSlugsWithNothingSent(t *testing.T) {
	for _, slug := range []string{"..", ".", "", "a/b", "%2e%2e", "a?b", "a#b"} {
		t.Run(slug, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(camelTeam)))
			teams := c.Teams()

			_, err := teams.Get(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = teams.Update(t.Context(), slug, storage.UpdateTeamRequest{})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = teams.Delete(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

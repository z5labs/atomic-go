package storage_test

import (
	"io"
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

// docsWorkspace is the WorkspaceInfo body the docs show
// (docs/remote/endpoints/storage.md#workspaceinfo).
const docsWorkspace = `{"id": "00000000-0000-0000-0000-000000000001", "tenantId": "00000000-0000-0000-0000-000000000002", "name": "workspace", "slug": "workspace", "description": null, "ownerId": "00000000-0000-0000-0000-000000000003", "visibility": "public", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"}`

// snakeWorkspace is docsWorkspace with every alias spelled in snake_case.
const snakeWorkspace = `{"id": "00000000-0000-0000-0000-000000000001", "tenant_id": "00000000-0000-0000-0000-000000000002", "name": "workspace", "slug": "workspace", "owner_id": "00000000-0000-0000-0000-000000000003", "visibility": "public", "created_at": "2025-01-01T00:00:00Z", "updated_at": "2025-01-01T00:00:00Z"}`

func wantDocsWorkspace() storage.WorkspaceInfo {
	at := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	return storage.WorkspaceInfo{
		ID:         uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		TenantID:   uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		Name:       "workspace",
		Slug:       "workspace",
		OwnerID:    uuid.MustParse("00000000-0000-0000-0000-000000000003"),
		Visibility: storage.VisibilityPublic,
		CreatedAt:  at,
		UpdatedAt:  at,
	}
}

// served is what the fake server saw. remotetest serves on the caller's
// goroutine, so it is read on the test's goroutine without a lock.
type served struct {
	calls      int
	method     string
	requestURI string
	body       string
}

// serve returns a Client on an org base whose requests are answered by a
// ServeMux holding pattern, answered by h, and the record of what it saw.
func serve(t *testing.T, pattern string, h http.HandlerFunc) (*storage.Client, *served) {
	t.Helper()
	got := &served{}
	mux := http.NewServeMux()
	mux.Handle(pattern, h)
	record := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		got.calls++
		got.method = r.Method
		got.requestURI = r.RequestURI
		got.body = string(b)
		mux.ServeHTTP(w, r)
	})
	c, err := storage.New(mustURL(t, "https://acme.atomic.storage"), remotetest.Client(record), remotetest.NewLogRecorder().Logger(), "0.22.1")
	require.NoError(t, err)
	return c, got
}

// raw answers every request with status and body.
func raw(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storagetest.Raw(w, status, body)
	}
}

func envelope(data string) string {
	return `{"success":true,"data":` + data + `}`
}

func ptr[T any](v T) *T { return &v }

func TestParseVisibility(t *testing.T) {
	for in, want := range map[string]storage.Visibility{
		"public":  storage.VisibilityPublic,
		"Public":  storage.VisibilityPublic,
		"PUBLIC":  storage.VisibilityPublic,
		"private": storage.VisibilityPrivate,
		"PrIvAtE": storage.VisibilityPrivate,
	} {
		got, err := storage.ParseVisibility(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "internal", " public", "publicx"} {
		_, err := storage.ParseVisibility(in)
		require.ErrorIs(t, err, storage.ErrInvalidArgument, in)
	}
}

func TestWorkspacesGetReadsTheDocsBody(t *testing.T) {
	for name, body := range map[string]string{"camelCase": docsWorkspace, "snake_case": snakeWorkspace} {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "GET /workspaces/{slug}", raw(http.StatusOK, envelope(body)))
			ws, err := c.Workspaces().Get(t.Context(), "workspace")
			require.NoError(t, err)
			require.Equal(t, wantDocsWorkspace(), ws)
			require.Equal(t, http.MethodGet, got.method)
			require.Equal(t, "/workspaces/workspace", got.requestURI)
			require.Empty(t, got.body)
		})
	}
}

func TestWorkspaceInfoDescription(t *testing.T) {
	body := `{"id": "00000000-0000-0000-0000-000000000001", "tenantId": "00000000-0000-0000-0000-000000000002", "name": "w", "slug": "w", "description": "A workspace", "ownerId": "00000000-0000-0000-0000-000000000003", "visibility": "private", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-02T00:00:00Z"}`
	c, _ := serve(t, "GET /workspaces/w", raw(http.StatusOK, envelope(body)))
	ws, err := c.Workspaces().Get(t.Context(), "w")
	require.NoError(t, err)
	require.Equal(t, ptr("A workspace"), ws.Description)
	require.Equal(t, storage.VisibilityPrivate, ws.Visibility)
	require.Equal(t, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), ws.UpdatedAt)
}

func TestWorkspaceInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing slug":          `{"id": "00000000-0000-0000-0000-000000000001", "tenantId": "00000000-0000-0000-0000-000000000002", "name": "w", "ownerId": "00000000-0000-0000-0000-000000000003", "visibility": "public", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"}`,
		"null ownerId":          `{"id": "00000000-0000-0000-0000-000000000001", "tenantId": "00000000-0000-0000-0000-000000000002", "name": "w", "slug": "w", "ownerId": null, "visibility": "public", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"}`,
		"both spellings":        `{"id": "00000000-0000-0000-0000-000000000001", "tenantId": "00000000-0000-0000-0000-000000000002", "tenant_id": "00000000-0000-0000-0000-000000000002", "name": "w", "slug": "w", "ownerId": "00000000-0000-0000-0000-000000000003", "visibility": "public", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"}`,
		"visibility in caps":    `{"id": "00000000-0000-0000-0000-000000000001", "tenantId": "00000000-0000-0000-0000-000000000002", "name": "w", "slug": "w", "ownerId": "00000000-0000-0000-0000-000000000003", "visibility": "PUBLIC", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"}`,
		"id that is not a uuid": `{"id": "one", "tenantId": "00000000-0000-0000-0000-000000000002", "name": "w", "slug": "w", "ownerId": "00000000-0000-0000-0000-000000000003", "visibility": "public", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"}`,
		"bad timestamp":         `{"id": "00000000-0000-0000-0000-000000000001", "tenantId": "00000000-0000-0000-0000-000000000002", "name": "w", "slug": "w", "ownerId": "00000000-0000-0000-0000-000000000003", "visibility": "public", "createdAt": "yesterday", "updatedAt": "2025-01-01T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, "GET /workspaces/w", raw(http.StatusOK, envelope(body)))
			_, err := c.Workspaces().Get(t.Context(), "w")
			var rerr *remote.Error
			require.ErrorAs(t, err, &rerr)
			require.Equal(t, remote.KindProtocol, rerr.Kind)
		})
	}
}

func TestWorkspacesList(t *testing.T) {
	c, got := serve(t, "GET /workspaces", raw(http.StatusOK, envelope("["+docsWorkspace+","+snakeWorkspace+"]")))
	list, err := c.Workspaces().List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []storage.WorkspaceInfo{wantDocsWorkspace(), wantDocsWorkspace()}, list)
	require.Equal(t, "/workspaces", got.requestURI)
}

func TestWorkspacesListEmpty(t *testing.T) {
	c, _ := serve(t, "GET /workspaces", raw(http.StatusOK, envelope("[]")))
	list, err := c.Workspaces().List(t.Context())
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestWorkspacesCreate(t *testing.T) {
	tests := map[string]struct {
		req  storage.CreateWorkspaceRequest
		want string
	}{
		"the docs body": {
			req:  storage.CreateWorkspaceRequest{Name: "my-workspace", Description: "A workspace", Visibility: storage.VisibilityPublic},
			want: `{"name": "my-workspace", "description": "A workspace", "visibility": "public"}`,
		},
		"no description": {
			req:  storage.CreateWorkspaceRequest{Name: "my-workspace", Visibility: storage.VisibilityPrivate},
			want: `{"name": "my-workspace", "visibility": "private"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "POST /workspaces", raw(http.StatusCreated, envelope(docsWorkspace)))
			ws, err := c.Workspaces().Create(t.Context(), tt.req)
			require.NoError(t, err)
			require.Equal(t, wantDocsWorkspace(), ws)
			require.Equal(t, "/workspaces", got.requestURI)
			require.JSONEq(t, tt.want, got.body)
		})
	}
}

func TestWorkspacesCreateConflictMatchesErrConflict(t *testing.T) {
	c, _ := serve(t, "POST /workspaces", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusConflict, "CONFLICT", "workspace exists"))
	})
	_, err := c.Workspaces().Create(t.Context(), storage.CreateWorkspaceRequest{Name: "w", Visibility: storage.VisibilityPublic})
	require.ErrorIs(t, err, remote.ErrConflict)
}

func TestWorkspacesUpdate(t *testing.T) {
	tests := map[string]struct {
		req  storage.UpdateWorkspaceRequest
		want string
	}{
		"empty": {
			want: `{}`,
		},
		"every field": {
			req:  storage.UpdateWorkspaceRequest{Name: ptr("n"), Description: ptr("d"), Visibility: ptr(storage.VisibilityPrivate)},
			want: `{"name": "n", "description": "d", "visibility": "private"}`,
		},
		"empty description is sent": {
			req:  storage.UpdateWorkspaceRequest{Description: ptr("")},
			want: `{"description": ""}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "PUT /workspaces/{slug}", raw(http.StatusOK, envelope(docsWorkspace)))
			ws, err := c.Workspaces().Update(t.Context(), "workspace", tt.req)
			require.NoError(t, err)
			require.Equal(t, wantDocsWorkspace(), ws)
			require.Equal(t, "/workspaces/workspace", got.requestURI)
			require.JSONEq(t, tt.want, got.body)
		})
	}
}

func TestWorkspacesDelete(t *testing.T) {
	c, got := serve(t, "DELETE /workspaces/{slug}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.Workspaces().Delete(t.Context(), "workspace"))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/workspaces/workspace", got.requestURI)
	require.Empty(t, got.body)
}

func TestWorkspacesDeleteNotFound(t *testing.T) {
	c, _ := serve(t, "DELETE /workspaces/{slug}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "workspace not found"))
	})
	err := c.Workspaces().Delete(t.Context(), "gone")
	require.ErrorIs(t, err, remote.ErrNotFound)
}

func TestWorkspacesRefuseBadVisibilityWithNothingSent(t *testing.T) {
	for _, v := range []storage.Visibility{"PUBLIC", "Private", "", "internal"} {
		t.Run(string(v), func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(docsWorkspace)))

			_, err := c.Workspaces().Create(t.Context(), storage.CreateWorkspaceRequest{Name: "w", Visibility: v})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = c.Workspaces().Update(t.Context(), "w", storage.UpdateWorkspaceRequest{Visibility: ptr(v)})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

func TestWorkspacesRefuseBadSlugsWithNothingSent(t *testing.T) {
	for _, slug := range []string{"..", ".", "", "a/b", "%2e%2e", "a?b"} {
		t.Run(slug, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(docsWorkspace)))
			ws := c.Workspaces()

			_, err := ws.Get(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = ws.Update(t.Context(), slug, storage.UpdateWorkspaceRequest{})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = ws.Delete(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

func TestWorkspacesSendSlugsAsOneSegment(t *testing.T) {
	c, got := serve(t, "/", raw(http.StatusOK, envelope(docsWorkspace)))
	_, err := c.Workspaces().Get(t.Context(), "my ws")
	require.NoError(t, err)
	require.Equal(t, "/workspaces/my%20ws", got.requestURI)
}

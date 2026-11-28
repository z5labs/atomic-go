package storage_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/storage"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

// docsProject is the ProjectInfo body the docs show
// (docs/remote/endpoints/storage.md#projectinfo).
const docsProject = `{"id": "00000000-0000-0000-0000-000000000010", "workspaceId": "00000000-0000-0000-0000-000000000020", "name": "project", "slug": "project", "description": "desc", "defaultView": "main", "visibility": "private", "createdAt": "2025-06-01T12:00:00Z", "updatedAt": "2025-06-01T12:00:00Z"}`

// snakeProject is docsProject with every alias spelled in snake_case.
const snakeProject = `{"id": "00000000-0000-0000-0000-000000000010", "workspace_id": "00000000-0000-0000-0000-000000000020", "name": "project", "slug": "project", "description": "desc", "default_view": "main", "visibility": "private", "created_at": "2025-06-01T12:00:00Z", "updated_at": "2025-06-01T12:00:00Z"}`

func wantDocsProject() storage.ProjectInfo {
	at := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	return storage.ProjectInfo{
		ID:          uuid.MustParse("00000000-0000-0000-0000-000000000010"),
		WorkspaceID: uuid.MustParse("00000000-0000-0000-0000-000000000020"),
		Name:        "project",
		Slug:        "project",
		Description: ptr("desc"),
		DefaultView: "main",
		Visibility:  storage.VisibilityPrivate,
		CreatedAt:   at,
		UpdatedAt:   at,
	}
}

func TestProjectsGetReadsTheDocsBody(t *testing.T) {
	for name, body := range map[string]string{"camelCase": docsProject, "snake_case": snakeProject} {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "GET /workspaces/{ws}/projects/{p}", raw(http.StatusOK, envelope(body)))
			p, err := c.Projects().Get(t.Context(), "ws", "project")
			require.NoError(t, err)
			require.Equal(t, wantDocsProject(), p)
			require.Equal(t, http.MethodGet, got.method)
			require.Equal(t, "/workspaces/ws/projects/project", got.requestURI)
			require.Empty(t, got.body)
		})
	}
}

func TestProjectInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing defaultView": `{"id": "00000000-0000-0000-0000-000000000010", "workspaceId": "00000000-0000-0000-0000-000000000020", "name": "p", "slug": "p", "visibility": "private", "createdAt": "2025-06-01T12:00:00Z", "updatedAt": "2025-06-01T12:00:00Z"}`,
		"both spellings":      `{"id": "00000000-0000-0000-0000-000000000010", "workspaceId": "00000000-0000-0000-0000-000000000020", "name": "p", "slug": "p", "defaultView": "main", "default_view": "main", "visibility": "private", "createdAt": "2025-06-01T12:00:00Z", "updatedAt": "2025-06-01T12:00:00Z"}`,
		"unknown visibility":  `{"id": "00000000-0000-0000-0000-000000000010", "workspaceId": "00000000-0000-0000-0000-000000000020", "name": "p", "slug": "p", "defaultView": "main", "visibility": "internal", "createdAt": "2025-06-01T12:00:00Z", "updatedAt": "2025-06-01T12:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, "GET /workspaces/ws/projects/p", raw(http.StatusOK, envelope(body)))
			_, err := c.Projects().Get(t.Context(), "ws", "p")
			var rerr *remote.Error
			require.ErrorAs(t, err, &rerr)
			require.Equal(t, remote.KindProtocol, rerr.Kind)
		})
	}
}

func TestProjectsGetMissingMatchesErrNotFound(t *testing.T) {
	c, got := serve(t, "GET /workspaces/ws/projects/missing", raw(http.StatusNotFound,
		`{"success":false,"error":{"code":"NOT_FOUND","message":"project not found"}}`))
	_, err := c.Projects().Get(t.Context(), "ws", "missing")
	require.ErrorIs(t, err, remote.ErrNotFound)
	var rerr *remote.Error
	require.ErrorAs(t, err, &rerr)
	require.Equal(t, "NOT_FOUND", rerr.Code)
	require.Equal(t, "project not found", rerr.Message)
	require.Equal(t, "get_project", rerr.Op)
	require.Equal(t, 1, got.calls)
}

func TestProjectsList(t *testing.T) {
	c, got := serve(t, "GET /workspaces/{ws}/projects", raw(http.StatusOK, envelope("["+docsProject+","+snakeProject+"]")))
	list, err := c.Projects().List(t.Context(), "ws")
	require.NoError(t, err)
	require.Equal(t, []storage.ProjectInfo{wantDocsProject(), wantDocsProject()}, list)
	require.Equal(t, "/workspaces/ws/projects", got.requestURI)
}

func TestProjectsCreate(t *testing.T) {
	tests := map[string]struct {
		req  storage.CreateProjectRequest
		want string
	}{
		"the docs body": {
			req:  storage.CreateProjectRequest{Name: "my-project", Description: "A project", DefaultView: "main", Kind: "rust", Visibility: storage.VisibilityPrivate},
			want: `{"name": "my-project", "description": "A project", "default_view": "main", "kind": "rust", "visibility": "private"}`,
		},
		"only what is required": {
			req:  storage.CreateProjectRequest{Name: "my-project", DefaultView: "dev", Visibility: storage.VisibilityPublic},
			want: `{"name": "my-project", "default_view": "dev", "visibility": "public"}`,
		},
		"empty default view is sent": {
			req:  storage.CreateProjectRequest{Name: "p", Visibility: storage.VisibilityPublic},
			want: `{"name": "p", "default_view": "", "visibility": "public"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "POST /workspaces/{ws}/projects", raw(http.StatusCreated, envelope(docsProject)))
			p, err := c.Projects().Create(t.Context(), "ws", tt.req)
			require.NoError(t, err)
			require.Equal(t, wantDocsProject(), p)
			require.Equal(t, "/workspaces/ws/projects", got.requestURI)
			require.JSONEq(t, tt.want, got.body)
		})
	}
}

func TestProjectsCreateSlugFeedsServerURL(t *testing.T) {
	c, _ := serve(t, "POST /workspaces/{ws}/projects", raw(http.StatusCreated, envelope(docsProject)))
	p, err := c.Projects().Create(t.Context(), "ws", storage.CreateProjectRequest{Name: "project", DefaultView: "dev", Visibility: storage.VisibilityPrivate})
	require.NoError(t, err)

	u, err := serverurl.NewProject(c.BaseURL(), "ws", p.Slug)
	require.NoError(t, err)
	require.Equal(t, "https://acme.atomic.storage/workspaces/ws/projects/project/code", u.String())
}

func TestProjectsUpdate(t *testing.T) {
	tests := map[string]struct {
		req  storage.UpdateProjectRequest
		want string
	}{
		"empty": {
			want: `{}`,
		},
		"every field": {
			req:  storage.UpdateProjectRequest{Name: ptr("n"), Description: ptr("d"), DefaultView: ptr("main"), Visibility: ptr(storage.VisibilityPublic)},
			want: `{"name": "n", "description": "d", "default_view": "main", "visibility": "public"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "PUT /workspaces/{ws}/projects/{p}", raw(http.StatusOK, envelope(docsProject)))
			p, err := c.Projects().Update(t.Context(), "ws", "project", tt.req)
			require.NoError(t, err)
			require.Equal(t, wantDocsProject(), p)
			require.Equal(t, "/workspaces/ws/projects/project", got.requestURI)
			require.JSONEq(t, tt.want, got.body)
		})
	}
}

func TestProjectsDelete(t *testing.T) {
	c, got := serve(t, "DELETE /workspaces/{ws}/projects/{p}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.Projects().Delete(t.Context(), "ws", "project"))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/workspaces/ws/projects/project", got.requestURI)
}

func TestProjectsDeleteIgnoresA2xxBody(t *testing.T) {
	for name, body := range map[string]string{
		"no data":   `{"success":true}`,
		"not JSON":  `deleted`,
		"some data": `{"success":true,"data":{"deleted":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "DELETE /workspaces/{ws}/projects/{p}", raw(http.StatusOK, body))
			require.NoError(t, c.Projects().Delete(t.Context(), "ws", "project"))
			require.Equal(t, 1, got.calls)
		})
	}
}

func TestProjectsDeleteNotFound(t *testing.T) {
	c, _ := serve(t, "DELETE /workspaces/{ws}/projects/{p}", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "project not found"))
	})
	require.ErrorIs(t, c.Projects().Delete(t.Context(), "ws", "missing"), remote.ErrNotFound)
}

func TestProjectsRefuseBadVisibilityWithNothingSent(t *testing.T) {
	for _, v := range []storage.Visibility{"PUBLIC", "Private", "", "internal"} {
		t.Run(string(v), func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(docsProject)))

			_, err := c.Projects().Create(t.Context(), "ws", storage.CreateProjectRequest{Name: "p", DefaultView: "dev", Visibility: v})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = c.Projects().Update(t.Context(), "ws", "p", storage.UpdateProjectRequest{Visibility: ptr(v)})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

func TestProjectsRefuseBadSlugsWithNothingSent(t *testing.T) {
	for _, slug := range []string{"..", ".", "", "a/b", "%2E%2e", "a#b", "a?b"} {
		t.Run(slug, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(docsProject)))
			ps := c.Projects()
			create := storage.CreateProjectRequest{Name: "p", DefaultView: "dev", Visibility: storage.VisibilityPublic}

			// slug as the workspace.
			_, err := ps.List(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			_, err = ps.Create(t.Context(), slug, create)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			_, err = ps.Get(t.Context(), slug, "p")
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			_, err = ps.Update(t.Context(), slug, "p", storage.UpdateProjectRequest{})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			require.ErrorIs(t, ps.Delete(t.Context(), slug, "p"), storage.ErrInvalidArgument)

			// slug as the project.
			_, err = ps.Get(t.Context(), "ws", slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			_, err = ps.Update(t.Context(), "ws", slug, storage.UpdateProjectRequest{})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			require.ErrorIs(t, ps.Delete(t.Context(), "ws", slug), storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}

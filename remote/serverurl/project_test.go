package serverurl_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/serverurl"
)

func TestNewProject(t *testing.T) {
	t.Run("the documented example", func(t *testing.T) {
		// docs/remote/transport.md#project-url
		base, err := mustServer(t, "https://atomic.storage", false).OrgBase("alice")
		require.NoError(t, err)
		require.Equal(t, "https://alice.atomic.storage", base.String())

		p, err := serverurl.NewProject(base, "my-ws", "my-app")
		require.NoError(t, err)
		require.False(t, p.IsZero())
		require.Equal(t, "https://alice.atomic.storage/workspaces/my-ws/projects/my-app/code", p.String())
		require.Equal(t, "https://alice.atomic.storage", base.String(), "NewProject must not change orgBase")
	})

	for _, tc := range []struct{ base, want string }{
		{"https://storage.acme.com/", "https://storage.acme.com/workspaces/w/projects/p/code"},
		{"https://storage.acme.com/atomic", "https://storage.acme.com/atomic/workspaces/w/projects/p/code"},
		{"HTTP://Alice.Localhost:8080", "http://alice.localhost:8080/workspaces/w/projects/p/code"},
	} {
		t.Run(tc.base, func(t *testing.T) {
			base, err := url.Parse(tc.base)
			require.NoError(t, err)
			p, err := serverurl.NewProject(base, "w", "p")
			require.NoError(t, err)
			require.Equal(t, tc.want, p.String())
		})
	}

	t.Run("a slug that needs escaping is escaped", func(t *testing.T) {
		base, err := url.Parse("https://h")
		require.NoError(t, err)
		p, err := serverurl.NewProject(base, "my ws", "a+b")
		require.NoError(t, err)
		require.Equal(t, "https://h/workspaces/my%20ws/projects/a+b/code", p.String())
		require.Equal(t, "/workspaces/my ws/projects/a+b/code", p.URL().Path)
	})
}

func TestNewProjectRefuses(t *testing.T) {
	base, err := url.Parse("https://alice.atomic.storage")
	require.NoError(t, err)

	t.Run("slugs", func(t *testing.T) {
		for _, slug := range []string{
			"", ".", "..", "a/b", `a\b`, "a?b", "a#b", "a%2fb",
			"a\x00b", "a\tb", "a\nb", "a\x7fb", "bücher",
		} {
			_, err := serverurl.NewProject(base, slug, "p")
			require.ErrorIs(t, err, serverurl.ErrSlug, "workspace %q", slug)

			_, err = serverurl.NewProject(base, "w", slug)
			require.ErrorIs(t, err, serverurl.ErrSlug, "project %q", slug)
		}
	})

	t.Run("org base URLs", func(t *testing.T) {
		_, err := serverurl.NewProject(nil, "w", "p")
		require.ErrorIs(t, err, serverurl.ErrHost)

		for _, tc := range []struct {
			raw  string
			want error
		}{
			{"ftp://h", serverurl.ErrScheme},
			{"https://h?a", serverurl.ErrQueryOrFragment},
			{"https://h#f", serverurl.ErrQueryOrFragment},
			{"https://bob@h", serverurl.ErrUserinfo},
			{"https:///x", serverurl.ErrHost},
		} {
			u, err := url.Parse(tc.raw)
			require.NoError(t, err)
			_, err = serverurl.NewProject(u, "w", "p")
			require.ErrorIs(t, err, tc.want, tc.raw)
		}
	})
}

func TestParseProject(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://alice.atomic.storage/workspaces/w/projects/p/code", "https://alice.atomic.storage/workspaces/w/projects/p/code"},
		{"https://H:443/x", "https://h/x"},
		{"http://H:80/x", "http://h/x"},
		{"http://h:443/x", "http://h:443/x"},
		{"HTTPS://Alice.Atomic.Storage:8443/X", "https://alice.atomic.storage:8443/X"},
		{"https://h", "https://h/"},
		{"https://api.example.com/tenant/t/portfolio/p/project/myrepo/code", "https://api.example.com/tenant/t/portfolio/p/project/myrepo/code"},
		{"https://h/a%2Fb/code", "https://h/a%2Fb/code"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			p, user, err := serverurl.ParseProject(tc.raw)
			require.NoError(t, err)
			require.Nil(t, user)
			require.Equal(t, tc.want, p.String())
		})
	}

	t.Run("userinfo is split off", func(t *testing.T) {
		p, user, err := serverurl.ParseProject("http://bob@alice.localhost:8080/workspaces/w/projects/p/code")
		require.NoError(t, err)
		require.NotNil(t, user)
		require.Equal(t, "bob", user.Username())
		_, hasPassword := user.Password()
		require.False(t, hasPassword)
		require.Equal(t, "http://alice.localhost:8080/workspaces/w/projects/p/code", p.String())
		require.Nil(t, p.URL().User)
	})

	t.Run("refuses", func(t *testing.T) {
		for _, tc := range []struct {
			raw  string
			want error
		}{
			{"https://h/x?a", serverurl.ErrQueryOrFragment},
			{"https://h/x?", serverurl.ErrQueryOrFragment},
			{"https://h/x#f", serverurl.ErrQueryOrFragment},
			{"https://h/x#", serverurl.ErrQueryOrFragment},
			{"ftp://h/x", serverurl.ErrScheme},
			{"/workspaces/w/projects/p/code", serverurl.ErrScheme},
			{"https://bücher.example/", serverurl.ErrHost},
			{"https://b%C3%BCcher.example/", serverurl.ErrHost},
			{"https:///x", serverurl.ErrHost},
		} {
			t.Run(tc.raw, func(t *testing.T) {
				_, _, err := serverurl.ParseProject(tc.raw)
				require.ErrorIs(t, err, tc.want)
			})
		}
	})
}

func TestProjectZero(t *testing.T) {
	var p serverurl.Project
	require.True(t, p.IsZero())
	require.Equal(t, "", p.String())
	require.Nil(t, p.URL())
	require.Nil(t, p.WithRawQuery("view=main"))
	require.Nil(t, p.Resource("refs/views"))
	require.Nil(t, p.Origin())
	require.Nil(t, p.TokenApex())
	require.Equal(t, "", p.Host())
	require.Equal(t, "", p.RepoName())
}

func TestProjectURLIsACopy(t *testing.T) {
	p := mustProject(t, "https://h/workspaces/w/projects/p/code")
	u := p.URL()
	u.Host = "evil.example"
	u.Path = "/elsewhere"
	require.Equal(t, "https://h/workspaces/w/projects/p/code", p.String())
}

func TestProjectWithRawQuery(t *testing.T) {
	p := mustProject(t, "https://h/workspaces/w/projects/p/code")

	for _, tc := range []struct{ q, want string }{
		{"project-identity=1", "https://h/workspaces/w/projects/p/code?project-identity=1"},
		{"view=feat%41&state=", "https://h/workspaces/w/projects/p/code?view=feat%41&state="},
		{"id", "https://h/workspaces/w/projects/p/code?id"},
		{"", "https://h/workspaces/w/projects/p/code"},
	} {
		t.Run(tc.q, func(t *testing.T) {
			u := p.WithRawQuery(tc.q)
			require.Equal(t, tc.want, u.String())
			require.Equal(t, tc.q, u.RawQuery)
		})
	}
	require.Equal(t, "https://h/workspaces/w/projects/p/code", p.String())
}

func TestProjectResource(t *testing.T) {
	// The first two rows are docs/remote/endpoints/project.md#list_view_refs.
	for _, tc := range []struct{ base, path, want string }{
		{"https://h/workspaces/w/projects/p/code", "refs/views", "https://h/workspaces/w/projects/p/refs/views"},
		{"https://h/workspaces/w/projects/p", "refs/views", "https://h/workspaces/w/projects/p/refs/views"},

		{"https://h/workspaces/w/projects/p/code/", "refs/views", "https://h/workspaces/w/projects/p/refs/views"},
		{"https://h/workspaces/w/projects/p/", "refs/views", "https://h/workspaces/w/projects/p/refs/views"},
		{"https://h/workspaces/w/projects/p/code/code", "refs/views", "https://h/workspaces/w/projects/p/code/refs/views"},
		{"https://h/workspaces/w/projects/p/xcode", "refs/views", "https://h/workspaces/w/projects/p/xcode/refs/views"},
		{"https://h", "refs/views", "https://h/refs/views"},
		{"https://h/a%2Fb/code", "refs/views", "https://h/a%2Fb/refs/views"},
		{"https://h/p/code", "a b?c", "https://h/p/a%20b%3Fc"},
	} {
		t.Run(tc.base+" "+tc.path, func(t *testing.T) {
			p := mustProject(t, tc.base)
			require.Equal(t, tc.want, p.Resource(tc.path).String())
		})
	}
}

func TestProjectOrigin(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://alice.atomic.storage/workspaces/w/projects/p/code", "https://alice.atomic.storage"},
		{"http://alice.localhost:8080/workspaces/w/projects/p/code", "http://alice.localhost:8080"},
		{"https://h:443/x", "https://h"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			require.Equal(t, tc.want, mustProject(t, tc.raw).Origin().String())
		})
	}
}

func TestProjectTokenApex(t *testing.T) {
	// The first three rows are docs/remote/transport.md#apex-host.
	for _, tc := range []struct{ raw, want string }{
		{"http://alice.localhost:8080/workspaces/w/projects/p/code", "http://localhost:8080"},
		{"https://alice.atomic.storage/workspaces/w/projects/p/code", "https://atomic.storage"},
		{"http://localhost:8080/workspaces/w/projects/p/code", "http://localhost:8080"},

		// Rust's derivation, reproduced where it does not find the apex.
		{"https://storage.acme.com/workspaces/w/projects/p/code", "https://acme.com"},
		{"http://127.0.0.1:8080/x", "http://0.0.1:8080"},
		{"http://[::1]:8080/x", "http://[::1]:8080"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			require.Equal(t, tc.want, mustProject(t, tc.raw).TokenApex().String())
		})
	}
}

func TestProjectHost(t *testing.T) {
	require.Equal(t, "alice.localhost:8080", mustProject(t, "http://alice.localhost:8080/x").Host())
	require.Equal(t, "atomic.storage", mustProject(t, "https://atomic.storage:443/x").Host())
}

func TestProjectRepoName(t *testing.T) {
	// The first three rows are docs/remote/transport.md#base-url-and-repository-name.
	for _, tc := range []struct{ raw, want string }{
		{"https://api.example.com/tenant/t/portfolio/p/project/myrepo/code", "myrepo"},
		{"https://api.example.com/tenant/t/portfolio/p/project/myrepo/.atomic", "myrepo"},
		{"https://example.com/myrepo.git", "myrepo"},

		{"https://alice.atomic.storage/workspaces/w/projects/my-app/code", "my-app"},
		{"https://h/a/code/b/code", "a"},
		{"https://h/myrepo.git/code", "myrepo"},
		{"https://h/a/b/", "b"},
		{"https://h/code", ""},
		{"https://h/code/a", "a"},
		{"https://h/", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			require.Equal(t, tc.want, mustProject(t, tc.raw).RepoName())
		})
	}
}

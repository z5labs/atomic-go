package serverurl_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/serverurl"
)

func mustServer(t *testing.T, raw string, singleTenant bool) serverurl.Server {
	t.Helper()
	s, err := serverurl.ParseServer(raw, singleTenant)
	require.NoError(t, err)
	return s
}

func mustProject(t *testing.T, raw string) serverurl.Project {
	t.Helper()
	p, _, err := serverurl.ParseProject(raw)
	require.NoError(t, err)
	return p
}

func TestServerOrgBase(t *testing.T) {
	// The rows of docs/remote/transport.md#org-host, then this package's
	// own normalization.
	for _, tc := range []struct {
		server       string
		singleTenant bool
		org          string
		want         string
	}{
		{"https://atomic.storage", false, "alice", "https://alice.atomic.storage"},
		{"http://localhost:8080", false, "alice", "http://alice.localhost:8080"},
		{"https://storage.acme.com", true, "anything", "https://storage.acme.com"},
		{"https://storage.acme.com/", true, "acme", "https://storage.acme.com"},

		{"https://atomic.storage/some/path/", false, "alice", "https://alice.atomic.storage"},
		{"HTTPS://Atomic.Storage:443", false, "Alice", "https://alice.atomic.storage"},
		{"http://localhost:80", false, "my_org-1", "http://my_org-1.localhost"},
		{"https://storage.acme.com/atomic/", true, "", "https://storage.acme.com/atomic"},
		{"https://storage.acme.com//", true, "", "https://storage.acme.com/"},
	} {
		t.Run(tc.server+" "+tc.org, func(t *testing.T) {
			got, err := mustServer(t, tc.server, tc.singleTenant).OrgBase(tc.org)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.String())
		})
	}
}

func TestServerOrgBaseRefuses(t *testing.T) {
	t.Run("org slugs", func(t *testing.T) {
		s := mustServer(t, "https://atomic.storage", false)
		for _, org := range []string{"", "a.b", "a/b", "a:1", "a@b", "a b", "ü", "a%2e"} {
			_, err := s.OrgBase(org)
			require.ErrorIs(t, err, serverurl.ErrSlug, "org %q", org)
		}
	})

	t.Run("IP hosts", func(t *testing.T) {
		for _, raw := range []string{"http://127.0.0.1:8080", "http://[::1]:8080"} {
			_, err := mustServer(t, raw, false).OrgBase("alice")
			require.ErrorIs(t, err, serverurl.ErrHost, raw)
		}
	})

	t.Run("a Server built by hand is checked", func(t *testing.T) {
		_, err := serverurl.Server{}.OrgBase("alice")
		require.ErrorIs(t, err, serverurl.ErrHost)

		u, err := url.Parse("https://bob@atomic.storage")
		require.NoError(t, err)
		_, err = serverurl.Server{URL: u}.OrgBase("alice")
		require.ErrorIs(t, err, serverurl.ErrUserinfo)

		u, err = url.Parse("ftp://atomic.storage")
		require.NoError(t, err)
		_, err = serverurl.Server{URL: u, SingleTenant: true}.OrgBase("alice")
		require.ErrorIs(t, err, serverurl.ErrScheme)
	})
}

func TestServerApex(t *testing.T) {
	for _, tc := range []struct{ server, want string }{
		{"https://atomic.storage", "https://atomic.storage"},
		{"https://atomic.storage/", "https://atomic.storage"},
		{"https://atomic.storage///", "https://atomic.storage"},
		{"https://atomic.storage/api//", "https://atomic.storage/api"},
		{"http://localhost:8080", "http://localhost:8080"},
	} {
		t.Run(tc.server, func(t *testing.T) {
			s := mustServer(t, tc.server, false)
			before := s.URL.String()
			require.Equal(t, tc.want, s.Apex().String())
			require.Equal(t, before, s.URL.String(), "Apex must not change the Server's URL")
		})
	}
	require.Nil(t, serverurl.Server{}.Apex())

	t.Run("a Server built by hand is checked", func(t *testing.T) {
		for _, raw := range []string{"https://bob:secret@h/x", "https://h/x?q", "https://h/x#f", "ftp://h"} {
			u, err := url.Parse(raw)
			require.NoError(t, err)
			require.Nil(t, serverurl.Server{URL: u}.Apex(), raw)
		}

		u, err := url.Parse("HTTPS://H:443/x/")
		require.NoError(t, err)
		before := u.String()
		require.Equal(t, "https://h/x", serverurl.Server{URL: u}.Apex().String())
		require.Equal(t, before, u.String(), "Apex must not change the Server's URL")
	})
}

func TestParseServerNormalizes(t *testing.T) {
	for _, tc := range []struct{ raw, wantHost string }{
		{"https://h:", "h"},
		{"https://h:443", "h"},
		{"https://h:8443", "h:8443"},
		{"http://[FE80::1%25EN0]:8080", "[fe80::1%EN0]:8080"},
		{"http://[FE80::1]:", "[fe80::1]"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			require.Equal(t, tc.wantHost, mustServer(t, tc.raw, false).URL.Host)
		})
	}

	base, err := mustServer(t, "https://h:", false).OrgBase("alice")
	require.NoError(t, err)
	require.Equal(t, "https://alice.h", base.String())
}

func TestParseServerRefuses(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want error
	}{
		{"ftp://atomic.storage", serverurl.ErrScheme},
		{"atomic.storage", serverurl.ErrScheme},
		{"https://atomic.storage?a", serverurl.ErrQueryOrFragment},
		{"https://atomic.storage?", serverurl.ErrQueryOrFragment},
		{"https://atomic.storage#f", serverurl.ErrQueryOrFragment},
		{"https://atomic.storage#", serverurl.ErrQueryOrFragment},
		{"https://bob@atomic.storage", serverurl.ErrUserinfo},
		{"https://bob:secret@atomic.storage", serverurl.ErrUserinfo},
		{"https:///path", serverurl.ErrHost},
		{"https:opaque", serverurl.ErrHost},
		{"https://bücher.example/", serverurl.ErrHost},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			_, err := serverurl.ParseServer(tc.raw, false)
			require.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("a secret is not repeated in the error", func(t *testing.T) {
		for _, raw := range []string{
			"https://bob:secret@atomic.storage",
			"https://bob:secret@atomic.storage/%zz",
			"https://bob:secret@atomic.storage:port",
			"https://atomic.storage/?token=secret",
			"https://atomic.storage/#secret",
			"ftp://bob:secret@atomic.storage/?token=secret",
		} {
			_, err := serverurl.ParseServer(raw, false)
			require.Error(t, err, raw)
			require.NotContains(t, err.Error(), "secret", raw)
		}
	})
}

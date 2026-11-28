package storage

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

// getWorkspace is a resource method as later stories write them: each slug
// goes through segment before anything is sent.
func getWorkspace(ctx context.Context, c *Client, slug string) (thing, error) {
	ws, err := segment(slug)
	if err != nil {
		return thing{}, err
	}
	return call[thing](ctx, c, request{op: "get_workspace", method: http.MethodGet, path: "/workspaces/" + ws})
}

func TestComponent(t *testing.T) {
	tests := map[string]string{
		"urn:atomic:delegation:X": "urn%3Aatomic%3Adelegation%3AX",
		"alice@example.com":       "alice%40example.com",
		"a b":                     "a%20b",
		"AZaz09-._~":              "AZaz09-._~",
		"a/b?c#d%e+f&g=h'":        "a%2Fb%3Fc%23d%25e%2Bf%26g%3Dh%27",
		"é":                       "%C3%A9",
		"did:atomic:abc":          "did%3Aatomic%3Aabc",
		"":                        "",
	}
	for in, want := range tests {
		require.Equal(t, want, component(in), in)
	}
}

func TestSegmentRefusesWithNothingSent(t *testing.T) {
	for _, slug := range []string{
		"", ".", "..",
		"%2e", "%2E", ".%2e", ".%2E", "%2e.", "%2E.", "%2e%2e", "%2E%2e", "%2E%2E",
		"a?b", "a#b", "a/b", `a\b`, "/", "?",
		"a\tb", "a\nb", "a\rb", "a\x00b", "a\x1fb", "a\x7fb",
		"a%", "a%4", "a%zz", "%g0",
	} {
		t.Run(slug, func(t *testing.T) {
			c, got, _ := muxClient(t, "/", func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, storagetest.Data(w, http.StatusOK, thing{}))
			})
			_, err := getWorkspace(t.Context(), c, slug)
			require.ErrorIs(t, err, ErrInvalidArgument)
			require.Zero(t, got.calls)
		})
	}
}

func TestSegmentSendsTheRustBytes(t *testing.T) {
	tests := []struct {
		slug string
		want string
	}{
		{"a b", "/workspaces/a%20b"},
		{"é", "/workspaces/%C3%A9"},
		{"acme", "/workspaces/acme"},
		{"%41", "/workspaces/%41"},
		{"%2F", "/workspaces/%2F"},
		{"a+b", "/workspaces/a+b"},
		{"it's", "/workspaces/it's"},
		{`"<>` + "`{}", "/workspaces/%22%3C%3E%60%7B%7D"},
		{"!$&()*,;=:@[]~-._", "/workspaces/!$&()*,;=:@[]~-._"},
		{"a|b^c", "/workspaces/a%7Cb%5Ec"},
		{"...", "/workspaces/..."},
		{".a", "/workspaces/.a"},
		{"a%2eb", "/workspaces/a%2eb"},
		{"did:atomic:NQYQ", "/workspaces/did:atomic:NQYQ"},
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			c, got, _ := muxClient(t, "/", func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, storagetest.Data(w, http.StatusOK, thing{}))
			})
			_, err := getWorkspace(t.Context(), c, tt.slug)
			require.NoError(t, err)
			require.Equal(t, 1, got.calls)
			require.Equal(t, tt.want, got.requestURI)
		})
	}
}

func TestComponentSurvivesInThePath(t *testing.T) {
	c, got, _ := muxClient(t, "/", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Data(w, http.StatusOK, thing{}))
	})
	_, err := call[thing](t.Context(), c, request{
		op: "delegation_status", method: http.MethodGet,
		path: "/delegations/" + component("urn:atomic:delegation:X") + "/status",
	})
	require.NoError(t, err)
	require.Equal(t, "/delegations/urn%3Aatomic%3Adelegation%3AX/status", got.requestURI)
}

func TestCallRefusesDotSegmentsInAnyPath(t *testing.T) {
	for _, p := range []string{
		"/delegations/" + component("..") + "/status",
		"/delegations/" + component(".") + "/status",
		"/a/%2E%2e",
	} {
		c, got, _ := muxClient(t, "/", func(w http.ResponseWriter, r *http.Request) {})
		_, err := call[thing](t.Context(), c, request{op: "op", method: http.MethodGet, path: p})
		require.ErrorIs(t, err, ErrInvalidArgument, p)
		require.Zero(t, got.calls, p)
	}
}

func TestCallPathFollowsTheBasePath(t *testing.T) {
	got := &seen{}
	h := recording(got, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Data(w, http.StatusOK, thing{}))
	}))
	base, err := url.Parse("https://acme.atomic.storage/api%20v1///")
	require.NoError(t, err)
	c, err := New(base, remotetest.Client(h), remotetest.NewLogRecorder().Logger(), testVersion)
	require.NoError(t, err)
	_, err = getWorkspace(t.Context(), c, "w s")
	require.NoError(t, err)
	require.Equal(t, "/api%20v1/workspaces/w%20s", got.requestURI)
}

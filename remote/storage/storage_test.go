package storage_test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/storage"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func doer() storage.Doer {
	return remotetest.Client(http.NotFoundHandler())
}

func TestNewNamesEveryMissingArgument(t *testing.T) {
	c, err := storage.New(nil, nil, nil, "")
	require.Nil(t, c)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)

	var joined interface{ Unwrap() []error }
	require.True(t, errors.As(err, &joined))
	require.Len(t, joined.Unwrap(), 4)
	for _, what := range []string{"the base URL", "the Doer", "the *slog.Logger", "the client version"} {
		require.Contains(t, err.Error(), what+" is required")
	}
}

func TestNewRefusesBadBases(t *testing.T) {
	logger := remotetest.NewLogRecorder().Logger()
	tests := map[string]string{
		"relative":    "/workspaces",
		"no scheme":   "acme.example.com",
		"ftp":         "ftp://acme.example.com",
		"no host":     "https:///path",
		"userinfo":    "https://user:pass@acme.example.com",
		"query":       "https://acme.example.com?x=1",
		"empty query": "https://acme.example.com?",
		"fragment":    "https://acme.example.com#frag",
		"opaque":      "https:acme.example.com",
		"dot segment": "https://acme.example.com/a/../b",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			c, err := storage.New(mustURL(t, raw), doer(), logger, "0.22.1")
			require.Nil(t, c)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			require.Contains(t, err.Error(), "the base URL")
		})
	}
}

func TestNewTrimsTrailingSlashes(t *testing.T) {
	logger := remotetest.NewLogRecorder().Logger()
	tests := map[string]string{
		"https://example.com/":         "https://example.com",
		"https://example.com":          "https://example.com",
		"https://example.com///":       "https://example.com",
		"http://example.com:8080/api/": "http://example.com:8080/api",
		"HTTPS://Example.com/":         "https://Example.com",
	}
	for in, want := range tests {
		base := mustURL(t, in)
		c, err := storage.New(base, doer(), logger, "0.22.1")
		require.NoError(t, err, in)
		require.Equal(t, want, c.BaseURL().String(), in)
		require.Equal(t, mustURL(t, in), base, "New must not change its argument")
	}
}

func TestNewRootsAHandBuiltPath(t *testing.T) {
	base := &url.URL{Scheme: "https", Host: "x.example.com", Path: "api/"}
	c, err := storage.New(base, doer(), remotetest.NewLogRecorder().Logger(), "0.22.1")
	require.NoError(t, err)
	require.Equal(t, "https://x.example.com/api", c.BaseURL().String())
}

func TestBaseURLReturnsACopy(t *testing.T) {
	c, err := storage.New(mustURL(t, "https://example.com"), doer(), remotetest.NewLogRecorder().Logger(), "0.22.1")
	require.NoError(t, err)
	c.BaseURL().Host = "evil.example.com"
	require.Equal(t, "https://example.com", c.BaseURL().String())
}

func TestNewRefusesOptions(t *testing.T) {
	logger := remotetest.NewLogRecorder().Logger()
	base := mustURL(t, "https://example.com")

	_, err := storage.New(base, doer(), logger, "0.22.1", nil)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
	require.Contains(t, err.Error(), "nil Option")

	_, err = storage.New(base, doer(), logger, "0.22.1", storage.WithUserAgent(""))
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
	require.Contains(t, err.Error(), "WithUserAgent")

	_, err = storage.New(base, doer(), logger, "0.22.1", storage.WithUserAgent("tool/1"))
	require.NoError(t, err)
}
